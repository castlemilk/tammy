//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

func TestBuildBASSourceCoverageNativeReconcilesEveryFrozenIndependentUnit(t *testing.T) {
	_, tx, source, ports, _, probe := newBASSourceProducerNativeFixtureWithBasis(t, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	beforeChanges := querySourceScalarInt(t, tx, "SELECT total_changes()")
	beforeRevisions := sourceScalarRevisionState(t, tx)
	captured, err := CaptureBASFinancialSourceRecords(context.Background(), BASFinancialSourceRequest{
		Source: source, GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH,
		AsOf: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}, ports)
	if err != nil || captured == nil || captured.Financial == nil {
		t.Fatalf("financial source capture=%#v err=%v", captured, err)
	}
	got, err := BuildBASSourceCoverage(context.Background(), captured)
	if err != nil || got == nil {
		t.Fatalf("source coverage=%#v err=%v", got, err)
	}
	wantCount := len(captured.InvoiceCandidates) + len(captured.ModernPurchaseCandidates.Candidates) +
		len(captured.LegacyPurchaseCandidates.Candidates) + len(captured.Accounting.Facts)
	if len(got.Coverage) != wantCount || len(got.ManifestHash) != 32 || got.CoveragePayloadBytes <= 0 || len(got.FindingTemplates) > 200 {
		t.Fatalf("coverage count/hash/findings=(%d,%d,%d), want %d rows and bounded proofs", len(got.Coverage),
			len(got.ManifestHash), len(got.FindingTemplates), wantCount)
	}
	if hash, payloadBytes, err := basCoverageManifest(context.Background(), got.Coverage); err != nil ||
		!bytes.Equal(hash, got.ManifestHash) || payloadBytes != got.CoveragePayloadBytes {
		t.Fatalf("coverage manifest did not reconcile: hash=%x bytes=%d err=%v", hash, payloadBytes, err)
	}
	index := 0
	for _, unit := range captured.Financial.Invoices.Units {
		coverage := got.Coverage[index]
		index++
		var table, id string
		var sequence *uint64
		var relatedID *string
		switch unit.Key.Kind {
		case invoices.BASCandidateInvoiceRoot:
			table, id = "invoices_v1", unit.Key.InvoiceID
		case invoices.BASCandidateIncomeLine:
			table, id = "invoice_income_lines_v1", unit.Key.InvoiceID
			sequence = &unit.Key.Sequence
		case invoices.BASCandidateReceipt:
			table, id = "invoice_payment_v1", unit.Key.ReceiptID
			relatedID = &unit.Key.InvoiceID
		case invoices.BASCandidateReceiptAllocation:
			table, id = "invoice_payment_lines_v1", unit.Key.ReceiptID
			sequence, relatedID = &unit.Key.Sequence, &unit.Key.InvoiceID
		default:
			t.Fatalf("unknown frozen invoice kind %v", unit.Key.Kind)
		}
		assertBASCoverageRawTuple(t, coverage.RawSource, table, id, sequence, relatedID, nil)
	}
	for _, key := range captured.ModernPurchaseCandidates.Candidates {
		coverage := got.Coverage[index]
		index++
		var table, id string
		var sequence, relatedSequence *uint64
		var relatedID *string
		switch key.Kind {
		case purchases.BASModernCandidatePosting:
			table, id = "purchase_postings_v1", key.PostingID
		case purchases.BASModernCandidatePostingAllocation:
			table, id, sequence = "purchase_posting_allocations_v1", key.PostingID, &key.Sequence
		case purchases.BASModernCandidatePayment:
			table, id, relatedID = "purchase_payments_v1", key.PaymentID, &key.PostingID
		case purchases.BASModernCandidatePaymentAllocation:
			table, id, sequence = "purchase_payment_allocations_v1", key.PaymentID, &key.Sequence
			relatedID, relatedSequence = &key.PostingID, &key.PostingAllocationSequence
		default:
			t.Fatalf("unknown frozen purchase kind %v", key.Kind)
		}
		assertBASCoverageRawTuple(t, coverage.RawSource, table, id, sequence, relatedID, relatedSequence)
	}
	candidateSet := make(map[purchases.BASLegacyCandidateKey]struct{}, len(captured.LegacyPurchaseCandidates.Candidates))
	for _, key := range captured.LegacyPurchaseCandidates.Candidates {
		candidateSet[key] = struct{}{}
	}
	for _, nomination := range captured.LegacyPurchaseCandidates.Nominations {
		if _, candidate := candidateSet[nomination.Key]; !candidate {
			continue
		}
		unit := got.Coverage[index]
		index++
		if nomination.Key.Kind == purchases.BASLegacyCandidateOpeningPayable {
			assertBASCoverageRawTuple(t, unit.RawSource, "purchase_opening_payables", nomination.Key.SourceID,
				nil, &nomination.Key.ConversionID, nil)
			if unit.RawSource.OriginalRevision == nil || *unit.RawSource.OriginalRevision != int64(nomination.SourceVersion) ||
				len(unit.RawSource.OriginalContentHash) != 32 {
				t.Fatal("opening payable raw identity lost its exact source revision/hash")
			}
			var sourceHash []byte
			for _, source := range captured.Purchases.Capture.LegacySources {
				if source != nil && source.Opening != nil && source.Opening.ID == nomination.Key.SourceID && source.Opening.ConversionID == nomination.Key.ConversionID {
					sourceHash = source.Opening.SourceHash
				}
			}
			if !bytes.Equal(unit.RawSource.OriginalContentHash, sourceHash) {
				t.Fatal("opening identity hash differs from the retained opening source hash")
			}
		} else {
			assertBASCoverageRawTuple(t, unit.RawSource, "documents", nomination.Key.SourceID, nil, nil, nil)
			if unit.RawSource.OriginalRevision == nil || *unit.RawSource.OriginalRevision != int64(nomination.SourceVersion) ||
				!bytes.Equal(unit.RawSource.OriginalContentHash, nomination.DocumentRawSHA256) {
				t.Fatal("legacy document raw identity lost its nomination revision/hash")
			}
		}
	}
	for indexFact, fact := range captured.Accounting.Facts {
		coverage := got.Coverage[index+indexFact]
		assertBASCoverageRawTuple(t, coverage.RawSource, "tax_facts", fact.ID, nil, nil, nil)
		if coverage.CandidateKind != tammyv1.BasCoverageCandidateKind_BAS_COVERAGE_CANDIDATE_KIND_MANUAL_TAX_FACT ||
			coverage.Disposition != tammyv1.BasCoverageDisposition_BAS_COVERAGE_DISPOSITION_UNSUPPORTED ||
			coverage.EligibleAmount != nil || coverage.TaxDate != nil ||
			coverage.ClassificationCode != "MANUAL_TAX_FACT_UNSUPPORTED" || len(coverage.EvidenceIdentities) != 1 ||
			coverage.EvidenceIdentities[0].RawSource == nil || coverage.EvidenceIdentities[0].RawSource.Type != "journals" ||
			coverage.EvidenceIdentities[0].RawSource.Id != fact.Journal.Id {
			t.Fatalf("accounting fact %q was inferred, dropped, or detached: %#v", fact.ID, coverage)
		}
	}
	if index+len(captured.Accounting.Facts) != len(got.Coverage) || probe.writes != 0 {
		t.Fatalf("coverage traversal ended at %d/%d rows or capture wrote %d times", index+len(captured.Accounting.Facts), len(got.Coverage), probe.writes)
	}
	assertSourceScalarTransactionUnchanged(t, tx, beforeChanges, beforeRevisions)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := BuildBASSourceCoverage(cancelled, captured); partial != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pure mapping returned partial=%#v err=%v", partial, err)
	}
}

