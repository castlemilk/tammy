//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestInvoiceChildOwnerWriterControlPreservesExactScalarAndFullSources(t *testing.T) {
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
		keys[0].PostingInvoiceVersion != nil || keys[0].ReceiptID != nil ||
		keys[0].JournalID != nil || len(keys[0].SourceHash) != 0 {
		t.Fatalf("root key did not preserve its version-only identity: %#v", keys[0])
	}
	if keys[1].PostingInvoiceVersion == nil || *keys[1].PostingInvoiceVersion != invoice.Version ||
		keys[1].JournalID == nil || *keys[1].JournalID != postingJournalID ||
		!bytes.Equal(keys[1].SourceHash, invoiceHash[:]) ||
		keys[1].RootVersion != nil || keys[1].ReceiptID != nil {
		t.Fatalf("posting key did not preserve version, journal and exact invoice provenance: %#v", keys[1])
	}
	if keys[2].Sequence == nil || *keys[2].Sequence != 1 ||
		keys[2].RootVersion != nil || keys[2].PostingInvoiceVersion != nil ||
		keys[2].ReceiptID != nil || keys[2].JournalID != nil || len(keys[2].SourceHash) != 0 {
		t.Fatalf("income-line key did not preserve its exact sequence identity: %#v", keys[2])
	}
	if keys[3].ReceiptID == nil || *keys[3].ReceiptID != payment.Id ||
		keys[3].JournalID == nil || *keys[3].JournalID != payment.JournalId ||
		!bytes.Equal(keys[3].SourceHash, paymentHash[:]) || keys[3].Sequence != nil {
		t.Fatalf("receipt key did not preserve exact payment and journal provenance: %#v", keys[3])
	}
	if keys[4].ReceiptID == nil || *keys[4].ReceiptID != payment.Id ||
		keys[4].Sequence == nil || *keys[4].Sequence != 1 ||
		keys[4].JournalID != nil || len(keys[4].SourceHash) != 0 {
		t.Fatalf("allocation key did not preserve its exact receipt/sequence identity: %#v", keys[4])
	}

	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 1 ||
		preflight.ReceiptCount != 1 || preflight.JournalCount != 2 ||
		len(preflight.JournalIDs) != 2 || len(preflight.JournalPayloadBytes) != 2 {
		t.Fatalf("real-writer full preflight=%#v err=%v; want one candidate, one receipt and two unique journals", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 1 ||
		capture.ReceiptCount != 1 || capture.JournalCount != 2 || len(capture.Candidates) != 1 ||
		capture.BasePayloadBytes != preflight.BasePayloadBytes ||
		capture.PayloadBytes != preflight.PayloadBytes ||
		!equalInvoiceReportingStrings(capture.JournalIDs, preflight.JournalIDs) ||
		!equalInvoiceReportingJournalBytes(capture.JournalPayloadBytes, preflight.JournalPayloadBytes) {
		t.Fatalf("real-writer full capture=%#v err=%v; want exact preflight counts and payload reservations", capture, err)
	}
	assertInvoiceReportingCandidateSource(t, capture.Candidates)
	candidate := capture.Candidates[0]
	if candidate.ID != invoice.Id || candidate.OrganisationID != orgID ||
		!proto.Equal(candidate.Invoice, invoice) || candidate.Posting == nil ||
		len(candidate.Receipts) != 1 {
		t.Fatalf("full capture lost exact writer invoice/posting/receipt lineage: %#v", candidate)
	}
	assertInvoiceReportingPostingJournal(t, candidate, "RECEIVABLE", invoice.Id, invoice.Version)
	invoiceChildOwnerStaticAssertReceipt(t, candidate, payment)
	assertInvoiceReportingReservationMetadata(t, capture.Candidates, capture.CandidateCount,
		capture.ReceiptCount, capture.JournalCount, capture.JournalIDs,
		capture.BasePayloadBytes, capture.PayloadBytes, capture.JournalPayloadBytes)

	wantBasePayload, ok := invoiceChildOwnerStaticExpectedBasePayload(capture)
	if !ok || capture.BasePayloadBytes != wantBasePayload {
		t.Fatalf("full capture base payload=%d want exact source-derived charge %d (valid=%t)",
			capture.BasePayloadBytes, wantBasePayload, ok)
	}
	var parentBytes int64
	for _, charge := range preflight.JournalPayloadBytes {
		if charge <= 0 {
			t.Fatalf("journal has nonpositive retained payload charge: %#v", preflight.JournalPayloadBytes)
		}
		parentBytes += charge
	}
	if preflight.PayloadBytes != wantBasePayload+parentBytes {
		t.Fatalf("full payload=%d; exact source base %d + unique journal payloads %d",
			preflight.PayloadBytes, wantBasePayload, parentBytes)
	}
	if invoiceReportingBudgetState(t, fixture) != stateBefore || !fixture.tx.Authenticated() {
		t.Fatal("real-writer scalar or full reporting reads changed retained state or caller transaction")
	}
}

func TestInvoiceChildOwnerScalarRejectsSameByteBlobWithoutTargetParents(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			fixture := sources.fixture
			foreignOwner := invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
			expected := invoiceChildOwnerStaticAssertEmptyScalar(t, fixture)

			if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID); err != nil {
				t.Fatal(err)
			}
			invoiceChildOwnerStaticAssertBlobOwner(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID)
			invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t, fixture)
			if foreignOwner == orgID {
				t.Fatal("foreign parent fixture accidentally used the target organisation")
			}
			afterCorruption := invoiceReportingBudgetState(t, fixture)

			probe := &invoiceBASScalarProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
			if summary != nil || !errors.Is(err, ErrRepository) {
				t.Fatalf("same-byte BLOB-owned child with foreign parents was omitted: summary=%#v err=%v", summary, err)
			}
			keys, err := port.ReadBASKeys(fixture.ctx, orgID, expected, 0, 0)
			if keys != nil || !errors.Is(err, ErrRepository) {
				t.Fatalf("same-byte BLOB-owned child read returned keys=%#v err=%v; want rejection without partial keys", keys, err)
			}
			if probe.summaryReads != 2 || probe.keyReads != 0 || probe.bodyReads != 0 ||
				invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatalf("static rejection used summaries/keys/bodies=%d/%d/%d or changed state/caller transaction",
					probe.summaryReads, probe.keyReads, probe.bodyReads)
			}
		})
	}
}

