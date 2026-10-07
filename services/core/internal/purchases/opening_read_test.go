package purchases

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestLegacyOpeningReadProjectionExists(t *testing.T) {
	if _, ok := reflect.TypeOf(&OpeningRepository{}).MethodByName("GetOpeningPayable"); !ok {
		t.Fatal("opening payable cannot be read with its retained conversion provenance")
	}
}

func TestLegacyOpeningCanonicalSourceWithoutDocument(t *testing.T) {
	org := "018f0000-0000-7000-8000-000000000001"
	id := "018f0000-0000-7000-8000-000000000002"
	conversion := "018f0000-0000-7000-8000-000000000003"
	account := "018f0000-0000-7000-8000-000000000004"
	date := &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 1}
	input := &tammyv1.OpeningBalanceInput{ClientLineId: id, AccountId: account, Kind: tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM,
		LedgerBalance: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 11000}, OriginalIssueDate: date, OriginalDueDate: date,
		OutstandingGst: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 1000}, PriorGstAttributed: &tammyv1.Money{CurrencyCode: "AUD"}}
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	base := openingPriorSourceRow{id: id, conversionID: conversion, version: 2, conversionDate: "2026-07-01", encoded: content,
		itemID: sql.NullString{String: id, Valid: true}, accountID: sql.NullString{String: account, Valid: true}, kind: sql.NullString{String: "SUPPLIER_OPEN_ITEM", Valid: true},
		currency: sql.NullString{String: "AUD", Valid: true}, debit: sql.NullInt64{Valid: true}, credit: sql.NullInt64{Int64: 11000, Valid: true},
		issueDate: sql.NullString{String: "2026-06-01", Valid: true}, dueDate: sql.NullString{String: "2026-06-01", Valid: true},
		outstandingGST: sql.NullInt64{Int64: 1000, Valid: true}, priorGST: sql.NullInt64{Valid: true}, accountOrganisation: sql.NullString{String: org, Valid: true},
		normalBalance: sql.NullString{String: "CREDIT", Valid: true}, reportClassification: sql.NullString{String: "balance_sheet.payables", Valid: true}}
	retained, err := base.verify(org)
	if err != nil || !proto.Equal(retained, input) {
		t.Fatalf("retained=%v err=%v", retained, err)
	}
	digest := sha256.Sum256(content)
	projection := &OpeningPayable{ID: id, OrganisationID: org, ConversionID: conversion, SourceVersion: 2, SourceHash: digest[:], Input: retained}
	source := projection.LegacySource()
	if source.GetDocument() != nil || source.GetOpeningPayable().SourceVersion != 2 || !bytes.Equal(source.GetOpeningPayable().SourceHash, digest[:]) {
		t.Fatal("opening source lost independent identity")
	}
	source.GetOpeningPayable().SourceHash[0] ^= 255
	if bytes.Equal(source.GetOpeningPayable().SourceHash, projection.SourceHash) {
		t.Fatal("source projection shares mutable hash bytes")
	}
	for _, mutate := range []func(*openingPriorSourceRow){
		func(r *openingPriorSourceRow) { r.accountOrganisation.String = conversion },
		func(r *openingPriorSourceRow) { r.sourceID = sql.NullString{String: id, Valid: true} },
		func(r *openingPriorSourceRow) { r.outstandingGST.Int64++ },
		func(r *openingPriorSourceRow) { r.credit.Int64++ },
		func(r *openingPriorSourceRow) { r.version = 0 },
	} {
		value := base
		mutate(&value)
		if _, err := value.verify(org); err == nil {
			t.Fatal("contradictory retained opening facts accepted")
		}
	}
}

func pureLegacyReview() *tammyv1.LegacyPurchaseReviewInput {
	date := func(day int32) *tammyv1.CivilDate { return &tammyv1.CivilDate{Year: 2026, Month: 8, Day: day} }
	return &tammyv1.LegacyPurchaseReviewInput{Source: &tammyv1.LegacyPurchaseSourceRef{Source: &tammyv1.LegacyPurchaseSourceRef_Document{Document: &tammyv1.LegacyPurchaseDocumentRef{DocumentId: "018f0000-0000-7000-8000-000000000001", ReviewedVersion: 2, OriginalContentHash: make([]byte, 32), ReviewedSnapshotHash: make([]byte, 32)}}},
		AffectedPeriod: &tammyv1.PurchaseClaimPeriod{PeriodStart: date(1), PeriodEnd: date(31)}, ReportingBalanceDate: date(31), Disposition: tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE,
		SupportingEvidence: []*tammyv1.PurchaseRetainedSourceRef{{SourceId: "018f0000-0000-7000-8000-000000000002", SourceVersion: 1, SourceHash: make([]byte, 32)}}, Reason: "Reviewed statement supporting unpaid through balance date"}
}

