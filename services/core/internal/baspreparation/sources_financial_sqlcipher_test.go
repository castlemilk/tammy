//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestCaptureBASFinancialSourceRecordsNativeEmptyAndPostedIncome(t *testing.T) {
	for _, posted := range []bool{false, true} {
		for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_CASH, tammyv1.GstBasis_GST_BASIS_NON_CASH} {
			t.Run(basis.String()+"/posted="+map[bool]string{false: "false", true: "true"}[posted], func(t *testing.T) {
				_, tx, source, ports, _, probe := newBASSourceProducerNativeFixtureWithBasis(t, false, basis)
				if posted {
					seedBASSourcePostedInvoice(t, tx, source.OrganisationID, basis)
				}
				request := BASFinancialSourceRequest{Source: source, GstBasis: basis, AsOf: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
				before, revision := querySourceScalarInt(t, tx, "SELECT total_changes()"), sourceScalarRevisionState(t, tx)
				got, err := CaptureBASFinancialSourceRecords(context.Background(), request, ports)
				if err != nil || got == nil || got.Financial == nil || got.Financial.Invoices == nil || got.Financial.Purchases == nil {
					t.Fatalf("financial source capture=%#v, %v", got, err)
				}
				wantG1, want1A := int64(0), int64(0)
				if posted {
					wantG1, want1A = 13000, 1000
					if basis == tammyv1.GstBasis_GST_BASIS_CASH {
						wantG1, want1A = 6500, 500
					}
				}
				if got.Financial.Invoices.G1GrossMinor != wantG1 || got.Financial.Invoices.GstOnSales1AMinor != want1A || len(got.Financial.Purchases.Units) != 0 {
					t.Fatalf("incorrect finite financial mapping: %#v", got.Financial)
				}
				if probe.writes != 0 {
					t.Fatal("financial capture wrote")
				}
				assertSourceScalarTransactionUnchanged(t, tx, before, revision)
				cancelled, cancel := context.WithCancel(context.Background())
				cancel()
				if partial, err := CaptureBASFinancialSourceRecords(cancelled, request, ports); partial != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled capture=%#v,%v", partial, err)
				}
			})
		}
	}
}