func TestInvoiceChildOwnerScalarRejectsBlobOwnerDriftAfterFreshEmptySummary(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			fixture := sources.fixture
			invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)

			probe := &invoiceBASScalarProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := port.SummarizeBASKeys(fixture.ctx, orgID)
			empty := BASScalarSummary{OrganisationID: orgID}
			if err != nil || expected == nil || *expected != empty {
				t.Fatalf("foreign-parent empty baseline=%#v err=%v", expected, err)
			}

			var afterCorruption [8]int64
			probe.beforeKeyRead = func() error {
				if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID); err != nil {
					return err
				}
				invoiceChildOwnerStaticAssertBlobOwner(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID)
				invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t, fixture)
				afterCorruption = invoiceReportingBudgetState(t, fixture)
				return nil
			}
			keys, err := port.ReadBASKeys(fixture.ctx, orgID, *expected, 0, 0)
			var bound *SourceBoundError
			if keys != nil || !errors.Is(err, ErrSourceBound) || !errors.As(err, &bound) ||
				bound.Resource != "BAS scalar keys" || bound.Limit != 0 {
				t.Fatalf("same-byte BLOB child drift missed the zero-key sentinel: keys=%#v err=%v", keys, err)
			}
			if probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 ||
				invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatalf("drift guard used summaries/keys/bodies=%d/%d/%d or changed state/caller transaction",
					probe.summaryReads, probe.keyReads, probe.bodyReads)
			}
		})
	}
}

func TestInvoiceChildOwnerLeavesUnrelatedBlobOwnerWithForeignParentsOutOfScope(t *testing.T) {
	sources := invoiceChildOwnerStaticNewWriter(t, true)
	fixture, invoice, payment := sources.fixture, sources.invoice, sources.payment
	foreignOwner := invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
	foreignBlobOwner := invoiceReportingID(99902)
	if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, "invoice_income_postings_v1",
		invoice.Id, foreignBlobOwner); err != nil {
		t.Fatal(err)
	}
	if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, "invoice_payment_v1",
		invoiceChildOwnerStaticRowID(t, sources, "invoice_payment_v1"), foreignBlobOwner); err != nil {
		t.Fatal(err)
	}
	invoiceChildOwnerStaticAssertBlobOwner(t, fixture, "invoice_income_postings_v1", invoice.Id, foreignBlobOwner)
	invoiceChildOwnerStaticAssertBlobOwner(t, fixture, "invoice_payment_v1",
		invoiceChildOwnerStaticRowID(t, sources, "invoice_payment_v1"), foreignBlobOwner)
	invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t, fixture)
	if foreignOwner == orgID || payment == nil {
		t.Fatal("foreign BLOB control lacks its distinct foreign parent graph or real payment")
	}
	afterCorruption := invoiceReportingBudgetState(t, fixture)

	invoiceChildOwnerStaticAssertEmptyScalar(t, fixture)
	invoiceChildOwnerStaticAssertEmptyFull(t, fixture)
	if invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
		t.Fatal("unrelated BLOB UUID with foreign parents changed retained state or caller transaction")
	}
}

