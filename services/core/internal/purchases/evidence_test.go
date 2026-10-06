package purchases

import (
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func purchaseException() (*tammyv1.PurchaseEvidenceInput, *ReviewedExceptionRule) {
	review := purchaseEvidence()
	review.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION
	rule := purchaseSource("018f0000-0000-7000-8000-000000000904")
	review.ExceptionReview = &tammyv1.PurchaseTaxInvoiceExceptionReview{
		Kind: tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION,
		Rule: rule, SupportingRecords: []*tammyv1.PurchaseRetainedSourceRef{purchaseSource("018f0000-0000-7000-8000-000000000905")},
		TaxInvoiceNotRequiredReviewConfirmed: true, SupportingRecordRequirementsReviewConfirmed: true, Reason: "Verified retained rule and supporting records"}
	return review, &ReviewedExceptionRule{Kind: review.ExceptionReview.Kind, Source: proto.Clone(rule).(*tammyv1.PurchaseRetainedSourceRef)}
}

func TestPurchaseEvidenceNormalMissingLateAndFutureReadiness(t *testing.T) {
	for _, test := range []struct {
		name              string
		classification    tammyv1.PurchaseEvidenceClassification
		held, event, asof *tammyv1.CivilDate
		want              EvidenceReadiness
	}{
		{"held before event", 2, purchaseDate(29), purchaseDate(30), purchaseDate(30), EvidenceNormalSupported},
		{"held on event", 2, purchaseDate(30), purchaseDate(30), purchaseDate(30), EvidenceNormalSupported},
		{"held after event", 2, purchaseDate(30), purchaseDate(29), purchaseDate(30), EvidenceTimingReviewRequired},
		{"future held", 2, purchaseDate(30), purchaseDate(30), purchaseDate(29), EvidenceFuture},
		{"future held before future event", 2, purchaseDate(29), purchaseDate(30), purchaseDate(28), EvidenceFuture},
		{"unknown genuinely missing", 1, nil, purchaseDate(30), purchaseDate(30), EvidenceMissing},
		{"unknown OCR with held date", 1, purchaseDate(29), purchaseDate(30), purchaseDate(30), EvidenceMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			review := purchaseEvidence()
			review.Classification = test.classification
			review.HeldDate = test.held
			if test.held == nil {
				review.Source = nil
			}
			got, err := EvidenceReadinessFor(review, test.event, test.asof, nil)
			if err != nil || got != test.want {
				t.Fatalf("readiness=%v want=%v error=%v", got, test.want, err)
			}
		})
	}
	review, rule := purchaseException()
	if got, err := EvidenceReadinessFor(review, purchaseDate(30), purchaseDate(30), rule); err != nil || got != EvidenceNormalSupported {
		t.Fatalf("verified exception: got=%v error=%v", got, err)
	}
	if got, err := EvidenceReadinessFor(review, purchaseDate(30), purchaseDate(30), nil); err != nil || got != EvidenceUnsupportedException {
		t.Fatalf("unverified exception must not clear: got=%v error=%v", got, err)
	}
}

