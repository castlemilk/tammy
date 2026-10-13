//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"errors"
	"runtime"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceBASScalarKeysGuardCumulativeAndSentinelCells(t *testing.T) {
	for _, sentinel := range []bool{false, true} {
		name := "cumulative bytes"
		if sentinel {
			name = "count sentinel"
		}
		t.Run(name, func(t *testing.T) {
			f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			firstID, secondID := invoiceReportingID(89001), invoiceReportingID(89002)
			f.saveDraft(t, firstID, "CUM-FIRST", "", 0, tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			seedSecond := func() { f.saveDraft(t, secondID, "CUM-SECOND", "", 0, tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE) }
			if !sentinel {
				seedSecond()
			}
			// Isolated storage fixtures exercise selected opaque SQL identities;
			// these are not valid public invoice commands or complete body proofs.
			if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1", `UPDATE invoices_v1 SET id='a'||CAST(zeroblob(3145727) AS TEXT) WHERE id=?`, firstID); err != nil {
				t.Fatal(err)
			}
			probe := &invoiceBASScalarProbe{Transaction: f.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := port.SummarizeBASKeys(f.ctx, orgID)
			if err != nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			probe.beforeKeyRead = func() error {
				if sentinel {
					seedSecond()
				}
				if err := invoiceReportingMutateWithTriggersRestored(f, "invoices_v1", `UPDATE invoices_v1 SET id='b'||CAST(zeroblob(3145727) AS TEXT) WHERE id=?`, secondID); err != nil {
					return err
				}
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, 8388608)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			var bound *SourceBoundError
			if keys != nil || !errors.As(err, &bound) || probe.keyReads != 1 || probe.bodyReads != 0 {
				t.Fatalf("drift returned keys=%t error kind=%T key/body reads=%d/%d", keys != nil, err, probe.keyReads, probe.bodyReads)
			}
			wantLimit := summary.ScalarBytes
			if sentinel {
				wantLimit = summary.ScalarKeyCount
			}
			if bound.Limit != wantLimit {
				t.Errorf("bound limit=%d, want actual reservation=%d", bound.Limit, wantLimit)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 5*1024*1024 {
				t.Errorf("read copied a rejected second 3 MiB key: allocated %d bytes", allocated)
			}
			if len(err.Error()) > 1024 || !f.tx.Authenticated() {
				t.Fatal("unbounded error or caller transaction changed")
			}
		})
	}
}

func TestInvoiceBASScalarKeysGuardSelectedCellsAfterSummary(t *testing.T) {
	for _, testCase := range []struct {
		name, table, column, expression string
		bound                           bool
	}{
		{"root version", "invoices_v1", "version", "zeroblob(8388609)", false},
		{"posting version", "invoice_income_postings_v1", "invoice_version", "zeroblob(8388609)", false},
		{"income sequence", "invoice_income_lines_v1", "sequence", "zeroblob(8388609)", false},
		{"allocation sequence", "invoice_payment_lines_v1", "sequence", "zeroblob(8388609)", false},
		{"selected root ID", "invoices_v1", "id", "CAST(zeroblob(8388609) AS TEXT)", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
			invoice := f.saveDraft(t, invoiceReportingID(89101), "CELL-DRIFT", "2026-10-12", 1000, tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
			if !testCase.bound {
				f.issue(t, invoice)
				f.post(t, invoice)
				f.recordPayment(t, invoice, 1, 1, 1)
			}
			probe := &invoiceBASScalarProbe{Transaction: f.tx}
			port, err := NewReportingPort(probe)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := port.SummarizeBASKeys(f.ctx, orgID)
			if err != nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			probe.beforeKeyRead = func() error {
				if err := invoiceReportingMutateWithTriggersRestored(f, testCase.table, "UPDATE "+testCase.table+" SET "+testCase.column+"="+testCase.expression); err != nil {
					return err
				}
				runtime.ReadMemStats(&before)
				return nil
			}
			keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, summary.ScalarKeyCount, 8388608)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			want := ErrRepository
			if testCase.bound {
				want = ErrSourceBound
			}
			if keys != nil || !errors.Is(err, want) || probe.keyReads != 1 || probe.bodyReads != 0 {
				t.Fatalf("unsafe cell keys=%t error kind=%T key/body reads=%d/%d", keys != nil, err, probe.keyReads, probe.bodyReads)
			}
			if len(err.Error()) > 1024 {
				t.Errorf("error expanded corrupt cell into %d bytes", len(err.Error()))
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 2*1024*1024 {
				t.Errorf("guarded read allocated %d bytes after fixture mutation", allocated)
			}
		})
	}
}