func TestInvoiceChildOwnerFullPreflightRejectsSameByteBlobWithoutTargetParents(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			fixture := sources.fixture
			invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
			invoiceChildOwnerStaticAssertEmptyFull(t, fixture)

			if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID); err != nil {
				t.Fatal(err)
			}
			invoiceChildOwnerStaticAssertBlobOwner(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID)
			invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t, fixture)
			afterCorruption := invoiceReportingBudgetState(t, fixture)

			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			preflight, err := port.PreflightBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
			if preflight != nil || !errors.Is(err, ErrRepository) ||
				probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
				t.Fatalf("same-byte BLOB child with foreign parents preflight=%#v err=%v bodyQueries=%d",
					preflight, err, probe.hydrationQueries)
			}
			if invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatal("full preflight rejection changed retained state or caller transaction")
			}
		})
	}
}

func TestInvoiceChildOwnerFullCaptureRejectsSameByteBlobWithoutTargetParents(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			fixture := sources.fixture
			invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
			invoiceChildOwnerStaticAssertEmptyFull(t, fixture)

			if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID); err != nil {
				t.Fatal(err)
			}
			invoiceChildOwnerStaticAssertBlobOwner(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID)
			invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t, fixture)
			afterCorruption := invoiceReportingBudgetState(t, fixture)

			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			capture, err := port.CaptureBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
			if capture != nil || !errors.Is(err, ErrRepository) ||
				probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
				t.Fatalf("same-byte BLOB child with foreign parents capture=%#v err=%v bodyQueries=%d",
					capture, err, probe.hydrationQueries)
			}
			if invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatal("full capture rejection changed retained state or caller transaction")
			}
		})
	}
}

func TestInvoiceChildOwnerFullRejectsTargetOwnerWithMissingInvoiceParent(t *testing.T) {
	for _, family := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		for _, storage := range []string{"same-byte-blob", "target-text"} {
			for _, api := range []string{"preflight", "capture"} {
				name := family.name + "/" + storage + "/" + api
				t.Run(name, func(t *testing.T) {
					sources := invoiceChildOwnerStaticNewWriter(t, family.withPayment)
					fixture := sources.fixture
					foreignOwner := invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
					missingInvoiceID := invoiceReportingID(99903)
					if err := invoiceChildOwnerStaticSetMissingInvoiceParent(
						t, sources, family.table, missingInvoiceID); err != nil {
						t.Fatal(err)
					}
					rowID := invoiceChildOwnerStaticRowID(t, sources, family.table)
					if family.table == "invoice_income_postings_v1" {
						rowID = missingInvoiceID
					}
					invoiceChildOwnerStaticAssertMissingInvoiceParent(
						t, fixture, family.table, rowID, missingInvoiceID)
					invoiceChildOwnerStaticAssertForeignParentsForChild(
						t, sources, family.table, foreignOwner)
					switch storage {
					case "same-byte-blob":
						if err := invoiceChildOwnerStaticSetOwnerBlob(
							t, fixture, family.table, rowID, orgID); err != nil {
							t.Fatal(err)
						}
						invoiceChildOwnerStaticAssertBlobOwner(
							t, fixture, family.table, rowID, orgID)
					case "target-text":
						if err := invoiceChildOwnerStaticSetOwnerText(
							t, fixture, family.table, rowID, orgID); err != nil {
							t.Fatal(err)
						}
						invoiceChildOwnerStaticAssertTextOwner(
							t, fixture, family.table, rowID, orgID)
					}
					afterCorruption := invoiceReportingBudgetState(t, fixture)

					probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
					port, err := NewReportingPort(probe)
					if err != nil {
						t.Fatal(err)
					}
					if api == "preflight" {
						result, err := port.PreflightBASWithPayloadBudget(
							fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
						if result != nil || !errors.Is(err, ErrRepository) ||
							probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
							t.Fatalf("missing-parent %s %s preflight=%#v err=%v bodyQueries=%d",
								storage, family.name, result, err, probe.hydrationQueries)
						}
					} else {
						result, err := port.CaptureBASWithPayloadBudget(
							fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
						if result != nil || !errors.Is(err, ErrRepository) ||
							probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
							t.Fatalf("missing-parent %s %s capture=%#v err=%v bodyQueries=%d",
								storage, family.name, result, err, probe.hydrationQueries)
						}
					}
					if invoiceReportingBudgetState(t, fixture) != afterCorruption ||
						!fixture.tx.Authenticated() {
						t.Fatal("missing-parent full rejection changed retained state or caller transaction")
					}
				})
			}
		}
	}
}

