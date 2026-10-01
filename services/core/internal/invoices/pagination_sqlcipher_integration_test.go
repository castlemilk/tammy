//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package invoices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestInvoicePagesRetrieveOldIssuedDocumentsBeyond200WithoutDuplicates(t *testing.T) {
	db := invoiceDB(t)
	ctx := context.Background()
	repo, _ := NewRepository(db)
	for index := 0; index < 205; index++ {
		invoice := savedInvoice()
		invoice.Id = fmt.Sprintf("018f0000-0000-7000-8000-%012x", 5000+index)
		invoice.Draft.Number = fmt.Sprintf("PAGE-%03d", index)
		if err := repo.Save(ctx, invoice, 0); err != nil {
			t.Fatal(err)
		}
		invoice.Version = 2
		invoice.State = tammyv1.InvoiceState_INVOICE_STATE_ISSUED
		if err := repo.Save(ctx, invoice, 1); err != nil {
			t.Fatal(err)
		}
	}
	first, next, err := repo.ListPage(ctx, orgID, "", 200)
	if err != nil || len(first) != 200 || next == "" {
		t.Fatalf("first=%d next=%q error=%v", len(first), next, err)
	}
	second, end, err := repo.ListPage(ctx, orgID, next, 200)
	if err != nil || len(second) != 5 || end != "" {
		t.Fatalf("second=%d next=%q error=%v", len(second), end, err)
	}
	all := append(first, second...)
	seen := map[string]bool{}
	for _, invoice := range all {
		if seen[invoice.Id] {
			t.Fatal("page repeated invoice")
		}
		seen[invoice.Id] = true
	}
	if all[204].Draft.Number != "PAGE-000" || all[204].State != tammyv1.InvoiceState_INVOICE_STATE_ISSUED {
		t.Fatalf("oldest issued document unreachable=%v", all[204])
	}
	replay, replayedCursor, err := repo.ListPage(ctx, orgID, next, 200)
	if err != nil || replayedCursor != "" || !proto.Equal(replay[4], second[4]) {
		t.Fatalf("page cursor replay changed=%v", err)
	}
	if _, _, err := repo.ListPage(ctx, "018f0000-0000-7000-8000-000000000999", next, 200); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("cross-tenant cursor=%v", err)
	}
	for _, cursor := range []string{"bad", next + "=", "v2:any", encodeInvoiceCursor(orgID, "2000-01-01T00:00:00Z", first[199].Id), encodeInvoiceCursor(orgID, first[199].CreatedAt.AsTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), "018f0000-0000-7000-8000-000000000999")} {
		if _, _, err := repo.ListPage(ctx, orgID, cursor, 200); !errors.Is(err, ErrInvalidDraft) {
			t.Fatalf("malformed or stale cursor accepted %q error=%v", cursor, err)
		}
	}
}

func TestInvoicePagesBoundLargeDocumentsAndRetainEveryNextPage(t *testing.T) {
	db := invoiceDB(t)
	ctx := context.Background()
	repo, _ := NewRepository(db)
	for index := 0; index < 12; index++ {
		invoice := savedInvoice()
		invoice.Id = fmt.Sprintf("018f0000-0000-7000-8000-%012x", 6000+index)
		invoice.Draft.Number = fmt.Sprintf("LARGE-%03d", index)
		invoice.Draft.Lines = make([]*tammyv1.InvoiceLineInput, 50)
		for line := range invoice.Draft.Lines {
			invoice.Draft.Lines[line] = &tammyv1.InvoiceLineInput{Description: strings.Repeat("王", 500), QuantityMilli: 1000, UnitPriceMinor: 100, TaxTreatment: tammyv1.TaxTreatment_TAX_TREATMENT_GST_FREE}
		}
		totals, err := Calculate(invoice.Draft)
		if err != nil {
			t.Fatal(err)
		}
		invoice.SubtotalMinor = totals.SubtotalMinor
		invoice.GstMinor = totals.GstMinor
		invoice.TotalMinor = totals.TotalMinor
		invoice.CostMinor = totals.CostMinor
		invoice.LineTotals = totals.LineTotals
		if err = repo.Save(ctx, invoice, 0); err != nil {
			t.Fatal(err)
		}
	}
	var retrieved, pages int
	cursor := ""
	for {
		values, next, err := repo.ListPage(ctx, orgID, cursor, 200)
		if err != nil {
			t.Fatal(err)
		}
		response := &tammyv1.ListInvoicesResponse{Invoices: values}
		if proto.Size(response) > 512*1024 {
			t.Fatalf("oversize page=%d bytes", proto.Size(response))
		}
		pages++
		retrieved += len(values)
		if next == "" {
			break
		}
		if len(values) == 0 || next == cursor {
			t.Fatal("pagination failed to progress")
		}
		cursor = next
	}
	if retrieved != 12 || pages < 2 {
		t.Fatalf("large invoice pages truncated: invoices=%d pages=%d", retrieved, pages)
	}
}
