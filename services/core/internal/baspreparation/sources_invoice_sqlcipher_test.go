//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/organisations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Seed through the ordinary invoice and accounting writers, including elected
// commands and revision claims. Both receipts remain part of complete coverage
// even though the second receipt falls after the requested quarter.
func seedBASSourcePostedInvoice(t *testing.T, tx *sqlcipher.Transaction, org string, basis tammyv1.GstBasis) *tammyv1.Invoice {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	profiles, err := organisations.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := profiles.Get(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if profile.GstBasis != basis {
		t.Fatal("invoice fixture basis must be established when the organisation is created")
	}
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	template, err := accounting.LoadAUSmallBusinessV1()
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.InstallTemplate(ctx, org, template, now); err != nil {
		t.Fatal(err)
	}
	repo, err := invoices.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	draft := &tammyv1.InvoiceDraft{Number: "COMBINED-POSTED", IssueDate: "2026-08-10", DueDate: "2026-08-10",
		CustomerName: "Customer", CustomerAddress: "1 High Street", SellerAddress: "27 Cambridge Road", GstRegistered: true,
		Lines: []*tammyv1.InvoiceLineInput{
			{Description: "Taxable consulting", QuantityMilli: 1000, UnitPriceMinor: 10000, TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_TAXABLE},
			{Description: "GST-free consulting", QuantityMilli: 1000, UnitPriceMinor: 2000, TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE},
		}}
	totals, err := invoices.Calculate(draft)
	if err != nil {
		t.Fatal(err)
	}
	invoice := &tammyv1.Invoice{Id: testUUID(9800), OrganisationId: org, Version: 1, State: tammyv1.InvoiceState_INVOICE_STATE_DRAFT,
		SellerName: profile.LegalName, SellerAbn: profile.Abn, Draft: draft,
		SubtotalMinor: totals.SubtotalMinor, GstMinor: totals.GstMinor, TotalMinor: totals.TotalMinor, CostMinor: totals.CostMinor, LineTotals: totals.LineTotals,
		CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}
	if err := repo.Save(ctx, invoice, 0); err != nil {
		t.Fatal(err)
	}
	invoice.Version++
	invoice.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	if err := repo.Save(ctx, invoice, 1); err != nil {
		t.Fatal(err)
	}
	next := uint64(9810)
	newID := func() (string, error) { next++; return testUUID(next), nil }
	elect := func(method string) string {
		id, _ := newID()
		_, err := tx.ExecContext(ctx, `INSERT INTO command_idempotency_v1(
			workspace_id,actor_user_id,fully_qualified_rpc_name,operation_key,
			semantic_hash_version,request_type,normalized_hash,outcome,created_at)
			VALUES(?,?,?,?,'v1','bas-source-test',zeroblob(32),'ELECTED',?)`,
			org, profile.OwnerUserId, "tammy.v1.InvoiceService."+method, id, now.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	writer, err := invoices.NewAccountingRepository(tx, func() time.Time { return now }, newID)
	if err != nil {
		t.Fatal(err)
	}
	posted, err := writer.Post(ctx, invoice, profile, elect("PostInvoiceIncome"))
	if err != nil {
		t.Fatal(err)
	}
	version := posted.Version
	for i, date := range []*tammyv1.CivilDate{{Year: 2026, Month: 8, Day: 15}, {Year: 2026, Month: 10, Day: 1}} {
		result, _, err := writer.RecordPayment(ctx, invoice, profile, &tammyv1.RecordInvoicePaymentRequest{
			CommandContext: &tammyv1.CommandContext{IdempotencyKey: elect("RecordInvoicePayment")}, OrganisationId: org, InvoiceId: invoice.Id,
			ExpectedAccountingVersion: version, PaymentDate: date, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 6500},
			BankAccountId: "018f0000-0000-7000-8000-000000000101", Reference: fmt.Sprintf("Combined receipt %d", i+1)})
		if err != nil {
			t.Fatal(err)
		}
		version = result.Version
	}
	return invoice
}

func TestCaptureBASBoundedSourceRecordsNativePostedIncomeAndPartialReceipts(t *testing.T) {
	for _, basis := range []tammyv1.GstBasis{tammyv1.GstBasis_GST_BASIS_NON_CASH, tammyv1.GstBasis_GST_BASIS_CASH} {
		t.Run(basis.String(), func(t *testing.T) {
			_, tx, request, ports, _, probe := newBASSourceProducerNativeFixtureWithBasis(t, false, basis)
			invoice := seedBASSourcePostedInvoice(t, tx, request.OrganisationID, basis)
			beforeChanges, beforeRevision := querySourceScalarInt(t, tx, "SELECT total_changes()"), sourceScalarRevisionState(t, tx)
			got, err := CaptureBASBoundedSourceRecords(context.Background(), request, ports)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got.Invoices.Candidates) != 1 || len(got.InvoiceCandidates) != 6 {
				t.Fatalf("complete posted source projection=%#v", got)
			}
			root := got.Invoices.Candidates[0]
			if root.ID != invoice.Id || root.Posting == nil || len(root.Posting.IncomeLines) != 2 || len(root.Receipts) != 2 || got.Invoices.JournalCount != 3 {
				t.Fatalf("posted invoice/receipts/journals were not retained: %#v", root)
			}
			families := make(map[invoices.BASScalarKeyKind]int)
			for _, key := range got.InvoiceKeys {
				families[key.Kind]++
			}
			if len(families) != 5 || len(got.InvoiceKeys) != 10 {
				t.Fatalf("all physical invoice families=%v (%d keys)", families, len(got.InvoiceKeys))
			}
			if got.Accounting == nil || len(got.Accounting.Facts) != 0 {
				t.Fatalf("writer unexpectedly emitted independent tax facts: %#v", got.Accounting)
			}
			if probe.writes != 0 {
				t.Fatalf("capture made %d writes", probe.writes)
			}
			assertSourceScalarTransactionUnchanged(t, tx, beforeChanges, beforeRevision)
		})
	}
}
