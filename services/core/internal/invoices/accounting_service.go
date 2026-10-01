package invoices

import (
	"context"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

func (s *Service) PostIncome(ctx context.Context, request *tammyv1.PostInvoiceIncomeRequest) (*tammyv1.PostInvoiceIncomeResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.InvoiceId) || request.ExpectedVersion < 1 {
		return nil, ErrInvalidDraft
	}
	var saved *tammyv1.InvoiceAccounting
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationPostInvoiceIncome, OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication, Request: request, NewResult: func() proto.Message { return &tammyv1.PostInvoiceIncomeResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.PostInvoiceIncomeRequest)
			if repos.Invoices == nil || repos.Profiles == nil || repos.Accounting == nil {
				return ErrRepository
			}
			invoice, err := repos.Invoices.Get(ctx, r.OrganisationId, r.InvoiceId)
			if err != nil {
				return err
			}
			if invoice.State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED || invoice.Version != r.ExpectedVersion {
				return ErrConflict
			}
			profile, err := repos.Profiles.Get(ctx, r.OrganisationId)
			if err != nil {
				return ErrNotFound
			}
			saved, err = repos.Accounting.Post(ctx, invoice, profile, r.CommandContext.IdempotencyKey)
			return err
		}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.PostInvoiceIncomeRequest)
			if saved == nil {
				return app.CommandResult{}, ErrRepository
			}
			return s.audit.Build(ctx, app.OrdinaryOperationPostInvoiceIncome, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, saved.InvoiceId, &tammyv1.PostInvoiceIncomeResponse{Accounting: saved}, saved)
		}})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.PostInvoiceIncomeResponse)
	if !ok || response.Accounting == nil {
		return nil, ErrRepository
	}
	return response, nil
}
func (s *Service) RecordPayment(ctx context.Context, request *tammyv1.RecordInvoicePaymentRequest) (*tammyv1.RecordInvoicePaymentResponse, error) {
	if s == nil || request == nil || !validCommand(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.InvoiceId) || !ids.IsCanonicalV7(request.BankAccountId) || request.ExpectedAccountingVersion < 1 || incomeDateString(request.PaymentDate) == "" || request.Amount == nil || request.Amount.CurrencyCode != "AUD" || request.Amount.MinorUnits <= 0 || request.Amount.MinorUnits > maxTotalMinor || !text(request.Reference, 200, false) {
		return nil, ErrPayment
	}
	var saved *tammyv1.InvoiceAccounting
	var payment *tammyv1.InvoicePayment
	result, err := s.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{Operation: app.OrdinaryOperationRecordInvoicePayment, OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication, Request: request, NewResult: func() proto.Message { return &tammyv1.RecordInvoicePaymentResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r := owned.(*tammyv1.RecordInvoicePaymentRequest)
			if repos.Invoices == nil || repos.Profiles == nil || repos.Accounting == nil {
				return ErrRepository
			}
			invoice, err := repos.Invoices.Get(ctx, r.OrganisationId, r.InvoiceId)
			if err != nil {
				return err
			}
			profile, err := repos.Profiles.Get(ctx, r.OrganisationId)
			if err != nil {
				return ErrNotFound
			}
			saved, payment, err = repos.Accounting.RecordPayment(ctx, invoice, profile, r)
			return err
		}, BuildResult: func(ctx context.Context, _ CommandRepositories, owned proto.Message) (app.CommandResult, error) {
			r := owned.(*tammyv1.RecordInvoicePaymentRequest)
			if saved == nil || payment == nil {
				return app.CommandResult{}, ErrRepository
			}
			return s.audit.Build(ctx, app.OrdinaryOperationRecordInvoicePayment, r.CommandContext.Authentication, r.CommandContext.IdempotencyKey, payment.Id, &tammyv1.RecordInvoicePaymentResponse{Accounting: saved, Payment: payment}, payment)
		}})
	if err != nil {
		return nil, err
	}
	response, ok := result.(*tammyv1.RecordInvoicePaymentResponse)
	if !ok || response.Accounting == nil || response.Payment == nil {
		return nil, ErrRepository
	}
	return response, nil
}
