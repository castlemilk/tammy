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

func (h *invoiceHandler) SaveInvoiceTemplate(ctx context.Context, r *connect.Request[tammyv1.SaveInvoiceTemplateRequest]) (*connect.Response[tammyv1.SaveInvoiceTemplateResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	value, err := h.service.SaveTemplate(ctx, r.Msg)
	if err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}

func (h *invoiceHandler) UploadInvoiceLogo(ctx context.Context, r *connect.Request[tammyv1.UploadInvoiceLogoRequest]) (*connect.Response[tammyv1.UploadInvoiceLogoResponse], error) {
	if h == nil || h.service == nil || r == nil || r.Msg == nil {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	value, err := h.service.UploadLogo(ctx, r.Msg)
	if err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}

func (h *invoiceHandler) GetInvoiceTemplate(ctx context.Context, r *connect.Request[tammyv1.GetInvoiceTemplateRequest]) (*connect.Response[tammyv1.GetInvoiceTemplateResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || r == nil || r.Msg == nil || r.Msg.Authentication == nil || !ids.IsCanonicalV7(r.Msg.OrganisationId) {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, r.Msg.Authentication); err != nil {
		return nil, invoiceError(err)
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, invoiceError(err)
	}
	defer tx.Rollback()
	repo, err := invoices.NewRepository(tx)
	if err != nil {
		return nil, invoiceError(err)
	}
	value, err := repo.GetTemplate(ctx, r.Msg.OrganisationId)
	if err != nil {
		return nil, invoiceError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(&tammyv1.GetInvoiceTemplateResponse{Template: value}), nil
}

func (h *invoiceHandler) GetInvoiceLogo(ctx context.Context, r *connect.Request[tammyv1.GetInvoiceLogoRequest]) (*connect.Response[tammyv1.GetInvoiceLogoResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || r == nil || r.Msg == nil || r.Msg.Authentication == nil || !ids.IsCanonicalV7(r.Msg.OrganisationId) || !ids.IsCanonicalV7(r.Msg.LogoAssetId) {
		return nil, invoiceError(invoices.ErrInvalidDraft)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, r.Msg.Authentication); err != nil {
		return nil, invoiceError(err)
	}
	tx, err := h.database.BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, invoiceError(err)
	}
	defer tx.Rollback()
	repo, err := invoices.NewRepository(tx)
	if err != nil {
		return nil, invoiceError(err)
	}
	value, err := repo.GetLogo(ctx, r.Msg.OrganisationId, r.Msg.LogoAssetId)
	if err != nil {
		return nil, invoiceError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, invoiceError(err)
	}
	return connect.NewResponse(value), nil
}
