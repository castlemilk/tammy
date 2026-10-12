//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestInvoiceReportingBudgetCaptureUsesExactUTF8ReservationAndPreservesDefaultAPI(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	draft := &tammyv1.InvoiceDraft{
		Number:          "UTF8-界-🌱",
		IssueDate:       "2026-10-12",
		DueDate:         "2026-10-12",
		CustomerName:    "München 客户",
		CustomerAddress: "一番地",
		SellerAddress:   "27 Cambridge Road",
		Lines: []*tammyv1.InvoiceLineInput{{
			Description:   "咨询🙂",
			QuantityMilli: 1000,
			TaxTreatment:  tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE,
		}},
	}
	fixture.saveDraftInput(t, invoiceReportingID(5101), draft)
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatalf("NewReportingPort() = %v", err)
	}

	stateBefore := invoiceReportingBudgetState(t, fixture)
	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil {
		t.Fatalf("exact UTF-8 preflight: %v", err)
	}
	if preflight.CandidateCount != 1 || preflight.ReceiptCount != 0 || preflight.JournalCount != 0 ||
		len(preflight.JournalIDs) != 0 || len(preflight.JournalPayloadBytes) != 0 {
		t.Fatalf("draft-only preflight counts = %#v; want one candidate and no receipt or journal parents", preflight)
	}

	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || len(capture.Candidates) != 1 {
		t.Fatalf("capture at the exact reserved byte budget = %#v, %v; want one complete candidate", capture, err)
	}
	candidate := capture.Candidates[0]
	expectedBaseBytes := int64(512 +
		len(candidate.ID) + len(candidate.OrganisationID) + len(candidate.State) + len(candidate.Number) +
		len(candidate.InvoiceProto) + len(candidate.CreatedAt) + len(candidate.UpdatedAt))
	if preflight.BasePayloadBytes != expectedBaseBytes || capture.BasePayloadBytes != expectedBaseBytes ||
		preflight.PayloadBytes != expectedBaseBytes || capture.PayloadBytes != expectedBaseBytes {
		t.Fatalf("UTF-8 raw retained-byte charge = preflight %d/%d capture %d/%d; want %d bytes",
			preflight.BasePayloadBytes, preflight.PayloadBytes, capture.BasePayloadBytes, capture.PayloadBytes,
			expectedBaseBytes)
	}
	if !strings.Contains(candidate.Number, "界") || !strings.Contains(candidate.Invoice.Draft.Lines[0].Description, "🙂") {
		t.Fatalf("fixture did not retain its multibyte UTF-8 fields: %#v", candidate.Invoice.Draft)
	}
	assertInvoiceReportingReservationMetadata(t, capture.Candidates, capture.CandidateCount,
		capture.ReceiptCount, capture.JournalCount, capture.JournalIDs,
		capture.BasePayloadBytes, capture.PayloadBytes, capture.JournalPayloadBytes)
	assertInvoiceReportingReservationMetadata(t, capture.Candidates, preflight.CandidateCount,
		preflight.ReceiptCount, preflight.JournalCount, preflight.JournalIDs,
		preflight.BasePayloadBytes, preflight.PayloadBytes, preflight.JournalPayloadBytes)

	// CaptureBAS remains the same default-budget API. The additive variant
	// returns the same complete source census and fingerprint at that budget.
	defaultCapture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || defaultCapture == nil ||
		!bytes.Equal(defaultCapture.Fingerprint, capture.Fingerprint) ||
		!sameInvoiceCandidateOrder(defaultCapture.Candidates, capture.Candidates) ||
		defaultCapture.PayloadBytes != capture.PayloadBytes ||
		defaultCapture.BasePayloadBytes != capture.BasePayloadBytes {
		t.Fatalf("default capture diverged from the caller-budget capture: default=%#v budget=%#v err=%v",
			defaultCapture, capture, err)
	}

	clamped, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes)+1024)
	if err != nil || clamped.PayloadBytes != preflight.PayloadBytes ||
		clamped.BasePayloadBytes != preflight.BasePayloadBytes {
		t.Fatalf("oversized budget was not clamped to the existing default ceiling: exact=%#v clamped=%#v err=%v",
			preflight, clamped, err)
	}

	short, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes-1)
	if err == nil || !errors.Is(err, ErrSourceBound) ||
		short != nil {
		t.Fatalf("one-byte-short preflight = %#v, %v; want a zero result and ErrSourceBound", short, err)
	}
	shortCapture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes-1)
	if shortCapture != nil || !errors.Is(err, ErrSourceBound) {
		t.Fatalf("one-byte-short capture = %#v, %v; want nil capture and ErrSourceBound", shortCapture, err)
	}

	probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
	probedPort, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	shortCapture, err = probedPort.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes-1)
	if shortCapture != nil || !errors.Is(err, ErrSourceBound) || probe.hydrationQueries != 0 {
		t.Fatalf("short-budget capture = %#v, %v after %d body reads; want nil, ErrSourceBound, and no hydration",
			shortCapture, err, probe.hydrationQueries)
	}

	negative, err := port.PreflightBASWithPayloadBudget(fixture.ctx, orgID, invoiceReportingPeriod(), -1)
	if err == nil || negative != nil {
		t.Fatalf("negative-budget preflight = %#v, %v; want an error and zero result", negative, err)
	}
	negativeCapture, err := port.CaptureBASWithPayloadBudget(fixture.ctx, orgID, invoiceReportingPeriod(), -1)
	if negativeCapture != nil || err == nil {
		t.Fatalf("negative-budget capture = %#v, %v; want nil capture and an error", negativeCapture, err)
	}

	cancelled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	cancelledPreflight, err := port.PreflightBASWithPayloadBudget(
		cancelled, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err == nil || !errors.Is(err, context.Canceled) ||
		cancelledPreflight != nil {
		t.Fatalf("cancelled preflight = %#v, %v; want zero result and context.Canceled", cancelledPreflight, err)
	}
	cancelledCapture, err := port.CaptureBASWithPayloadBudget(
		cancelled, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if cancelledCapture != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture = %#v, %v; want nil capture and context.Canceled", cancelledCapture, err)
	}

	if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore {
		t.Fatalf("read-only invoice budget APIs changed total_changes or financial revisions: before=%v after=%v",
			stateBefore, stateAfter)
	}
}

