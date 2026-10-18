//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestInvoiceComponentOrphanWriterControlPreservesExactSources(t *testing.T) {
	sources := invoiceChildOwnerStaticNewWriter(t, true)
	fixture, invoice, payment := sources.fixture, sources.invoice, sources.payment
	if payment == nil {
		t.Fatal("writer control omitted its real invoice payment")
	}
	stateBefore := invoiceReportingBudgetState(t, fixture)
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}

	invoiceProto, err := (proto.MarshalOptions{Deterministic: true}).Marshal(invoice)
	if err != nil {
		t.Fatal(err)
	}
	invoiceHash := sha256.Sum256(invoiceProto)
	paymentProto, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payment)
	if err != nil {
		t.Fatal(err)
	}
	paymentHash := sha256.Sum256(paymentProto)
	var postingJournalID string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT journal_id FROM invoice_income_postings_v1 WHERE invoice_id=?", invoice.Id,
	).Scan(&postingJournalID); err != nil {
		t.Fatal(err)
	}

	summary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
	wantSummary := BASScalarSummary{
		OrganisationID: orgID,
		RootCount:      1, PostingCount: 1, IncomeLineCount: 1, ReceiptCount: 1, AllocationCount: 1,
		ScalarKeyCount: 5,
		ScalarBytes: int64(5*512 + 5*len(orgID) + 5*len(invoice.Id) +
			len(postingJournalID) + 2*len(payment.Id) + len(payment.JournalId) + 2*sha256.Size),
	}
	if err != nil || summary == nil || *summary != wantSummary {
		t.Fatalf("real-writer scalar summary=%#v err=%v; want exact five-family census %#v", summary, err, wantSummary)
	}
	keys, err := port.ReadBASKeys(fixture.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || len(keys) != 5 {
		t.Fatalf("real-writer scalar keys=%#v err=%v; want one exact key in every retained family", keys, err)
	}
	wantKinds := []BASScalarKeyKind{
		BASScalarInvoice, BASScalarPosting, BASScalarIncomeLine, BASScalarReceipt, BASScalarAllocation,
	}
	for index, key := range keys {
		if key.Kind != wantKinds[index] || key.OrganisationID != orgID || key.InvoiceID != invoice.Id {
			t.Fatalf("scalar key %d lost family, exact invoice identity, or owner: %#v", index, key)
		}
	}
	if keys[0].RootVersion == nil || *keys[0].RootVersion != invoice.Version ||
		keys[1].PostingInvoiceVersion == nil || *keys[1].PostingInvoiceVersion != invoice.Version ||
		keys[1].JournalID == nil || *keys[1].JournalID != postingJournalID ||
		!bytes.Equal(keys[1].SourceHash, invoiceHash[:]) ||
		keys[2].Sequence == nil || *keys[2].Sequence != 1 ||
		keys[3].ReceiptID == nil || *keys[3].ReceiptID != payment.Id ||
		keys[3].JournalID == nil || *keys[3].JournalID != payment.JournalId ||
		!bytes.Equal(keys[3].SourceHash, paymentHash[:]) ||
		keys[4].ReceiptID == nil || *keys[4].ReceiptID != payment.Id ||
		keys[4].Sequence == nil || *keys[4].Sequence != 1 {
		t.Fatalf("scalar keys lost exact writer provenance: %#v", keys)
	}

	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 1 ||
		preflight.ReceiptCount != 1 || preflight.JournalCount != 2 {
		t.Fatalf("real-writer full preflight=%#v err=%v; want one candidate, receipt, and two unique journals", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 1 || capture.ReceiptCount != 1 ||
		capture.JournalCount != 2 || len(capture.Candidates) != 1 ||
		capture.BasePayloadBytes != preflight.BasePayloadBytes || capture.PayloadBytes != preflight.PayloadBytes ||
		!equalInvoiceReportingStrings(capture.JournalIDs, preflight.JournalIDs) ||
		!equalInvoiceReportingJournalBytes(capture.JournalPayloadBytes, preflight.JournalPayloadBytes) {
		t.Fatalf("real-writer full capture=%#v err=%v; want exact preflight counts and charges", capture, err)
	}
	assertInvoiceReportingCandidateSource(t, capture.Candidates)
	candidate := capture.Candidates[0]
	if candidate.ID != invoice.Id || candidate.OrganisationID != orgID || !proto.Equal(candidate.Invoice, invoice) ||
		candidate.Posting == nil || len(candidate.Receipts) != 1 {
		t.Fatalf("full capture lost exact writer invoice/posting/receipt lineage: %#v", candidate)
	}
	assertInvoiceReportingPostingJournal(t, candidate, "RECEIVABLE", invoice.Id, invoice.Version)
	invoiceChildOwnerStaticAssertReceipt(t, candidate, payment)
	assertInvoiceReportingReservationMetadata(t, capture.Candidates, capture.CandidateCount,
		capture.ReceiptCount, capture.JournalCount, capture.JournalIDs,
		capture.BasePayloadBytes, capture.PayloadBytes, capture.JournalPayloadBytes)
	wantBasePayload, ok := invoiceChildOwnerStaticExpectedBasePayload(capture)
	if !ok || capture.BasePayloadBytes != wantBasePayload {
		t.Fatalf("full writer base payload=%d want exact source-derived charge %d (valid=%t)",
			capture.BasePayloadBytes, wantBasePayload, ok)
	}
	var journalBytes int64
	for _, charge := range preflight.JournalPayloadBytes {
		if charge <= 0 {
			t.Fatalf("journal has nonpositive retained payload charge: %#v", preflight.JournalPayloadBytes)
		}
		journalBytes += charge
	}
	if preflight.PayloadBytes != wantBasePayload+journalBytes {
		t.Fatalf("full payload=%d; exact source base %d + unique journal payloads %d",
			preflight.PayloadBytes, wantBasePayload, journalBytes)
	}
	if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
		t.Fatal("real-writer reads changed retained state or caller transaction")
	}
}

func TestInvoiceComponentOrphanForeignTextGraphStaysOutsideTargetScope(t *testing.T) {
	sources := invoiceChildOwnerStaticNewWriter(t, true)
	fixture := sources.fixture
	invoiceComponentOrphanAssertCanonicalWriter(t, sources)
	foreignOwner := invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
	invoiceChildOwnerStaticAssertForeignGraph(t, sources, foreignOwner)

	// This mutates a real writer graph to coherent TEXT links under an owner ID
	// absent from the fixture. It is only a non-target scope control, not a claim
	// that another organisation authentically produced this graph.
	stateBefore := invoiceReportingBudgetState(t, fixture)
	invoiceChildOwnerStaticAssertEmptyScalar(t, fixture)
	invoiceChildOwnerStaticAssertEmptyFull(t, fixture)
	if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
		t.Fatal("foreign TEXT scope control changed retained state or caller transaction")
	}
}