func TestLegacyReviewScopeDoesNotWaiveOtherPeriodsOrChangedFacts(t *testing.T) {
	input := pureLegacyReview()
	if err := ValidateLegacyReview(input); err != nil {
		t.Fatal(err)
	}
	record := &tammyv1.LegacyPurchaseReview{Review: input}
	if !LegacyReviewApplies(record, input.Source, input.AffectedPeriod, input.ReportingBalanceDate) {
		t.Fatal("matching retained scope not applied")
	}
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.LegacyPurchaseReviewInput)
	}{
		{"different period", func(v *tammyv1.LegacyPurchaseReviewInput) {
			v.AffectedPeriod.PeriodStart.Month = 9
			v.AffectedPeriod.PeriodEnd.Month = 9
			v.AffectedPeriod.PeriodEnd.Day = 30
		}},
		{"later balance", func(v *tammyv1.LegacyPurchaseReviewInput) {
			v.ReportingBalanceDate.Month = 9
			v.ReportingBalanceDate.Day = 30
		}},
		{"new source version", func(v *tammyv1.LegacyPurchaseReviewInput) { v.Source.GetDocument().ReviewedVersion++ }},
		{"new reviewed source", func(v *tammyv1.LegacyPurchaseReviewInput) { v.Source.GetDocument().ReviewedSnapshotHash[0] = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := proto.Clone(input).(*tammyv1.LegacyPurchaseReviewInput)
			test.mutate(current)
			if LegacyReviewApplies(record, current.Source, current.AffectedPeriod, current.ReportingBalanceDate) {
				t.Fatal("review excluded facts beyond its retained scope")
			}
		})
	}
}

func TestLegacyReviewRequiresApplicableRetainedFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.LegacyPurchaseReviewInput)
	}{
		{"no evidence", func(v *tammyv1.LegacyPurchaseReviewInput) { v.SupportingEvidence = nil }},
		{"no balance date", func(v *tammyv1.LegacyPurchaseReviewInput) { v.ReportingBalanceDate = nil }},
		{"claimed lacks period", func(v *tammyv1.LegacyPurchaseReviewInput) {
			v.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED
			v.ClaimEvidence = v.SupportingEvidence[0]
		}},
		{"unsupported lacks payment", func(v *tammyv1.LegacyPurchaseReviewInput) {
			v.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT
		}},
		{"unpaid carries unrelated claim", func(v *tammyv1.LegacyPurchaseReviewInput) { v.PreviouslyClaimedPeriod = v.AffectedPeriod }},
		{"evidence only lacks purchase", func(v *tammyv1.LegacyPurchaseReviewInput) {
			v.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE
		}},
		{"duplicate supports", func(v *tammyv1.LegacyPurchaseReviewInput) {
			v.SupportingEvidence = append(v.SupportingEvidence, proto.Clone(v.SupportingEvidence[0]).(*tammyv1.PurchaseRetainedSourceRef))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := pureLegacyReview()
			test.mutate(value)
			if ValidateLegacyReview(value) == nil {
				t.Fatal("unsupported legacy facts accepted")
			}
		})
	}
}

func TestLegacyDecisionConsistencyRequiresSameOwnedIdentityAndClaimPeriod(t *testing.T) {
	first := pureLegacyReview()
	first.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE
	first.LinkedPostingId = proto.String("018f0000-0000-7000-8000-000000000003")
	confirmed := proto.Clone(first).(*tammyv1.LegacyPurchaseReviewInput)
	confirmed.Reason = "Another statement confirms the same source identity"
	if !consistentLegacyDecision(first, confirmed) {
		t.Fatal("corroborating evidence changed transaction identity")
	}
	confirmed.LinkedPostingId = proto.String("018f0000-0000-7000-8000-000000000004")
	if consistentLegacyDecision(first, confirmed) {
		t.Fatal("same evidence linked to different owned purchases was consistent")
	}
	confirmed = proto.Clone(first).(*tammyv1.LegacyPurchaseReviewInput)
	confirmed.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT
	if consistentLegacyDecision(first, confirmed) {
		t.Fatal("unsupported settlement agreed with evidence-only transaction identity")
	}
	first.Disposition = tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_ALREADY_CLAIMED
	first.LinkedPostingId = nil
	first.PreviouslyClaimedPeriod = first.AffectedPeriod
	confirmed = proto.Clone(first).(*tammyv1.LegacyPurchaseReviewInput)
	confirmed.PreviouslyClaimedPeriod.PeriodStart.Day++
	if consistentLegacyDecision(first, confirmed) {
		t.Fatal("different claimed periods were consistent")
	}
}
