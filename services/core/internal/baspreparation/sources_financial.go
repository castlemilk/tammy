package baspreparation

import (
	"context"
	"errors"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

type BASFinancialSourceRequest struct {
	Source   BASBoundedSourceRequest
	GstBasis tammyv1.GstBasis
	AsOf     time.Time
}

type BASSourceFinancialMapping struct {
	Invoices  *BASInvoiceFinancialMapping
	Purchases *purchases.BASPurchaseFinancialMap
}

// CaptureBASFinancialSourceRecords maps the exact frozen source units before
// replacing proven duplicate tax-fact bodies. All readers and both financial
// mappers run within the caller's existing transaction, with no additional SQL
// or transaction ownership in this layer.
func CaptureBASFinancialSourceRecords(ctx context.Context, request BASFinancialSourceRequest,
	ports BASBoundedSourcePorts) (*BASBoundedSourceRecordCapture, error) {
	if ctx == nil || request.AsOf.IsZero() ||
		(request.GstBasis != tammyv1.GstBasis_GST_BASIS_CASH && request.GstBasis != tammyv1.GstBasis_GST_BASIS_NON_CASH) {
		return nil, ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrSourceReservation, err)
	}
	return captureBASBoundedSourceRecords(ctx, request.Source, ports, &request)
}