func TestInvoiceComponentOrphanPreflightAndCaptureRejectBrokenLinks(t *testing.T) {
	cases := []struct {
		name        string
		withPayment bool
		mutate      func(*testing.T, invoiceChildOwnerStaticSources)
	}{
		{
			name:   "target invoice edge with missing posting and no receipt",
			mutate: invoiceComponentOrphanBreakTargetIncomePosting,
		},
		{
			name: "fully orphaned income line with TEXT reference",
			mutate: func(t *testing.T, sources invoiceChildOwnerStaticSources) {
				invoiceComponentOrphanMoveIncomeReference(t, sources, invoiceReportingID(99841), false)
			},
		},
		{
			name:        "allocation with missing receipt",
			withPayment: true,
			mutate:      invoiceComponentOrphanDeleteReceiptParent,
		},
		{
			name:        "fully orphaned allocation with TEXT reference",
			withPayment: true,
			mutate: func(t *testing.T, sources invoiceChildOwnerStaticSources) {
				invoiceComponentOrphanMoveAllocationReference(t, sources, invoiceReportingID(99843), false)
			},
		},
		{
			name: "income BLOB reference to canonical TEXT invoice",
			mutate: func(t *testing.T, sources invoiceChildOwnerStaticSources) {
				invoiceComponentOrphanMoveIncomeReference(t, sources, sources.invoice.Id, true)
			},
		},
		{
			name:        "allocation BLOB reference to canonical TEXT receipt",
			withPayment: true,
			mutate: func(t *testing.T, sources invoiceChildOwnerStaticSources) {
				invoiceComponentOrphanMoveAllocationReference(t, sources, sources.payment.Id, true)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			invoiceComponentOrphanAssertCanonicalWriter(t, sources)
			testCase.mutate(t, sources)
			invoiceComponentOrphanAssertFullRejected(t, sources.fixture, orgID)
			invoiceComponentOrphanAssertScalarSummaryRejected(t, sources.fixture, orgID)
		})
	}
}

