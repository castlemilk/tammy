//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceBASScalarKeysProjectRetainedIdentitiesAndExactCharges(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)

	draft := fixture.saveDraft(t, invoiceReportingID(9101), "SCALAR-DRAFT", "2026-10-01", 100,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)

	unposted := fixture.saveDraft(t, invoiceReportingID(9102), "SCALAR-UNPOSTED", "2026-10-02", 200,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, unposted)

	zero := fixture.saveDraft(t, invoiceReportingID(9103), "SCALAR-ZERO", "2026-10-03", 0,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, zero)
	fixture.post(t, zero)

	paid := fixture.saveDraftInput(t, invoiceReportingID(9104), &tammyv1.InvoiceDraft{
		Number:          "SCALAR-MULTILINE",
		IssueDate:       "2026-10-04",
		DueDate:         "2026-10-04",
		CustomerName:    "Customer",
		CustomerAddress: "1 High Street",
		SellerAddress:   "27 Cambridge Road",
		Lines: []*tammyv1.InvoiceLineInput{
			{Description: "First service", QuantityMilli: 1000, UnitPriceMinor: 100,
				TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
			{Description: "Second service", QuantityMilli: 1000, UnitPriceMinor: 200,
				TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
		},
	})
	fixture.issue(t, paid)
	fixture.post(t, paid)
	payment := fixture.recordPayment(t, paid, 1, 1, 0)
	var zeroAllocations int
	if err := fixture.tx.QueryRowContext(fixture.ctx, `SELECT count(*) FROM invoice_payment_lines_v1 WHERE payment_id=? AND gross_minor=0`, payment.Id).Scan(&zeroAllocations); err != nil || zeroAllocations == 0 {
		t.Fatalf("real partial payment did not retain a zero allocation: count=%d error=%v", zeroAllocations, err)
	}

	probe := &invoiceBASScalarProbe{Transaction: fixture.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatalf("NewReportingPort(probe) = %v", err)
	}
	stateBefore := invoiceReportingBudgetState(t, fixture)
	summary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || summary == nil {
		t.Fatalf("SummarizeBASKeys() = %#v, %v", summary, err)
	}
	if summary.OrganisationID != orgID || summary.RootCount != 4 || summary.PostingCount != 2 ||
		summary.IncomeLineCount != 3 || summary.ReceiptCount != 1 || summary.AllocationCount != 2 ||
		summary.ScalarKeyCount != 12 {
		t.Fatalf("mixed-source scalar summary = %#v; want roots/postings/lines/receipts/allocations/keys = 4/2/3/1/2/12", summary)
	}

	keys, err := port.ReadBASKeys(fixture.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("ReadBASKeys(exact count and bytes) returned %d keys, %v; summary=%#v", len(keys), err, summary)
	}
	if probe.summaryReads == 0 || probe.keyReads == 0 || probe.bodyReads != 0 {
		t.Fatalf("scalar SQL probe saw summary/key/body reads %d/%d/%d; want both tagged scalar queries and no body reads",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
	if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
		t.Fatalf("scalar summary/read changed retained state or transaction authentication: before=%v after=%v", stateBefore, stateAfter)
	}

	wantRoots := map[string]uint64{draft.Id: 1, unposted.Id: 2, zero.Id: 2, paid.Id: 2}
	seenRoots := map[string]bool{}
	seenLines := map[string]map[uint64]bool{}
	seenPostings := map[string]bool{}
	var zeroPostingHasJournal, paidPostingHasJournal bool
	var receiptSeen bool
	allocationSequences := map[uint64]bool{}
	var actualScalarBytes int64
	for _, key := range keys {
		if key.OrganisationID != orgID || key.InvoiceID == "" {
			t.Fatalf("scalar key lost its organisation or invoice identity: %#v", key)
		}
		actualScalarBytes += invoiceBASScalarKeyCharge(key)
		switch key.Kind {
		case BASScalarInvoice:
			if key.RootVersion == nil || *key.RootVersion != wantRoots[key.InvoiceID] || key.Sequence != nil ||
				key.PostingInvoiceVersion != nil || key.ReceiptID != nil || key.JournalID != nil || key.SourceHash != nil {
				t.Fatalf("invoice root scalar key has unexpected fields: %#v", key)
			}
			if _, ok := wantRoots[key.InvoiceID]; !ok || seenRoots[key.InvoiceID] {
				t.Fatalf("unexpected or duplicate invoice root scalar key: %#v", key)
			}
			seenRoots[key.InvoiceID] = true
		case BASScalarPosting:
			if key.PostingInvoiceVersion == nil || *key.PostingInvoiceVersion != 2 || key.RootVersion != nil ||
				key.Sequence != nil || key.ReceiptID != nil || len(key.SourceHash) != sha256.Size {
				t.Fatalf("posting scalar key lost its version/hash or included unrelated fields: %#v", key)
			}
			if seenPostings[key.InvoiceID] {
				t.Fatalf("duplicate posting scalar key for invoice %s", key.InvoiceID)
			}
			seenPostings[key.InvoiceID] = true
			switch key.InvoiceID {
			case zero.Id:
				zeroPostingHasJournal = key.JournalID != nil
			case paid.Id:
				paidPostingHasJournal = key.JournalID != nil && *key.JournalID != ""
			default:
				t.Fatalf("unexpected posting scalar key: %#v", key)
			}
		case BASScalarIncomeLine:
			if key.Sequence == nil || key.RootVersion != nil || key.PostingInvoiceVersion != nil ||
				key.ReceiptID != nil || key.JournalID != nil || key.SourceHash != nil {
				t.Fatalf("income-line scalar key has unexpected fields: %#v", key)
			}
			if seenLines[key.InvoiceID] == nil {
				seenLines[key.InvoiceID] = map[uint64]bool{}
			}
			if seenLines[key.InvoiceID][*key.Sequence] {
				t.Fatalf("duplicate income-line scalar key: %#v", key)
			}
			seenLines[key.InvoiceID][*key.Sequence] = true
		case BASScalarReceipt:
			if key.ReceiptID == nil || *key.ReceiptID != payment.Id || key.JournalID == nil ||
				*key.JournalID != payment.JournalId || key.Sequence != nil || key.RootVersion != nil ||
				key.PostingInvoiceVersion != nil || len(key.SourceHash) != sha256.Size || receiptSeen {
				t.Fatalf("receipt scalar key lost nullable identity fields or its hash: %#v", key)
			}
			receiptSeen = true
		case BASScalarAllocation:
			if key.ReceiptID == nil || *key.ReceiptID != payment.Id || key.Sequence == nil ||
				key.RootVersion != nil || key.PostingInvoiceVersion != nil || key.JournalID != nil || key.SourceHash != nil {
				t.Fatalf("allocation scalar key has unexpected fields: %#v", key)
			}
			if allocationSequences[*key.Sequence] {
				t.Fatalf("duplicate allocation scalar sequence: %#v", key)
			}
			allocationSequences[*key.Sequence] = true
		default:
			t.Fatalf("unknown scalar key family: %#v", key)
		}
	}
	if len(seenRoots) != 4 || len(seenPostings) != 2 || len(seenLines[zero.Id]) != 1 ||
		len(seenLines[paid.Id]) != 2 || len(seenLines[draft.Id])+len(seenLines[unposted.Id]) != 0 ||
		zeroPostingHasJournal || !paidPostingHasJournal || !receiptSeen ||
		!allocationSequences[1] || !allocationSequences[2] || len(allocationSequences) != 2 {
		t.Fatalf("projected scalar families lost roots, a zero-journal posting, ordered lines, receipt, or allocations: roots=%v postings=%v lines=%v allocation sequences=%v zeroJournal=%t paidJournal=%t receipt=%t",
			seenRoots, seenPostings, seenLines, allocationSequences, zeroPostingHasJournal, paidPostingHasJournal, receiptSeen)
	}
	if actualScalarBytes != summary.ScalarBytes {
		t.Fatalf("summary scalar bytes=%d, key charges=%d; want one 512-byte base plus selected identity/hash bytes per key", summary.ScalarBytes, actualScalarBytes)
	}
}

func TestInvoiceBASScalarKeysEnforceExactBudgetsCancellationAndReadOnlyBehavior(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	fixture.saveDraft(t, invoiceReportingID(9201), "SCALAR-BUDGET", "", 0,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	probe := &invoiceBASScalarProbe{Transaction: fixture.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatalf("NewReportingPort(probe) = %v", err)
	}
	stateBefore := invoiceReportingBudgetState(t, fixture)
	summary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || summary == nil || summary.RootCount != 1 || summary.ScalarKeyCount != 1 {
		t.Fatalf("single-root scalar summary = %#v, %v", summary, err)
	}
	wantBytes := int64(512 + len(orgID) + len(invoiceReportingID(9201)))
	if summary.ScalarBytes != wantBytes {
		t.Fatalf("single-root summary charged %d bytes; want exactly one 512-byte key plus organisation and invoice IDs (%d)", summary.ScalarBytes, wantBytes)
	}

	keys, err := port.ReadBASKeys(fixture.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || len(keys) != 1 || keys[0].Kind != BASScalarInvoice {
		t.Fatalf("read at exact count/byte budgets = %#v, %v; want one invoice root", keys, err)
	}
	if keys, err := port.ReadBASKeys(fixture.ctx, orgID, *summary, summary.ScalarKeyCount-1, summary.ScalarBytes); keys != nil || !errors.Is(err, ErrSourceBound) {
		t.Fatalf("one-under key-count budget = %#v, %v; want nil keys and ErrSourceBound", keys, err)
	}
	if keys, err := port.ReadBASKeys(fixture.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes-1); keys != nil || !errors.Is(err, ErrSourceBound) {
		t.Fatalf("one-under byte budget = %#v, %v; want nil keys and ErrSourceBound", keys, err)
	}

	cancelled, cancel := context.WithCancel(fixture.ctx)
	cancel()
	if got, err := port.SummarizeBASKeys(cancelled, orgID); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scalar summary = %#v, %v; want nil summary and context.Canceled", got, err)
	}
	if got, err := port.ReadBASKeys(cancelled, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scalar read = %#v, %v; want nil keys and context.Canceled", got, err)
	}
	if probe.summaryReads == 0 || probe.keyReads == 0 || probe.bodyReads != 0 {
		t.Fatalf("scalar SQL probe saw summary/key/body reads %d/%d/%d; want both tagged scalar queries and no body reads",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
	if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
		t.Fatalf("budget/cancellation calls changed retained state or transaction authentication: before=%v after=%v", stateBefore, stateAfter)
	}
}

func TestInvoiceBASScalarKeysAllowMoreThanOneThousandIncomeLineKeys(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := fixture.saveDraft(t, invoiceReportingID(9301), "SCALAR-PHYSICAL-LIMIT", "2026-10-01", 100,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)

	// Keep one real invoice root and posting, then add schema-valid immutable source rows
	// directly to prove the key API's physical 16,384-row ceiling is independent
	// of the 1,000 root/receipt resource caps.
	if _, err := fixture.tx.ExecContext(fixture.ctx, "\n\t\tWITH RECURSIVE sequence(n) AS (\n\t\t\tSELECT 2 UNION ALL SELECT n + 1 FROM sequence WHERE n < 1001\n\t\t)\n\t\tINSERT INTO invoice_income_lines_v1(invoice_id,sequence,gross_minor,net_minor,gst_minor,treatment)\n\t\tSELECT source.invoice_id,sequence.n,source.gross_minor,source.net_minor,source.gst_minor,source.treatment\n\t\tFROM invoice_income_lines_v1 AS source CROSS JOIN sequence\n\t\tWHERE source.invoice_id=? AND source.sequence=1", invoice.Id); err != nil {
		t.Fatalf("insert additional valid income-line identities: %v", err)
	}

	probe := &invoiceBASScalarProbe{Transaction: fixture.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatalf("NewReportingPort(probe) = %v", err)
	}
	stateBefore := invoiceReportingBudgetState(t, fixture)
	summary, err := port.SummarizeBASKeys(fixture.ctx, orgID)
	if err != nil || summary == nil || summary.RootCount != 1 || summary.ReceiptCount != 0 ||
		summary.IncomeLineCount != 1001 || summary.ScalarKeyCount != 1003 {
		t.Fatalf("1,001-line physical scalar summary = %#v, %v; want roots/receipts/lines/keys = 1/0/1001/1003", summary, err)
	}
	keys, err := port.ReadBASKeys(fixture.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("read 1,001 component keys within root/receipt caps = %d keys, %v", len(keys), err)
	}
	seenSequences := make(map[uint64]bool, 1001)
	for _, key := range keys {
		if key.Kind != BASScalarIncomeLine {
			continue
		}
		if key.InvoiceID != invoice.Id || key.Sequence == nil || seenSequences[*key.Sequence] {
			t.Fatalf("income-line key has missing, duplicated, or wrong composite identity: %#v", key)
		}
		seenSequences[*key.Sequence] = true
	}
	if len(seenSequences) != 1001 || !seenSequences[1] || !seenSequences[1001] {
		t.Fatalf("income-line scalar sequences retained=%d, endpoints=%t/%t; want 1..1001", len(seenSequences), seenSequences[1], seenSequences[1001])
	}
	if probe.summaryReads == 0 || probe.keyReads == 0 || probe.bodyReads != 0 {
		t.Fatalf("scalar SQL probe saw summary/key/body reads %d/%d/%d; want both tagged scalar queries and no body reads",
			probe.summaryReads, probe.keyReads, probe.bodyReads)
	}
	if stateAfter := invoiceReportingBudgetState(t, fixture); stateAfter != stateBefore || !fixture.tx.Authenticated() {
		t.Fatalf("large scalar read changed retained state or transaction authentication: before=%v after=%v", stateBefore, stateAfter)
	}
}

func invoiceBASScalarKeyCharge(key BASScalarKey) int64 {
	charge := int64(512 + len(key.OrganisationID) + len(key.InvoiceID))
	if key.ReceiptID != nil {
		charge += int64(len(*key.ReceiptID))
	}
	if key.JournalID != nil {
		charge += int64(len(*key.JournalID))
	}
	charge += int64(len(key.SourceHash))
	return charge
}