func TestInvoiceChildOwnerFullPreflightRejectsTargetTextOwnerWithForeignParents(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			fixture := sources.fixture
			foreignOwner := invoiceChildOwnerStaticMoveWriterGraphToForeign(t, sources)
			invoiceChildOwnerStaticAssertEmptyFull(t, fixture)
			rowID := invoiceChildOwnerStaticRowID(t, sources, testCase.table)
			if err := invoiceChildOwnerStaticSetOwnerText(t, fixture, testCase.table, rowID, orgID); err != nil {
				t.Fatal(err)
			}
			invoiceChildOwnerStaticAssertTextOwner(t, fixture, testCase.table, rowID, orgID)
			invoiceChildOwnerStaticAssertForeignParentsForChild(t, sources, testCase.table, foreignOwner)
			afterCorruption := invoiceReportingBudgetState(t, fixture)

			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			preflight, err := port.PreflightBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
			if preflight != nil || !errors.Is(err, ErrRepository) ||
				probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
				t.Fatalf("target-TEXT child with foreign parents preflight=%#v err=%v bodyQueries=%d",
					preflight, err, probe.hydrationQueries)
			}
			if invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatal("target-TEXT child foreign-parent rejection changed retained state or caller transaction")
			}
		})
	}
}

func TestInvoiceChildOwnerFullPreflightAndCaptureRejectBlobOwnerUnderCanonicalTargetParent(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		table       string
		withPayment bool
	}{
		{name: "posting", table: "invoice_income_postings_v1"},
		{name: "receipt", table: "invoice_payment_v1", withPayment: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, testCase.withPayment)
			fixture := sources.fixture
			if err := invoiceChildOwnerStaticSetOwnerBlob(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID); err != nil {
				t.Fatal(err)
			}
			invoiceChildOwnerStaticAssertBlobOwner(t, fixture, testCase.table, invoiceChildOwnerStaticRowID(t, sources, testCase.table), orgID)
			afterCorruption := invoiceReportingBudgetState(t, fixture)

			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			preflight, err := port.PreflightBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
			if preflight != nil || !errors.Is(err, ErrRepository) ||
				probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
				t.Fatalf("same-byte BLOB child under canonical target parent preflight=%#v err=%v bodyQueries=%d",
					preflight, err, probe.hydrationQueries)
			}
			capture, err := port.CaptureBASWithPayloadBudget(
				fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
			if capture != nil || !errors.Is(err, ErrRepository) ||
				probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
				t.Fatalf("same-byte BLOB child under canonical target parent capture=%#v err=%v bodyQueries=%d",
					capture, err, probe.hydrationQueries)
			}
			if invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatal("canonical-parent full rejection changed retained state or caller transaction")
			}
		})
	}
}

func TestInvoiceChildOwnerFullRejectsTargetTextReceiptWithoutPostingParent(t *testing.T) {
	for _, api := range []string{"preflight", "capture"} {
		t.Run(api, func(t *testing.T) {
			sources := invoiceChildOwnerStaticNewWriter(t, true)
			fixture, invoice, payment := sources.fixture, sources.invoice, sources.payment
			if payment == nil {
				t.Fatal("required-parent fixture omitted its real invoice payment")
			}
			if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
				t.Fatal(err)
			}
			if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_lines_v1",
				"DELETE FROM invoice_income_lines_v1 WHERE invoice_id=?", invoice.Id); err != nil {
				t.Fatal(err)
			}
			if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_postings_v1",
				"DELETE FROM invoice_income_postings_v1 WHERE invoice_id=?", invoice.Id); err != nil {
				t.Fatal(err)
			}
			var invoiceStorage, invoiceOwner, receiptStorage, receiptOwner string
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?", invoice.Id).
				Scan(&invoiceStorage, &invoiceOwner); err != nil {
				t.Fatal(err)
			}
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT typeof(organisation_id),organisation_id FROM invoice_payment_v1 WHERE id=?", payment.Id).
				Scan(&receiptStorage, &receiptOwner); err != nil {
				t.Fatal(err)
			}
			var postingCount, incomeLineCount int
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT count(*) FROM invoice_income_lines_v1 WHERE invoice_id=?", invoice.Id).
				Scan(&incomeLineCount); err != nil {
				t.Fatal(err)
			}
			if err := fixture.tx.QueryRowContext(fixture.ctx,
				"SELECT count(*) FROM invoice_income_postings_v1 WHERE invoice_id=?", invoice.Id).
				Scan(&postingCount); err != nil {
				t.Fatal(err)
			}
			if invoiceStorage != "text" || invoiceOwner != orgID || receiptStorage != "text" ||
				receiptOwner != orgID || incomeLineCount != 0 || postingCount != 0 {
				t.Fatalf("required-parent fixture storage/owners/line/posting state=%q/%q/%q/%q/%d/%d; want target TEXT owners and absent line/posting",
					invoiceStorage, invoiceOwner, receiptStorage, receiptOwner, incomeLineCount, postingCount)
			}
			afterCorruption := invoiceReportingBudgetState(t, fixture)

			probe := &invoiceReportingBudgetProbe{Transaction: fixture.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			if api == "preflight" {
				result, err := port.PreflightBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
				if result != nil || !errors.Is(err, ErrRepository) ||
					probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
					t.Fatalf("target receipt with missing posting parent preflight=%#v err=%v bodyQueries=%d",
						result, err, probe.hydrationQueries)
				}
			} else {
				result, err := port.CaptureBASWithPayloadBudget(
					fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
				if result != nil || !errors.Is(err, ErrRepository) ||
					probe.hydrationQueries != 0 || probe.mutatedBeforeFirstHydration {
					t.Fatalf("target receipt with missing posting parent capture=%#v err=%v bodyQueries=%d",
						result, err, probe.hydrationQueries)
				}
			}
			if invoiceReportingBudgetState(t, fixture) != afterCorruption || !fixture.tx.Authenticated() {
				t.Fatal("required-parent full rejection changed retained state or caller transaction")
			}
		})
	}
}

