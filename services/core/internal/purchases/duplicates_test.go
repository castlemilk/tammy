package purchases

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

const duplicatePostingID = "018f0000-0000-7000-8000-000000000920"
const duplicateDocumentID = "018f0000-0000-7000-8000-000000000921"

func duplicatePosting() *tammyv1.PurchasePosting {
	return &tammyv1.PurchasePosting{Id: duplicatePostingID, OrganisationId: purchaseOrgID, DocumentId: duplicateDocumentID,
		ReviewedVersion: 7, OriginalContentHash: bytes.Repeat([]byte{1}, 32), ReviewedSnapshotHash: bytes.Repeat([]byte{2}, 32),
		SourceHash: bytes.Repeat([]byte{3}, 32), SupplierName: "Supplier", InvoiceNumber: "INV-1"}
}

func distinctDuplicateReview(id string) *tammyv1.PurchaseDuplicateReview {
	return &tammyv1.PurchaseDuplicateReview{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_DISTINCT_TRANSACTION,
		MatchedPostingId: &id, Reason: "Reviewed separate acquisition with reused invoice number"}
}

func TestNormalizeDuplicateIdentityUnicode(t *testing.T) {
	for _, test := range []struct {
		name, supplier, invoice, supplierKey, invoiceKey string
	}{
		{"canonical accents", "CAFÉ", "INVOICE-É", "café", "invoice-é"},
		{"decomposed accents", "CAFE\u0301", "INVOICE-E\u0301", "café", "invoice-é"},
		{"full case fold", "Straße Σς", "INV-ß-Σς", "strasse σσ", "inv-ss-σσ"},
		{"Unicode whitespace", "\u2003Café\u00a0\t Supplies\n", "\u2002INV\t\u2003 1\r", "café supplies", "inv 1"},
		{"compatibility invoice", "ＡＣＭＥ\u3000Ｓｕｐｐｌｉｅｓ", "ＩＮＶ－１２３", "acme supplies", "inv-123"},
		{"punctuation retained", "A & B Pty Ltd", "INV-001/2", "a & b pty ltd", "inv-001/2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			supplier, invoice, err := NormalizeDuplicateIdentity(test.supplier, test.invoice)
			if err != nil || supplier != test.supplierKey || invoice != test.invoiceKey {
				t.Fatalf("normalized=%q/%q error=%v want=%q/%q", supplier, invoice, err, test.supplierKey, test.invoiceKey)
			}
			if againSupplier, againInvoice, err := NormalizeDuplicateIdentity(supplier, invoice); err != nil || againSupplier != supplier || againInvoice != invoice {
				t.Fatalf("normalization is not idempotent: again=%q/%q error=%v", againSupplier, againInvoice, err)
			}
		})
	}
}

func TestNormalizeDuplicateIdentityRejectsMissingInvalidAndOversized(t *testing.T) {
	for _, test := range []struct{ name, supplier, invoice string }{
		{"missing supplier", "", "INV-1"},
		{"blank supplier", "\t\u00a0\u2003", "INV-1"},
		{"missing invoice", "Supplier", ""},
		{"blank invoice", "Supplier", "\u2002\n"},
		{"invalid supplier UTF-8", string([]byte{0xff}), "INV-1"},
		{"invalid invoice UTF-8", "Supplier", string([]byte{0xff})},
		{"oversized supplier", strings.Repeat("é", 513), "INV-1"},
		{"oversized invoice", "Supplier", strings.Repeat("é", 129)},
		{"expanded supplier too large", strings.Repeat("ﬃ", 171), "INV-1"},
		{"expanded invoice too large", "Supplier", strings.Repeat("ﬃ", 43)},
		{"case folded invoice too large", "Supplier", strings.Repeat("ß", 65)},
	} {
		t.Run(test.name, func(t *testing.T) {
			supplier, invoice, err := NormalizeDuplicateIdentity(test.supplier, test.invoice)
			if !errors.Is(err, ErrInvalidDuplicateIdentity) || supplier != "" || invoice != "" {
				t.Fatalf("invalid identity must fail closed: normalized=%q/%q error=%v", supplier, invoice, err)
			}
		})
	}
	if supplier, invoice, err := NormalizeDuplicateIdentity(strings.Repeat("é", 512), strings.Repeat("é", 128)); err != nil || supplier == "" || invoice == "" {
		t.Fatalf("rune limits rejected: normalized=%q/%q error=%v", supplier, invoice, err)
	}
	if supplier, invoice, err := NormalizeDuplicateIdentity(strings.Repeat("ﬃ", 170)+"XX", strings.Repeat("ß", 64)); err != nil || supplier != strings.Repeat("ffi", 170)+"xx" || invoice != strings.Repeat("ss", 64) {
		t.Fatalf("expanded key limits rejected: normalized=%q/%q error=%v", supplier, invoice, err)
	} else if againSupplier, againInvoice, err := NormalizeDuplicateIdentity(supplier, invoice); err != nil || againSupplier != supplier || againInvoice != invoice {
		t.Fatalf("expanded keys are not idempotent: again=%q/%q error=%v", againSupplier, againInvoice, err)
	}
}

