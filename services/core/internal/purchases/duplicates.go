package purchases

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrInvalidDuplicateIdentity  = errors.New("purchases: missing or invalid duplicate identity")
	ErrInvalidDuplicateCandidate = errors.New("purchases: invalid duplicate candidate facts")
	ErrInvalidDuplicateReview    = errors.New("purchases: missing or invalid duplicate review")
	ErrDuplicatePurchase         = errors.New("purchases: source or content already posted")
)

// NormalizeDuplicateIdentity supplies the supplier-name fallback and invoice
// keys used when the reviewed document has no supplier identity. Compatibility
// normalization, Unicode case folding and collapsed Unicode whitespace catch
// equivalent text without discarding invoice punctuation or leading zeroes.
// Limits apply before and after normalization so expansion cannot overflow the
// retained SQL keys. Reviewed DocumentCandidate input has its own narrower
// supplier-name contract; this function also accepts retained posting names.
func NormalizeDuplicateIdentity(supplier, invoice string) (string, string, error) {
	if !utf8.ValidString(supplier) || !utf8.ValidString(invoice) ||
		utf8.RuneCountInString(supplier) > 512 || utf8.RuneCountInString(invoice) > 128 {
		return "", "", ErrInvalidDuplicateIdentity
	}
	normalize := func(value string) string {
		value = norm.NFKC.String(cases.Fold().String(norm.NFKC.String(value)))
		return strings.Join(strings.Fields(value), " ")
	}
	supplierKey, invoiceKey := normalize(supplier), normalize(invoice)
	if supplierKey == "" || invoiceKey == "" || utf8.RuneCountInString(supplierKey) > 512 || utf8.RuneCountInString(invoiceKey) > 128 {
		return "", "", ErrInvalidDuplicateIdentity
	}
	return supplierKey, invoiceKey, nil
}

// ValidateDuplicateReview checks a review against the complete server-discovered
// union of supplier/invoice, same-document and same-content posting matches.
// The command owner must discover every match, verify retained source hashes
// and reload those immutable facts within the same organisation transaction.
// Discovery must not require invoice date or amount equality or truncate at 50.
// Invalid facts return no refs; blocked reviews return all validated sorted refs
// so corrective review can retain the discovered provenance. A posting's source
// revision is always one; ReviewedVersion identifies its document snapshot.
func ValidateDuplicateReview(review *tammyv1.PurchaseDuplicateReview, organisationID, documentID string, originalHash []byte, candidates []*tammyv1.PurchasePosting) ([]*tammyv1.SourceRef, error) {
	if !ids.IsCanonicalV7(organisationID) || !ids.IsCanonicalV7(documentID) || len(originalHash) != 32 || len(candidates) > 50 {
		return nil, ErrInvalidDuplicateCandidate
	}
	refs := make([]*tammyv1.SourceRef, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	sameSourceOrContent := false
	for _, candidate := range candidates {
		if candidate == nil || !ids.IsCanonicalV7(candidate.Id) || candidate.OrganisationId != organisationID ||
			!ids.IsCanonicalV7(candidate.DocumentId) || candidate.ReviewedVersion == 0 ||
			len(candidate.OriginalContentHash) != 32 || len(candidate.ReviewedSnapshotHash) != 32 || len(candidate.SourceHash) != 32 || seen[candidate.Id] {
			return nil, ErrInvalidDuplicateCandidate
		}
		seen[candidate.Id] = true
		if candidate.DocumentId == documentID || bytes.Equal(candidate.OriginalContentHash, originalHash) {
			sameSourceOrContent = true
		}
		refs = append(refs, &tammyv1.SourceRef{Type: "purchase_posting", Id: candidate.Id, Revision: 1,
			ContentHash: append([]byte(nil), candidate.SourceHash...)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Id < refs[j].Id })
	if sameSourceOrContent {
		return refs, ErrDuplicatePurchase
	}
	if review == nil || !purchaseReviewText(review.Reason, 512, false) {
		return refs, ErrInvalidDuplicateReview
	}
	switch review.Decision {
	case tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_NO_MATCH:
		if len(refs) != 0 || review.MatchedPostingId != nil {
			return refs, ErrInvalidDuplicateReview
		}
	case tammyv1.PurchaseDuplicateDecision_PURCHASE_DUPLICATE_DECISION_DISTINCT_TRANSACTION:
		// The current contract can confirm exactly one discovered match. It
		// cannot express a complete review of two or more matching postings.
		if len(refs) != 1 || review.MatchedPostingId == nil || *review.MatchedPostingId != refs[0].Id || !purchaseReviewText(review.Reason, 512, true) {
			return refs, ErrInvalidDuplicateReview
		}
	default:
		return refs, ErrInvalidDuplicateReview
	}
	return refs, nil
}
