package invoices

import (
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"testing"
)

func validDraft() *tammyv1.InvoiceDraft {
	return &tammyv1.InvoiceDraft{Number: "INV-001", IssueDate: "2026-09-30", DueDate: "2026-10-14", CustomerName: "Customer", CustomerAddress: "1 High Street", SellerAddress: "27 Cambridge Road", GstRegistered: true,
		Lines: []*tammyv1.InvoiceLineInput{{Description: "Consulting", QuantityMilli: 1500, UnitPriceMinor: 10005, UnitCostMinor: ptr(int64(4000)), TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE}}}
}
func ptr[T any](v T) *T { return &v }

func TestInvoiceTotalsUseIntegerHalfUpPerLine(t *testing.T) {
	calculated, err := Calculate(validDraft())
	if err != nil {
		t.Fatal(err)
	}
	if calculated.SubtotalMinor != 15008 || calculated.GstMinor != 1501 || calculated.TotalMinor != 16509 || calculated.CostMinor != 6000 {
		t.Fatalf("unexpected exact totals: %v", calculated)
	}
	if calculated.LineTotals[0].GetCostMinor() != 6000 {
		t.Fatal("cost lost")
	}
}
func TestDraftValidationRejectsTaxAndInvalidInputs(t *testing.T) {
	cases := map[string]func(*tammyv1.InvoiceDraft){
		"unregistered GST":  func(d *tammyv1.InvoiceDraft) { d.GstRegistered = false },
		"invalid ABN":       func(d *tammyv1.InvoiceDraft) { d.CustomerAbn = "12345678901" },
		"invalid date":      func(d *tammyv1.InvoiceDraft) { d.IssueDate = "2026-02-30" },
		"year zero":         func(d *tammyv1.InvoiceDraft) { d.IssueDate = "0000-01-01" },
		"historical date":   func(d *tammyv1.InvoiceDraft) { d.IssueDate = "1899-01-01" },
		"control character": func(d *tammyv1.InvoiceDraft) { d.Lines[0].Description = "Consulting\x01" },
		"due before issue":  func(d *tammyv1.InvoiceDraft) { d.DueDate = "2026-01-01" },
		"no lines":          func(d *tammyv1.InvoiceDraft) { d.Lines = nil },
		"zero quantity":     func(d *tammyv1.InvoiceDraft) { d.Lines[0].QuantityMilli = 0 },
		"price overflow":    func(d *tammyv1.InvoiceDraft) { d.Lines[0].UnitPriceMinor = 100000001 },
		"unknown treatment": func(d *tammyv1.InvoiceDraft) { d.Lines[0].TaxTreatment = 99 },
		"bad source":        func(d *tammyv1.InvoiceDraft) { d.Lines[0].SourceDocumentId = ptr("not-an-id") },
		"blank description": func(d *tammyv1.InvoiceDraft) { d.Lines[0].Description = " " },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			d := validDraft()
			change(d)
			if _, err := Calculate(d); err == nil {
				t.Fatal("invalid draft accepted")
			}
		})
	}
}
func TestIssueRequiresCompleteDetailsWhileDraftAllowsIncomplete(t *testing.T) {
	d := validDraft()
	d.Number = ""
	d.CustomerAddress = ""
	d.IssueDate = ""
	d.DueDate = ""
	if _, err := Calculate(d); err != nil {
		t.Fatalf("incomplete draft rejected: %v", err)
	}
	if ValidateIssue(d) == nil {
		t.Fatal("incomplete invoice issued")
	}
	if ValidateIssue(validDraft()) != nil {
		t.Fatal("complete invoice denied")
	}
}
func TestNonTaxableLinesDoNotAccrueGSTAndUnknownCostIsDistinct(t *testing.T) {
	d := validDraft()
	d.GstRegistered = false
	d.Lines[0].TaxTreatment = tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE
	d.Lines[0].UnitCostMinor = nil
	v, err := Calculate(d)
	if err != nil {
		t.Fatal(err)
	}
	if v.GstMinor != 0 || v.TotalMinor != 15008 || v.LineTotals[0].CostMinor != nil {
		t.Fatalf("unexpected GST or unknown cost: %v", v)
	}
}
