package invoices

import (
	"context"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ProfileStore interface {
	Get(context.Context, string) (*tammyv1.Organisation, error)
}
type CommandRepositories struct {
	Invoices *Repository
	Profiles ProfileStore
	// Authorization is supplied by the transaction adapter, never by the caller.
	Authorize func(context.Context, *tammyv1.AuthenticationContext, authorisation.Action) error
}
type CommandRunner interface {
	Execute(context.Context, app.OrdinaryCommand[CommandRepositories]) (proto.Message, error)
}
type AuditFactory interface {
	Build(context.Context, app.OrdinaryOperation, *tammyv1.AuthenticationContext, string, string, proto.Message, proto.Message) (app.CommandResult, error)
}
type Service struct {
	commands CommandRunner
	audit    AuditFactory
	now      func() time.Time
	newID    func() (string, error)
}

func NewService(commands CommandRunner, audit AuditFactory, now func() time.Time, newID func() (string, error)) (*Service, error) {
	if commands == nil || audit == nil || now == nil || newID == nil {
		return nil, ErrRepository
	}
	return &Service{commands, audit, now, newID}, nil
}
func validCommand(c *tammyv1.CommandContext) bool {
	return c != nil && c.Authentication != nil && ids.IsCanonicalV7(c.IdempotencyKey) && ids.IsCanonicalV7(c.Authentication.ActorUserId) && ids.IsCanonicalV7(c.Authentication.SessionId)
}

func (s *Service) SaveDraft(ctx context.Context, request *tammyv1.SaveInvoiceDraftRequest) (*tammyv1.SaveInvoiceDraftResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || (request.InvoiceId == "" && request.ExpectedVersion != 0) || (request.InvoiceId != "" && (!ids.IsCanonicalV7(request.InvoiceId) || request.ExpectedVersion == 0)) {
		return nil, ErrInvalidDraft
	}
	if _, err := Calculate(request.Draft); err != nil {
		return nil, err
	}
	var saved *tammyv1.Invoice
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationSaveInvoiceDraft, OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication, Request: request, NewResult: func() proto.Message { return &tammyv1.SaveInvoiceDraftResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.SaveInvoiceDraftRequest)
			if repos.Invoices == nil || repos.Profiles == nil {
				return ErrRepository
			}
			profile, err := repos.Profiles.Get(ctx, r.OrganisationId)
			if err != nil {
				return ErrNotFound
			}
			saved, err = Calculate(r.Draft)
			if err != nil {
				return err
			}
			now := timestamppb.New(s.now())
			saved.Id = r.InvoiceId
			saved.OrganisationId = r.OrganisationId
			saved.Version = r.ExpectedVersion + 1
			saved.State = tammyv1.InvoiceState_INVOICE_STATE_DRAFT
			saved.SellerName = profile.LegalName
			saved.SellerAbn = profile.Abn
			saved.Draft = proto.Clone(r.Draft).(*tammyv1.InvoiceDraft)
			saved.UpdatedAt = now
			saved.CreatedAt = proto.Clone(now).(*timestamppb.Timestamp)
			if r.InvoiceId == "" {
				saved.Id, err = s.newID()
				if err != nil {
					return ErrRepository
				}
			} else {
				before, err := repos.Invoices.Get(ctx, r.OrganisationId, r.InvoiceId)
				if err != nil {
					return err
				}
				if before.State != tammyv1.InvoiceState_INVOICE_STATE_DRAFT || before.Version != r.ExpectedVersion {
					return ErrConflict
				}
				saved.CreatedAt = before.CreatedAt
			}
			return repos.Invoices.Save(ctx, saved, r.ExpectedVersion)
		}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.SaveInvoiceDraftRequest)
			if saved == nil {
				return app.CommandResult{}, ErrRepository
			}
			result := &tammyv1.SaveInvoiceDraftResponse{Invoice: saved}
			return s.audit.Build(ctx, app.OrdinaryOperationSaveInvoiceDraft, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, saved.Id, result, saved)
		}})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.SaveInvoiceDraftResponse)
	if !ok || response.Invoice == nil {
		return nil, ErrRepository
	}
	return response, nil
}
func (s *Service) Issue(ctx context.Context, request *tammyv1.IssueInvoiceRequest) (*tammyv1.IssueInvoiceResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.InvoiceId) || request.ExpectedVersion < 1 {
		return nil, ErrInvalidDraft
	}
	var issued *tammyv1.Invoice
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationIssueInvoice, OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication, Request: request, NewResult: func() proto.Message { return &tammyv1.IssueInvoiceResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.IssueInvoiceRequest)
			if repos.Invoices == nil || repos.Profiles == nil {
				return ErrRepository
			}
			var err error
			issued, err = repos.Invoices.Get(ctx, r.OrganisationId, r.InvoiceId)
			if err != nil {
				return err
			}
			if issued.State != tammyv1.InvoiceState_INVOICE_STATE_DRAFT || issued.Version != r.ExpectedVersion {
				return ErrConflict
			}
			if err := ValidateIssue(issued.Draft); err != nil {
				return err
			}
			profile, err := repos.Profiles.Get(ctx, r.OrganisationId)
			if err != nil {
				return ErrNotFound
			}
			// Issue exactly the seller identity reviewed with the saved draft. A
			// profile change requires another save/review, never a silent swap.
			if issued.SellerName != profile.LegalName || issued.SellerAbn != profile.Abn {
				return ErrConflict
			}
			issued.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
			issued.Version++
			issued.UpdatedAt = timestamppb.New(s.now())
			return repos.Invoices.Save(ctx, issued, r.ExpectedVersion)
		}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.IssueInvoiceRequest)
			if issued == nil {
				return app.CommandResult{}, ErrRepository
			}
			result := &tammyv1.IssueInvoiceResponse{Invoice: issued}
			return s.audit.Build(ctx, app.OrdinaryOperationIssueInvoice, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, issued.Id, result, issued)
		}})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.IssueInvoiceResponse)
	if !ok || response.Invoice == nil {
		return nil, ErrRepository
	}
	return response, nil
}