func TestValidateDuplicateReviewNoMatchRequiresNoDiscoveredCandidates(t *testing.T) {
	review := &tammyv1.PurchaseDuplicateReview{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH}
	refs, err := ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), nil)
	if err != nil || len(refs) != 0 {
		t.Fatalf("no candidates: refs=%v error=%v", refs, err)
	}
	refs, err = ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), []*tammyv1.PurchasePosting{duplicatePosting()})
	if !errors.Is(err, ErrInvalidDuplicateReview) || len(refs) != 1 || refs[0].Id != duplicatePostingID {
		t.Fatalf("known candidate requires explicit distinct review: refs=%v error=%v", refs, err)
	}
}

func TestValidateDuplicateReviewDistinctRetainsExactServerPosting(t *testing.T) {
	posting := duplicatePosting()
	// Supplier + invoice identity is sufficient: a changed date and amount do
	// not make a server-discovered semantic match disappear.
	posting.InvoiceDate, posting.Gross = purchaseDate(1), purchaseMoney(3000)
	review := distinctDuplicateReview(posting.Id)
	beforePosting, beforeReview := proto.Clone(posting), proto.Clone(review)
	refs, err := ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), []*tammyv1.PurchasePosting{posting})
	if err != nil || len(refs) != 1 || refs[0].Type != "purchase_posting" || refs[0].Id != posting.Id || refs[0].Revision != 1 || !bytes.Equal(refs[0].ContentHash, posting.SourceHash) {
		t.Fatalf("retained immutable posting ref: refs=%v error=%v", refs, err)
	}
	if !proto.Equal(posting, beforePosting) || !proto.Equal(review, beforeReview) {
		t.Fatal("duplicate validation mutated retained candidate or review")
	}
	refs[0].ContentHash[0]++
	if !proto.Equal(posting, beforePosting) {
		t.Fatal("returned provenance aliases the retained posting hash")
	}
}

func TestValidateDuplicateReviewCannotBypassSameSourceOrContent(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.PurchasePosting)
	}{
		{"same document", func(p *tammyv1.PurchasePosting) { p.DocumentId = purchaseDocID }},
		{"same original content", func(p *tammyv1.PurchasePosting) { p.OriginalContentHash = make([]byte, 32) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			posting := duplicatePosting()
			test.mutate(posting)
			for _, review := range []*tammyv1.PurchaseDuplicateReview{
				{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH},
				distinctDuplicateReview(posting.Id),
			} {
				refs, err := ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), []*tammyv1.PurchasePosting{posting})
				if !errors.Is(err, ErrDuplicatePurchase) || len(refs) != 1 || refs[0].Id != posting.Id {
					t.Fatalf("known same source/content bypassed: refs=%v error=%v", refs, err)
				}
			}
		})
	}
}

func TestValidateDuplicateReviewRejectsUnseenStaleAndInvalidDecisions(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.PurchaseDuplicateReview)
	}{
		{"unspecified", func(r *tammyv1.PurchaseDuplicateReview) { r.Decision = 0 }},
		{"unknown", func(r *tammyv1.PurchaseDuplicateReview) { r.Decision = 3 }},
		{"missing matched posting", func(r *tammyv1.PurchaseDuplicateReview) { r.MatchedPostingId = nil }},
		{"invented matched posting", func(r *tammyv1.PurchaseDuplicateReview) { id := purchaseAccountID; r.MatchedPostingId = &id }},
		{"invalid matched posting", func(r *tammyv1.PurchaseDuplicateReview) { id := "bad"; r.MatchedPostingId = &id }},
		{"missing reason", func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = "" }},
		{"blank reason", func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = "\u2003" }},
		{"untrimmed reason", func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = " Reviewed" }},
		{"invalid reason UTF-8", func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = string([]byte{0xff}) }},
		{"oversized reason", func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = strings.Repeat("é", 513) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			review := distinctDuplicateReview(duplicatePostingID)
			test.mutate(review)
			refs, err := ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), []*tammyv1.PurchasePosting{duplicatePosting()})
			if !errors.Is(err, ErrInvalidDuplicateReview) || len(refs) != 1 {
				t.Fatalf("unmatched or invalid decision accepted: refs=%v error=%v", refs, err)
			}
		})
	}
	for _, review := range []*tammyv1.PurchaseDuplicateReview{nil, distinctDuplicateReview(duplicatePostingID)} {
		if _, err := ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), nil); !errors.Is(err, ErrInvalidDuplicateReview) {
			t.Fatalf("unseen/stale candidate accepted: error=%v", err)
		}
	}
	for _, mutate := range []func(*tammyv1.PurchaseDuplicateReview){
		func(r *tammyv1.PurchaseDuplicateReview) { id := duplicatePostingID; r.MatchedPostingId = &id },
		func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = string([]byte{0xff}) },
		func(r *tammyv1.PurchaseDuplicateReview) { r.Reason = strings.Repeat("x", 513) },
	} {
		review := &tammyv1.PurchaseDuplicateReview{Decision: tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH}
		mutate(review)
		if _, err := ValidateDuplicateReview(review, purchaseOrgID, purchaseDocID, make([]byte, 32), nil); !errors.Is(err, ErrInvalidDuplicateReview) {
			t.Fatalf("invalid no-match metadata accepted: error=%v", err)
		}
	}
}

