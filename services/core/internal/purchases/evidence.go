package purchases

import (
	"bytes"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// ReviewedExceptionRule is supplied only after the command owner has verified
// the current typed rule and supporting records for this source. Matching refs
// here does not establish source ownership or legal exception eligibility.
type ReviewedExceptionRule struct {
	Kind   tammyv1.PurchaseTaxInvoiceExceptionKind
	Source *tammyv1.PurchaseRetainedSourceRef
}
type EvidenceReadiness uint8

const (
	EvidenceNormalSupported EvidenceReadiness = iota + 1
	EvidenceMissing
	EvidenceTimingReviewRequired
	EvidenceFuture
	EvidenceUnsupportedException
)

// ValidatePurchaseEvidence checks retained review structure independently of
// claim readiness. UNKNOWN supports retaining a known entitlement in suspense.
func ValidatePurchaseEvidence(review *tammyv1.PurchaseEvidenceInput, rule *ReviewedExceptionRule) error {
	if review == nil || !review.RegistrationReviewConfirmed || !review.BusinessUseReviewConfirmed || !review.EvidenceReviewConfirmed ||
		!purchaseReviewText(review.Reason, 512, true) ||
		(review.Classification != tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN && review.Classification != tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE && review.Classification != tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION) ||
		(review.Source != nil && !validPurchaseSourceRef(review.Source)) || (review.HeldDate != nil && !validPurchaseDate(review.HeldDate)) {
		return ErrInvalidEvidence
	}
	if review.Classification != tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN &&
		(review.Source == nil || review.HeldDate == nil) {
		return ErrInvalidEvidence
	}
	if review.Classification != tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION {
		if review.ExceptionReview != nil {
			return ErrInvalidEvidence
		}
		return nil
	}
	exception := review.ExceptionReview
	if exception == nil || rule == nil || !validPurchaseSourceRef(rule.Source) || !validPurchaseSourceRef(exception.Rule) ||
		(exception.Kind != tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION && exception.Kind != tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_SPECIFIC_RETAINED_WAIVER) ||
		exception.Kind != rule.Kind || exception.Rule.SourceId != rule.Source.SourceId || exception.Rule.SourceVersion != rule.Source.SourceVersion ||
		!bytes.Equal(exception.Rule.SourceHash, rule.Source.SourceHash) || !exception.TaxInvoiceNotRequiredReviewConfirmed || !exception.SupportingRecordRequirementsReviewConfirmed ||
		!purchaseReviewText(exception.Reason, 512, true) || len(exception.SupportingRecords) < 1 || len(exception.SupportingRecords) > 50 {
		return ErrUnsupportedException
	}
	for _, ref := range exception.SupportingRecords {
		if !validPurchaseSourceRef(ref) {
			return ErrUnsupportedException
		}
	}
	return nil
}

// EvidenceReadinessFor compares actual held/event dates with a trusted as-of
// date. It never supplies claim periods, return due dates or deadline rules.
func EvidenceReadinessFor(review *tammyv1.PurchaseEvidenceInput, eventDate, asOf *tammyv1.CivilDate, rule *ReviewedExceptionRule) (EvidenceReadiness, error) {
	if !validPurchaseDate(eventDate) || !validPurchaseDate(asOf) {
		return 0, ErrInvalidEvidence
	}
	if err := ValidatePurchaseEvidence(review, rule); err != nil {
		if errors.Is(err, ErrUnsupportedException) {
			return EvidenceUnsupportedException, nil
		}
		return 0, err
	}
	if review.Classification == tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN {
		return EvidenceMissing, nil
	}
	if purchaseDateKey(review.HeldDate) > purchaseDateKey(asOf) {
		return EvidenceFuture, nil
	}
	if purchaseDateKey(review.HeldDate) > purchaseDateKey(eventDate) {
		return EvidenceTimingReviewRequired, nil
	}
	return EvidenceNormalSupported, nil
}

func validPurchaseSourceRef(ref *tammyv1.PurchaseRetainedSourceRef) bool {
	return ref != nil && ids.IsCanonicalV7(ref.SourceId) && ref.SourceVersion > 0 && len(ref.SourceHash) == 32
}

func purchaseDateKey(value *tammyv1.CivilDate) int32 {
	return value.Year*10000 + value.Month*100 + value.Day
}
