//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestInvoiceBASScalarKeysEmptyReservation(t *testing.T) {
	f := newInvoiceReportingFixture(t, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	port, err := NewReportingPort(f.tx)
	if err != nil {
		t.Fatal(err)
	}
	before := invoiceReportingBudgetState(t, f)
	summary, err := port.SummarizeBASKeys(f.ctx, orgID)
	if err != nil || summary == nil || summary.OrganisationID != orgID || summary.RootCount != 0 || summary.ReceiptCount != 0 || summary.ScalarKeyCount != 0 || summary.ScalarBytes != 0 {
		t.Fatalf("empty scalar summary = %#v, %v", summary, err)
	}
	keys, err := port.ReadBASKeys(f.ctx, orgID, *summary, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("empty scalar keys = %#v, %v", keys, err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if got, err := port.SummarizeBASKeys(ctx, orgID); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled summary = %#v, %v", got, err)
	}
	if got, err := port.ReadBASKeys(ctx, orgID, *summary, 0, 0); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled keys = %#v, %v", got, err)
	}
	if !f.tx.Authenticated() || invoiceReportingBudgetState(t, f) != before {
		t.Fatal("scalar APIs changed retained state or caller transaction")
	}
}
