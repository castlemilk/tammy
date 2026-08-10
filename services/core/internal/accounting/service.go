package accounting

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrAccountService = errors.New("accounting: service failure")

type AccountStore interface {
	Create(context.Context, *tammyv1.Account, time.Time) error
	Get(context.Context, string) (*tammyv1.Account, error)
	Update(context.Context, uint64, *tammyv1.Account, time.Time) error
}

type CommandRepositories struct {
	Accounts AccountStore
	Journals JournalStore
	TaxCodes TaxCodeReadPort
}

type CommandRunner interface {
	Execute(context.Context, app.OrdinaryCommand[CommandRepositories]) (proto.Message, error)
}

type AuditFactory interface {
	Build(context.Context, app.OrdinaryOperation, *tammyv1.AuthenticationContext,
		string, string, proto.Message, proto.Message) (app.CommandResult, error)
}

type Service struct {
	commands CommandRunner
	audit    AuditFactory
	clock    func() time.Time
	newID    func() string
}

func NewService(commands CommandRunner, audit AuditFactory, clock func() time.Time, newID func() string) (*Service, error) {
	if commands == nil || audit == nil || clock == nil || newID == nil {
		return nil, ErrAccountService
	}
	return &Service{commands: commands, audit: audit, clock: clock, newID: newID}, nil
}

func (service *Service) CreateAccount(ctx context.Context, request *tammyv1.CreateAccountRequest) (*tammyv1.CreateAccountResponse, error) {
	if !validCommandContext(request.GetCommandContext()) {
		return nil, ErrAccountService
	}
	id := service.newID()
	if !ids.IsCanonicalV7(id) {
		return nil, ErrAccountService
	}
	var created *tammyv1.Account
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationCreateAccount,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext),
		Request: request, NewResult: func() proto.Message { return &tammyv1.CreateAccountResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.CreateAccountRequest)
			if !ok || repositories.Accounts == nil {
				return ErrAccountService
			}
			created = &tammyv1.Account{Id: id, OrganisationId: ownedRequest.OrganisationId, Version: 1,
				Code: ownedRequest.Code, Name: ownedRequest.Name, Type: ownedRequest.Type,
				Subtype: cloneString(ownedRequest.Subtype), NormalBalance: ownedRequest.NormalBalance,
				Status:                 tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
				Designation:            tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
				DefaultTaxCodeId:       cloneString(ownedRequest.DefaultTaxCodeId),
				ReportClassification:   ownedRequest.ReportClassification,
				CashFlowClassification: ownedRequest.CashFlowClassification}
			if ValidateAccount(created) != nil {
				return ErrInvalidAccount
			}
			return repositories.Accounts.Create(ctx, created, service.clock())
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.CreateAccountRequest)
			if !ok || created == nil {
				return app.CommandResult{}, ErrAccountService
			}
			result := &tammyv1.CreateAccountResponse{Account: proto.Clone(created).(*tammyv1.Account)}
			return service.audit.Build(ctx, app.OrdinaryOperationCreateAccount, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, created.Id, result, created)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.CreateAccountResponse)
	if !ok || response.Account == nil {
		return nil, ErrAccountService
	}
	return response, nil
}

func (service *Service) UpdateAccount(ctx context.Context, request *tammyv1.UpdateAccountRequest) (*tammyv1.UpdateAccountResponse, error) {
	if !validCommandContext(request.GetCommandContext()) || request.UpdateMask == nil || request.Patch == nil {
		return nil, ErrAccountService
	}
	var updated *tammyv1.Account
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationUpdateAccount,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext),
		Request: request, NewResult: func() proto.Message { return &tammyv1.UpdateAccountResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.UpdateAccountRequest)
			if !ok || repositories.Accounts == nil {
				return ErrAccountService
			}
			before, err := repositories.Accounts.Get(ctx, ownedRequest.AccountId)
			if err != nil {
				return err
			}
			if before.Version != ownedRequest.ExpectedVersion {
				return ErrAccountConflict
			}
			updated, err = applyAccountPatch(before, ownedRequest)
			if err != nil {
				return err
			}
			return repositories.Accounts.Update(ctx, before.Version, updated, service.clock())
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.UpdateAccountRequest)
			if !ok || updated == nil {
				return app.CommandResult{}, ErrAccountService
			}
			result := &tammyv1.UpdateAccountResponse{Account: proto.Clone(updated).(*tammyv1.Account)}
			return service.audit.Build(ctx, app.OrdinaryOperationUpdateAccount, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, updated.Id, result, updated)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.UpdateAccountResponse)
	if !ok || response.Account == nil {
		return nil, ErrAccountService
	}
	return response, nil
}