func TestBindBASSourceSnapshotNativeSeparatesAggregateFinancialRevision(t *testing.T) {
	_, tx, source, ports, _, _ := newBASSourceProducerNativeFixtureWithBasis(t, true, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	ctx := context.Background()
	profileRepository := newProfileRepositoryForTest(t, tx, newBASRepositoryCursorCodec(t))
	retainedProfile := validBASProfile(source.OrganisationID, testUUID(9950), 1)
	retainedProfile.ActorUserId = testUUID(9962)
	profileRef := appendBASProfile(t, profileRepository, retainedProfile, nil)
	periodRepository, err := NewPeriodRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	retainedPeriod := proto.Clone(source.ConfirmedPeriod).(*tammyv1.BasConfirmedPeriod)
	retainedPeriod.ContentHash = nil
	retainedPeriod.ActorUserId = testUUID(9962)
	retainedPeriod.Confirmation.Profile = profileRef
	periodRef, err := periodRepository.Append(ctx, retainedPeriod, nil)
	if err != nil {
		t.Fatal(err)
	}
	retainedPeriod.ContentHash = bytes.Clone(periodRef.ContentHash)
	source.ConfirmedPeriod = retainedPeriod
	captured, err := CaptureBASFinancialSourceRecords(ctx, BASFinancialSourceRequest{
		Source: source, GstBasis: tammyv1.GstBasis_GST_BASIS_NON_CASH,
		AsOf: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}, ports)
	if err != nil || captured == nil {
		t.Fatalf("financial source capture=%#v err=%v", captured, err)
	}
	coverage, err := BuildBASSourceCoverage(ctx, captured)
	if err != nil || coverage == nil {
		t.Fatalf("source coverage=%#v err=%v", coverage, err)
	}
	organisationRepo, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	organisation, err := organisationRepo.GetOrganisation(ctx, source.OrganisationID)
	if err != nil || organisation == nil {
		t.Fatalf("organisation snapshot=%#v err=%v", organisation, err)
	}
	profiles, err := NewProfileRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := profiles.GetExact(ctx, source.OrganisationID, source.ConfirmedPeriod.Confirmation.Profile)
	if err != nil || profile == nil {
		t.Fatalf("profile snapshot=%#v err=%v", profile, err)
	}
	periods, err := NewPeriodRepository(tx, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	period, err := periods.GetExact(ctx, source.OrganisationID, &tammyv1.SourceRef{
		Type: "bas_period", Id: source.ConfirmedPeriod.PeriodId, Revision: source.ConfirmedPeriod.Version,
		ContentHash: append([]byte(nil), source.ConfirmedPeriod.ContentHash...),
	})
	if err != nil || period == nil || !proto.Equal(period, source.ConfirmedPeriod) {
		t.Fatalf("period snapshot=%#v err=%v", period, err)
	}
	binding := BASSourceSnapshotBinding{
		Organisation: organisation, Profile: profile, Period: period,
		PaygwInputs: paygwInputs(), PaygiInputs: paygiInputs(),
		RevisionVector: &tammyv1.BasSourceRevisionVector{
			FinancialRevision: 10, LedgerRevision: 11, SettlementRevision: 12, BankingRevision: 13,
			TaxSourceRevision: 14, OrganisationProfileRevision: 15, RuleBundleRevision: 16,
			PaygRevision: 17, BasProfileRevision: 18, PeriodRevision: 19,
		},
	}
	remaining := maximumBASSourcePayloadBytes - captured.PayloadReservation.ReservedPayloadBytes
	if remaining < 0 {
		t.Fatalf("source capture reservation exceeded global payload limit: %d", captured.PayloadReservation.ReservedPayloadBytes)
	}
	base, err := BindBASSourceSnapshot(ctx, captured, binding, coverage, remaining)
	if err != nil || base == nil || len(base.SourceSnapshotHash) != 32 || len(base.RevisionVectorHash) != 32 || base.BindingPayloadBytes <= 0 {
		t.Fatalf("base binding=%#v err=%v", base, err)
	}
	if base.CombinedPayloadBytes != base.BindingPayloadBytes+coverage.CoveragePayloadBytes ||
		base.CombinedPayloadBytes > remaining ||
		captured.PayloadReservation.ReservedPayloadBytes+base.CombinedPayloadBytes > maximumBASSourcePayloadBytes {
		t.Fatalf("combined binding/coverage charge=%d, binding=%d coverage=%d", base.CombinedPayloadBytes,
			base.BindingPayloadBytes, coverage.CoveragePayloadBytes)
	}
	if len(coverage.FindingTemplates) == 0 {
		t.Fatal("mixed fixture must retain at least one source finding template")
	}
	invoiceFingerprint := bytes.Clone(captured.Invoices.Fingerprint)
	purchaseFingerprint := bytes.Clone(captured.Purchases.OriginalFingerprint)
	accountingFingerprint := bytes.Clone(captured.Accounting.Fingerprint)
	timingOnly := *coverage
	timingOnly.FindingTemplates = append([]BASSourceFindingTemplate(nil), coverage.FindingTemplates...)
	// Model an expiry/currentness review transition that changes the finding
	// result while the retained source rows and coverage manifest remain fixed.
	// Keep the exact source identities and coverage rows; only severity changes.
	if timingOnly.FindingTemplates[0].Severity == tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER {
		timingOnly.FindingTemplates[0].Severity = tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED
	} else {
		timingOnly.FindingTemplates[0].Severity = tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER
	}
	timingChanged, err := BindBASSourceSnapshot(ctx, captured, binding, &timingOnly, remaining)
	if err != nil || timingChanged == nil {
		t.Fatalf("timing-only finding change should remain a reviewable source binding: result=%#v err=%v", timingChanged, err)
	}
	if !bytes.Equal(timingOnly.ManifestHash, coverage.ManifestHash) ||
		timingOnly.CoveragePayloadBytes != coverage.CoveragePayloadBytes ||
		!bytes.Equal(captured.Invoices.Fingerprint, invoiceFingerprint) ||
		!bytes.Equal(captured.Purchases.OriginalFingerprint, purchaseFingerprint) ||
		!bytes.Equal(captured.Accounting.Fingerprint, accountingFingerprint) {
		t.Fatal("timing-only test unexpectedly changed a retained source fingerprint or row manifest")
	}
	if !bytes.Equal(timingChanged.RevisionVectorHash, base.RevisionVectorHash) {
		t.Fatalf("finding-only currentness change altered revision vector: base=%x changed=%x", base.RevisionVectorHash, timingChanged.RevisionVectorHash)
	}
	if bytes.Equal(timingChanged.SourceSnapshotHash, base.SourceSnapshotHash) {
		t.Fatal("source snapshot hash omitted changed finding semantics when coverage rows and source fingerprints were unchanged")
	}

	financialOnly := BASSourceSnapshotBinding{
		Organisation: binding.Organisation, Profile: binding.Profile, Period: binding.Period,
		PaygwInputs: binding.PaygwInputs, PaygiInputs: binding.PaygiInputs,
		RevisionVector: proto.Clone(binding.RevisionVector).(*tammyv1.BasSourceRevisionVector),
	}
	financialOnly.RevisionVector.FinancialRevision++
	financial, err := BindBASSourceSnapshot(ctx, captured, financialOnly, coverage, remaining)
	if err != nil || financial == nil || !bytes.Equal(financial.SourceSnapshotHash, base.SourceSnapshotHash) ||
		bytes.Equal(financial.RevisionVectorHash, base.RevisionVectorHash) {
		t.Fatalf("aggregate-only revision changed source snapshot or was omitted from vector: base=%#v changed=%#v err=%v", base, financial, err)
	}
	meaningful := BASSourceSnapshotBinding{
		Organisation: binding.Organisation, Profile: binding.Profile, Period: binding.Period,
		PaygwInputs: binding.PaygwInputs, PaygiInputs: binding.PaygiInputs,
		RevisionVector: proto.Clone(binding.RevisionVector).(*tammyv1.BasSourceRevisionVector),
	}
	meaningful.RevisionVector.LedgerRevision++
	changed, err := BindBASSourceSnapshot(ctx, captured, meaningful, coverage, remaining)
	if err != nil || changed == nil || bytes.Equal(changed.SourceSnapshotHash, base.SourceSnapshotHash) ||
		bytes.Equal(changed.RevisionVectorHash, base.RevisionVectorHash) {
		t.Fatalf("meaningful ledger revision was not sealed: base=%#v changed=%#v err=%v", base, changed, err)
	}
	if bounded, err := BindBASSourceSnapshot(ctx, captured, binding, coverage, 1); bounded != nil || !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("binding exceeded remaining byte budget: result=%#v err=%v", bounded, err)
	}
	if bounded, err := BindBASSourceSnapshot(ctx, captured, binding, coverage, coverage.CoveragePayloadBytes-1); bounded != nil || !errors.Is(err, ErrSourceReservation) {
		t.Fatalf("coverage plus binding escaped remaining byte budget: result=%#v err=%v", bounded, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if partial, err := BindBASSourceSnapshot(cancelled, captured, binding, coverage, remaining); partial != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled currentness binding returned partial=%#v err=%v", partial, err)
	}
}

func assertBASCoverageRawTuple(t *testing.T, got *tammyv1.BasRawSourceRef, wantType, wantID string,
	sequence *uint64, relatedID *string, relatedSequence *uint64,
) {
	t.Helper()
	if got == nil || got.Type != wantType || got.Id != wantID ||
		(got.ComponentSequence == nil) != (sequence == nil) || sequence != nil && *got.ComponentSequence != *sequence ||
		(got.RelatedId == nil) != (relatedID == nil) || relatedID != nil && *got.RelatedId != *relatedID ||
		(got.RelatedSequence == nil) != (relatedSequence == nil) || relatedSequence != nil && *got.RelatedSequence != *relatedSequence {
		t.Fatalf("raw source tuple=%#v want (%q,%q,%v,%v,%v)", got, wantType, wantID, sequence, relatedID, relatedSequence)
	}
}