func TestInvoiceComponentOrphanRejectsAnOtherwiseEmptyRequestedOrganisation(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		withPayment bool
		orphan      func(*testing.T, invoiceChildOwnerStaticSources)
	}{
		{
			name: "income line",
			orphan: func(t *testing.T, sources invoiceChildOwnerStaticSources) {
				invoiceComponentOrphanMoveIncomeReference(t, sources, invoiceReportingID(99851), false)
			},
		},
		{
			name:        "allocation",
			withPayment: true,
			orphan: func(t *testing.T, sources invoiceChildOwnerStaticSources) {
				invoiceComponentOrphanMoveAllocationReference(t, sources, invoiceReportingID(99852), false)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			invoiceComponentOrphanAssertCanonicalWriter(t, sources)
			fixture := sources.fixture
			emptyTarget := invoiceReportingID(99859)
			if emptyTarget == orgID {
				t.Fatal("empty target must be distinct from the writer organisation")
			}
			// This is a canonical requested organisation ID with no source rows
			// in the fixture; no second organisations row is inserted.
			invoiceComponentOrphanAssertEmptyTarget(t, fixture, emptyTarget)
			testCase.orphan(t, sources)
			for _, query := range []string{
				"SELECT count(*) FROM invoices_v1 WHERE organisation_id=?",
				"SELECT count(*) FROM invoice_income_postings_v1 WHERE organisation_id=?",
				"SELECT count(*) FROM invoice_payment_v1 WHERE organisation_id=?",
			} {
				var count int
				if err := fixture.tx.QueryRowContext(fixture.ctx, query, emptyTarget).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("empty requested organisation has %d own source rows for %q", count, query)
				}
			}
			invoiceComponentOrphanAssertScalarSummaryRejected(t, fixture, emptyTarget)
			invoiceComponentOrphanAssertFullRejected(t, fixture, emptyTarget)
		})
	}
}

func invoiceComponentOrphanAssertCanonicalWriter(t *testing.T, sources invoiceChildOwnerStaticSources) {
	t.Helper()
	wantJournalCount := 1
	if sources.payment != nil {
		wantJournalCount = 2
	}
	invoiceChildOwnerAtomicAssertCanonicalWriterControl(t, sources.fixture, sources.invoice,
		sources.payment, sources.payment != nil, wantJournalCount)
}

