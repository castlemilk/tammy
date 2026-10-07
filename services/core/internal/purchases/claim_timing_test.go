package purchases

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func claimDate(t *testing.T, value string) *tammyv1.CivilDate {
	t.Helper()
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatal(err)
	}
	return &tammyv1.CivilDate{Year: int32(date.Year()), Month: int32(date.Month()), Day: int32(date.Day())}
}

func claimRef(id string) *tammyv1.PurchaseRetainedSourceRef {
	return &tammyv1.PurchaseRetainedSourceRef{SourceId: id, SourceVersion: 1, SourceHash: make([]byte, 32)}
}

func claimInput(t *testing.T) ClaimTimingInput {
	t.Helper()
	invoice := claimRef("018f0000-0000-7000-8000-000000000101")
	payment := claimRef("018f0000-0000-7000-8000-000000000102")
	return ClaimTimingInput{
		Use: ClaimTimingAutomatic, Basis: tammyv1.GstBasis_GST_BASIS_CASH,
		InvoiceSource: invoice, InvoiceDate: claimDate(t, "2026-06-01"),
		PaymentSource: payment, PaymentDate: claimDate(t, "2026-06-15"),
		Evidence: &tammyv1.PurchaseEvidenceInput{Source: proto.Clone(invoice).(*tammyv1.PurchaseRetainedSourceRef),
			Classification: tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE,
			HeldDate:       claimDate(t, "2026-06-01"), RegistrationReviewConfirmed: true, BusinessUseReviewConfirmed: true,
			EvidenceReviewConfirmed: true, Reason: "Retained invoice and reviewed entitlement"},
		ClaimPeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: claimDate(t, "2026-04-01"), PeriodEnd: claimDate(t, "2026-06-30")},
		ReleaseDate: claimDate(t, "2026-06-15"), ReviewedAsOf: claimDate(t, "2026-06-15"),
	}
}

func claimRequiredReturn(t *testing.T, input ClaimTimingInput, due string) *ReviewedRequiredReturn {
	t.Helper()
	eventSource, eventDate := input.InvoiceSource, input.InvoiceDate
	if input.Basis == tammyv1.GstBasis_GST_BASIS_CASH {
		eventSource, eventDate = input.PaymentSource, input.PaymentDate
	}
	return &ReviewedRequiredReturn{EventSource: proto.Clone(eventSource).(*tammyv1.PurchaseRetainedSourceRef),
		NormalEventDate:                 proto.Clone(eventDate).(*tammyv1.CivilDate),
		EarliestOtherwiseEligiblePeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: proto.Clone(eventDate).(*tammyv1.CivilDate), PeriodEnd: proto.Clone(eventDate).(*tammyv1.CivilDate)},
		DueDate:                         claimDate(t, due), Evidence: claimRef("018f0000-0000-7000-8000-000000000103")}
}

func TestClaimTimingEvidenceReadinessKeepsSeparateLinkedFacts(t *testing.T) {
	for _, test := range []struct {
		name, held     string
		classification tammyv1.PurchaseEvidenceClassification
		want           ClaimTimingStatus
	}{
		{"on time", "2026-06-01", 2, ClaimTimingNormalSupported},
		{"held at payment", "2026-06-15", 2, ClaimTimingNormalSupported},
		{"held after payment", "2026-06-16", 2, ClaimTimingReviewRequired},
		{"future held", "2026-07-01", 2, ClaimTimingFutureEvidence},
		{"missing", "", 1, ClaimTimingMissingEvidence},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := claimInput(t)
			input.ReviewedAsOf = claimDate(t, "2026-06-20")
			input.Evidence.Classification = test.classification
			if test.held == "" {
				input.Evidence.Source, input.Evidence.HeldDate = nil, nil
			} else {
				input.Evidence.HeldDate = claimDate(t, test.held)
			}
			result, err := EvaluateClaimTiming(input)
			if err != nil || result.Status != test.want {
				t.Fatalf("status=%s want=%s error=%v", result.Status, test.want, err)
			}
			if !proto.Equal(result.EventDate, input.PaymentDate) || !proto.Equal(result.InvoiceDate, input.InvoiceDate) ||
				!proto.Equal(result.PaymentDate, input.PaymentDate) || !proto.Equal(result.HeldDate, input.Evidence.HeldDate) ||
				!proto.Equal(result.ReleaseDate, input.ReleaseDate) || !proto.Equal(result.ClaimPeriod, input.ClaimPeriod) ||
				!proto.Equal(result.ReviewedAsOf, input.ReviewedAsOf) || !proto.Equal(result.EventSource, input.PaymentSource) {
				t.Fatal("linked dates or normal payment event were lost or replaced")
			}
			if result.OrdinaryLastClaimDate != nil {
				t.Fatal("ordinary posting invented a due-date deadline")
			}
		})
	}
	input := claimInput(t)
	input.Basis = tammyv1.GstBasis_GST_BASIS_NON_CASH
	result, err := EvaluateClaimTiming(input)
	if err != nil || !proto.Equal(result.EventDate, input.InvoiceDate) || !proto.Equal(result.EventSource, input.InvoiceSource) {
		t.Fatalf("non-cash event must remain invoice: %v error=%v", result, err)
	}
}

