//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Draft native test for package baspreparation. It uses the new module-owned
// producer for source inputs and calls mature reporting only as a test oracle.
package baspreparation

import (
	"context"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/reporting"
)

func TestMapBASInvoiceFinancialEffectsNativeMatchesMatureWriterBackedSales(t *testing.T) {
	ctx := context.Background()
	for _, basis := range []tammyv1.GstBasis{
		tammyv1.GstBasis_GST_BASIS_NON_CASH,
		tammyv1.GstBasis_GST_BASIS_CASH,
	} {
		t.Run(basis.String(), func(t *testing.T) {
			_, tx, request, ports, _, probe := newBASSourceProducerNativeFixtureWithBasis(t, false, basis)
			invoice := seedBASSourcePostedInvoice(t, tx, request.OrganisationID, basis)
			beforeChanges := querySourceScalarInt(t, tx, "SELECT total_changes()")
			beforeRevision := sourceScalarRevisionState(t, tx)

			captured, err := CaptureBASBoundedSourceRecords(ctx, request, ports)
			if err != nil || captured == nil {
				t.Fatalf("native source producer returned %#v, %v", captured, err)
			}
			if len(captured.Invoices.Candidates) != 1 || len(captured.InvoiceCandidates) != 6 {
				t.Fatalf("complete invoice structural census: %d roots, %d candidates", len(captured.Invoices.Candidates), len(captured.InvoiceCandidates))
			}
			root := captured.Invoices.Candidates[0]
			if root.ID != invoice.Id || len(root.Receipts) != 2 || root.Receipts[0].PaymentDate != "2026-08-15" ||
				root.Receipts[1].PaymentDate != "2026-10-01" {
				t.Fatalf("capture did not retain both actual receipts: %#v", root.Receipts)
			}

			period := request.ConfirmedPeriod.Confirmation.Period
			mapped, err := MapBASInvoiceFinancialEffects(ctx, captured.Invoices, captured.InvoiceCandidates,
				basis, period)
			if err != nil || mapped == nil {
				t.Fatalf("pure financial mapper returned %#v, %v", mapped, err)
			}
			if len(mapped.Units) != len(captured.InvoiceCandidates) {
				t.Fatalf("mapped %d of %d nominated invoice/receipt units", len(mapped.Units), len(captured.InvoiceCandidates))
			}
			if probe.writes != 0 {
				t.Fatalf("bounded source capture made %d SQL writes", probe.writes)
			}
			assertSourceScalarTransactionUnchanged(t, tx, beforeChanges, beforeRevision)

			// The mature report is deliberately used only as an independent test
			// oracle; mapper inputs above are exclusively retained module captures.
			mature, err := reporting.NewRepository(tx)
			if err != nil {
				t.Fatal(err)
			}
			want, err := mature.CreateBASDraft(ctx, testUUID(9871), testUUID(9872), request.OrganisationID,
				period.StartDate, period.EndDate, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("mature BAS oracle failed: %v", err)
			}
			if want.SalesG1 == nil || want.GstOnSales_1A == nil ||
				mapped.G1GrossMinor != want.SalesG1.MinorUnits || mapped.GstOnSales1AMinor != want.GstOnSales_1A.MinorUnits {
				t.Fatalf("basis %s mapper G1/1A=%d/%d differs from mature report %v/%v",
					basis, mapped.G1GrossMinor, mapped.GstOnSales1AMinor, want.SalesG1, want.GstOnSales_1A)
			}
		})
	}
}
