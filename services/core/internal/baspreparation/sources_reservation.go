package baspreparation

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
)

const (
	maximumBASSourceRoots       = int64(1000)
	maximumBASSourceReceipts    = int64(1000)
	maximumBASSourceScalarBytes = int64(8 * 1024 * 1024)
	maximumBASSourceScalarKeys  = maximumBASSourceScalarBytes / 512
)

var (
	ErrSourceReservation = errors.New("baspreparation: source reservation failed")
	ErrSourceBound       = errors.New("baspreparation: complete source exceeds supported capacity")
)

type SourceBoundError struct {
	Resource string
	Limit    int64
}

func (err *SourceBoundError) Error() string {
	if err == nil {
		return ErrSourceBound.Error()
	}
	return fmt.Sprintf("%s: %s exceeds limit %d", ErrSourceBound, err.Resource, err.Limit)
}

func (err *SourceBoundError) Unwrap() error { return ErrSourceBound }

// BASSourceScalarPorts exposes aggregate summaries only. The command supplies
// repositories backed by its single serializable transaction. No typed-key,
// parent-ID or source-body method is available during this stage.
type BASSourceScalarPorts struct {
	Invoices interface {
		SummarizeBASKeys(context.Context, string) (*invoices.BASScalarSummary, error)
	}
	Purchases interface {
		SummarizeBASKeys(context.Context, string) (*purchases.BASScalarSummary, error)
	}
	Accounting interface {
		SummarizeTaxFactBASKeys(context.Context, string) (*accounting.TaxFactBASScalarSummary, error)
	}
}

// BASSourceScalarReservation preserves each module's exact count/size summary.
// Accounting fact keys do not add roots or receipts. Physical keys are not
// semantic candidates, and these summaries are not content fingerprints.
type BASSourceScalarReservation struct {
	Invoices                                             invoices.BASScalarSummary
	Purchases                                            purchases.BASScalarSummary
	Accounting                                           accounting.TaxFactBASScalarSummary
	RootCount, ReceiptCount, ScalarKeyCount, ScalarBytes int64
}

// ReserveBASSourceScalars establishes the shared aggregate fence before the
// caller starts payload preflight or typed-key discovery. It reserves only
// physical scalar cells; decision/document/selector and complete-payload
// reservations remain separate stages. It never owns transaction completion.
func ReserveBASSourceScalars(ctx context.Context, organisationID string, ports BASSourceScalarPorts, maximumScalarBytes int64) (*BASSourceScalarReservation, error) {
	if ctx == nil || !ids.IsCanonicalV7(organisationID) || maximumScalarBytes < 0 || ports.Invoices == nil || ports.Purchases == nil || ports.Accounting == nil {
		return nil, ErrSourceReservation
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrSourceReservation, err)
	}
	invoice, err := ports.Invoices.SummarizeBASKeys(ctx, organisationID)
	if err != nil || ctx.Err() != nil {
		return nil, errors.Join(ErrSourceReservation, err, ctx.Err())
	}
	if invoice == nil || invoice.OrganisationID != organisationID || !validBASSourceScalarCharge(invoice.ScalarKeyCount, invoice.ScalarBytes) {
		return nil, ErrSourceReservation
	}
	count, valid := sumBASSourceScalars(invoice.RootCount, invoice.PostingCount, invoice.IncomeLineCount, invoice.ReceiptCount, invoice.AllocationCount)
	if !valid || count != invoice.ScalarKeyCount || invoice.PostingCount > invoice.RootCount {
		return nil, ErrSourceReservation
	}
	result := &BASSourceScalarReservation{Invoices: *invoice}

	purchase, err := ports.Purchases.SummarizeBASKeys(ctx, organisationID)
	if err != nil || ctx.Err() != nil {
		return nil, errors.Join(ErrSourceReservation, err, ctx.Err())
	}
	if purchase == nil || purchase.OrganisationID != organisationID || !validBASSourceScalarCharge(purchase.ScalarKeyCount, purchase.ScalarBytes) ||
		purchase.RootCount < 0 || purchase.ReceiptCount < 0 || purchase.RootCount > purchase.ScalarKeyCount || purchase.ReceiptCount > purchase.ScalarKeyCount {
		return nil, ErrSourceReservation
	}
	count, valid = sumBASSourceScalars(purchase.FamilyCounts[:]...)
	if !valid || count != purchase.ScalarKeyCount {
		return nil, ErrSourceReservation
	}
	postingCount := purchase.FamilyCounts[purchases.BASScalarPosting-1]
	paymentCount := purchase.FamilyCounts[purchases.BASScalarPayment-1]
	maximumRoots, validRoots := sumBASSourceScalars(postingCount, purchase.FamilyCounts[purchases.BASScalarDocument-1], purchase.FamilyCounts[purchases.BASScalarOpeningPayable-1])
	maximumReceipts, validReceipts := sumBASSourceScalars(paymentCount, purchase.FamilyCounts[purchases.BASScalarManualJournal-1])
	if !validRoots || !validReceipts || purchase.RootCount < postingCount || purchase.RootCount > maximumRoots || purchase.ReceiptCount < paymentCount || purchase.ReceiptCount > maximumReceipts {
		return nil, ErrSourceReservation
	}
	result.Purchases = *purchase

	facts, err := ports.Accounting.SummarizeTaxFactBASKeys(ctx, organisationID)
	if err != nil || ctx.Err() != nil {
		return nil, errors.Join(ErrSourceReservation, err, ctx.Err())
	}
	if facts == nil || facts.OrganisationID != organisationID || facts.RootCount != 0 || facts.ReceiptCount != 0 || !validBASSourceScalarCharge(facts.ScalarKeyCount, facts.ScalarBytes) {
		return nil, ErrSourceReservation
	}
	result.Accounting = *facts
	if maximumScalarBytes > maximumBASSourceScalarBytes {
		maximumScalarBytes = maximumBASSourceScalarBytes
	}
	for _, category := range []struct {
		values   []int64
		total    *int64
		resource string
		limit    int64
	}{
		{[]int64{result.Invoices.RootCount, result.Purchases.RootCount}, &result.RootCount, "source roots", maximumBASSourceRoots},
		{[]int64{result.Invoices.ReceiptCount, result.Purchases.ReceiptCount}, &result.ReceiptCount, "receipt sources", maximumBASSourceReceipts},
		{[]int64{result.Invoices.ScalarKeyCount, result.Purchases.ScalarKeyCount, facts.ScalarKeyCount}, &result.ScalarKeyCount, "scalar keys", maximumBASSourceScalarKeys},
		{[]int64{result.Invoices.ScalarBytes, result.Purchases.ScalarBytes, facts.ScalarBytes}, &result.ScalarBytes, "scalar bytes", maximumScalarBytes},
	} {
		var total int64
		for _, value := range category.values {
			if value > category.limit-total {
				return nil, errors.Join(ErrSourceReservation, &SourceBoundError{Resource: category.resource, Limit: category.limit})
			}
			total += value
		}
		*category.total = total
	}
	return result, nil
}

func validBASSourceScalarCharge(count, charged int64) bool {
	return count >= 0 && count <= math.MaxInt64/512 && charged >= count*512 && (count != 0 || charged == 0)
}

func sumBASSourceScalars(values ...int64) (int64, bool) {
	var total int64
	for _, value := range values {
		if value < 0 || value > math.MaxInt64-total {
			return 0, false
		}
		total += value
	}
	return total, true
}
