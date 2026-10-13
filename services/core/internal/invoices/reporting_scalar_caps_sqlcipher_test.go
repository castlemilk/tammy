//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceBASScalarIndependentRootAndReceiptCapBoundaries(t *testing.T) {
	t.Run("root exact 1000", func(t *testing.T) {
		f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		invoice := f.saveDraft(t, invoiceReportingID(94001), "ROOT-CAP", "", 0,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		if err := insertInvoiceBASScalarRootClones(f, invoice.Id, 1, 999); err != nil {
			t.Fatalf("insert root cap fixture rows: %v", err)
		}
		probe := &invoiceBASScalarProbe{Transaction: f.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		before := invoiceReportingBudgetState(t, f)
		summary, err := port.SummarizeBASKeys(f.ctx, orgID)
		if err != nil || summary == nil || summary.RootCount != maxBASReportingInvoices || summary.ScalarKeyCount != maxBASReportingInvoices {
			t.Fatalf("exact root cap summary present=%t error=%T", summary != nil, err)
		}
		keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
		if err != nil || int64(len(keys)) != maxBASReportingInvoices {
			t.Fatalf("exact root cap read keys=%d error=%T", len(keys), err)
		}
		if probe.keyReads != 1 || probe.bodyReads != 0 || invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
			t.Fatal("exact root cap read changed state, read bodies, or lost transaction authentication")
		}
	})

	t.Run("root over 1000", func(t *testing.T) {
		f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		invoice := f.saveDraft(t, invoiceReportingID(94002), "ROOT-CAP", "", 0,
			tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
		probe := &invoiceBASScalarProbe{Transaction: f.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		prior, err := port.SummarizeBASKeys(f.ctx, orgID)
		if err != nil {
			t.Fatal(err)
		}
		if err := insertInvoiceBASScalarRootClones(f, invoice.Id, 1, 1000); err != nil {
			t.Fatalf("insert over-cap root fixture rows: %v", err)
		}
		before := invoiceReportingBudgetState(t, f)
		got, err := port.SummarizeBASKeys(f.ctx, orgID)
		if got != nil {
			t.Fatal("over-cap root summary returned a value")
		}
		assertInvoiceBASScalarCapError(t, err, "invoices")
		keys, readErr := port.ReadBASKeys(f.ctx, orgID, *prior, prior.ScalarKeyCount, prior.ScalarBytes)
		if keys != nil {
			t.Fatal("over-cap root read returned keys")
		}
		assertInvoiceBASScalarCapError(t, readErr, "invoices")
		if probe.keyReads != 0 || probe.bodyReads != 0 || invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
			t.Fatal("over-cap root path read keys or bodies, changed state, or lost transaction authentication")
		}
	})

	t.Run("receipt exact 1000", func(t *testing.T) {
		f, payment := newInvoiceBASScalarReceiptCapFixture(t, 94003)
		if err := insertInvoiceBASScalarReceiptClones(f, payment.Id, payment.JournalId, 1, 999); err != nil {
			t.Fatalf("insert receipt cap fixture rows: %v", err)
		}
		probe := &invoiceBASScalarProbe{Transaction: f.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		before := invoiceReportingBudgetState(t, f)
		summary, err := port.SummarizeBASKeys(f.ctx, orgID)
		if err != nil || summary == nil || summary.RootCount != 1 || summary.ReceiptCount != maxBASReportingReceipts || summary.ScalarKeyCount <= maxBASReportingReceipts {
			t.Fatalf("exact receipt cap summary present=%t error=%T", summary != nil, err)
		}
		keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
		if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
			t.Fatalf("exact receipt cap read keys=%d error=%T", len(keys), err)
		}
		var receipts int64
		for _, key := range keys {
			if key.Kind == BASScalarReceipt {
				receipts++
			}
		}
		if receipts != maxBASReportingReceipts || probe.keyReads != 1 || probe.bodyReads != 0 ||
			invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
			t.Fatal("exact receipt cap did not retain all receipt keys without changing state or reading bodies")
		}
	})

	t.Run("receipt over 1000", func(t *testing.T) {
		f, payment := newInvoiceBASScalarReceiptCapFixture(t, 94004)
		probe := &invoiceBASScalarProbe{Transaction: f.tx}
		port, err := NewReportingPort(probe)
		if err != nil {
			t.Fatal(err)
		}
		prior, err := port.SummarizeBASKeys(f.ctx, orgID)
		if err != nil {
			t.Fatal(err)
		}
		if err := insertInvoiceBASScalarReceiptClones(f, payment.Id, payment.JournalId, 1, 1000); err != nil {
			t.Fatalf("insert over-cap receipt fixture rows: %v", err)
		}
		before := invoiceReportingBudgetState(t, f)
		got, err := port.SummarizeBASKeys(f.ctx, orgID)
		if got != nil {
			t.Fatal("over-cap receipt summary returned a value")
		}
		assertInvoiceBASScalarCapError(t, err, "receipts")
		keys, readErr := port.ReadBASKeys(f.ctx, orgID, *prior, prior.ScalarKeyCount, prior.ScalarBytes)
		if keys != nil {
			t.Fatal("over-cap receipt read returned keys")
		}
		assertInvoiceBASScalarCapError(t, readErr, "receipts")
		if probe.keyReads != 0 || probe.bodyReads != 0 || invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
			t.Fatal("over-cap receipt path read keys or bodies, changed state, or lost transaction authentication")
		}
	})
}