func invoiceComponentOrphanAssertEmptyTarget(
	t *testing.T,
	fixture *invoiceReportingFixture,
	target string,
) {
	t.Helper()
	stateBefore := invoiceReportingBudgetState(t, fixture)
	scalarProbe := &invoiceBASScalarProbe{Transaction: fixture.tx}
	port, err := NewReportingPort(scalarProbe)
	if err != nil {
		t.Fatal(err)
	}
	wantSummary := BASScalarSummary{OrganisationID: target}
	summary, err := port.SummarizeBASKeys(fixture.ctx, target)
	if err != nil || summary == nil || *summary != wantSummary {
		t.Fatalf("empty-target scalar summary=%#v err=%v; want %#v", summary, err, wantSummary)
	}
	keys, err := port.ReadBASKeys(fixture.ctx, target, *summary, 0, 0)
	if err != nil || len(keys) != 0 || scalarProbe.summaryReads != 2 || scalarProbe.keyReads != 1 ||
		scalarProbe.bodyReads != 0 {
		t.Fatalf("empty-target scalar keys=%#v err=%v summaries/keys/bodies=%d/%d/%d",
			keys, err, scalarProbe.summaryReads, scalarProbe.keyReads, scalarProbe.bodyReads)
	}

	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, target, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 0 || preflight.ReceiptCount != 0 ||
		preflight.JournalCount != 0 || preflight.BasePayloadBytes != 0 || preflight.PayloadBytes != 0 ||
		len(preflight.JournalIDs) != 0 || len(preflight.JournalPayloadBytes) != 0 {
		t.Fatalf("empty-target full preflight=%#v err=%v", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, target, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 0 || capture.ReceiptCount != 0 ||
		capture.JournalCount != 0 || capture.BasePayloadBytes != 0 || capture.PayloadBytes != 0 ||
		len(capture.Candidates) != 0 || len(capture.JournalIDs) != 0 || len(capture.JournalPayloadBytes) != 0 {
		t.Fatalf("empty-target full capture=%#v err=%v", capture, err)
	}
	if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
		t.Fatal("empty-target baseline reads changed retained state or caller transaction")
	}
}

func TestInvoiceComponentOrphanValidSmallBudgetStopsBeforeHydration(t *testing.T) {
	sources := invoiceChildOwnerStaticNewWriter(t, true)
	fixture := sources.fixture
	const budget int64 = 1
	for _, api := range []string{"preflight", "capture"} {
		t.Run(api, func(t *testing.T) {
			stateBefore := invoiceReportingBudgetState(t, fixture)
			probe := invoiceComponentOrphanNewProbe(fixture)
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			var resultPresent bool
			if api == "preflight" {
				preflight, preflightErr := port.PreflightBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), budget)
				resultPresent = preflight != nil
				err = preflightErr
			} else {
				capture, captureErr := port.CaptureBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), budget)
				resultPresent = capture != nil
				err = captureErr
			}
			var bound *SourceBoundError
			if resultPresent || !errors.Is(err, ErrSourceBound) || !errors.As(err, &bound) ||
				bound.Resource != "capture bytes" || bound.Limit != budget {
				t.Fatalf("valid tiny-budget %s resultPresent=%t err=%v; want exact capture-bytes bound %d", api, resultPresent, err, budget)
			}
			invoiceComponentOrphanAssertNoReadsPastPreflight(t, probe, api)
			if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
				t.Fatal("tiny-budget read changed retained state or caller transaction")
			}
		})
	}
}

func invoiceComponentOrphanAssertFullRejected(t *testing.T, fixture *invoiceReportingFixture, target string) {
	t.Helper()
	for _, api := range []string{"preflight", "capture"} {
		t.Run(api, func(t *testing.T) {
			stateBefore := invoiceReportingBudgetState(t, fixture)
			probe := invoiceComponentOrphanNewProbe(fixture)
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			var resultPresent bool
			if api == "preflight" {
				preflight, preflightErr := port.PreflightBASWithPayloadBudget(
					fixture.ctx, target, invoiceReportingPeriod(), int64(maxBASReportingBytes))
				resultPresent = preflight != nil
				err = preflightErr
			} else {
				capture, captureErr := port.CaptureBASWithPayloadBudget(
					fixture.ctx, target, invoiceReportingPeriod(), int64(maxBASReportingBytes))
				resultPresent = capture != nil
				err = captureErr
			}
			if resultPresent || !errors.Is(err, ErrRepository) {
				t.Fatalf("orphan %s for target %s resultPresent=%t, err=%v; want nil and ErrRepository",
					api, target, resultPresent, err)
			}
			invoiceComponentOrphanAssertNoReadsPastPreflight(t, probe, api)
			if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
				t.Fatalf("orphan %s changed revisions/total_changes or caller transaction", api)
			}
		})
	}
}

