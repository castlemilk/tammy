package accounting

import (
	"context"
	"errors"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

const (
	ClosePeriodFactorPurpose  = "close_accounting_period"
	ReopenPeriodFactorPurpose = "reopen_accounting_period"
)

var ErrPeriodService = errors.New("accounting: period service failure")

type PeriodService struct {
	commands CommandRunner
	audit    AuditFactory
	clock    func() time.Time
	newID    func() string
}

func NewPeriodService(commands CommandRunner, audit AuditFactory, clock func() time.Time, newID func() string) (*PeriodService, error) {
	if commands == nil || audit == nil || clock == nil || newID == nil {
		return nil, ErrPeriodService
	}
	return &PeriodService{commands: commands, audit: audit, clock: clock, newID: newID}, nil
}

func (service *PeriodService) ClosePeriod(ctx context.Context, request *tammyv1.ClosePeriodRequest) (*tammyv1.ClosePeriodResponse, error) {
	if request == nil || !validCommandContext(request.CommandContext) || request.CommandContext.FreshFactor == nil ||
		!ids.IsCanonicalV7(request.OrganisationId) || !validCivilDate(request.EndDate) || request.ExpectedFinancialRevision == 0 {
		return nil, ErrPeriodService
	}
	periodID := service.newID()
	if !ids.IsCanonicalV7(periodID) {
		return nil, ErrPeriodService
	}
	var period *tammyv1.AccountingPeriod
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationClosePeriod,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext), Request: request,
		NewResult: func() proto.Message { return &tammyv1.ClosePeriodResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.ClosePeriodRequest)
			if !ok || repositories.Periods == nil || repositories.Factors == nil {
				return ErrPeriodService
			}
			if err := repositories.Factors.Consume(ctx, ownedRequest.CommandContext.FreshFactor, ClosePeriodFactorPurpose); err != nil {
				return err
			}
			var err error
			period, err = repositories.Periods.Close(ctx, ownedRequest.OrganisationId, ownedRequest.EndDate,
				ownedRequest.ExpectedFinancialRevision, periodID, service.clock())
			return err
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.ClosePeriodRequest)
			if !ok || period == nil {
				return app.CommandResult{}, ErrPeriodService
			}
			result := &tammyv1.ClosePeriodResponse{Period: clonePeriod(period)}
			payload := &tammyv1.PeriodStateChangedEvent{PeriodId: period.Id,
				FromState: tammyv1.PeriodState_PERIOD_STATE_OPEN, ToState: period.State}
			return service.audit.Build(ctx, app.OrdinaryOperationClosePeriod, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, period.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.ClosePeriodResponse)
	if !ok || response.Period == nil {
		return nil, ErrPeriodService
	}
	return response, nil
}

func (service *PeriodService) ReopenPeriod(ctx context.Context, request *tammyv1.ReopenPeriodRequest) (*tammyv1.ReopenPeriodResponse, error) {
	if request == nil || !validCommandContext(request.CommandContext) || request.CommandContext.FreshFactor == nil ||
		!ids.IsCanonicalV7(request.PeriodId) || request.ExpectedVersion == 0 || !canonicalText(request.Reason, 512) {
		return nil, ErrPeriodService
	}
	var period *tammyv1.AccountingPeriod
	command := app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationReopenPeriod,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: cloneAuthentication(request.CommandContext), Request: request,
		NewResult: func() proto.Message { return &tammyv1.ReopenPeriodResponse{} },
		SaveSource: func(ctx context.Context, repositories CommandRepositories, owned proto.Message) error {
			ownedRequest, ok := owned.(*tammyv1.ReopenPeriodRequest)
			if !ok || repositories.Periods == nil || repositories.Factors == nil || repositories.Reports == nil {
				return ErrPeriodService
			}
			if err := repositories.Factors.Consume(ctx, ownedRequest.CommandContext.FreshFactor, ReopenPeriodFactorPurpose); err != nil {
				return err
			}
			if err := repositories.Reports.RequirePeriodReopenAllowed(ctx, ownedRequest.PeriodId, ownedRequest.Reason); err != nil {
				return err
			}
			var err error
			period, err = repositories.Periods.Reopen(ctx, ownedRequest.PeriodId, ownedRequest.ExpectedVersion, service.clock())
			return err
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			ownedRequest, ok := owned.(*tammyv1.ReopenPeriodRequest)
			if !ok || period == nil {
				return app.CommandResult{}, ErrPeriodService
			}
			result := &tammyv1.ReopenPeriodResponse{Period: clonePeriod(period)}
			reason := reasonHash(ownedRequest.Reason)
			payload := &tammyv1.PeriodStateChangedEvent{PeriodId: period.Id,
				FromState: tammyv1.PeriodState_PERIOD_STATE_CLOSED, ToState: period.State, ReasonHash: reason}
			return service.audit.Build(ctx, app.OrdinaryOperationReopenPeriod, ownedRequest.CommandContext.Authentication,
				ownedRequest.CommandContext.IdempotencyKey, period.Id, result, payload)
		}}
	result, err := service.commands.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.ReopenPeriodResponse)
	if !ok || response.Period == nil {
		return nil, ErrPeriodService
	}
	return response, nil
}
