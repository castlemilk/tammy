package purchases

import (
	"context"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

// BASSnapshot is the complete, immutable purchase contribution to a workpaper.
// The caller supplies the same encrypted transaction used for sales and saving.
// Fingerprint includes retained facts which do not change the claimed cents.
type BASSnapshot struct {
	Sources                   []*tammyv1.BasPurchaseSourceLine
	Reviews                   []*tammyv1.BasPurchaseReview
	LegacySupport             []*tammyv1.BasPurchaseLegacySupport
	LegacyCandidateGST        *tammyv1.Money
	ExcludedCashPurchaseCount uint64
	Fingerprint               []byte
}

type ReportingPort interface {
	CaptureBAS(context.Context, string, *tammyv1.PurchaseClaimPeriod, *tammyv1.CivilDate, time.Time) (*BASSnapshot, error)
}