func invoiceComponentOrphanAssertNoReadsPastPreflight(
	t *testing.T,
	probe *invoiceComponentOrphanProbe,
	api string,
) {
	t.Helper()
	if probe.hydrationQueries != 0 || probe.journalIdentityReads != 0 || probe.journalParentPreflightReads != 0 ||
		probe.mutatedBeforeFirstHydration {
		t.Fatalf("%s crossed preflight boundary: body=%d journalIDs=%d journalParents=%d",
			api, probe.hydrationQueries, probe.journalIdentityReads, probe.journalParentPreflightReads)
	}
}

func invoiceComponentOrphanAssertScalarSummaryRejected(
	t *testing.T,
	fixture *invoiceReportingFixture,
	target string,
) {
	t.Helper()
	stateBefore := invoiceReportingBudgetState(t, fixture)
	probe := &invoiceBASScalarProbe{Transaction: fixture.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := port.SummarizeBASKeys(fixture.ctx, target)
	if summary != nil || !errors.Is(err, ErrRepository) || probe.summaryReads != 1 ||
		probe.keyReads != 0 || probe.bodyReads != 0 {
		t.Fatalf("orphan scalar summary=%#v err=%v summaries/keys/bodies=%d/%d/%d; want nil ErrRepository before keys or bodies",
			summary, err, probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
	if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
		t.Fatal("orphan scalar summary changed revisions/total_changes or caller transaction")
	}
}

func invoiceComponentOrphanNewProbe(fixture *invoiceReportingFixture) *invoiceComponentOrphanProbe {
	return &invoiceComponentOrphanProbe{
		invoiceReportingBudgetProbe: &invoiceReportingBudgetProbe{Transaction: fixture.tx},
	}
}

type invoiceComponentOrphanProbe struct {
	*invoiceReportingBudgetProbe
	journalIdentityReads        int
	journalParentPreflightReads int
}

func (probe *invoiceComponentOrphanProbe) QueryContext(
	ctx context.Context,
	query string,
	arguments ...any,
) (*sql.Rows, error) {
	compact := invoiceChildOwnerAtomicCompact(query)
	if strings.Contains(compact, invoiceChildOwnerAtomicCompact(invoiceChildOwnerAtomicPostingIDsMarker)) ||
		strings.Contains(compact, invoiceChildOwnerAtomicCompact(invoiceChildOwnerAtomicReceiptIDsMarker)) {
		probe.journalIdentityReads++
	}
	if strings.Contains(compact, "fromjournalswhereid=?") ||
		strings.Contains(compact, "fromjournal_linesaslinewherejournal_id=?") {
		probe.journalParentPreflightReads++
	}
	return probe.invoiceReportingBudgetProbe.QueryContext(ctx, query, arguments...)
}

func invoiceComponentOrphanDeferForeignKeys(t *testing.T, fixture *invoiceReportingFixture) {
	t.Helper()
	if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatalf("defer foreign keys for corruption-only component reference: %v", err)
	}
}

func invoiceComponentOrphanBreakTargetIncomePosting(t *testing.T, sources invoiceChildOwnerStaticSources) {
	t.Helper()
	fixture, invoice := sources.fixture, sources.invoice
	if sources.payment != nil {
		t.Fatal("target income-edge case must start without a receipt")
	}
	missingInvoiceID := invoiceReportingID(99840)
	foreignOwnerID := invoiceReportingID(99849)
	invoiceComponentOrphanDeferForeignKeys(t, fixture)
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_postings_v1",
		"UPDATE invoice_income_postings_v1 SET invoice_id=?,organisation_id=? WHERE invoice_id=?",
		missingInvoiceID, foreignOwnerID, invoice.Id); err != nil {
		t.Fatalf("hide the real posting beyond the target invoice edge: %v", err)
	}
	var targetOwnerStorage, targetOwner, lineStorage, lineInvoiceID string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?", invoice.Id).
		Scan(&targetOwnerStorage, &targetOwner); err != nil {
		t.Fatal(err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(invoice_id),invoice_id FROM invoice_income_lines_v1 WHERE sequence=1").
		Scan(&lineStorage, &lineInvoiceID); err != nil {
		t.Fatal(err)
	}
	var matchingPosting, receiptCount, missingInvoiceCount, movedPosting int
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=?", invoice.Id).Scan(&matchingPosting); err != nil {
		t.Fatal(err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM invoice_payment_v1 WHERE invoice_id=?", invoice.Id).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM invoices_v1 WHERE id=?", missingInvoiceID).Scan(&missingInvoiceCount); err != nil {
		t.Fatal(err)
	}
	var movedPostingOwnerStorage, movedPostingOwner, movedPostingInvoiceStorage string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id,typeof(invoice_id) FROM invoice_income_postings_v1 WHERE invoice_id=?",
		missingInvoiceID).Scan(&movedPostingOwnerStorage, &movedPostingOwner, &movedPostingInvoiceStorage); err != nil {
		t.Fatal(err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=?", missingInvoiceID).Scan(&movedPosting); err != nil {
		t.Fatal(err)
	}
	if targetOwnerStorage != "text" || targetOwner != orgID || lineStorage != "text" || lineInvoiceID != invoice.Id ||
		matchingPosting != 0 || receiptCount != 0 || missingInvoiceCount != 0 || movedPosting != 1 ||
		movedPostingOwnerStorage != "text" || movedPostingOwner != foreignOwnerID || movedPostingInvoiceStorage != "text" {
		t.Fatalf("target invoice edge fixture lost isolation: target=%q/%q line=%q/%q posting=%d receipt=%d missingInvoice=%d moved=%d owner=%q/%q invoiceStorage=%q",
			targetOwnerStorage, targetOwner, lineStorage, lineInvoiceID, matchingPosting, receiptCount,
			missingInvoiceCount, movedPosting, movedPostingOwnerStorage, movedPostingOwner, movedPostingInvoiceStorage)
	}
}

func invoiceComponentOrphanMoveIncomeReference(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
	value string,
	asBlob bool,
) {
	t.Helper()
	fixture := sources.fixture
	invoiceComponentOrphanDeferForeignKeys(t, fixture)
	update := "UPDATE invoice_income_lines_v1 SET invoice_id=? WHERE sequence=1"
	if asBlob {
		update = "UPDATE invoice_income_lines_v1 SET invoice_id=CAST(? AS BLOB) WHERE sequence=1"
	}
	if err := invoiceReportingMutateWithTriggersRestored(
		fixture, "invoice_income_lines_v1", update, value,
	); err != nil {
		t.Fatalf("move only income-line invoice reference: %v", err)
	}
	if asBlob {
		invoiceComponentOrphanAssertBlobReference(t, fixture, "invoice_income_lines_v1", value, "invoices_v1")
	} else {
		invoiceComponentOrphanAssertMissingTextReference(t, fixture, "invoice_income_lines_v1", value, "invoices_v1")
	}
}

func invoiceComponentOrphanMoveAllocationReference(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
	value string,
	asBlob bool,
) {
	t.Helper()
	fixture := sources.fixture
	if sources.payment == nil {
		t.Fatal("allocation corruption fixture omitted its real writer payment")
	}
	invoiceComponentOrphanDeferForeignKeys(t, fixture)
	update := "UPDATE invoice_payment_lines_v1 SET payment_id=? WHERE sequence=1"
	if asBlob {
		update = "UPDATE invoice_payment_lines_v1 SET payment_id=CAST(? AS BLOB) WHERE sequence=1"
	}
	if err := invoiceReportingMutateWithTriggersRestored(
		fixture, "invoice_payment_lines_v1", update, value,
	); err != nil {
		t.Fatalf("move only allocation payment reference: %v", err)
	}
	if asBlob {
		invoiceComponentOrphanAssertBlobReference(t, fixture, "invoice_payment_lines_v1", value, "invoice_payment_v1")
	} else {
		invoiceComponentOrphanAssertMissingTextReference(t, fixture, "invoice_payment_lines_v1", value, "invoice_payment_v1")
	}
}

func invoiceComponentOrphanDeleteReceiptParent(t *testing.T, sources invoiceChildOwnerStaticSources) {
	t.Helper()
	fixture, payment := sources.fixture, sources.payment
	if payment == nil {
		t.Fatal("missing-receipt corruption fixture omitted its real writer payment")
	}
	invoiceComponentOrphanDeferForeignKeys(t, fixture)
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1",
		"DELETE FROM invoice_payment_v1 WHERE id=?", payment.Id); err != nil {
		t.Fatalf("delete the real receipt parent while retaining its allocation reference: %v", err)
	}
	var allocationStorage, allocationPaymentID string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(payment_id),payment_id FROM invoice_payment_lines_v1 WHERE sequence=1").
		Scan(&allocationStorage, &allocationPaymentID); err != nil {
		t.Fatal(err)
	}
	var receiptCount int
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM invoice_payment_v1 WHERE id=?", payment.Id).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if allocationStorage != "text" || allocationPaymentID != payment.Id || receiptCount != 0 {
		t.Fatalf("missing-receipt fixture allocation=%q/%q receiptCount=%d; want unchanged TEXT reference and absent parent",
			allocationStorage, allocationPaymentID, receiptCount)
	}
}