type invoiceChildOwnerStaticSources struct {
	fixture *invoiceReportingFixture
	invoice *tammyv1.Invoice
	payment *tammyv1.InvoicePayment
}

func invoiceChildOwnerStaticNewWriter(t *testing.T, withPayment bool) invoiceChildOwnerStaticSources {
	t.Helper()
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := fixture.saveDraft(t, invoiceReportingID(99801), "CHILD-OWNER-SCOPE", "2026-10-12", 391,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)
	var payment *tammyv1.InvoicePayment
	if withPayment {
		payment = fixture.recordPayment(t, invoice, 1, 1, 1)
	}
	return invoiceChildOwnerStaticSources{fixture: fixture, invoice: invoice, payment: payment}
}

func invoiceChildOwnerStaticRowID(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
	table string,
) string {
	t.Helper()
	switch table {
	case "invoice_income_postings_v1":
		return sources.invoice.Id
	case "invoice_payment_v1":
		if sources.payment == nil {
			t.Fatal("receipt row requested from a writer fixture without a payment")
		}
		return sources.payment.Id
	default:
		t.Fatalf("unsupported child owner table %q", table)
		return ""
	}
}

func invoiceChildOwnerStaticAssertTextOwner(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, rowID, want string,
) {
	t.Helper()
	var query string
	switch table {
	case "invoice_income_postings_v1":
		query = "SELECT typeof(organisation_id),organisation_id FROM invoice_income_postings_v1 WHERE invoice_id=?"
	case "invoice_payment_v1":
		query = "SELECT typeof(organisation_id),organisation_id FROM invoice_payment_v1 WHERE id=?"
	default:
		t.Fatalf("unsupported child owner table %q", table)
	}
	var storage, owner string
	if err := fixture.tx.QueryRowContext(fixture.ctx, query, rowID).Scan(&storage, &owner); err != nil {
		t.Fatal(err)
	}
	if storage != "text" || owner != want {
		t.Fatalf("%s owner storage/value=%q/%q; want TEXT/%q", table, storage, owner, want)
	}
}

func invoiceChildOwnerStaticAssertForeignParentsForChild(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
	table, foreignOwner string,
) {
	t.Helper()
	fixture, invoice := sources.fixture, sources.invoice
	var invoiceStorage, invoiceOwner string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?", invoice.Id).
		Scan(&invoiceStorage, &invoiceOwner); err != nil {
		t.Fatal(err)
	}
	if invoiceStorage != "text" || invoiceOwner != foreignOwner || foreignOwner == orgID {
		t.Fatalf("invoice parent storage/owner=%q/%q; want foreign TEXT owner %q", invoiceStorage, invoiceOwner, foreignOwner)
	}
	if table == "invoice_payment_v1" {
		var postingStorage, postingOwner string
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT typeof(organisation_id),organisation_id FROM invoice_income_postings_v1 WHERE invoice_id=?", invoice.Id).
			Scan(&postingStorage, &postingOwner); err != nil {
			t.Fatal(err)
		}
		if postingStorage != "text" || postingOwner != foreignOwner {
			t.Fatalf("receipt parent posting storage/owner=%q/%q; want foreign TEXT owner %q",
				postingStorage, postingOwner, foreignOwner)
		}
	}
}

func invoiceChildOwnerStaticSetMissingInvoiceParent(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
	table, missingInvoiceID string,
) error {
	t.Helper()
	fixture, invoice := sources.fixture, sources.invoice
	if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return err
	}
	switch table {
	case "invoice_income_postings_v1":
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_lines_v1",
			"DELETE FROM invoice_income_lines_v1 WHERE invoice_id=?", invoice.Id); err != nil {
			return err
		}
		return invoiceReportingMutateWithTriggersRestored(fixture, table,
			"UPDATE invoice_income_postings_v1 SET invoice_id=? WHERE invoice_id=?", missingInvoiceID, invoice.Id)
	case "invoice_payment_v1":
		if sources.payment == nil {
			return ErrRepository
		}
		return invoiceReportingMutateWithTriggersRestored(fixture, table,
			"UPDATE invoice_payment_v1 SET invoice_id=? WHERE id=?", missingInvoiceID, sources.payment.Id)
	default:
		return ErrRepository
	}
}

