package purchases

import (
	"context"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// GetWithLatestEvidence returns one immutable evidence record after verifying the
// complete owned accounting history. Its retained ID can be selected again
// after reopening; unposted documents have no purchase evidence record.
func (r *Repository) GetWithLatestEvidence(ctx context.Context, org, document string) (*tammyv1.PurchaseAccounting, *tammyv1.PurchaseTaxEvidence, error) {
	state, err := r.Get(ctx, org, document)
	if err != nil || !state.Posted {
		return state, nil, err
	}
	evidence, err := r.retainedEvidence(ctx, state.Posting)
	if err != nil {
		return nil, nil, err
	}
	return state, proto.Clone(evidence).(*tammyv1.PurchaseTaxEvidence), nil
}

// GetWithLatestDecisions returns retained decision identities for a reopened
// detail view. A deferral's amount remains the original decision amount; the
// accounting projection and complete BAS capture derive its remaining extent.
func (r *Repository) GetWithLatestDecisions(ctx context.Context, org, document string) (*tammyv1.PurchaseAccounting, *tammyv1.PurchaseTaxEvidence, *tammyv1.PurchaseGstDeferral, error) {
	state, evidence, err := r.GetWithLatestEvidence(ctx, org, document)
	if err != nil || !state.Posted {
		return state, evidence, nil, err
	}
	deferrals, err := r.loadDeferrals(ctx, state.Posting)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(deferrals) == 0 {
		return state, evidence, nil, nil
	}
	return state, evidence, proto.Clone(deferrals[len(deferrals)-1]).(*tammyv1.PurchaseGstDeferral), nil
}

// DuplicateMatches exposes the same complete immutable matches the posting
// command verifies. This read never treats changed invoice dates or amounts as
// proof of a distinct transaction, and never substitutes a UI page as coverage.
func (r *Repository) DuplicateMatches(ctx context.Context, org, documentID string) ([]*tammyv1.PurchaseDuplicateMatch, error) {
	doc, err := r.document(ctx, org, documentID)
	if err != nil {
		return nil, err
	}
	candidates, err := r.duplicateCandidates(ctx, doc)
	if err != nil {
		return nil, err
	}
	result := make([]*tammyv1.PurchaseDuplicateMatch, 0, len(candidates))
	for _, posting := range candidates {
		result = append(result, &tammyv1.PurchaseDuplicateMatch{
			Posting:    &tammyv1.SourceRef{Type: "purchase_posting", Id: posting.Id, Revision: 1, ContentHash: append([]byte(nil), posting.SourceHash...)},
			DocumentId: posting.DocumentId, SupplierName: posting.SupplierName, InvoiceNumber: posting.InvoiceNumber,
			InvoiceDate: proto.Clone(posting.InvoiceDate).(*tammyv1.CivilDate), Gross: proto.Clone(posting.Gross).(*tammyv1.Money),
		})
	}
	return result, nil
}