func TestClaimTimingRequiresActualDueDateForDelayedAndDeferredPortions(t *testing.T) {
	for _, use := range []ClaimTimingUse{ClaimTimingDelayed, ClaimTimingDeferral} {
		input := claimInput(t)
		input.Use = use
		result, err := EvaluateClaimTiming(input)
		if err != nil || result.Status != ClaimTimingUndeterminableLimit || result.OrdinaryLastClaimDate != nil {
			t.Fatalf("use=%v must not guess nominal-quarter due date: %v error=%v", use, result, err)
		}
		input.RequiredReturn = claimRequiredReturn(t, input, "2026-08-17")
		result, err = EvaluateClaimTiming(input)
		if err != nil || result.Status != ClaimTimingNormalSupported || !proto.Equal(result.OrdinaryLastClaimDate, claimDate(t, "2030-08-17")) {
			t.Fatalf("actual reviewed extension ignored: %v error=%v", result, err)
		}
		input.RequiredReturn.Evidence = nil
		result, err = EvaluateClaimTiming(input)
		if err != nil || result.Status != ClaimTimingUndeterminableLimit {
			t.Fatalf("unproven due date accepted: %v error=%v", result, err)
		}
	}
	input := claimInput(t)
	input.Use = ClaimTimingDelayed
	input.Evidence.HeldDate = claimDate(t, "2026-06-16")
	input.ReviewedAsOf = claimDate(t, "2026-06-20")
	input.RequiredReturn = claimRequiredReturn(t, input, "2026-07-28")
	result, err := EvaluateClaimTiming(input)
	if err != nil || result.Status != ClaimTimingReviewRequired || !proto.Equal(result.OrdinaryLastClaimDate, claimDate(t, "2030-07-28")) {
		t.Fatalf("deadline does not replace explicit late-evidence review: %v error=%v", result, err)
	}
}

