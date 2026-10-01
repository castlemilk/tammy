//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"database/sql"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

func (h *invoiceHandler) GetInvoiceAccounting(ctx context.Context, request *connect.Request[tammyv1.GetInvoiceAccountingRequest]) (*connect.Response[tammyv1.GetInvoiceAccountingResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || request == nil || request.Msg == nil || !ids.IsCanonicalV7(request.Msg.OrganisationId) || !ids.IsCanonicalV7(request.Msg.InvoiceId) {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, invoiceError(err)
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, invoiceError(err)
	}
	defer tx.Rollback()
	repo, err := invoices.NewAccountingRepository(tx, h.now, h.newID)
	if err != nil {
		return nil, invoiceError(err)
	}
	value, err := repo.Get(ctx, request.Msg.OrganisationId, request.Msg.InvoiceId)
	if err != nil {
		return nil, invoiceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(&tammyv1.GetInvoiceAccountingResponse{Accounting: value}), nil
}
func (h *invoiceHandler) PostInvoiceIncome(ctx context.Context, request *connect.Request[tammyv1.PostInvoiceIncomeRequest]) (*connect.Response[tammyv1.PostInvoiceIncomeResponse], error) {
	if h == nil || h.service == nil || request == nil || request.Msg == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	value, err := h.service.PostIncome(ctx, request.Msg)
	if err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}
func (h *invoiceHandler) RecordInvoicePayment(ctx context.Context, request *connect.Request[tammyv1.RecordInvoicePaymentRequest]) (*connect.Response[tammyv1.RecordInvoicePaymentResponse], error) {
	if h == nil || h.service == nil || request == nil || request.Msg == nil {
		return nil, invoiceError(invoices.ErrPayment)
	}
	value, err := h.service.RecordPayment(ctx, request.Msg)
	if err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}