func TestInvoiceReportingBudgetMetadataCountsCompleteCensusAndUniqueJournalParents(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
	first := fixture.saveDraft(t, invoiceReportingID(5201), "BUDGET-PAID-001", "2026-10-03", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, first)
	fixture.post(t, first)
	fixture.recordPayment(t, first, 1, 1, 1)

	second := fixture.saveDraft(t, invoiceReportingID(5202), "BUDGET-PAID-002", "2026-10-04", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, second)
	fixture.post(t, second)
	fixture.recordPayment(t, second, 1, 1, 2)

	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil {
		t.Fatalf("PreflightBASWithPayloadBudget() = %v", err)
	}
	if preflight.CandidateCount != 2 || preflight.ReceiptCount != 2 || preflight.JournalCount != 4 {
		t.Fatalf("complete invoice/receipt census counts = candidates %d receipts %d journals %d; want 2/2/4",
			preflight.CandidateCount, preflight.ReceiptCount, preflight.JournalCount)
	}
	if !sort.StringsAreSorted(preflight.JournalIDs) || len(preflight.JournalIDs) != preflight.JournalCount {
		t.Fatalf("journal parent identities are not sorted and unique: %#v", preflight.JournalIDs)
	}
	for index := 1; index < len(preflight.JournalIDs); index++ {
		if preflight.JournalIDs[index-1] == preflight.JournalIDs[index] {
			t.Fatalf("preflight repeated one complete parent proof: %#v", preflight.JournalIDs)
		}
	}
	if len(preflight.JournalPayloadBytes) != preflight.JournalCount {
		t.Fatalf("per-parent byte reservations = %#v; want one reservation for each of %d unique parents",
			preflight.JournalPayloadBytes, preflight.JournalCount)
	}
	var parentBytes int64
	for _, id := range preflight.JournalIDs {
		charge, ok := preflight.JournalPayloadBytes[id]
		if !ok || charge <= 0 {
			t.Fatalf("unique parent %s has no positive retained-byte reservation: %#v", id, preflight.JournalPayloadBytes)
		}
		parentBytes += charge
	}
	if preflight.PayloadBytes != preflight.BasePayloadBytes+parentBytes {
		t.Fatalf("preflight total %d != base %d + unique parent charges %d",
			preflight.PayloadBytes, preflight.BasePayloadBytes, parentBytes)
	}

	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil {
		t.Fatalf("CaptureBASWithPayloadBudget(exact reservation) = %#v, %v", capture, err)
	}
	if capture.CandidateCount != preflight.CandidateCount || capture.ReceiptCount != preflight.ReceiptCount ||
		capture.JournalCount != preflight.JournalCount || capture.BasePayloadBytes != preflight.BasePayloadBytes ||
		capture.PayloadBytes != preflight.PayloadBytes ||
		!equalInvoiceReportingStrings(capture.JournalIDs, preflight.JournalIDs) ||
		!equalInvoiceReportingJournalBytes(capture.JournalPayloadBytes, preflight.JournalPayloadBytes) {
		t.Fatalf("loaded capture does not match its exact preflight reservation: preflight=%#v capture=%#v",
			preflight, capture)
	}
	assertInvoiceReportingReservationMetadata(t, capture.Candidates, capture.CandidateCount,
		capture.ReceiptCount, capture.JournalCount, capture.JournalIDs,
		capture.BasePayloadBytes, capture.PayloadBytes, capture.JournalPayloadBytes)

	defaultCapture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || defaultCapture == nil ||
		!bytes.Equal(defaultCapture.Fingerprint, capture.Fingerprint) ||
		!sameInvoiceCandidateOrder(defaultCapture.Candidates, capture.Candidates) {
		t.Fatalf("default and caller-budget captures differ: default=%#v budget=%#v err=%v",
			defaultCapture, capture, err)
	}
}

