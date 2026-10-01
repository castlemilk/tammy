//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	tammyv1connect "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1/tammyv1connect"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"google.golang.org/protobuf/proto"
)

func assertInvoiceIncomeThroughRealServer(t *testing.T, module *LedgerModule, client tammyv1connect.InvoiceServiceClient, auth *tammyv1.AuthenticationContext, invoice *tammyv1.Invoice, capability string) {
	t.Helper()
	ctx := context.Background()
	get := &tammyv1.GetInvoiceAccountingRequest{Authentication: auth, OrganisationId: invoice.OrganisationId, InvoiceId: invoice.Id}
	current, err := client.GetInvoiceAccounting(ctx, brandingRequest(get, capability))
	if err != nil || !current.Msg.Accounting.Posted || current.Msg.Accounting.OutstandingMinor != 38500 || current.Msg.Accounting.Version != 1 {
		t.Fatalf("actual accounting read=%v error=%v", current, err)
	}
	foreign := proto.Clone(get).(*tammyv1.GetInvoiceAccountingRequest)
	foreign.OrganisationId = "018f0000-0000-7000-8000-000000000999"
	if _, err := client.GetInvoiceAccounting(ctx, brandingRequest(foreign, capability)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("foreign accounting read=%v", err)
	}
	payment := &tammyv1.RecordInvoicePaymentRequest{CommandContext: &tammyv1.CommandContext{Authentication: auth, IdempotencyKey: "018f0000-0000-7000-8000-000000000880"}, OrganisationId: invoice.OrganisationId, InvoiceId: invoice.Id, ExpectedAccountingVersion: 1, PaymentDate: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30}, Amount: &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 16050}, BankAccountId: "018f0000-0000-7000-8000-000000000101", Reference: "Customer receipt"}
	denied := proto.Clone(payment).(*tammyv1.RecordInvoicePaymentRequest)
	denied.CommandContext.Authentication.SessionId = "018f0000-0000-7000-8000-000000000999"
	if _, err := client.RecordInvoicePayment(ctx, brandingRequest(denied, capability)); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unauthenticated receipt=%v", err)
	}
	result, err := client.RecordInvoicePayment(ctx, brandingRequest(payment, capability))
	if err != nil || result.Msg.Accounting.Version != 2 || result.Msg.Accounting.ReceivedMinor != 16050 || result.Msg.Accounting.OutstandingMinor != 22450 || result.Msg.Payment == nil {
		t.Fatalf("actual recorded receipt=%v error=%v", result, err)
	}
	replay, err := client.RecordInvoicePayment(ctx, brandingRequest(payment, capability))
	if err != nil || !proto.Equal(replay.Msg, result.Msg) {
		t.Fatalf("receipt replay=%v error=%v", replay, err)
	}
	changed := proto.Clone(payment).(*tammyv1.RecordInvoicePaymentRequest)
	changed.Reference = "Changed same key"
	if _, err := client.RecordInvoicePayment(ctx, brandingRequest(changed, capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("receipt semantic conflict=%v", err)
	}
	stale := proto.Clone(payment).(*tammyv1.RecordInvoicePaymentRequest)
	stale.CommandContext.IdempotencyKey = "018f0000-0000-7000-8000-000000000881"
	if _, err := client.RecordInvoicePayment(ctx, brandingRequest(stale, capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("receipt stale=%v", err)
	}
	existing := &tammyv1.PostInvoiceIncomeRequest{CommandContext: &tammyv1.CommandContext{Authentication: auth, IdempotencyKey: "018f0000-0000-7000-8000-000000000882"}, OrganisationId: invoice.OrganisationId, InvoiceId: invoice.Id, ExpectedVersion: invoice.Version}
	if _, err := client.PostInvoiceIncome(ctx, brandingRequest(existing, capability)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("duplicate income=%v", err)
	}
	var payments, audits int
	if err := module.db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_payment_v1`).Scan(&payments); err != nil || payments != 1 {
		t.Fatalf("actual retained receipts=%d error=%v", payments, err)
	}
	if err := module.db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_income_command_audit_v1`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("actual audited receipt=%d error=%v", audits, err)
	}
	// Simulate a retained issued document from before accounting integration.
	// Reading it must preserve the explicit review step instead of backfilling.
	legacy := proto.Clone(invoice).(*tammyv1.Invoice)
	legacy.Id = "018f0000-0000-7000-8000-000000000883"
	legacy.Version = 1
	legacy.State = tammyv1.InvoiceState_INVOICE_STATE_DRAFT
	legacy.Draft.Number = "LEGACY-ZERO"
	for _, line := range legacy.Draft.Lines {
		line.UnitPriceMinor = 0
	}
	totals, err := invoices.Calculate(legacy.Draft)
	if err != nil {
		t.Fatal(err)
	}
	legacy.SubtotalMinor = totals.SubtotalMinor
	legacy.GstMinor = totals.GstMinor
	legacy.TotalMinor = totals.TotalMinor
	legacy.CostMinor = totals.CostMinor
	legacy.LineTotals = totals.LineTotals
	documents, _ := invoices.NewRepository(module.db)
	if err = documents.Save(ctx, legacy, 0); err != nil {
		t.Fatal(err)
	}
	legacy.Version = 2
	legacy.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
	if err = documents.Save(ctx, legacy, 1); err != nil {
		t.Fatal(err)
	}
	legacyGet := proto.Clone(get).(*tammyv1.GetInvoiceAccountingRequest)
	legacyGet.InvoiceId = legacy.Id
	if read, err := client.GetInvoiceAccounting(ctx, brandingRequest(legacyGet, capability)); err != nil || read.Msg.Accounting.Posted || read.Msg.Accounting.Version != 0 {
		t.Fatalf("legacy read silently posted=%v error=%v", read, err)
	}
	post := &tammyv1.PostInvoiceIncomeRequest{CommandContext: &tammyv1.CommandContext{Authentication: auth, IdempotencyKey: "018f0000-0000-7000-8000-000000000884"}, OrganisationId: legacy.OrganisationId, InvoiceId: legacy.Id, ExpectedVersion: legacy.Version}
	posted, err := client.PostInvoiceIncome(ctx, brandingRequest(post, capability))
	if err != nil || !posted.Msg.Accounting.Posted || posted.Msg.Accounting.Version != 1 || posted.Msg.Accounting.JournalId != nil {
		t.Fatalf("legacy reviewed posting=%v error=%v", posted, err)
	}
	if replay, err := client.PostInvoiceIncome(ctx, brandingRequest(post, capability)); err != nil || !proto.Equal(replay.Msg, posted.Msg) {
		t.Fatalf("legacy post replay=%v error=%v", replay, err)
	}
	if read, err := client.GetInvoice(ctx, brandingRequest(&tammyv1.GetInvoiceRequest{Authentication: auth, OrganisationId: legacy.OrganisationId, InvoiceId: legacy.Id}, capability)); err != nil || !proto.Equal(read.Msg.Invoice, legacy) {
		t.Fatalf("legacy accounting changed document=%v error=%v", read, err)
	}
	if err := module.db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_income_command_audit_v1`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("legacy post not audited exactly once=%d error=%v", audits, err)
	}
	// Every invoice remains reachable beyond the bounded initial page.
	for index := 0; index < 201; index++ {
		draft := proto.Clone(invoice).(*tammyv1.Invoice)
		draft.Id = fmt.Sprintf("018f0000-0000-7000-8000-%012x", 9000+index)
		draft.Version = 1
		draft.State = tammyv1.InvoiceState_INVOICE_STATE_DRAFT
		draft.Draft.Number = fmt.Sprintf("PAGING-%03d", index)
		if err := documents.Save(ctx, draft, 0); err != nil {
			t.Fatal(err)
		}
	}
	pageRequest := &tammyv1.ListInvoicesRequest{Authentication: auth, OrganisationId: invoice.OrganisationId, Page: &tammyv1.PageRequest{PageSize: 200}}
	first, err := client.ListInvoices(ctx, brandingRequest(pageRequest, capability))
	if err != nil || len(first.Msg.Invoices) != 200 || first.Msg.Page.NextCursor == nil {
		t.Fatalf("actualfirstinvoicepage=%v error=%v", first, err)
	}
	pageRequest.Page.Cursor = first.Msg.Page.NextCursor
	second, err := client.ListInvoices(ctx, brandingRequest(pageRequest, capability))
	if err != nil || len(second.Msg.Invoices) != 3 || second.Msg.Page.NextCursor != nil {
		t.Fatalf("actualolderinvoicepage=%v error=%v", second, err)
	}
	seen := map[string]bool{}
	for _, entry := range append(first.Msg.Invoices, second.Msg.Invoices...) {
		if seen[entry.Id] {
			t.Fatal("actualserverpaginationduplicatesinvoice")
		}
		seen[entry.Id] = true
	}
	if !seen[legacy.Id] || !seen[invoice.Id] {
		t.Fatal("old issued invoices missing from signed pagination")
	}
	for _, bad := range []struct {
		name, org, cursor string
		size              uint32
	}{{"forged", invoice.OrganisationId, "forged", 200}, {"noncanonical", invoice.OrganisationId, *first.Msg.Page.NextCursor + "=", 200}, {"wrongorganisation", "018f0000-0000-7000-8000-000000000999", *first.Msg.Page.NextCursor, 200}, {"wrongpagesize", invoice.OrganisationId, *first.Msg.Page.NextCursor, 100}} {
		invalid := proto.Clone(pageRequest).(*tammyv1.ListInvoicesRequest)
		invalid.OrganisationId = bad.org
		invalid.Page.Cursor = &bad.cursor
		invalid.Page.PageSize = bad.size
		if _, err := client.ListInvoices(ctx, brandingRequest(invalid, capability)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("%s invoicecursor accepted: %v", bad.name, err)
		}
	}
}
