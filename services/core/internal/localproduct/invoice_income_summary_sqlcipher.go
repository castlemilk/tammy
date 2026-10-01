//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/reporting"
)

func (h *invoiceHandler) GetBusinessIncomeSummary(ctx context.Context, request *connect.Request[tammyv1.GetBusinessIncomeSummaryRequest]) (*connect.Response[tammyv1.GetBusinessIncomeSummaryResponse], error) {
	if h == nil || h.database == nil || h.identity == nil || request == nil || request.Msg == nil || request.Msg.Authentication == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, ErrLedgerModule)
	}
	if err := h.identity.RequireActiveSessionReadOnly(ctx, request.Msg.Authentication); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, ErrLedgerModule)
	}
	// One read snapshot covers ledger, invoice links, receipts and source revision.
	// It does not reserve SQLite's writer slot or create historical postings.
	tx, err := h.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	defer tx.Rollback()
	repository, err := reporting.NewRepository(tx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	summary, err := repository.GetBusinessIncomeSummary(ctx, request.Msg.OrganisationId, request.Msg.PeriodStart, request.Msg.PeriodEnd, request.Msg.AsOfDate)
	if err != nil {
		code := connect.CodeFailedPrecondition
		if errors.Is(err, reporting.ErrNotFound) {
			code = connect.CodeNotFound
		}
		return nil, connect.NewError(code, ErrLedgerModule)
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, ErrLedgerModule)
	}
	return connect.NewResponse(&tammyv1.GetBusinessIncomeSummaryResponse{Summary: summary}), nil
}