func TestInvoiceReportingBudgetPreflightRejectsMissingAndForeignParentsBeforeHydration(t *testing.T) {
	t.Run("missing parent", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		invoice := fixture.saveDraft(t, invoiceReportingID(5301), "MISSING-PARENT", "2026-10-12", 391,
			tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
		fixture.issue(t, invoice)
		fixture.post(t, invoice)
		const missingJournalID = "018f0000-0000-7000-8000-000000053099"
		makeInvoicePostingJournalReferenceCorruptForPreflight(t, fixture, invoice.Id, missingJournalID, true)

		probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		preflight, err := port.PreflightBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
		assertInvoiceReportingParentPreflightFailure(t, preflight, err, probe)
	})

	t.Run("foreign parent", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		invoice := fixture.saveDraft(t, invoiceReportingID(5401), "FOREIGN-PARENT", "2026-10-12", 391,
			tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
		fixture.issue(t, invoice)
		fixture.post(t, invoice)

		const foreignOrganisationID = "018f0000-0000-7000-8000-000000054098"
		const foreignJournalID = "018f0000-0000-7000-8000-000000054099"
		createdAt := fixture.now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		if _, err := fixture.tx.ExecContext(fixture.ctx, "DROP TRIGGER organisations_singleton_insert"); err != nil {
			t.Fatalf("open transaction-local second-organisation fixture: %v", err)
		}
		if _, err := fixture.tx.ExecContext(fixture.ctx,
			"INSERT INTO organisations(id,legal_name,display_name,entity_type,abn,gst_basis,gst_reporting_frequency,"+
				"financial_year_end_month,owner_user_id,active_tax_rule_type,active_tax_rule_id,"+
				"active_tax_rule_revision,active_tax_rule_content_hash,status,verification_state,"+
				"version,created_at,updated_at) "+
				"SELECT ?,legal_name,display_name,entity_type,NULL,gst_basis,gst_reporting_frequency,"+
				"financial_year_end_month,owner_user_id,active_tax_rule_type,active_tax_rule_id,"+
				"active_tax_rule_revision,active_tax_rule_content_hash,status,verification_state,"+
				"version,?,? FROM organisations WHERE id=?",
			foreignOrganisationID, createdAt, createdAt, orgID); err != nil {
			t.Fatalf("insert temporary foreign organisation parent: %v", err)
		}
		if _, err := fixture.tx.ExecContext(fixture.ctx,
			"INSERT INTO journals(id,organisation_id,version,source_type,source_kind,source_id,source_revision,state,"+
				"journal_date,description,total_debits_minor,total_credits_minor,currency_code,"+
				"financial_revision,posted_at,created_at) "+
				"VALUES(?,?,1,'SYSTEM',0,?,1,'DRAFT','2026-10-12','foreign reporting parent',"+
				"1,1,'AUD',1,NULL,?)",
			foreignJournalID, foreignOrganisationID, foreignJournalID, createdAt); err != nil {
			t.Fatalf("insert temporary foreign journal parent: %v", err)
		}
		makeInvoicePostingJournalReferenceCorruptForPreflight(t, fixture, invoice.Id, foreignJournalID, false)

		probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		preflight, err := port.PreflightBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
		assertInvoiceReportingParentPreflightFailure(t, preflight, err, probe)
	})
}