func TestInvoiceBASScalarRetainsReceiptWithoutAllocationRows(t *testing.T) {
	f, payment := newInvoiceBASScalarReceiptCapFixture(t, 94005)
	if err := invoiceReportingMutateWithTriggersRestored(f, "invoice_payment_lines_v1",
		"DELETE FROM invoice_payment_lines_v1 WHERE payment_id=?", payment.Id); err != nil {
		t.Fatalf("remove allocation fixture rows: %v", err)
	}
	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	before := invoiceReportingBudgetState(t, f)
	summary, err := port.SummarizeBASKeys(f.ctx, orgID)
	if err != nil || summary == nil || summary.ReceiptCount != 1 || summary.AllocationCount != 0 || summary.ScalarKeyCount != 4 {
		t.Fatalf("zero-allocation receipt summary present=%t error=%T", summary != nil, err)
	}
	keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("zero-allocation receipt read keys=%d error=%T", len(keys), err)
	}
	var receiptSeen bool
	var allocations int
	for _, key := range keys {
		if key.Kind == BASScalarReceipt && key.ReceiptID != nil && *key.ReceiptID == payment.Id {
			receiptSeen = true
		}
		if key.Kind == BASScalarAllocation {
			allocations++
		}
	}
	if !receiptSeen || allocations != 0 || probe.keyReads != 1 || probe.bodyReads != 0 ||
		invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
		t.Fatal("receipt without allocation rows was omitted or reader changed state/read bodies")
	}
}

