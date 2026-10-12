//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"encoding/hex"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"testing"
)

func TestInvoiceReportingPreBudgetFingerprintCompatibility(t *testing.T) {
	fixture := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_CASH)
	invoice := fixture.saveDraft(t, invoiceReportingID(5801), "COMPAT-PAID", "2026-10-03", 1000,
		tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE)
	fixture.issue(t, invoice)
	fixture.post(t, invoice)
	fixture.recordPayment(t, invoice, 1, 1, 1)
	fixture.saveDraft(t, invoiceReportingID(5802), "COMPAT-UNDATED", "", 0,
		tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE)
	port, err := NewReportingPort(fixture.tx)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := port.CaptureBAS(fixture.ctx, orgID, invoiceReportingPeriod())
	if err != nil || capture == nil || len(capture.Candidates) != 2 {
		t.Fatalf("legacy fingerprint fixture = %#v, %v", capture, err)
	}
	// Pinned against the original reader before the additive budget APIs.
	if got := hex.EncodeToString(capture.Fingerprint); got != "017b2f706ee600fa97cb7b6a4ffc10bf4feab6789c99c3f417796cfa837f9ca6" {
		t.Fatalf("legacy fingerprint changed: %s", got)
	}
}
