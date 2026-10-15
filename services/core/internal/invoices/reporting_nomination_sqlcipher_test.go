//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceBASNominationRealWritersAgreeWithCompleteCapture(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{
		tammyv1.GstBasis_GST_BASIS_CASH,
		tammyv1.GstBasis_GST_BASIS_NON_CASH,
	} {
		t.Run(basis.String(), func(t *testing.T) {
			f := newInvoiceReportingFixtureAt(t, basis, time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC))
			olderDraft := f.saveDraft(t, invoiceReportingID(96001), "NOMINATION-OLDER-DRAFT", "2026-09-30", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			undated := f.saveDraft(t, invoiceReportingID(96002), "NOMINATION-UNDATED", "", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			future := f.saveDraft(t, invoiceReportingID(96003), "NOMINATION-FUTURE", "2027-01-01", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			f.issue(t, future)
			unposted := f.saveDraft(t, invoiceReportingID(96004), "NOMINATION-UNPOSTED", "2026-10-02", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			f.issue(t, unposted)
			zero := f.saveDraft(t, invoiceReportingID(96005), "NOMINATION-ZERO", "2026-10-31", 0,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			f.issue(t, zero)
			f.post(t, zero)
			unpaid := f.saveDraft(t, invoiceReportingID(96006), "NOMINATION-UNPAID", "2026-10-01", 100,
				tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			f.issue(t, unpaid)
			f.post(t, unpaid)
			partPaid := f.saveDraftInput(t, invoiceReportingID(96007), &tammyv1.InvoiceDraft{
				Number: "NOMINATION-PART-PAID", IssueDate: "2026-09-01", DueDate: "2026-09-01",
				CustomerName: "Customer", CustomerAddress: "1 High Street", SellerAddress: "27 Cambridge Road",
				Lines: []*tammyv1.InvoiceLineInput{
					{Description: "First service", QuantityMilli: 1000, UnitPriceMinor: 100,
						TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
					{Description: "Second service", QuantityMilli: 1000, UnitPriceMinor: 200,
						TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
				},
			})
			f.issue(t, partPaid)
			f.post(t, partPaid)
			beforePeriod := f.recordPaymentOn(t, partPaid, 1, 1, 0, &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30})
			inPeriod := f.recordPaymentOn(t, partPaid, 2, 1, 1, &tammyv1.CivilDate{Year: 2026, Month: 10, Day: 12})
			afterPeriod := f.recordPaymentOn(t, partPaid, 3, 1, 2, &tammyv1.CivilDate{Year: 2026, Month: 11, Day: 1})
			if !(beforePeriod.Id < inPeriod.Id && inPeriod.Id < afterPeriod.Id) {
				t.Fatal("fixture receipt IDs must increase to make the expected nomination order explicit")
			}

			// These are exact identities expected from the writer operations above,
			// independent of the scalar keys and of the complete-reader projection.
			want := []BASCandidateKey{
				{Kind: BASCandidateInvoiceRoot, OrganisationID: orgID, InvoiceID: olderDraft.Id},
				{Kind: BASCandidateInvoiceRoot, OrganisationID: orgID, InvoiceID: undated.Id},
				{Kind: BASCandidateInvoiceRoot, OrganisationID: orgID, InvoiceID: future.Id},
				{Kind: BASCandidateInvoiceRoot, OrganisationID: orgID, InvoiceID: unposted.Id},
				{Kind: BASCandidateIncomeLine, OrganisationID: orgID, InvoiceID: zero.Id, Sequence: 1},
				{Kind: BASCandidateIncomeLine, OrganisationID: orgID, InvoiceID: unpaid.Id, Sequence: 1},
				{Kind: BASCandidateIncomeLine, OrganisationID: orgID, InvoiceID: partPaid.Id, Sequence: 1},
				{Kind: BASCandidateIncomeLine, OrganisationID: orgID, InvoiceID: partPaid.Id, Sequence: 2},
				{Kind: BASCandidateReceiptAllocation, OrganisationID: orgID, InvoiceID: partPaid.Id, ReceiptID: beforePeriod.Id, Sequence: 1},
				{Kind: BASCandidateReceiptAllocation, OrganisationID: orgID, InvoiceID: partPaid.Id, ReceiptID: beforePeriod.Id, Sequence: 2},
				{Kind: BASCandidateReceiptAllocation, OrganisationID: orgID, InvoiceID: partPaid.Id, ReceiptID: inPeriod.Id, Sequence: 1},
				{Kind: BASCandidateReceiptAllocation, OrganisationID: orgID, InvoiceID: partPaid.Id, ReceiptID: inPeriod.Id, Sequence: 2},
				{Kind: BASCandidateReceiptAllocation, OrganisationID: orgID, InvoiceID: partPaid.Id, ReceiptID: afterPeriod.Id, Sequence: 1},
				{Kind: BASCandidateReceiptAllocation, OrganisationID: orgID, InvoiceID: partPaid.Id, ReceiptID: afterPeriod.Id, Sequence: 2},
			}
			probe := &invoiceBASScalarProbe{Transaction: f.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := port.SummarizeBASKeys(f.ctx, orgID)
			if err != nil || summary == nil || summary.RootCount != 7 || summary.PostingCount != 3 ||
				summary.IncomeLineCount != 4 || summary.ReceiptCount != 3 || summary.AllocationCount != 6 || summary.ScalarKeyCount != 23 {
				t.Fatalf("real-writer scalar summary = %#v, %v; want families 7/3/4/3/6 and 23 physical keys", summary, err)
			}
			keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
			if err != nil || len(keys) != 23 {
				t.Fatalf("read real-writer keys = %d, %v; want 23", len(keys), err)
			}
			// Deliberately destroy SQL ordering. Nomination must sort its result
			// without sorting the caller's physical-key reservation in place.
			for left, right := 0, len(keys)-1; left < right; left, right = left+1, right-1 {
				keys[left], keys[right] = keys[right], keys[left]
			}
			keysBefore := nominationNativeCloneKeys(keys)
			summaryBefore := *summary
			stateBefore := invoiceReportingBudgetState(t, f)
			readsBefore := [3]int{probe.summaryReads, probe.keyReads, probe.bodyReads}
			got, err := NominateBASCandidates(f.ctx, *summary, keys, int64(len(want)))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("real-writer nominations = %#v, %v; want exact ordered identities %#v", got, err, want)
			}
			for _, allowance := range []int64{0, int64(len(want) - 1)} {
				if got, err := NominateBASCandidates(f.ctx, *summary, keys, allowance); got != nil || !errors.Is(err, ErrSourceBound) {
					t.Fatalf("nomination allowance %d = %#v, %v; want nil and ErrSourceBound", allowance, got, err)
				}
			}
			cancelled, cancel := context.WithCancel(f.ctx)
			cancel()
			if got, err := NominateBASCandidates(cancelled, *summary, keys, 1000); got != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled nomination = %#v, %v; want nil and context.Canceled", got, err)
			}
			if !reflect.DeepEqual(keys, keysBefore) || *summary != summaryBefore {
				t.Fatal("nomination changed the caller's scalar keys, nullable cells, hashes, or summary")
			}
			if readsAfter := [3]int{probe.summaryReads, probe.keyReads, probe.bodyReads}; readsAfter != readsBefore {
				t.Fatalf("pure nomination issued SQL: reads before=%v after=%v", readsBefore, readsAfter)
			}
			if stateAfter := invoiceReportingBudgetState(t, f); stateAfter != stateBefore || !f.tx.Authenticated() {
				t.Fatalf("nomination changed retained state or caller transaction: before=%v after=%v", stateBefore, stateAfter)
			}

			capture, err := port.CaptureBAS(f.ctx, orgID, invoiceReportingPeriod())
			if err != nil || capture == nil || len(capture.Candidates) != 7 || capture.CandidateCount != 7 || capture.ReceiptCount != 3 {
				t.Fatalf("full capture = %#v, %v; want seven invoice bodies and three receipts", capture, err)
			}
			if got := nominationNativeCaptureIdentities(t, capture); !reflect.DeepEqual(got, want) {
				t.Fatalf("full-reader structural identities = %#v; want writer identities %#v", got, want)
			}
			byID := invoiceReportingCandidatesByID(t, capture)
			if byID[olderDraft.Id].PeriodDisposition != "OUT_OF_PERIOD" || byID[olderDraft.Id].State != "DRAFT" ||
				byID[undated.Id].PeriodDisposition != "UNDATED" || byID[undated.Id].State != "DRAFT" || byID[undated.Id].Invoice.Draft.IssueDate != "" ||
				byID[future.Id].PeriodDisposition != "OUT_OF_PERIOD" || byID[future.Id].State != "ISSUED" ||
				byID[unposted.Id].PeriodDisposition != "IN_PERIOD" || byID[unposted.Id].State != "ISSUED" ||
				byID[partPaid.Id].PeriodDisposition != "OUT_OF_PERIOD" {
				t.Fatal("full capture lost old/future/undated/draft/issued source distinctions")
			}
			for _, id := range []string{olderDraft.Id, undated.Id, future.Id, unposted.Id} {
				if byID[id].Posting != nil || len(byID[id].Receipts) != 0 {
					t.Fatalf("full capture fabricated posting or receipt children for fallback invoice %s", id)
				}
			}
			for _, id := range []string{zero.Id, unpaid.Id, partPaid.Id} {
				if byID[id].Posting == nil || byID[id].Posting.GSTBasis != basis {
					t.Fatalf("full capture lost retained posting basis for %s: %#v", id, byID[id].Posting)
				}
			}
			if byID[zero.Id].Posting.JournalID != nil || byID[zero.Id].Posting.InvoiceTotalMinor != 0 ||
				len(byID[zero.Id].Posting.IncomeLines) != 1 || byID[zero.Id].Posting.IncomeLines[0].GrossMinor != 0 ||
				len(byID[zero.Id].Receipts) != 0 || len(byID[unpaid.Id].Receipts) != 0 {
				t.Fatal("zero and unpaid postings did not retain their income coverage without receipt children")
			}
			paidBody := byID[partPaid.Id]
			if paidBody.Posting.InvoiceTotalMinor != 300 || len(paidBody.Posting.IncomeLines) != 2 || len(paidBody.Receipts) != 3 {
				t.Fatalf("partly paid full source lost invoice lines or receipt history: %#v", paidBody)
			}
			wantDates := map[string]string{beforePeriod.Id: "2026-09-30", inPeriod.Id: "2026-10-12", afterPeriod.Id: "2026-11-01"}
			var capturedPaid int64
			seenReceipts := map[string]bool{}
			for _, receipt := range paidBody.Receipts {
				if receipt.PaymentDate != wantDates[receipt.ID] || seenReceipts[receipt.ID] || receipt.AmountMinor != 1 ||
					len(receipt.Allocations) != 2 || receipt.Allocations[0].Sequence != 1 || receipt.Allocations[1].Sequence != 2 {
					t.Fatalf("full receipt lost exact before/in/after-period or allocation identities: %#v", receipt)
				}
				seenReceipts[receipt.ID] = true
				capturedPaid += receipt.AmountMinor
				if receipt.ID == beforePeriod.Id && (receipt.Allocations[0].GrossMinor != 0 || receipt.Allocations[1].GrossMinor != 1) {
					t.Fatalf("real first partial receipt did not preserve its zero-valued allocation: %#v", receipt.Allocations)
				}
			}
			if capturedPaid != 3 || paidBody.Posting.InvoiceTotalMinor-capturedPaid != 297 {
				t.Fatal("fixture must retain a partly unpaid invoice alongside all three settlements")
			}
			if stateAfter := invoiceReportingBudgetState(t, f); stateAfter != stateBefore || !f.tx.Authenticated() {
				t.Fatalf("complete reader changed retained state or caller transaction: before=%v after=%v", stateBefore, stateAfter)
			}

			// A successful caller commit proves both APIs left the transaction
			// lifecycle with their owner. The committed full reader must agree.
			if err := f.tx.Commit(); err != nil {
				t.Fatalf("caller commit after scalar nomination/full read: %v", err)
			}
			f.tx = nil
			f.tx, err = f.workspace.Database.BeginEncryptedTx(f.ctx, nil)
			if err != nil {
				t.Fatalf("caller begin after commit: %v", err)
			}
			committedPort, err := NewReportingPort(f.tx)
			if err != nil {
				t.Fatal(err)
			}
			committed, err := committedPort.CaptureBAS(f.ctx, orgID, invoiceReportingPeriod())
			if err != nil || committed == nil || !bytes.Equal(committed.Fingerprint, capture.Fingerprint) ||
				!reflect.DeepEqual(nominationNativeCaptureIdentities(t, committed), want) {
				t.Fatalf("committed complete-reader identities/fingerprint changed: capture=%#v error=%v", committed, err)
			}
		})
	}
}

func TestInvoiceBASNominationReceiptFallbackForCountOnlyZeroAllocationFixture(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(97001), "NOMINATION-RECEIPT-FALLBACK", "2026-10-01", 100,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	f.issue(t, invoice)
	f.post(t, invoice)
	payment := f.recordPayment(t, invoice, 1, 1, 0)
	// The ordinary writer always retains allocation rows. Removing them is
	// count-only structural evidence for this fallback, not financial body
	// validity or a claim about sources accepted by the complete BAS reader.
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoice_payment_lines_v1",
		"DELETE FROM invoice_payment_lines_v1 WHERE payment_id=?", payment.Id); err != nil {
		t.Fatalf("remove count-only receipt allocation rows: %v", err)
	}
	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := port.SummarizeBASKeys(f.ctx, orgID)
	if err != nil || summary == nil || summary.RootCount != 1 || summary.PostingCount != 1 ||
		summary.IncomeLineCount != 1 || summary.ReceiptCount != 1 || summary.AllocationCount != 0 || summary.ScalarKeyCount != 4 {
		t.Fatalf("count-only zero-allocation summary = %#v, %v; want families 1/1/1/1/0", summary, err)
	}
	keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || len(keys) != 4 {
		t.Fatalf("read count-only receipt keys = %d, %v; want four", len(keys), err)
	}
	before := invoiceReportingBudgetState(t, f)
	readsBefore := [3]int{probe.summaryReads, probe.keyReads, probe.bodyReads}
	want := []BASCandidateKey{
		{Kind: BASCandidateIncomeLine, OrganisationID: orgID, InvoiceID: invoice.Id, Sequence: 1},
		{Kind: BASCandidateReceipt, OrganisationID: orgID, InvoiceID: invoice.Id, ReceiptID: payment.Id},
	}
	got, err := NominateBASCandidates(f.ctx, *summary, keys, 2)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("count-only receipt fallback nominations = %#v, %v; want %#v", got, err, want)
	}
	if readsAfter := [3]int{probe.summaryReads, probe.keyReads, probe.bodyReads}; readsAfter != readsBefore ||
		invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
		t.Fatal("count-only fallback nomination issued SQL, changed retained state, or took the caller transaction")
	}
}

func TestInvoiceBASNominationCountOnlyIncomeRowsEnforceOneThousandLimit(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(97002), "NOMINATION-COUNT-BOUND", "2026-10-01", 100,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	f.issue(t, invoice)
	f.post(t, invoice)
	// These extra immutable rows establish count-only nomination limits. Their
	// copied amounts do not reconcile to the real invoice, so this test makes
	// no complete-reader or financial-body validity claim for the fixture.
	if _, err := f.tx.ExecContext(f.ctx, `
		WITH RECURSIVE sequence(n) AS (
			SELECT 2 UNION ALL SELECT n+1 FROM sequence WHERE n<1000
		)
		INSERT INTO invoice_income_lines_v1(invoice_id,sequence,gross_minor,net_minor,gst_minor,treatment)
		SELECT source.invoice_id,sequence.n,source.gross_minor,source.net_minor,source.gst_minor,source.treatment
		FROM invoice_income_lines_v1 AS source CROSS JOIN sequence
		WHERE source.invoice_id=? AND source.sequence=1`, invoice.Id); err != nil {
		t.Fatalf("insert count-only income identities 2..1000: %v", err)
	}
	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int64{1000, 1001} {
		if count == 1001 {
			if _, err := f.tx.ExecContext(f.ctx, `
				INSERT INTO invoice_income_lines_v1(invoice_id,sequence,gross_minor,net_minor,gst_minor,treatment)
				SELECT invoice_id,1001,gross_minor,net_minor,gst_minor,treatment
				FROM invoice_income_lines_v1 WHERE invoice_id=? AND sequence=1`, invoice.Id); err != nil {
				t.Fatalf("insert count-only income identity 1001: %v", err)
			}
		}
		summary, err := port.SummarizeBASKeys(f.ctx, orgID)
		if err != nil || summary == nil || summary.RootCount != 1 || summary.PostingCount != 1 ||
			summary.IncomeLineCount != count || summary.ReceiptCount != 0 || summary.AllocationCount != 0 || summary.ScalarKeyCount != count+2 {
			t.Fatalf("%d-line count-only summary = %#v, %v", count, summary, err)
		}
		keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
		if err != nil || int64(len(keys)) != count+2 {
			t.Fatalf("%d-line count-only read = %d keys, %v", count, len(keys), err)
		}
		before := invoiceReportingBudgetState(t, f)
		readsBefore := [3]int{probe.summaryReads, probe.keyReads, probe.bodyReads}
		got, err := NominateBASCandidates(f.ctx, *summary, keys, 1001)
		if count == 1000 {
			if err != nil || len(got) != 1000 {
				t.Fatalf("exact local limit = %d candidates, %v; want 1000", len(got), err)
			}
			for index, candidate := range got {
				want := BASCandidateKey{Kind: BASCandidateIncomeLine, OrganisationID: orgID, InvoiceID: invoice.Id, Sequence: uint64(index + 1)}
				if candidate != want {
					t.Fatalf("1000-candidate identity %d = %#v; want %#v", index, candidate, want)
				}
			}
			if got, err := NominateBASCandidates(f.ctx, *summary, keys, 999); got != nil || !errors.Is(err, ErrSourceBound) {
				t.Fatalf("one-short local allowance = %#v, %v; want nil and ErrSourceBound", got, err)
			}
		} else if got != nil || !errors.Is(err, ErrSourceBound) {
			t.Fatalf("1001 candidates with a 1001 caller allowance = %#v, %v; want nil and the hard 1000 bound", got, err)
		}
		if readsAfter := [3]int{probe.summaryReads, probe.keyReads, probe.bodyReads}; readsAfter != readsBefore ||
			invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
			t.Fatal("count-only boundary nomination issued SQL, changed retained state, or took the caller transaction")
		}
	}
}