func invoiceChildOwnerStaticAssertMissingInvoiceParent(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, rowID, missingInvoiceID string,
) {
	t.Helper()
	var query string
	switch table {
	case "invoice_income_postings_v1":
		query = "SELECT invoice_id FROM invoice_income_postings_v1 WHERE invoice_id=?"
	case "invoice_payment_v1":
		query = "SELECT invoice_id FROM invoice_payment_v1 WHERE id=?"
	default:
		t.Fatalf("unsupported child owner table %q", table)
	}
	var actualParentID string
	if err := fixture.tx.QueryRowContext(fixture.ctx, query, rowID).Scan(&actualParentID); err != nil {
		t.Fatal(err)
	}
	if actualParentID != missingInvoiceID {
		t.Fatalf("%s parent invoice ID=%q; want missing %q", table, actualParentID, missingInvoiceID)
	}
	var parentCount int
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT count(*) FROM invoices_v1 WHERE id=?", missingInvoiceID).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	if parentCount != 0 {
		t.Fatalf("missing invoice parent %s unexpectedly exists (%d rows)", missingInvoiceID, parentCount)
	}
}

func invoiceChildOwnerStaticAssertReceipt(
	t *testing.T,
	candidate BASInvoiceCandidate,
	payment *tammyv1.InvoicePayment,
) {
	t.Helper()
	if len(candidate.Receipts) != 1 || payment == nil {
		t.Fatalf("full writer capture has receipts=%d payment=%t", len(candidate.Receipts), payment != nil)
	}
	receipt := candidate.Receipts[0]
	if receipt.ID != payment.Id || receipt.OrganisationID != orgID ||
		receipt.InvoiceID != candidate.ID || receipt.PaymentDate != "2026-10-12" ||
		receipt.AmountMinor != 1 || receipt.BankAccountID != templateBankID() ||
		receipt.Reference != payment.Reference || receipt.JournalID != payment.JournalId ||
		receipt.Payment == nil || !proto.Equal(receipt.Payment, payment) ||
		receipt.Payment.Id != receipt.ID || receipt.Payment.OrganisationId != orgID ||
		receipt.Payment.InvoiceId != candidate.ID || receipt.Payment.Amount == nil ||
		receipt.Payment.Amount.MinorUnits != receipt.AmountMinor ||
		receipt.Payment.AllocatedGst == nil ||
		receipt.Payment.AllocatedGst.MinorUnits != receipt.AllocatedGSTMinor ||
		receipt.Payment.BasSalesGross == nil ||
		receipt.Payment.BasSalesGross.MinorUnits != receipt.BASSalesGrossMinor ||
		receipt.Payment.BankAccountId != receipt.BankAccountID ||
		receipt.Payment.JournalId != receipt.JournalID ||
		receipt.Payment.Reference != receipt.Reference || receipt.CreatedAt == "" ||
		len(receipt.PaymentProto) == 0 || len(receipt.SourceHash) != sha256.Size ||
		len(receipt.PaymentHash) != sha256.Size {
		t.Fatalf("full receipt lost exact row, protobuf, or payment provenance: %#v", receipt)
	}
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payment)
	if err != nil || !bytes.Equal(receipt.PaymentProto, encoded) {
		t.Fatalf("receipt did not preserve the exact deterministic payment proto: err=%v", err)
	}
	hash := sha256.Sum256(receipt.PaymentProto)
	if !bytes.Equal(receipt.PaymentHash, hash[:]) || !bytes.Equal(receipt.SourceHash, hash[:]) {
		t.Fatalf("receipt payload/source hashes=%x/%x; want exact payload hash %x",
			receipt.PaymentHash, receipt.SourceHash, hash)
	}
	if len(receipt.Allocations) != 1 || receipt.Allocations[0].Sequence != 1 ||
		receipt.Allocations[0].GrossMinor != receipt.AmountMinor ||
		receipt.Allocations[0].GSTMinor < 0 ||
		receipt.Allocations[0].GSTMinor > receipt.Allocations[0].GrossMinor {
		t.Fatalf("receipt allocation provenance is incomplete: %#v", receipt.Allocations)
	}
	assertInvoiceReportingJournalProof(t, receipt.Journal, "BANKING", receipt.ID, 1,
		tammyv1.JournalSource_JOURNAL_SOURCE_INVOICE_PAYMENT)
}

