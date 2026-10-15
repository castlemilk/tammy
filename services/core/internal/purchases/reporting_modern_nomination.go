package purchases

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
)

const maximumBASModernCandidates = 1000

type BASModernCandidateKind int64

const (
	BASModernCandidatePosting BASModernCandidateKind = iota + 1
	BASModernCandidatePostingAllocation
	BASModernCandidatePayment
	BASModernCandidatePaymentAllocation
)

// BASModernCandidateKey is an exact structural coverage unit, without a
// monetary amount, eligibility decision, borrowed provenance or journal-line
// mapping. Sequence is the own component sequence; PostingAllocationSequence
// is separate settlement context and is populated only for payment allocations.
type BASModernCandidateKey struct {
	Kind                                             BASModernCandidateKind
	OrganisationID, PostingID, DocumentID, PaymentID string
	Sequence, PostingAllocationSequence              uint64
}

// BASModernCandidateDiscovery retains all physical families while nominating
// only modern posting/payment components. Candidates is not the complete
// purchase set: legacy nomination and the shared global fence remain separate.
type BASModernCandidateDiscovery struct {
	PhysicalKeys []BASScalarKey
	Candidates   []BASModernCandidateKey
}

// ReadModernBASCandidates uses the guarded physical read exactly once and
// performs no financial body loading. The caller must apply shared preflight
// reservations before this read and retain PhysicalKeys for full reconciliation.
func (r *Repository) ReadModernBASCandidates(ctx context.Context, organisationID string, expected BASScalarSummary, maximumScalarKeys, maximumScalarBytes, maximumCandidates int64) (*BASModernCandidateDiscovery, error) {
	if maximumCandidates < 0 {
		return nil, ErrRepository
	}
	keys, err := r.ReadBASKeys(ctx, organisationID, expected, maximumScalarKeys, maximumScalarBytes)
	if err != nil {
		return nil, err
	}
	candidates, err := nominateModernBASCandidates(keys)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRepository, err)
	}
	if maximumCandidates > maximumBASModernCandidates {
		maximumCandidates = maximumBASModernCandidates
	}
	if int64(len(candidates)) > maximumCandidates {
		return nil, &SourceBoundError{Resource: "modern BAS candidates", Limit: maximumCandidates}
	}
	return &BASModernCandidateDiscovery{PhysicalKeys: keys, Candidates: candidates}, nil
}

// nominateModernBASCandidates accepts only the guarded ReadBASKeys output.
// Its extra checks assert candidate graph/linkage, not full SQL/body validity.
func nominateModernBASCandidates(keys []BASScalarKey) ([]BASModernCandidateKey, error) {
	type component struct {
		parent   string
		sequence uint64
	}
	postings, payments := make(map[string]BASScalarKey), make(map[string]BASScalarKey)
	postingAllocations, paymentAllocations := make(map[component]BASScalarKey), make(map[component]BASScalarKey)
	postingChildren, paymentChildren := make(map[string]bool), make(map[string]bool)
	invalid := func() error {
		return fmt.Errorf("%w: modern BAS candidate linkage differs from physical keys", ErrRepository)
	}
	text := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	for _, key := range keys {
		switch key.Kind {
		case BASScalarPosting:
			if key.ID == "" || text(key.DocumentID) == "" {
				return nil, invalid()
			}
			if _, exists := postings[key.ID]; exists {
				return nil, invalid()
			}
			postings[key.ID] = key
		case BASScalarPayment:
			if key.ID == "" || text(key.PostingID) == "" {
				return nil, invalid()
			}
			if _, exists := payments[key.ID]; exists {
				return nil, invalid()
			}
			payments[key.ID] = key
		case BASScalarPostingAllocation, BASScalarPaymentAllocation:
			if key.ID == "" || key.Sequence == nil || *key.Sequence == 0 || text(key.PostingID) == "" {
				return nil, invalid()
			}
			id := component{key.ID, *key.Sequence}
			if key.Kind == BASScalarPostingAllocation {
				if key.ID != text(key.PostingID) {
					return nil, invalid()
				}
				if _, exists := postingAllocations[id]; exists {
					return nil, invalid()
				}
				postingAllocations[id] = key
				postingChildren[key.ID] = true
			} else {
				if key.ID != text(key.PaymentID) || key.PostingAllocationSequence == nil || *key.PostingAllocationSequence == 0 {
					return nil, invalid()
				}
				if _, exists := paymentAllocations[id]; exists {
					return nil, invalid()
				}
				paymentAllocations[id] = key
				paymentChildren[key.ID] = true
			}
		}
	}
	var out []BASModernCandidateKey
	base := func(key BASScalarKey, postingID string) (BASModernCandidateKey, error) {
		posting, exists := postings[postingID]
		if !exists || key.OrganisationID != posting.OrganisationID {
			return BASModernCandidateKey{}, invalid()
		}
		return BASModernCandidateKey{OrganisationID: posting.OrganisationID, PostingID: postingID, DocumentID: *posting.DocumentID}, nil
	}
	for id, key := range postings {
		if !postingChildren[id] {
			out = append(out, BASModernCandidateKey{Kind: BASModernCandidatePosting, OrganisationID: key.OrganisationID, PostingID: id, DocumentID: *key.DocumentID})
		}
	}
	for id, key := range postingAllocations {
		candidate, err := base(key, id.parent)
		if err != nil {
			return nil, err
		}
		candidate.Kind, candidate.Sequence = BASModernCandidatePostingAllocation, id.sequence
		out = append(out, candidate)
	}
	for id, key := range payments {
		candidate, err := base(key, *key.PostingID)
		if err != nil {
			return nil, err
		}
		if !paymentChildren[id] {
			candidate.Kind, candidate.PaymentID = BASModernCandidatePayment, id
			out = append(out, candidate)
		}
	}
	for id, key := range paymentAllocations {
		payment, exists := payments[id.parent]
		if !exists || payment.OrganisationID != key.OrganisationID || text(payment.PostingID) != text(key.PostingID) {
			return nil, invalid()
		}
		if allocation, exists := postingAllocations[component{*key.PostingID, *key.PostingAllocationSequence}]; !exists || allocation.OrganisationID != key.OrganisationID {
			return nil, invalid()
		}
		candidate, err := base(key, *key.PostingID)
		if err != nil {
			return nil, err
		}
		candidate.Kind, candidate.PaymentID, candidate.Sequence = BASModernCandidatePaymentAllocation, id.parent, id.sequence
		candidate.PostingAllocationSequence = *key.PostingAllocationSequence
		out = append(out, candidate)
	}
	slices.SortFunc(out, func(left, right BASModernCandidateKey) int {
		if order := cmp.Compare(left.Kind, right.Kind); order != 0 {
			return order
		}
		if order := cmp.Compare(left.PostingID, right.PostingID); order != 0 {
			return order
		}
		if order := cmp.Compare(left.PaymentID, right.PaymentID); order != 0 {
			return order
		}
		return cmp.Compare(left.Sequence, right.Sequence)
	})
	return out, nil
}
