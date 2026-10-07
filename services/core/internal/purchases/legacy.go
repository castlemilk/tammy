package purchases

import (
	"bytes"
	"errors"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrInvalidLegacyReview = errors.New("purchases: invalid or unsupported legacy review")

// ValidateLegacyReview checks explicit, bounded facts. Retained source ownership,
// payment lineage, dates and contiguous event history are transaction checks.
func ValidateLegacyReview(input *tammyv1.LegacyPurchaseReviewInput) error {
	if input == nil || !validClaimTimingPeriod(input.AffectedPeriod) ||
		purchaseDateString(input.AffectedPeriod.PeriodStart) == "" || purchaseDateString(input.AffectedPeriod.PeriodEnd) == "" ||
		purchaseDateString(input.ReportingBalanceDate) == "" || purchaseDateKey(input.ReportingBalanceDate) < purchaseDateKey(input.AffectedPeriod.PeriodEnd) ||
		!purchaseReviewText(input.Reason, 512, true) || len(input.SupportingPayments) > 200 || len(input.SupportingEvidence) > 200 {
		return ErrInvalidLegacyReview
	}
	if _, _, _, _, _, err := legacySourceScope(input.Source); err != nil {
		return err
	}
	for _, refs := range [][]*tammyv1.PurchaseRetainedSourceRef{input.SupportingPayments, input.SupportingEvidence} {
		seen := make(map[string]bool, len(refs))
		for _, ref := range refs {
			if !validLegacySupport(ref) || seen[ref.SourceId] {
				return ErrInvalidLegacyReview
			}
			seen[ref.SourceId] = true
		}
	}
	if input.LinkedPostingId != nil && !ids.IsCanonicalV7(*input.LinkedPostingId) {
		return ErrInvalidLegacyReview
	}
	claimed := input.Disposition == tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED
	if claimed {
		if !validClaimTimingPeriod(input.PreviouslyClaimedPeriod) || purchaseDateString(input.PreviouslyClaimedPeriod.PeriodStart) == "" ||
			purchaseDateString(input.PreviouslyClaimedPeriod.PeriodEnd) == "" ||
			purchaseDateKey(input.PreviouslyClaimedPeriod.PeriodEnd) > purchaseDateKey(input.ReportingBalanceDate) || !validLegacySupport(input.ClaimEvidence) {
			return ErrInvalidLegacyReview
		}
	} else if input.PreviouslyClaimedPeriod != nil || input.ClaimEvidence != nil {
		return ErrInvalidLegacyReview
	}
	switch input.Disposition {
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE,
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE:
		if len(input.SupportingEvidence) == 0 || input.LinkedPostingId != nil {
			return ErrInvalidLegacyReview
		}
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED:
		if input.LinkedPostingId != nil {
			return ErrInvalidLegacyReview
		}
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_DUPLICATE:
		if input.LinkedPostingId == nil && len(input.SupportingEvidence) == 0 {
			return ErrInvalidLegacyReview
		}
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE:
		if input.LinkedPostingId == nil || input.Source.GetDocument() == nil || len(input.SupportingEvidence) == 0 {
			return ErrInvalidLegacyReview
		}
	case tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT:
		if len(input.SupportingPayments) == 0 || len(input.SupportingEvidence) == 0 {
			return ErrInvalidLegacyReview
		}
	default:
		return ErrInvalidLegacyReview
	}
	return nil
}

func validLegacySupport(ref *tammyv1.PurchaseRetainedSourceRef) bool {
	return ref != nil && ids.IsCanonicalV7(ref.SourceId) && ref.SourceVersion > 0 && ref.SourceVersion <= math.MaxInt64 && len(ref.SourceHash) == 32
}

func legacySourceScope(source *tammyv1.LegacyPurchaseSourceRef) (kind, id string, version uint64, hash, reviewedHash []byte, err error) {
	if source == nil {
		return "", "", 0, nil, nil, ErrInvalidLegacyReview
	}
	switch value := source.Source.(type) {
	case *tammyv1.LegacyPurchaseSourceRef_Document:
		if value == nil || value.Document == nil {
			return "", "", 0, nil, nil, ErrInvalidLegacyReview
		}
		kind, id, version, hash, reviewedHash = "DOCUMENT", value.Document.DocumentId, value.Document.ReviewedVersion, value.Document.OriginalContentHash, value.Document.ReviewedSnapshotHash
	case *tammyv1.LegacyPurchaseSourceRef_OpeningPayable:
		if value == nil || value.OpeningPayable == nil {
			return "", "", 0, nil, nil, ErrInvalidLegacyReview
		}
		kind, id, version, hash = "OPENING_PAYABLE", value.OpeningPayable.OpeningPayableId, value.OpeningPayable.SourceVersion, value.OpeningPayable.SourceHash
	default:
		return "", "", 0, nil, nil, ErrInvalidLegacyReview
	}
	if !ids.IsCanonicalV7(id) || version == 0 || version > math.MaxInt64 || len(hash) != 32 || (kind == "DOCUMENT" && len(reviewedHash) != 32) {
		return "", "", 0, nil, nil, ErrInvalidLegacyReview
	}
	return
}

// LegacyReviewApplies only accepts the reviewed scope and source version. A
// decision about one balance date cannot waive newly discovered later payment.
func LegacyReviewApplies(record *tammyv1.LegacyPurchaseReview, source *tammyv1.LegacyPurchaseSourceRef, period *tammyv1.PurchaseClaimPeriod, balance *tammyv1.CivilDate) bool {
	if record == nil || ValidateLegacyReview(record.Review) != nil || !validClaimTimingPeriod(period) || !validPurchaseDate(balance) {
		return false
	}
	kind, id, version, hash, reviewedHash, err := legacySourceScope(source)
	oldKind, oldID, oldVersion, oldHash, oldReviewedHash, oldErr := legacySourceScope(record.Review.Source)
	if err != nil || oldErr != nil || kind != oldKind || id != oldID || version != oldVersion || !bytes.Equal(hash, oldHash) || !bytes.Equal(reviewedHash, oldReviewedHash) {
		return false
	}
	return purchaseDateKey(period.PeriodStart) >= purchaseDateKey(record.Review.AffectedPeriod.PeriodStart) &&
		purchaseDateKey(period.PeriodEnd) <= purchaseDateKey(record.Review.AffectedPeriod.PeriodEnd) &&
		purchaseDateKey(balance) <= purchaseDateKey(record.Review.ReportingBalanceDate)
}

type LegacyCandidate struct {
	Source       *tammyv1.LegacyPurchaseSourceRef
	Document     *tammyv1.Document
	Opening      *OpeningPayable
	Reviews      []*tammyv1.LegacyPurchaseReview
	PriorSources []*tammyv1.SourceRef
	Status       tammyv1.PurchaseReviewStatus
	CandidateGST int64
	EvidenceOnly bool
	Excluded     bool
}