func TestPurchaseEvidenceRequiresReviewedTypedFactsAndCurrentRule(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.PurchaseEvidenceInput)
		want   error
	}{
		{"unspecified", func(v *tammyv1.PurchaseEvidenceInput) { v.Classification = 0 }, ErrInvalidEvidence},
		{"unknown enum", func(v *tammyv1.PurchaseEvidenceInput) { v.Classification = 99 }, ErrInvalidEvidence},
		{"no source", func(v *tammyv1.PurchaseEvidenceInput) { v.Source = nil }, ErrInvalidEvidence},
		{"bad source id", func(v *tammyv1.PurchaseEvidenceInput) { v.Source.SourceId = "bad" }, ErrInvalidEvidence},
		{"source version zero", func(v *tammyv1.PurchaseEvidenceInput) { v.Source.SourceVersion = 0 }, ErrInvalidEvidence},
		{"source hash short", func(v *tammyv1.PurchaseEvidenceInput) { v.Source.SourceHash = make([]byte, 31) }, ErrInvalidEvidence},
		{"no held date", func(v *tammyv1.PurchaseEvidenceInput) { v.HeldDate = nil }, ErrInvalidEvidence},
		{"bad held calendar", func(v *tammyv1.PurchaseEvidenceInput) { v.HeldDate = &tammyv1.CivilDate{Year: 2026, Month: 4, Day: 31} }, ErrInvalidEvidence},
		{"registration missing", func(v *tammyv1.PurchaseEvidenceInput) { v.RegistrationReviewConfirmed = false }, ErrInvalidEvidence},
		{"business missing", func(v *tammyv1.PurchaseEvidenceInput) { v.BusinessUseReviewConfirmed = false }, ErrInvalidEvidence},
		{"evidence missing", func(v *tammyv1.PurchaseEvidenceInput) { v.EvidenceReviewConfirmed = false }, ErrInvalidEvidence},
		{"reason blank", func(v *tammyv1.PurchaseEvidenceInput) { v.Reason = " " }, ErrInvalidEvidence},
		{"reason oversized", func(v *tammyv1.PurchaseEvidenceInput) { v.Reason = string(make([]byte, 513)) }, ErrInvalidEvidence},
		{"exception absent", func(v *tammyv1.PurchaseEvidenceInput) { v.Classification = 3 }, ErrUnsupportedException},
		{"exception attached to invoice", func(v *tammyv1.PurchaseEvidenceInput) {
			other, _ := purchaseException()
			v.ExceptionReview = other.ExceptionReview
		}, ErrInvalidEvidence},
	} {
		t.Run(test.name, func(t *testing.T) {
			review := purchaseEvidence()
			test.mutate(review)
			if err := ValidatePurchaseEvidence(review, nil); !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.PurchaseEvidenceInput, *ReviewedExceptionRule)
	}{
		{"missing current rule", func(_ *tammyv1.PurchaseEvidenceInput, r *ReviewedExceptionRule) { r.Source = nil }},
		{"unknown kind", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) { v.ExceptionReview.Kind = 99 }},
		{"different rule kind", func(_ *tammyv1.PurchaseEvidenceInput, r *ReviewedExceptionRule) { r.Kind = 2 }},
		{"different rule id", func(_ *tammyv1.PurchaseEvidenceInput, r *ReviewedExceptionRule) { r.Source.SourceId = purchaseDocID }},
		{"stale rule", func(_ *tammyv1.PurchaseEvidenceInput, r *ReviewedExceptionRule) { r.Source.SourceVersion++ }},
		{"different rule hash", func(_ *tammyv1.PurchaseEvidenceInput, r *ReviewedExceptionRule) { r.Source.SourceHash[0] = 1 }},
		{"rule ref absent", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) { v.ExceptionReview.Rule = nil }},
		{"support absent", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) {
			v.ExceptionReview.SupportingRecords = nil
		}},
		{"support too many", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) {
			ref := v.ExceptionReview.SupportingRecords[0]
			v.ExceptionReview.SupportingRecords = make([]*tammyv1.PurchaseRetainedSourceRef, 51)
			for i := range v.ExceptionReview.SupportingRecords {
				v.ExceptionReview.SupportingRecords[i] = ref
			}
		}},
		{"support bad version", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) {
			v.ExceptionReview.SupportingRecords[0].SourceVersion = 0
		}},
		{"support bad hash", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) {
			v.ExceptionReview.SupportingRecords[0].SourceHash = nil
		}},
		{"rule review absent", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) {
			v.ExceptionReview.TaxInvoiceNotRequiredReviewConfirmed = false
		}},
		{"support review absent", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) {
			v.ExceptionReview.SupportingRecordRequirementsReviewConfirmed = false
		}},
		{"reason absent", func(v *tammyv1.PurchaseEvidenceInput, _ *ReviewedExceptionRule) { v.ExceptionReview.Reason = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			review, rule := purchaseException()
			test.mutate(review, rule)
			if err := ValidatePurchaseEvidence(review, rule); !errors.Is(err, ErrUnsupportedException) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, date := range []*tammyv1.CivilDate{nil, {Year: 0, Month: 1, Day: 1}, {Year: 2026, Month: 2, Day: 29}, {Year: 2026, Month: 13, Day: 1}} {
		if _, err := EvidenceReadinessFor(purchaseEvidence(), date, purchaseDate(30), nil); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("invalid event date accepted: %v error=%v", date, err)
		}
		if _, err := EvidenceReadinessFor(purchaseEvidence(), purchaseDate(30), date, nil); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("invalid as-of accepted: %v error=%v", date, err)
		}
	}
	if err := ValidatePurchaseEvidence(nil, nil); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("nil review error=%v", err)
	}
}