func TestClaimTimingGoldenCivilAnniversariesAndFinalDays(t *testing.T) {
	var fixture struct {
		RuleVersion string `json:"rule_version"`
		AccessDate  string `json:"access_date"`
		Cases       []struct {
			Name       string `json:"name"`
			Due        string `json:"required_return_due_date"`
			Last       string `json:"last_claim_date"`
			Outside    string `json:"first_outside_date"`
			Amendment  string `json:"amendment_review_last_date"`
			URL        string `json:"source_url"`
			Derivation string `json:"derivation"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("../../../../test/fixtures/purchases/claim-timing.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.RuleVersion != OrdinaryClaimLimitRuleVersion || fixture.AccessDate == "" || len(fixture.Cases) != 6 {
		t.Fatal("incomplete pinned golden fixture")
	}
	for _, golden := range fixture.Cases {
		t.Run(golden.Name, func(t *testing.T) {
			if golden.URL == "" || golden.Derivation == "" {
				t.Fatal("golden case lacks primary provenance/inference")
			}
			input := claimInput(t)
			input.Use = ClaimTimingDelayed
			due := claimDate(t, golden.Due)
			event := &tammyv1.CivilDate{Year: due.Year, Month: 1, Day: 1}
			input.InvoiceDate, input.PaymentDate, input.Evidence.HeldDate = event, proto.Clone(event).(*tammyv1.CivilDate), proto.Clone(event).(*tammyv1.CivilDate)
			input.RequiredReturn = claimRequiredReturn(t, input, golden.Due)
			if golden.Last == "" {
				input.ClaimPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: event, PeriodEnd: due}
				input.ReleaseDate, input.ReviewedAsOf = due, proto.Clone(due).(*tammyv1.CivilDate)
				result, err := EvaluateClaimTiming(input)
				if err != nil || result.Status != ClaimTimingUndeterminableLimit || result.OrdinaryLastClaimDate != nil {
					t.Fatalf("unsupported anniversary normalized: %v error=%v", result, err)
				}
				return
			}
			for _, day := range []struct {
				value string
				want  ClaimTimingStatus
			}{{golden.Last, ClaimTimingNormalSupported}, {golden.Outside, ClaimTimingExpired}} {
				claim := claimDate(t, day.value)
				input.ClaimPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: claim, PeriodEnd: proto.Clone(claim).(*tammyv1.CivilDate)}
				input.ReleaseDate, input.ReviewedAsOf = claim, proto.Clone(claim).(*tammyv1.CivilDate)
				result, err := EvaluateClaimTiming(input)
				if err != nil || result.Status != day.want || !proto.Equal(result.OrdinaryLastClaimDate, claimDate(t, golden.Last)) {
					t.Fatalf("day %s status=%v want=%s error=%v", day.value, result, day.want, err)
				}
			}
			if golden.Amendment != "" && golden.Amendment != golden.Outside {
				t.Fatal("amendment review boundary was merged into credit limit")
			}
		})
	}
}

func TestClaimTimingValidatesChronologyAndVerifiedPortionBinding(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ClaimTimingInput)
	}{
		{"impossible invoice", func(v *ClaimTimingInput) { v.InvoiceDate = &tammyv1.CivilDate{Year: 2026, Month: 2, Day: 29} }},
		{"invoice beyond calendar", func(v *ClaimTimingInput) { v.InvoiceDate.Year = 10000 }},
		{"payment before invoice", func(v *ClaimTimingInput) { v.PaymentDate = claimDate(t, "2026-05-31") }},
		{"missing cash payment", func(v *ClaimTimingInput) { v.PaymentDate = nil }},
		{"reversed period", func(v *ClaimTimingInput) { v.ClaimPeriod.PeriodStart = claimDate(t, "2026-07-01") }},
		{"period before event", func(v *ClaimTimingInput) { v.ClaimPeriod.PeriodEnd = claimDate(t, "2026-06-14") }},
		{"release outside period", func(v *ClaimTimingInput) { v.ReleaseDate = claimDate(t, "2026-07-01") }},
		{"release before event", func(v *ClaimTimingInput) { v.ReleaseDate = claimDate(t, "2026-06-14") }},
		{"missing trusted asof", func(v *ClaimTimingInput) { v.ReviewedAsOf = nil }},
		{"event after review", func(v *ClaimTimingInput) { v.ReviewedAsOf = claimDate(t, "2026-06-14") }},
		{"unknown basis", func(v *ClaimTimingInput) { v.Basis = 99 }},
		{"unknown use", func(v *ClaimTimingInput) { v.Use = 99 }},
		{"missing event source", func(v *ClaimTimingInput) { v.PaymentSource = nil }},
		{"due-date event id mismatch", func(v *ClaimTimingInput) { v.RequiredReturn.EventSource.SourceId = v.InvoiceSource.SourceId }},
		{"due-date event version mismatch", func(v *ClaimTimingInput) { v.RequiredReturn.EventSource.SourceVersion++ }},
		{"due-date event hash mismatch", func(v *ClaimTimingInput) { v.RequiredReturn.EventSource.SourceHash[0] = 1 }},
		{"due-date normal event mismatch", func(v *ClaimTimingInput) { v.RequiredReturn.NormalEventDate = claimDate(t, "2026-06-16") }},
		{"earliest period excludes event", func(v *ClaimTimingInput) {
			v.RequiredReturn.EarliestOtherwiseEligiblePeriod.PeriodStart = claimDate(t, "2026-06-16")
		}},
		{"due date before eligible period", func(v *ClaimTimingInput) { v.RequiredReturn.DueDate = claimDate(t, "2026-06-14") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := claimInput(t)
			input.Use = ClaimTimingDelayed
			input.RequiredReturn = claimRequiredReturn(t, input, "2026-07-28")
			test.mutate(&input)
			if _, err := EvaluateClaimTiming(input); !errors.Is(err, ErrInvalidClaimTiming) {
				t.Fatalf("invalid linked facts accepted: %v", err)
			}
		})
	}
}

func TestClaimTimingCashPortionsHaveIndependentDeadlinesAndImmutableResults(t *testing.T) {
	first := claimInput(t)
	first.Use = ClaimTimingDelayed
	first.RequiredReturn = claimRequiredReturn(t, first, "2026-07-28")
	second := claimInput(t)
	second.Use = ClaimTimingDelayed
	second.PaymentDate = claimDate(t, "2026-07-01")
	second.PaymentSource.SourceId = "018f0000-0000-7000-8000-000000000104"
	second.ClaimPeriod = &tammyv1.PurchaseClaimPeriod{PeriodStart: claimDate(t, "2026-07-01"), PeriodEnd: claimDate(t, "2026-09-30")}
	second.ReleaseDate = claimDate(t, "2026-07-01")
	second.ReviewedAsOf = claimDate(t, "2026-07-01")
	second.RequiredReturn = claimRequiredReturn(t, second, "2026-10-28")
	a, err := EvaluateClaimTiming(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EvaluateClaimTiming(second)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(a.OrdinaryLastClaimDate, claimDate(t, "2030-07-28")) || !proto.Equal(b.OrdinaryLastClaimDate, claimDate(t, "2030-10-28")) || proto.Equal(a.EventSource, b.EventSource) {
		t.Fatal("cash portions reused invoice deadline or source")
	}
	first.InvoiceDate.Day = 2
	first.PaymentDate.Day = 16
	first.Evidence.HeldDate.Day = 3
	first.RequiredReturn.DueDate.Day = 29
	first.RequiredReturn.Evidence.SourceHash[0] = 1
	first.PaymentSource.SourceHash[0] = 1
	first.ClaimPeriod.PeriodEnd.Day = 29
	if a.InvoiceDate.Day != 1 || a.EventDate.Day != 15 || a.HeldDate.Day != 1 || a.RequiredReturn.DueDate.Day != 28 || a.RequiredReturn.Evidence.SourceHash[0] != 0 || a.EventSource.SourceHash[0] != 0 || a.ClaimPeriod.PeriodEnd.Day != 30 {
		t.Fatal("result aliases mutable caller facts")
	}
}

func TestClaimTimingUnsupportedExceptionAndUnverifiedElectionCannotClear(t *testing.T) {
	input := claimInput(t)
	input.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION
	ruleRef := claimRef("018f0000-0000-7000-8000-000000000105")
	input.Evidence.ExceptionReview = &tammyv1.PurchaseTaxInvoiceExceptionReview{Kind: tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION, Rule: ruleRef, SupportingRecords: []*tammyv1.PurchaseRetainedSourceRef{input.InvoiceSource}, TaxInvoiceNotRequiredReviewConfirmed: true, SupportingRecordRequirementsReviewConfirmed: true, Reason: "Verified retained waiver facts"}
	result, err := EvaluateClaimTiming(input)
	if err != nil || result.Status != ClaimTimingUnsupportedException {
		t.Fatalf("request rule refs conferred authority: %v error=%v", result, err)
	}
	input.ExceptionRule = &ReviewedExceptionRule{Kind: input.Evidence.ExceptionReview.Kind, Source: proto.Clone(ruleRef).(*tammyv1.PurchaseRetainedSourceRef)}
	result, err = EvaluateClaimTiming(input)
	if err != nil || result.Status != ClaimTimingNormalSupported {
		t.Fatalf("verified exception rejected: %v error=%v", result, err)
	}
	input.LaterPeriodElection = true
	result, err = EvaluateClaimTiming(input)
	if err != nil || result.Status != ClaimTimingUnsupportedException {
		t.Fatalf("unverified approved-form election accepted: %v error=%v", result, err)
	}
}

func TestClaimTimingDeferralDoesNotInventReleaseJournalDate(t *testing.T) {
	input := claimInput(t)
	input.Use = ClaimTimingDeferral
	input.ReleaseDate = nil
	input.RequiredReturn = claimRequiredReturn(t, input, "2026-07-28")
	result, err := EvaluateClaimTiming(input)
	if err != nil || result.Status != ClaimTimingNormalSupported || result.ReleaseDate != nil {
		t.Fatalf("deferral requires or invents a release journal date: %v error=%v", result, err)
	}
}

func TestClaimTimingRetainsDifferentEvidenceProvenanceWithIdenticalDates(t *testing.T) {
	input := claimInput(t)
	baseline, err := EvaluateClaimTiming(input)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Evidence == nil || !proto.Equal(baseline.Evidence, input.Evidence) {
		t.Fatal("timing result dropped retained evidence snapshot")
	}
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.PurchaseEvidenceInput)
	}{
		{"different source", func(v *tammyv1.PurchaseEvidenceInput) { v.Source.SourceId = "018f0000-0000-7000-8000-000000000106" }},
		{"different source version", func(v *tammyv1.PurchaseEvidenceInput) { v.Source.SourceVersion++ }},
		{"different source hash", func(v *tammyv1.PurchaseEvidenceInput) { v.Source.SourceHash[0] = 1 }},
		{"different retained reason", func(v *tammyv1.PurchaseEvidenceInput) { v.Reason = "Separately reviewed retained invoice" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := claimInput(t)
			test.mutate(changed.Evidence)
			result, err := EvaluateClaimTiming(changed)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != baseline.Status || !proto.Equal(result.HeldDate, baseline.HeldDate) ||
				!proto.Equal(result.EventSource, baseline.EventSource) || !proto.Equal(result.EventDate, baseline.EventDate) {
				t.Fatal("fixture changed timing facts instead of evidence provenance")
			}
			if result.Evidence == nil || !proto.Equal(result.Evidence, changed.Evidence) || proto.Equal(result.Evidence, baseline.Evidence) {
				t.Fatal("distinct retained evidence produced identical linked result facts")
			}
		})
	}
}

func TestClaimTimingEvidenceSnapshotIsolatesNestedExceptionAndSupportingFacts(t *testing.T) {
	input := claimInput(t)
	input.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_SUPPORTED_EXCEPTION
	input.Evidence.ExceptionReview = &tammyv1.PurchaseTaxInvoiceExceptionReview{
		Kind:                                 tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_LOW_VALUE_ACQUISITION,
		Rule:                                 claimRef("018f0000-0000-7000-8000-000000000105"),
		SupportingRecords:                    []*tammyv1.PurchaseRetainedSourceRef{claimRef("018f0000-0000-7000-8000-000000000106")},
		TaxInvoiceNotRequiredReviewConfirmed: true, SupportingRecordRequirementsReviewConfirmed: true,
		Reason: "Reviewed exception and retained supporting records",
	}
	expected := proto.Clone(input.Evidence).(*tammyv1.PurchaseEvidenceInput)
	result, err := EvaluateClaimTiming(input)
	if err != nil || result.Status != ClaimTimingUnsupportedException || result.Evidence == nil || !proto.Equal(result.Evidence, expected) {
		t.Fatalf("unsupported exception lost retained review facts: %v error=%v", result, err)
	}
	input.Evidence.Source.SourceId = "018f0000-0000-7000-8000-000000000107"
	input.Evidence.Source.SourceVersion++
	input.Evidence.Source.SourceHash[0] = 1
	input.Evidence.HeldDate.Day++
	input.Evidence.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_UNKNOWN
	input.Evidence.RegistrationReviewConfirmed = false
	input.Evidence.BusinessUseReviewConfirmed = false
	input.Evidence.EvidenceReviewConfirmed = false
	input.Evidence.Reason = "Changed input reason"
	exception := input.Evidence.ExceptionReview
	exception.Kind = tammyv1.PurchaseTaxInvoiceExceptionKind_PURCHASE_TAX_INVOICE_EXCEPTION_KIND_SPECIFIC_RETAINED_WAIVER
	exception.Rule.SourceVersion++
	exception.Rule.SourceHash[0] = 1
	exception.SupportingRecords[0].SourceId = "018f0000-0000-7000-8000-000000000108"
	exception.SupportingRecords[0].SourceVersion++
	exception.SupportingRecords[0].SourceHash[0] = 1
	exception.TaxInvoiceNotRequiredReviewConfirmed = false
	exception.SupportingRecordRequirementsReviewConfirmed = false
	exception.Reason = "Changed exception reason"
	if !proto.Equal(result.Evidence, expected) || !proto.Equal(result.HeldDate, expected.HeldDate) {
		t.Fatal("returned evidence aliases mutable retained evidence or nested exception facts")
	}
	inputAfterMutation := proto.Clone(input.Evidence).(*tammyv1.PurchaseEvidenceInput)
	result.Evidence.HeldDate.Day++
	result.Evidence.Source.SourceHash[0] = 2
	result.Evidence.ExceptionReview.Rule.SourceHash[0] = 2
	result.Evidence.ExceptionReview.SupportingRecords[0].SourceHash[0] = 2
	if !proto.Equal(input.Evidence, inputAfterMutation) || !proto.Equal(result.HeldDate, expected.HeldDate) {
		t.Fatal("mutating result evidence changed input or independent held-date fact")
	}
}