func invoiceChildOwnerStaticExpectedBasePayload(capture *BASReportingCapture) (int64, bool) {
	if capture == nil {
		return 0, false
	}
	var total int64
	for _, candidate := range capture.Candidates {
		if !addBASReportingRowPayload(&total, len(candidate.ID), len(candidate.OrganisationID),
			len(candidate.State), len(candidate.Number), len(candidate.InvoiceProto),
			len(candidate.CreatedAt), len(candidate.UpdatedAt)) {
			return 0, false
		}
		if posting := candidate.Posting; posting != nil {
			journalIDBytes := 0
			if posting.JournalID != nil {
				journalIDBytes = len(*posting.JournalID)
			}
			if !addBASReportingRowPayload(&total, len(posting.InvoiceID), len(posting.OrganisationID),
				len(posting.InvoiceDate), len(posting.RevenueAccountID), journalIDBytes,
				len(posting.SourceHash), len(posting.CreatedAt)) {
				return 0, false
			}
			for _, line := range posting.IncomeLines {
				if !addBASReportingRowPayload(&total, len(line.InvoiceID)) {
					return 0, false
				}
			}
		}
		for _, receipt := range candidate.Receipts {
			if !addBASReportingRowPayload(&total, len(receipt.ID), len(receipt.OrganisationID),
				len(receipt.InvoiceID), len(receipt.PaymentDate), len(receipt.BankAccountID),
				len(receipt.JournalID), len(receipt.Reference), len(receipt.PaymentProto),
				len(receipt.SourceHash), len(receipt.CreatedAt)) {
				return 0, false
			}
			for _, allocation := range receipt.Allocations {
				if !addBASReportingRowPayload(&total, len(allocation.PaymentID)) {
					return 0, false
				}
			}
		}
	}
	return total, true
}

func invoiceChildOwnerStaticMoveWriterGraphToForeign(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
) string {
	t.Helper()
	fixture, invoice := sources.fixture, sources.invoice
	foreignOwner := invoiceReportingID(99901)
	if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatalf("defer corruption-only foreign owner references: %v", err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoices_v1",
		"UPDATE invoices_v1 SET organisation_id=? WHERE id=?", foreignOwner, invoice.Id); err != nil {
		t.Fatalf("move invoice parent to a foreign TEXT owner: %v", err)
	}
	if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_income_postings_v1",
		"UPDATE invoice_income_postings_v1 SET organisation_id=? WHERE invoice_id=?", foreignOwner, invoice.Id); err != nil {
		t.Fatalf("move posting parent to the foreign TEXT owner: %v", err)
	}
	if sources.payment != nil {
		if err := invoiceReportingMutateWithTriggersRestored(fixture, "invoice_payment_v1",
			"UPDATE invoice_payment_v1 SET organisation_id=? WHERE id=?", foreignOwner, sources.payment.Id); err != nil {
			t.Fatalf("move receipt parent to the foreign TEXT owner: %v", err)
		}
	}
	invoiceChildOwnerStaticAssertForeignGraph(t, sources, foreignOwner)
	return foreignOwner
}

func invoiceChildOwnerStaticAssertForeignGraph(
	t *testing.T,
	sources invoiceChildOwnerStaticSources,
	foreignOwner string,
) {
	t.Helper()
	fixture, invoice := sources.fixture, sources.invoice
	var invoiceStorage, invoiceOwner, postingStorage, postingOwner string
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id FROM invoices_v1 WHERE id=?", invoice.Id).
		Scan(&invoiceStorage, &invoiceOwner); err != nil {
		t.Fatal(err)
	}
	if err := fixture.tx.QueryRowContext(fixture.ctx,
		"SELECT typeof(organisation_id),organisation_id FROM invoice_income_postings_v1 WHERE invoice_id=?", invoice.Id).
		Scan(&postingStorage, &postingOwner); err != nil {
		t.Fatal(err)
	}
	if invoiceStorage != "text" || postingStorage != "text" ||
		invoiceOwner != foreignOwner || postingOwner != foreignOwner || foreignOwner == orgID {
		t.Fatalf("foreign parent graph storage/owners invoice=%q/%q posting=%q/%q target=%q",
			invoiceStorage, invoiceOwner, postingStorage, postingOwner, orgID)
	}
	if sources.payment != nil {
		var receiptStorage, receiptOwner string
		if err := fixture.tx.QueryRowContext(fixture.ctx,
			"SELECT typeof(organisation_id),organisation_id FROM invoice_payment_v1 WHERE id=?", sources.payment.Id).
			Scan(&receiptStorage, &receiptOwner); err != nil {
			t.Fatal(err)
		}
		if receiptStorage != "text" || receiptOwner != foreignOwner {
			t.Fatalf("foreign receipt storage/owner=%q/%q want TEXT/%q", receiptStorage, receiptOwner, foreignOwner)
		}
	}
}