// This projection starts with full verified bodies, never the scalar-key slice.
// Its output must independently match the exact writer identities in the test.
func nominationNativeCaptureIdentities(t *testing.T, capture *BASReportingCapture) []BASCandidateKey {
	t.Helper()
	var identities []BASCandidateKey
	for _, invoice := range capture.Candidates {
		if invoice.Posting == nil || len(invoice.Posting.IncomeLines) == 0 {
			identities = append(identities, BASCandidateKey{Kind: BASCandidateInvoiceRoot, OrganisationID: invoice.OrganisationID, InvoiceID: invoice.ID})
		} else {
			for _, line := range invoice.Posting.IncomeLines {
				if line.InvoiceID != invoice.ID {
					t.Fatalf("full-reader income line has wrong parent: %#v", line)
				}
				identities = append(identities, BASCandidateKey{Kind: BASCandidateIncomeLine, OrganisationID: invoice.OrganisationID, InvoiceID: invoice.ID, Sequence: line.Sequence})
			}
		}
		for _, receipt := range invoice.Receipts {
			if receipt.InvoiceID != invoice.ID || receipt.OrganisationID != invoice.OrganisationID {
				t.Fatalf("full-reader receipt has wrong parents: %#v", receipt)
			}
			if len(receipt.Allocations) == 0 {
				identities = append(identities, BASCandidateKey{Kind: BASCandidateReceipt, OrganisationID: receipt.OrganisationID, InvoiceID: invoice.ID, ReceiptID: receipt.ID})
			} else {
				for _, allocation := range receipt.Allocations {
					if allocation.PaymentID != receipt.ID {
						t.Fatalf("full-reader allocation has wrong parent: %#v", allocation)
					}
					identities = append(identities, BASCandidateKey{Kind: BASCandidateReceiptAllocation, OrganisationID: receipt.OrganisationID, InvoiceID: invoice.ID, ReceiptID: receipt.ID, Sequence: allocation.Sequence})
				}
			}
		}
	}
	sort.Slice(identities, func(left, right int) bool {
		a, b := identities[left], identities[right]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.InvoiceID != b.InvoiceID {
			return a.InvoiceID < b.InvoiceID
		}
		if a.ReceiptID != b.ReceiptID {
			return a.ReceiptID < b.ReceiptID
		}
		return a.Sequence < b.Sequence
	})
	return identities
}

func nominationNativeCloneKeys(keys []BASScalarKey) []BASScalarKey {
	cloned := append([]BASScalarKey(nil), keys...)
	for index := range cloned {
		key := &cloned[index]
		if key.ReceiptID != nil {
			value := *key.ReceiptID
			key.ReceiptID = &value
		}
		if key.JournalID != nil {
			value := *key.JournalID
			key.JournalID = &value
		}
		for _, target := range []**uint64{&key.Sequence, &key.RootVersion, &key.PostingInvoiceVersion} {
			if *target != nil {
				value := **target
				*target = &value
			}
		}
		key.SourceHash = bytes.Clone(key.SourceHash)
	}
	return cloned
}
