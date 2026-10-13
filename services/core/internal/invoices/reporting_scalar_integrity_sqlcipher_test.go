//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestInvoiceBASScalarKeysRejectCorruptFamiliesBeforeTypedReads(t *testing.T) {
	for _, testCase := range []struct {
		name, table, column, expression string
		ignoreChecks, bound             bool
	}{
		{"root version blob", "invoices_v1", "version", "zeroblob(8388609)", false, false},
		{"root version text", "invoices_v1", "version", "CAST(zeroblob(8388609) AS TEXT)", false, false},
		{"root version real", "invoices_v1", "version", "1.5", false, false},
		{"posting invoice version blob", "invoice_income_postings_v1", "invoice_version", "zeroblob(8388609)", false, false},
		{"posting invoice version zero", "invoice_income_postings_v1", "invoice_version", "0", true, false},
		{"income sequence blob", "invoice_income_lines_v1", "sequence", "zeroblob(8388609)", false, false},
		{"income sequence real", "invoice_income_lines_v1", "sequence", "1.5", false, false},
		{"allocation sequence blob", "invoice_payment_lines_v1", "sequence", "zeroblob(8388609)", false, false},
		{"allocation sequence real", "invoice_payment_lines_v1", "sequence", "1.5", false, false},
		{"foreign posting owner", "invoice_income_postings_v1", "organisation_id", "'018f0000-0000-7000-8000-000000088888'", false, false},
		{"foreign receipt owner", "invoice_payment_v1", "organisation_id", "'018f0000-0000-7000-8000-000000088888'", false, false},
		{"orphan posting", "invoice_income_postings_v1", "invoice_id", "'missing-invoice'", false, false},
		{"orphan income", "invoice_income_lines_v1", "invoice_id", "'missing-invoice-and-posting'", false, false},
		{"orphan receipt", "invoice_payment_v1", "invoice_id", "'missing-invoice-and-posting'", false, false},
		{"orphan allocation", "invoice_payment_lines_v1", "payment_id", "'missing-receipt'", false, false},
		{"missing posting journal", "invoice_income_postings_v1", "journal_id", "'missing-journal'", false, false},
		{"missing required posting journal", "invoice_income_postings_v1", "journal_id", "NULL", true, false},
		{"mistyped posting linkage amount", "invoice_income_postings_v1", "invoice_total_minor", "zeroblob(8388609)", true, false},
		{"missing receipt journal", "invoice_payment_v1", "journal_id", "'missing-journal'", false, false},
		{"empty posting journal", "invoice_income_postings_v1", "journal_id", "''", false, false},
		{"posting source hash empty", "invoice_income_postings_v1", "source_hash", "zeroblob(0)", true, false},
		{"receipt source hash empty", "invoice_payment_v1", "source_hash", "zeroblob(0)", true, false},
		{"oversized scalar root ID", "invoices_v1", "id", "CAST(zeroblob(8388609) AS TEXT)", false, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			invoice := f.saveDraft(t, invoiceReportingID(88001), "SCALAR-INTEGRITY", "2026-10-12", 1000, tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			f.issue(t, invoice)
			f.post(t, invoice)
			f.recordPayment(t, invoice, 1, 1, 1)
			probe := &invoiceBASScalarProbe{Transaction: f.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := port.SummarizeBASKeys(f.ctx, orgID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.tx.ExecContext(f.ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
				t.Fatal(err)
			}
			if testCase.ignoreChecks {
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
					t.Fatal(err)
				}
			}
			if err := invoiceReportingMutateWithTriggersRestored(f, testCase.table, "UPDATE "+testCase.table+" SET "+testCase.column+"="+testCase.expression); err != nil {
				t.Fatal(err)
			}
			if testCase.ignoreChecks {
				if _, err := f.tx.ExecContext(f.ctx, "PRAGMA ignore_check_constraints=OFF"); err != nil {
					t.Fatal(err)
				}
			}
			before := invoiceReportingBudgetState(t, f)
			want := ErrRepository
			if testCase.bound {
				want = ErrSourceBound
			}
			got, err := port.SummarizeBASKeys(f.ctx, orgID)
			if got != nil || !errors.Is(err, want) || probe.keyReads != 0 || probe.bodyReads != 0 {
				t.Fatalf("corrupt summary present=%t error=%v key/body reads %d/%d", got != nil, err, probe.keyReads, probe.bodyReads)
			}
			keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, 16384, 8388608)
			if keys != nil || !errors.Is(err, want) || probe.keyReads != 0 || probe.bodyReads != 0 {
				t.Fatalf("corrupt read present=%t error=%v key/body reads %d/%d", keys != nil, err, probe.keyReads, probe.bodyReads)
			}
			if !f.tx.Authenticated() || before != invoiceReportingBudgetState(t, f) {
				t.Fatal("scalar readers mutated retained state or caller transaction")
			}
		})
	}
}

type invoiceBASScalarProbe struct {
	*sqlcipher.Transaction
	keyReads, bodyReads, summaryReads int
	beforeKeyRead                     func() error
	afterKeyQuery                     func()
}

func (probe *invoiceBASScalarProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "/* BAS invoice scalar summary */") {
		probe.summaryReads++
	}
	if strings.Contains(query, "/* BAS invoice scalar keys */") {
		probe.keyReads++
		if probe.beforeKeyRead != nil {
			change := probe.beforeKeyRead
			probe.beforeKeyRead = nil
			if err := change(); err != nil {
				return nil, err
			}
		}
	}
	if isInvoiceReportingBodyQuery(query) {
		probe.bodyReads++
	}
	rows, err := probe.Transaction.QueryContext(ctx, query, args...)
	if err == nil && strings.Contains(query, "/* BAS invoice scalar keys */") && probe.afterKeyQuery != nil {
		after := probe.afterKeyQuery
		probe.afterKeyQuery = nil
		after()
	}
	return rows, err
}