func invoiceChildOwnerStaticAssertNoTargetTextOwnerEdges(t *testing.T, fixture *invoiceReportingFixture) {
	t.Helper()
	for _, query := range []string{
		"SELECT count(*) FROM invoices_v1 WHERE typeof(organisation_id)='text' AND organisation_id=?",
		"SELECT count(*) FROM invoice_income_postings_v1 WHERE typeof(organisation_id)='text' AND organisation_id=?",
		"SELECT count(*) FROM invoice_payment_v1 WHERE typeof(organisation_id)='text' AND organisation_id=?",
	} {
		var count int
		if err := fixture.tx.QueryRowContext(fixture.ctx, query, orgID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("corruption fixture retained %d target TEXT owner edges for query %q", count, query)
		}
	}
}

func invoiceChildOwnerStaticSetOwnerBlob(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, rowID, owner string,
) error {
	t.Helper()
	if _, err := fixture.tx.ExecContext(fixture.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return err
	}
	switch table {
	case "invoice_income_postings_v1":
		return invoiceReportingMutateWithTriggersRestored(fixture, table,
			"UPDATE invoice_income_postings_v1 SET organisation_id=CAST(? AS BLOB) WHERE invoice_id=?",
			owner, rowID)
	case "invoice_payment_v1":
		return invoiceReportingMutateWithTriggersRestored(fixture, table,
			"UPDATE invoice_payment_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?",
			owner, rowID)
	default:
		return ErrRepository
	}
}

func invoiceChildOwnerStaticSetOwnerText(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, rowID, owner string,
) error {
	t.Helper()
	switch table {
	case "invoice_income_postings_v1":
		return invoiceReportingMutateWithTriggersRestored(fixture, table,
			"UPDATE invoice_income_postings_v1 SET organisation_id=? WHERE invoice_id=?", owner, rowID)
	case "invoice_payment_v1":
		return invoiceReportingMutateWithTriggersRestored(fixture, table,
			"UPDATE invoice_payment_v1 SET organisation_id=? WHERE id=?", owner, rowID)
	default:
		return ErrRepository
	}
}

func invoiceChildOwnerStaticAssertBlobOwner(
	t *testing.T,
	fixture *invoiceReportingFixture,
	table, rowID, want string,
) {
	t.Helper()
	var query string
	switch table {
	case "invoice_income_postings_v1":
		query = "SELECT typeof(organisation_id),organisation_id FROM invoice_income_postings_v1 WHERE invoice_id=?"
	case "invoice_payment_v1":
		query = "SELECT typeof(organisation_id),organisation_id FROM invoice_payment_v1 WHERE id=?"
	default:
		t.Fatalf("unsupported child owner table %q", table)
	}
	var storage string
	var owner []byte
	if err := fixture.tx.QueryRowContext(fixture.ctx, query, rowID).Scan(&storage, &owner); err != nil {
		t.Fatal(err)
	}
	if storage != "blob" || len(owner) != 36 || !bytes.Equal(owner, []byte(want)) {
		t.Fatalf("%s owner storage/length/bytes=%q/%d/%x; want BLOB/36/%x",
			table, storage, len(owner), owner, []byte(want))
	}
}

func invoiceChildOwnerStaticAssertEmptyScalar(t *testing.T, fixture *invoiceReportingFixture) BASScalarSummary {
	t.Helper()
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
	empty := BASScalarSummary{OrganisationID: orgID}
	if err != nil || summary == nil || *summary != empty {
		t.Fatalf("foreign-parent empty scalar baseline=%#v err=%v", summary, err)
	}
	keys, err := port.ReadBASKeys(fixture.ctx, orgID, empty, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("foreign-parent empty scalar keys=%#v err=%v", keys, err)
	}
	return empty
}

func invoiceChildOwnerStaticAssertEmptyFull(t *testing.T, fixture *invoiceReportingFixture) {
	t.Helper()
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := port.PreflightBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), int64(maxBASReportingBytes))
	if err != nil || preflight == nil || preflight.CandidateCount != 0 ||
		preflight.ReceiptCount != 0 || preflight.JournalCount != 0 ||
		preflight.BasePayloadBytes != 0 || preflight.PayloadBytes != 0 ||
		len(preflight.JournalIDs) != 0 || len(preflight.JournalPayloadBytes) != 0 {
		t.Fatalf("foreign-parent empty full preflight=%#v err=%v", preflight, err)
	}
	capture, err := port.CaptureBASWithPayloadBudget(
		fixture.ctx, orgID, invoiceReportingPeriod(), preflight.PayloadBytes)
	if err != nil || capture == nil || capture.CandidateCount != 0 ||
		capture.ReceiptCount != 0 || capture.JournalCount != 0 ||
		capture.BasePayloadBytes != 0 || capture.PayloadBytes != 0 ||
		len(capture.Candidates) != 0 || len(capture.JournalIDs) != 0 ||
		len(capture.JournalPayloadBytes) != 0 {
		t.Fatalf("foreign-parent empty full capture=%#v err=%v", capture, err)
	}
}
