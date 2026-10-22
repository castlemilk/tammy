package baspreparation

import (
	"bytes"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/invoices"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"google.golang.org/protobuf/proto"
)

func TestBASRebindSharedJournalBodiesUsesOneCanonicalRetainedProof(t *testing.T) {
	org, id := testUUID(4101), testUUID(4102)
	journal := &tammyv1.Journal{Id: id, OrganisationId: org}
	hash := bytes.Repeat([]byte{0x5a}, 32)
	proof := func() *accounting.JournalReportingRecord {
		return &accounting.JournalReportingRecord{
			Journal: proto.Clone(journal).(*tammyv1.Journal), ContentHash: append([]byte(nil), hash...), PayloadBytes: 4096,
		}
	}
	accountingProof, purchaseProof, postingProof, receiptProof := proof(), proof(), proof(), proof()
	invoiceCapture := &invoices.BASReportingCapture{Candidates: []invoices.BASInvoiceCandidate{{
		OrganisationID: org,
		Posting:        &invoices.BASInvoicePosting{JournalID: &id, Journal: postingProof},
		Receipts:       []invoices.BASInvoiceReceipt{{JournalID: id, Journal: receiptProof}},
	}}}
	purchaseCapture := &purchases.PurchaseCoverageReportingCapture{Journals: []*accounting.JournalReportingRecord{purchaseProof}}
	accountingCapture := &accounting.TaxFactReportingCapture{
		Journals: []*accounting.JournalReportingRecord{accountingProof},
		Facts:    []accounting.TaxFactReportingRecord{{ID: testUUID(4103), Journal: proto.Clone(journal).(*tammyv1.Journal)}},
	}

	if err := basRebindSharedJournalBodies(invoiceCapture, purchaseCapture, accountingCapture); err != nil {
		t.Fatal(err)
	}
	canonical := accountingCapture.Journals[0]
	if purchaseCapture.Journals[0] != canonical || invoiceCapture.Candidates[0].Posting.Journal != canonical ||
		invoiceCapture.Candidates[0].Receipts[0].Journal != canonical || accountingCapture.Facts[0].Journal != canonical.Journal {
		t.Fatal("shared journal body references were not rebound to one retained proof")
	}
}

func TestBASDropProvedPurchaseTaxFactBodiesKeepsExplicitOriginalMetadata(t *testing.T) {
	fingerprint := bytes.Repeat([]byte{0x24}, 32)
	capture := &purchases.PurchaseCoverageReportingCapture{
		Fingerprint: append([]byte(nil), fingerprint...), PayloadBytes: 8192, TaxFactPayloadBytes: 1024,
		Rows: []purchases.PurchaseReportingRetainedRow{{Table: "tax_facts"}, {Table: "purchase_postings_v1"}},
	}
	view, err := basDropProvedPurchaseTaxFactBodies(capture, 1024, &BASSharedFactProof{Count: 1, PurchaseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if !view.TaxFactBodiesReplaced || len(view.Capture.Rows) != 1 || view.Capture.Rows[0].Table != "purchase_postings_v1" ||
		view.OriginalPayloadBytes != 8192 || view.OriginalTaxFactPayloadBytes != 1024 || !bytes.Equal(view.OriginalFingerprint, fingerprint) ||
		!bytes.Equal(view.Capture.Fingerprint, fingerprint) {
		t.Fatalf("proven replacement lost explicit source metadata: %#v", view)
	}
	if len(capture.Rows) != 2 {
		t.Fatal("view construction mutated the source module capture")
	}
	if result, err := basDropProvedPurchaseTaxFactBodies(capture, 1025, &BASSharedFactProof{Count: 1, PurchaseBytes: 1024}); err == nil || result != nil {
		t.Fatalf("mismatched exact duplicate byte charge returned result=%#v err=%v", result, err)
	}
	if result, err := basDropProvedPurchaseTaxFactBodies(capture, 1024, &BASSharedFactProof{Count: 2, PurchaseBytes: 1024}); err == nil || result != nil {
		t.Fatalf("mismatched raw fact count returned result=%#v err=%v", result, err)
	}
}

func TestBASRebindSharedJournalBodiesRejectsDifferentRawProof(t *testing.T) {
	org, id := testUUID(4201), testUUID(4202)
	journal := &tammyv1.Journal{Id: id, OrganisationId: org}
	first := &accounting.JournalReportingRecord{Journal: proto.Clone(journal).(*tammyv1.Journal), ContentHash: bytes.Repeat([]byte{1}, 32), PayloadBytes: 4096}
	other := &accounting.JournalReportingRecord{Journal: proto.Clone(journal).(*tammyv1.Journal), ContentHash: bytes.Repeat([]byte{2}, 32), PayloadBytes: 4096}
	invoiceCapture := &invoices.BASReportingCapture{Candidates: []invoices.BASInvoiceCandidate{{OrganisationID: org, Posting: &invoices.BASInvoicePosting{JournalID: &id, Journal: other}}}}
	purchaseCapture := &purchases.PurchaseCoverageReportingCapture{Journals: []*accounting.JournalReportingRecord{first}}
	accountingCapture := &accounting.TaxFactReportingCapture{Journals: []*accounting.JournalReportingRecord{first}}
	// The production coordinator calls verifyBASSharedJournalCaptureProofs first;
	// this helper also refuses rebinding if a caller bypasses that boundary.
	if err := basRebindSharedJournalBodies(invoiceCapture, purchaseCapture, accountingCapture); err == nil {
		t.Fatal("different retained journal hash was silently replaced")
	}
}