func TestInvoiceBASScalarCancellationAfterKeyQueryReturnsNoKeys(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	f.saveDraft(t, invoiceReportingID(94006), "CANCEL-KEY-READ", "", 0,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	probe := &invoiceBASScalarProbe{Transaction: f.tx}
	port, err := NewReportingPort(probe)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := port.SummarizeBASKeys(f.ctx, orgID)
	if err != nil || summary == nil {
		t.Fatalf("cancellation fixture summary present=%t error=%T", summary != nil, err)
	}
	before := invoiceReportingBudgetState(t, f)
	ctx, cancel := context.WithCancel(f.ctx)
	probe.afterKeyQuery = cancel
	keys, err := port.ReadBASKeys(ctx, orgID, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	cancel()
	if keys != nil || !errors.Is(err, context.Canceled) || probe.keyReads != 1 || probe.bodyReads != 0 {
		t.Fatalf("cancelled key read returned keys=%t canceled=%t key/body reads=%d/%d", keys != nil, errors.Is(err, context.Canceled), probe.keyReads, probe.bodyReads)
	}
	if invoiceReportingBudgetState(t, f) != before || !f.tx.Authenticated() {
		t.Fatal("cancelled key read changed retained state or caller transaction")
	}
}

func insertInvoiceBASScalarRootClones(f *invoiceReportingFixture, sourceID string, first, last int) error {
	_, err := f.tx.ExecContext(f.ctx, `
		WITH RECURSIVE clone_sequence(n) AS (
			SELECT ? UNION ALL SELECT n+1 FROM clone_sequence WHERE n<?
		)
		INSERT INTO invoices_v1(id,organisation_id,version,state,number,invoice_proto,created_at,updated_at)
		SELECT 'scalar-root-'||printf('%04d',clone_sequence.n),source.organisation_id,source.version,source.state,
			'SCALAR-ROOT-'||printf('%04d',clone_sequence.n),source.invoice_proto,source.created_at,source.updated_at
		FROM clone_sequence CROSS JOIN invoices_v1 AS source WHERE source.id=?`, first, last, sourceID)
	return err
}

func newInvoiceBASScalarReceiptCapFixture(t *testing.T, id uint64) (*invoiceReportingFixture, *tammyv1.InvoicePayment) {
	t.Helper()
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	invoice := f.saveDraft(t, invoiceReportingID(id), "RECEIPT-CAP", "2026-10-12", 100,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	f.issue(t, invoice)
	f.post(t, invoice)
	payment := f.recordPayment(t, invoice, 1, 100, 1)
	return f, payment
}

// These CTE clones provide count-only scalar rows. They are not complete
// financial body proofs or claims about records a public command may create.
// Parent headers start in DRAFT, respecting the guarded posting transition.
func insertInvoiceBASScalarReceiptClones(
	f *invoiceReportingFixture,
	sourcePaymentID, sourceJournalID string,
	first, last int,
) error {
	_, err := f.tx.ExecContext(f.ctx, `
		WITH RECURSIVE clone_sequence(n) AS (
			SELECT ? UNION ALL SELECT n+1 FROM clone_sequence WHERE n<?
		), source AS (SELECT * FROM journals WHERE id=?)
		INSERT INTO journals(id,organisation_id,version,source_type,source_id,source_revision,state,journal_date,description,
			reversal_of_journal_id,reversed_by_journal_id,total_debits_minor,total_credits_minor,currency_code,financial_revision,posted_at,created_at,source_kind)
		SELECT 'scalar-journal-'||printf('%04d',clone_sequence.n),source.organisation_id,source.version,source.source_type,
			'SCALAR-RECEIPT-SOURCE-'||printf('%04d',clone_sequence.n),source.source_revision,'DRAFT',source.journal_date,source.description,
			NULL,NULL,source.total_debits_minor,source.total_credits_minor,source.currency_code,source.financial_revision,NULL,source.created_at,source.source_kind
		FROM clone_sequence CROSS JOIN source`, first, last, sourceJournalID)
	if err != nil {
		return err
	}
	_, err = f.tx.ExecContext(f.ctx, `
		WITH RECURSIVE clone_sequence(n) AS (
			SELECT ? UNION ALL SELECT n+1 FROM clone_sequence WHERE n<?
		), source AS (SELECT * FROM invoice_payment_v1 WHERE id=?)
		INSERT INTO invoice_payment_v1(id,organisation_id,invoice_id,payment_date,amount_minor,allocated_gst_minor,bas_sales_gross_minor,
			bank_account_id,journal_id,reference,payment_proto,source_hash,created_at)
		SELECT 'scalar-receipt-'||printf('%04d',clone_sequence.n),source.organisation_id,source.invoice_id,source.payment_date,
			source.amount_minor,source.allocated_gst_minor,source.bas_sales_gross_minor,source.bank_account_id,
			'scalar-journal-'||printf('%04d',clone_sequence.n),source.reference,source.payment_proto,source.source_hash,source.created_at
		FROM clone_sequence CROSS JOIN source`, first, last, sourcePaymentID)
	return err
}

func assertInvoiceBASScalarCapError(t *testing.T, err error, resource string) {
	t.Helper()
	var bound *SourceBoundError
	if !errors.Is(err, ErrSourceBound) || !errors.As(err, &bound) || bound.Resource != resource || bound.Limit != 1000 {
		t.Fatalf("unexpected scalar cap error type %T for resource %s", err, resource)
	}
}