func (service *Service) SetAccountStatus(ctx context.Context, request *tammyv1.SetAccountStatusRequest) (*tammyv1.SetAccountStatusResponse, error) {
	if !validCommandContext(request.GetCommandContext()) || !canonicalText(request.GetReason(), 512) {
		return nil, ErrAccountService
	}
	var before, updated *tammyv1.Account
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationSetAccountStatus,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext),
		Request: request, NewResult: func() proto.Message { return &tammyv1.SetAccountStatusResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.SetAccountStatusRequest)
			if !ok || repositories.Accounts == nil {
				return ErrAccountService
			}
			var err error
			before, err = repositories.Accounts.Get(ctx, ownedRequest.AccountId)
			if err != nil || before.Version != ownedRequest.ExpectedVersion {
				return ErrAccountConflict
			}
			updated, err = TransitionAccountStatus(before, ownedRequest.Status)
			if err != nil {
				return err
			}
			return repositories.Accounts.Update(ctx, before.Version, updated, service.clock())
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.SetAccountStatusRequest)
			if !ok || before == nil || updated == nil {
				return app.CommandResult{}, ErrAccountService
			}
			result := &tammyv1.SetAccountStatusResponse{Account: proto.Clone(updated).(*tammyv1.Account)}
			payload := &tammyv1.AccountStatusChangedEvent{AccountId: updated.Id, FromStatus: before.Status,
				ToStatus: updated.Status, ReasonHash: reasonHash(ownedRequest.Reason)}
			return service.audit.Build(ctx, app.OrdinaryOperationSetAccountStatus, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, updated.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.SetAccountStatusResponse)
	if !ok || response.Account == nil {
		return nil, ErrAccountService
	}
	return response, nil
}

func applyAccountPatch(before *tammyv1.Account, request *tammyv1.UpdateAccountRequest) (*tammyv1.Account, error) {
	if ValidateAccount(before) != nil || request == nil || request.UpdateMask == nil || request.Patch == nil {
		return nil, ErrInvalidAccount
	}
	after := proto.Clone(before).(*tammyv1.Account)
	paths := append([]string(nil), request.UpdateMask.Paths...)
	sort.Strings(paths)
	seen := map[string]struct{}{}
	for _, path := range paths {
		if _, duplicate := seen[path]; duplicate {
			return nil, ErrInvalidAccount
		}
		seen[path] = struct{}{}
		switch path {
		case "code":
			after.Code = request.Patch.Code
		case "name":
			after.Name = request.Patch.Name
		case "subtype":
			after.Subtype = cloneString(request.Patch.Subtype)
		case "default_tax_code_id":
			after.DefaultTaxCodeId = cloneString(request.Patch.DefaultTaxCodeId)
		case "report_classification":
			after.ReportClassification = request.Patch.ReportClassification
		case "cash_flow_classification":
			after.CashFlowClassification = request.Patch.CashFlowClassification
		default:
			return nil, ErrInvalidAccount
		}
	}
	after.Version++
	if ValidateAccountMutation(before, after) != nil {
		return nil, ErrInvalidAccount
	}
	return after, nil
}

func validCommandContext(context *tammyv1.CommandContext) bool {
	return context != nil && context.Authentication != nil && ids.IsCanonicalV7(context.IdempotencyKey)
}
func cloneAuthentication(context *tammyv1.CommandContext) *tammyv1.AuthenticationContext {
	return proto.Clone(context.Authentication).(*tammyv1.AuthenticationContext)
}
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
func reasonHash(value string) []byte { digest := sha256.Sum256([]byte(value)); return digest[:] }