func invoiceComponentOrphanAssertMissingTextReference(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, reference, parentTable string,
) {
	t.Helper()
	refColumn, parentColumn := "invoice_id", "id"
	if table == "invoice_payment_lines_v1" {
		refColumn = "payment_id"
	}
	var storage, actual string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof("+refColumn+"),"+refColumn+" FROM "+table+" WHERE sequence=1").
		Scan(&storage, &actual); err != nil {
		t.Fatal(err)
	}
	var parentCount int
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM "+parentTable+" WHERE "+parentColumn+"=?", reference).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	postingCount := 0
	if table == "invoice_income_lines_v1" {
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=?", reference).Scan(&postingCount); err != nil {
			t.Fatal(err)
		}
	}
	if storage != "text" || actual != reference || parentCount != 0 || postingCount != 0 {
		t.Fatalf("%s reference storage/value=%q/%q parentCount=%d postingParentCount=%d; want missing TEXT parent and posting %q",
			table, storage, actual, parentCount, postingCount, reference)
	}
}

func invoiceComponentOrphanAssertBlobReference(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, canonicalParentID, parentTable string,
) {
	t.Helper()
	refColumn, parentColumn := "invoice_id", "id"
	if table == "invoice_payment_lines_v1" {
		refColumn = "payment_id"
	}
	var storage string
	var actual []byte
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof("+refColumn+"),CAST("+refColumn+" AS BLOB) FROM "+table+" WHERE sequence=1").
		Scan(&storage, &actual); err != nil {
		t.Fatal(err)
	}
	var textParentCount, blobEqualityCount int
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM "+parentTable+" WHERE "+parentColumn+"=?", canonicalParentID).Scan(&textParentCount); err != nil {
		t.Fatal(err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM "+parentTable+" WHERE "+parentColumn+"=CAST(? AS BLOB)", canonicalParentID).Scan(&blobEqualityCount); err != nil {
		t.Fatal(err)
	}
	postingTextCount, postingBlobEqualityCount := 0, 0
	if table == "invoice_income_lines_v1" {
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=?", canonicalParentID).Scan(&postingTextCount); err != nil {
			t.Fatal(err)
		}
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=CAST(? AS BLOB)", canonicalParentID).Scan(&postingBlobEqualityCount); err != nil {
			t.Fatal(err)
		}
	}
	if storage != "blob" || !bytes.Equal(actual, []byte(canonicalParentID)) ||
		textParentCount != 1 || blobEqualityCount != 0 ||
		(table == "invoice_income_lines_v1" && (postingTextCount != 1 || postingBlobEqualityCount != 0)) {
		t.Fatalf("%s BLOB reference=%q/%x textParents=%d BLOB-equal parents=%d postingTEXT=%d postingBLOB-equal=%d",
			table, storage, actual, textParentCount, blobEqualityCount, postingTextCount, postingBlobEqualityCount)
	}
}