func TestInvoiceReportingBudgetCaptureRejectsSourcesChangedAfterPreflightWithoutPartialResult(t *testing.T) {
	t.Run("candidate added after preflight", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		fixture.saveDraft(t, invoiceReportingID(5501), "RESERVATION-BASE", "2026-10-12", 100,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
		probe.beforeFirstInvoiceHydration = func() {
			fixture.saveDraft(t, invoiceReportingID(5502), "RESERVATION-LATE", "2026-10-13", 200,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		capture, err := port.CaptureBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
		if capture != nil || err == nil || probe.hydrationQueries == 0 {
			t.Fatalf("capture after candidate census changed = %#v, %v after %d body reads; want nil result and a mismatch error",
				capture, err, probe.hydrationQueries)
		}
	})

	t.Run("retained payload changed after preflight", func(t *testing.T) {
		fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		invoice := fixture.saveDraft(t, invoiceReportingID(5601), "RESERVATION-PAYLOAD", "2026-10-12", 100,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
		probe.beforeFirstInvoiceHydration = func() {
			changed := proto.Clone(invoice).(*tammyv1.Invoice)
			changed.Version++
			changed.Draft.Number = strings.Repeat("n", 31)
			changed.UpdatedAt = timestamppb.New(fixture.now.Add(time.Minute))
			if err := fixture.invoiceRepository.Save(fixture.ctx, changed, invoice.Version); err != nil {
				t.Fatalf("change retained draft after preflight: %v", err)
			}
		}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		capture, err := port.CaptureBASWithPayloadBudget(
			fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
		if capture != nil || err == nil || probe.hydrationQueries == 0 {
			t.Fatalf("capture after retained payload changed = %#v, %v after %d body reads; want nil result and a mismatch error",
				capture, err, probe.hydrationQueries)
		}
	})
}

func makeInvoicePostingJournalReferenceCorruptForPreflight(
	t *testing.T,
	fixture *invoiceReportingFixture,
	invoiceID, journalID string,
	deferMissingForeignKey bool,
) {
	t.Helper()
	if deferMissingForeignKey {
		if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
			t.Fatalf("defer transaction-local missing-parent foreign key: %v", err)
		}
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx, "DROP TRIGGER invoice_income_postings_no_update"); err != nil {
		t.Fatalf("open transaction-local immutable-posting fixture: %v", err)
	}
	if _, err := fixture.tx.ExecContext(fixture.ctx,
		"UPDATE invoice_income_postings_v1 SET journal_id=? WHERE invoice_id=?",
		journalID, invoiceID); err != nil {
		t.Fatalf("point retained invoice posting at invalid parent %s: %v", journalID, err)
	}
}

func assertInvoiceReportingParentPreflightFailure(
	t *testing.T,
	preflight *BASReportingPreflight,
	err error,
	probe *invoiceReportingBudgetProbe,
) {
	t.Helper()
	if err == nil || preflight != nil || probe.hydrationQueries != 0 {
		t.Fatalf("invalid-parent preflight = %#v, %v after %d body reads; want zero result, error, and no hydration",
			preflight, err, probe.hydrationQueries)
	}
}

func assertInvoiceReportingReservationMetadata(
	t *testing.T,
	candidates []BASInvoiceCandidate,
	candidateCount, receiptCount, journalCount int,
	journalIDs []string,
	basePayloadBytes, payloadBytes int64,
	journalPayloadBytes map[string]int64,
) {
	t.Helper()
	seenJournals := make(map[string]struct{})
	actualReceipts := 0
	for _, candidate := range candidates {
		actualReceipts += len(candidate.Receipts)
		if candidate.Posting != nil && candidate.Posting.JournalID != nil {
			seenJournals[*candidate.Posting.JournalID] = struct{}{}
		}
		for _, receipt := range candidate.Receipts {
			seenJournals[receipt.JournalID] = struct{}{}
		}
	}
	expectedIDs := make([]string, 0, len(seenJournals))
	for id := range seenJournals {
		expectedIDs = append(expectedIDs, id)
	}
	sort.Strings(expectedIDs)
	var summedParents int64
	for _, charge := range journalPayloadBytes {
		if charge <= 0 {
			t.Fatalf("journal payload reservation is not positive: %#v", journalPayloadBytes)
		}
		summedParents += charge
	}
	if candidateCount != len(candidates) || receiptCount != actualReceipts ||
		journalCount != len(expectedIDs) || len(journalIDs) != len(expectedIDs) ||
		!equalInvoiceReportingStrings(journalIDs, expectedIDs) ||
		!sort.StringsAreSorted(journalIDs) || len(journalPayloadBytes) != len(expectedIDs) ||
		payloadBytes != basePayloadBytes+summedParents {
		t.Fatalf("reservation metadata disagrees with complete loaded sources: candidates=%d/%d receipts=%d/%d journals=%d/%d ids=%#v/%#v bytes=%d base=%d parents=%d map=%#v",
			candidateCount, len(candidates), receiptCount, actualReceipts, journalCount, len(expectedIDs),
			journalIDs, expectedIDs, payloadBytes, basePayloadBytes, summedParents, journalPayloadBytes)
	}
	for _, id := range expectedIDs {
		if _, ok := journalPayloadBytes[id]; !ok {
			t.Fatalf("complete parent %s has no unique retained-byte charge: %#v", id, journalPayloadBytes)
		}
	}
}

func equalInvoiceReportingStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalInvoiceReportingJournalBytes(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for id, value := range left {
		if right[id] != value {
			return false
		}
	}
	return true
}

func invoiceReportingBudgetState(t *testing.T, fixture *invoiceReportingFixture) [8]int64 {
	t.Helper()
	var state [8]int64
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT financial_revision,ledger_revision,settlement_revision,banking_revision,"+
			"tax_source_revision,organisation_profile_revision,rule_bundle_revision "+
			"FROM financial_revisions WHERE id=1").Scan(
		&state[0], &state[1], &state[2], &state[3], &state[4], &state[5], &state[6]); err != nil {
		t.Fatalf("read invoice reporting budget revision vector: %v", err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx, "SELECT total_changes()").Scan(&state[7]); err != nil {
		t.Fatalf("read SQL total_changes before/after budget API: %v", err)
	}
	return state
}

type invoiceReportingBudgetProbe struct {
	*sqlcipher.Transaction
	hydrationQueries            int
	beforeFirstInvoiceHydration func()
	mutatedBeforeFirstHydration bool
}

func (executor *invoiceReportingBudgetProbe) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	if isInvoiceReportingBodyQuery(query) {
		executor.hydrationQueries++
		if !executor.mutatedBeforeFirstHydration && executor.beforeFirstInvoiceHydration != nil &&
			strings.Contains(strings.ToLower(strings.Join(strings.Fields(query), " ")),
				"select id,organisation_id,version,state,number") {
			executor.mutatedBeforeFirstHydration = true
			executor.beforeFirstInvoiceHydration()
		}
	}
	return executor.Transaction.QueryContext(ctx, query, arguments...)
}

func isInvoiceReportingBodyQuery(query string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))
	for _, marker := range []string{
		"select id,organisation_id,version,state,number",
		"select posting.invoice_id,posting.organisation_id,posting.invoice_version",
		"select income_line.invoice_id,income_line.sequence",
		"select receipt.id,receipt.organisation_id,receipt.invoice_id, receipt.payment_date",
		"select allocation.payment_id,allocation.sequence",
		"select id, organisation_id, version, source_type, source_kind, source_id",
		"select id, journal_id, line_number, account_id",
	} {
		if strings.Contains(strings.ReplaceAll(normalized, " ", ""), strings.ReplaceAll(marker, " ", "")) {
			return true
		}
	}
	return false
}

var _ interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
} = (*invoiceReportingBudgetProbe)(nil)