func TestValidateDuplicateReviewMultipleMatchesRetainsBoundedSortedRefs(t *testing.T) {
	for _, count := range []int{2, 50, 51} {
		candidates := make([]*tammyv1.PurchasePosting, count)
		for i := range candidates {
			candidates[i] = duplicatePosting()
			candidates[i].Id = fmt.Sprintf("018f0000-0000-7000-8000-%012d", count-i+930)
			candidates[i].SourceHash = bytes.Repeat([]byte{byte(count - i)}, 32)
		}
		firstID := candidates[0].Id
		refs, err := ValidateDuplicateReview(distinctDuplicateReview(firstID), purchaseOrgID, purchaseDocID, make([]byte, 32), candidates)
		if count == 51 {
			if !errors.Is(err, ErrInvalidDuplicateCandidate) || refs != nil {
				t.Fatalf("unbounded matches accepted: refs=%v error=%v", refs, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidDuplicateReview) || len(refs) != count {
			t.Fatalf("multiple matches silently reduced: count=%d refs=%v error=%v", count, refs, err)
		}
		for i, ref := range refs {
			want := candidates[count-i-1]
			if ref.Id != want.Id || ref.Type != "purchase_posting" || ref.Revision != 1 || !bytes.Equal(ref.ContentHash, want.SourceHash) {
				t.Fatalf("candidate %d lost exact provenance: ref=%v want=%v", i, ref, want)
			}
		}
		if candidates[0].Id != firstID {
			t.Fatal("validation sorted caller-owned candidate slice")
		}
	}
}

func TestValidateDuplicateReviewRejectsInvalidCandidateFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*tammyv1.PurchasePosting)
	}{
		{"invalid id", func(p *tammyv1.PurchasePosting) { p.Id = "bad" }},
		{"foreign organisation", func(p *tammyv1.PurchasePosting) { p.OrganisationId = purchaseAccountID }},
		{"invalid document id", func(p *tammyv1.PurchasePosting) { p.DocumentId = "bad" }},
		{"missing reviewed version", func(p *tammyv1.PurchasePosting) { p.ReviewedVersion = 0 }},
		{"missing original hash", func(p *tammyv1.PurchasePosting) { p.OriginalContentHash = nil }},
		{"invalid original hash", func(p *tammyv1.PurchasePosting) { p.OriginalContentHash = make([]byte, 31) }},
		{"missing reviewed hash", func(p *tammyv1.PurchasePosting) { p.ReviewedSnapshotHash = nil }},
		{"invalid source hash", func(p *tammyv1.PurchasePosting) { p.SourceHash = make([]byte, 33) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			posting := duplicatePosting()
			test.mutate(posting)
			refs, err := ValidateDuplicateReview(distinctDuplicateReview(duplicatePostingID), purchaseOrgID, purchaseDocID, make([]byte, 32), []*tammyv1.PurchasePosting{posting})
			if !errors.Is(err, ErrInvalidDuplicateCandidate) || refs != nil {
				t.Fatalf("invalid server facts accepted: refs=%v error=%v", refs, err)
			}
		})
	}
	for _, candidates := range [][]*tammyv1.PurchasePosting{{nil}, {duplicatePosting(), duplicatePosting()}} {
		if refs, err := ValidateDuplicateReview(distinctDuplicateReview(duplicatePostingID), purchaseOrgID, purchaseDocID, make([]byte, 32), candidates); !errors.Is(err, ErrInvalidDuplicateCandidate) || refs != nil {
			t.Fatalf("nil/repeated candidate accepted: refs=%v error=%v", refs, err)
		}
	}
	for _, test := range []struct {
		organisationID, documentID string
		hash                       []byte
	}{
		{"bad", purchaseDocID, make([]byte, 32)},
		{purchaseOrgID, "bad", make([]byte, 32)},
		{purchaseOrgID, purchaseDocID, nil},
		{purchaseOrgID, purchaseDocID, make([]byte, 33)},
	} {
		if refs, err := ValidateDuplicateReview(nil, test.organisationID, test.documentID, test.hash, nil); !errors.Is(err, ErrInvalidDuplicateCandidate) || refs != nil {
			t.Fatalf("invalid current source accepted: refs=%v error=%v", refs, err)
		}
	}
}
