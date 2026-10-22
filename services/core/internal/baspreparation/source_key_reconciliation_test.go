package baspreparation

import (
	"context"
	"errors"
	"github.com/tammyapp/tammy/services/core/internal/accounting"
	v1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"testing"
)

func TestReconcileBASInvoicePhysicalKeysRejectsSameCountIdentityAndVersionDrift(t *testing.T) {
	version := uint64(2)
	receiptID, journalID := "receipt", "journal"
	sequence := uint64(1)
	hash := make([]byte, 32)
	keys := []invoices.BASScalarKey{
		{Kind: invoices.BASScalarInvoice, OrganisationID: basSharedTestOrganisation, InvoiceID: "invoice", RootVersion: &version},
		{Kind: invoices.BASScalarPosting, OrganisationID: basSharedTestOrganisation, InvoiceID: "invoice", PostingInvoiceVersion: &version, JournalID: &journalID, SourceHash: hash},
		{Kind: invoices.BASScalarIncomeLine, OrganisationID: basSharedTestOrganisation, InvoiceID: "invoice", Sequence: &sequence},
		{Kind: invoices.BASScalarReceipt, OrganisationID: basSharedTestOrganisation, InvoiceID: "invoice", ReceiptID: &receiptID, JournalID: &journalID, SourceHash: hash},
		{Kind: invoices.BASScalarAllocation, OrganisationID: basSharedTestOrganisation, InvoiceID: "invoice", ReceiptID: &receiptID, Sequence: &sequence},
	}
	capture := &invoices.BASReportingCapture{Candidates: []invoices.BASInvoiceCandidate{{ID: "invoice", OrganisationID: basSharedTestOrganisation, Version: 2,
		Posting:  &invoices.BASInvoicePosting{InvoiceID: "invoice", OrganisationID: basSharedTestOrganisation, InvoiceVersion: 2, JournalID: &journalID, SourceHash: hash, IncomeLines: []invoices.BASInvoiceIncomeLine{{InvoiceID: "invoice", Sequence: 1}}},
		Receipts: []invoices.BASInvoiceReceipt{{ID: receiptID, InvoiceID: "invoice", OrganisationID: basSharedTestOrganisation, JournalID: journalID, SourceHash: hash, Allocations: []invoices.BASInvoicePaymentLine{{PaymentID: receiptID, Sequence: 1}}}},
	}}}
	if err := reconcileBASInvoicePhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err != nil {
		t.Fatal(err)
	}
	capture.Candidates[0].Posting.IncomeLines[0].Sequence = 2
	if err := reconcileBASInvoicePhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err == nil {
		t.Fatal("same-count income sequence drift accepted")
	}
	capture.Candidates[0].Posting.IncomeLines[0].Sequence = 1
	capture.Candidates[0].Receipts[0].Allocations[0].Sequence = 2
	if err := reconcileBASInvoicePhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err == nil {
		t.Fatal("same-count receipt allocation sequence drift accepted")
	}
	capture.Candidates[0].Receipts[0].Allocations[0].Sequence = 1
	capture.Candidates[0].Version = 3
	if err := reconcileBASInvoicePhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err == nil {
		t.Fatal("root version drift accepted")
	}
}
func TestReconcileBASTaxFactPhysicalKeysChecksLineAndNullableProvenance(t *testing.T) {
	purchase, capture := basSharedTestFactCaptures()
	_ = purchase
	var keys []accounting.TaxFactBASScalarKey
	for index := range capture.Facts {
		fact := &capture.Facts[index]
		fact.Journal.Lines = []*v1.JournalLine{{Id: fact.JournalLineID, JournalId: fact.Journal.Id, Sequence: 1}}
		keys = append(keys, accounting.TaxFactBASScalarKey{ID: fact.ID, OrganisationID: fact.OrganisationID, JournalLineID: fact.JournalLineID, JournalID: fact.Journal.Id, LineNumber: 1, SourceType: fact.SourceType, SourceID: fact.SourceID, SourceRevision: fact.SourceRevision})
	}
	if err := reconcileBASTaxFactPhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err != nil {
		t.Fatal(err)
	}
	capture.Facts[0].SourceContentHash = []byte{}
	if err := reconcileBASTaxFactPhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err == nil {
		t.Fatal("non-nil absent source hash accepted")
	}
	capture.Facts[0].SourceContentHash = nil
	capture.Facts[0].Journal.Lines[0].Sequence = 2
	if err := reconcileBASTaxFactPhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err == nil {
		t.Fatal("same-count fact line sequence drift accepted")
	}
}
func TestReconcileBASPhysicalKeysRejectsDuplicateMissingAndCancellation(t *testing.T) {
	version := uint64(1)
	key := invoices.BASScalarKey{Kind: invoices.BASScalarInvoice, OrganisationID: basSharedTestOrganisation, InvoiceID: "invoice", RootVersion: &version}
	capture := &invoices.BASReportingCapture{Candidates: []invoices.BASInvoiceCandidate{{ID: "invoice", OrganisationID: basSharedTestOrganisation, Version: 1}}}
	for _, keys := range [][]invoices.BASScalarKey{nil, {key, key}} {
		if err := reconcileBASInvoicePhysicalKeys(context.Background(), basSharedTestOrganisation, keys, capture); err == nil {
			t.Fatal("incomplete or duplicate selected physical set accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reconcileBASInvoicePhysicalKeys(ctx, basSharedTestOrganisation, []invoices.BASScalarKey{key}, capture); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := reconcileBASTaxFactPhysicalKeys(ctx, basSharedTestOrganisation, nil, &accounting.TaxFactReportingCapture{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("fact cancellation: %v", err)
	}
}
