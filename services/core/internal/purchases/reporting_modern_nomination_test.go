package purchases

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
)

const modernNominationOrg = "018f0000-0000-7000-8000-000000000020"

func modernNominationText(value string) *string   { return &value }
func modernNominationNumber(value uint64) *uint64 { return &value }

// Synthetic identities establish structural mapping, not financial validity.
func modernNominationFixture() []BASScalarKey {
	return []BASScalarKey{
		{Kind: BASScalarPosting, OrganisationID: modernNominationOrg, ID: "posting/one", DocumentID: modernNominationText("document:one"), IsRoot: true},
		{Kind: BASScalarPosting, OrganisationID: modernNominationOrg, ID: "posting/two", DocumentID: modernNominationText("document:two"), IsRoot: true},
		{Kind: BASScalarPostingAllocation, OrganisationID: modernNominationOrg, ID: "posting/one", PostingID: modernNominationText("posting/one"), Sequence: modernNominationNumber(1)},
		{Kind: BASScalarPostingAllocation, OrganisationID: modernNominationOrg, ID: "posting/one", PostingID: modernNominationText("posting/one"), Sequence: modernNominationNumber(2)},
		{Kind: BASScalarPayment, OrganisationID: modernNominationOrg, ID: "payment/one", PostingID: modernNominationText("posting/one"), IsReceipt: true},
		{Kind: BASScalarPayment, OrganisationID: modernNominationOrg, ID: "payment/two", PostingID: modernNominationText("posting/one"), IsReceipt: true},
		{Kind: BASScalarPaymentAllocation, OrganisationID: modernNominationOrg, ID: "payment/one", PostingID: modernNominationText("posting/one"), PaymentID: modernNominationText("payment/one"), Sequence: modernNominationNumber(1), PostingAllocationSequence: modernNominationNumber(2)},
		{Kind: BASScalarPaymentAllocation, OrganisationID: modernNominationOrg, ID: "payment/one", PostingID: modernNominationText("posting/one"), PaymentID: modernNominationText("payment/one"), Sequence: modernNominationNumber(2), PostingAllocationSequence: modernNominationNumber(1)},
		{Kind: BASScalarDocument, OrganisationID: modernNominationOrg, ID: "legacy/document", IsRoot: true},
		{Kind: BASScalarLegacyReview, OrganisationID: modernNominationOrg, ID: "legacy/review"},
		{Kind: BASScalarDeferralPortion, OrganisationID: modernNominationOrg, ID: "retained/decision", Sequence: modernNominationNumber(1)},
	}
}

func TestPurchaseBASModernNominationStructuredFallbackOrderAndLinkage(t *testing.T) {
	keys := modernNominationFixture()
	slices.Reverse(keys)
	before, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	want := []BASModernCandidateKey{
		{Kind: BASModernCandidatePosting, OrganisationID: modernNominationOrg, PostingID: "posting/two", DocumentID: "document:two"},
		{Kind: BASModernCandidatePostingAllocation, OrganisationID: modernNominationOrg, PostingID: "posting/one", DocumentID: "document:one", Sequence: 1},
		{Kind: BASModernCandidatePostingAllocation, OrganisationID: modernNominationOrg, PostingID: "posting/one", DocumentID: "document:one", Sequence: 2},
		{Kind: BASModernCandidatePayment, OrganisationID: modernNominationOrg, PostingID: "posting/one", DocumentID: "document:one", PaymentID: "payment/two"},
		{Kind: BASModernCandidatePaymentAllocation, OrganisationID: modernNominationOrg, PostingID: "posting/one", DocumentID: "document:one", PaymentID: "payment/one", Sequence: 1, PostingAllocationSequence: 2},
		{Kind: BASModernCandidatePaymentAllocation, OrganisationID: modernNominationOrg, PostingID: "posting/one", DocumentID: "document:one", PaymentID: "payment/one", Sequence: 2, PostingAllocationSequence: 1},
	}
	got, err := nominateModernBASCandidates(keys)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("modern candidates = %#v, %v; want %#v", got, err, want)
	}
	after, err := json.Marshal(keys)
	if err != nil || string(before) != string(after) {
		t.Fatal("mapping changed caller input or pointer values")
	}
	slices.Reverse(keys)
	again, err := nominateModernBASCandidates(keys)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("input order changed result: %#v, %v", again, err)
	}
	if got, err := nominateModernBASCandidates(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty map = %#v, %v", got, err)
	}
}

func TestPurchaseBASModernNominationRejectsBrokenCandidateLinks(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]BASScalarKey) []BASScalarKey
	}{
		{"missing posting document", func(keys []BASScalarKey) []BASScalarKey { keys[0].DocumentID = nil; return keys }},
		{"missing posting parent", func(keys []BASScalarKey) []BASScalarKey { return keys[1:] }},
		{"missing payment parent", func(keys []BASScalarKey) []BASScalarKey { return append(keys[:4:4], keys[5:]...) }},
		{"wrong payment ancestor", func(keys []BASScalarKey) []BASScalarKey {
			keys[6].PostingID = modernNominationText("posting/two")
			return keys
		}},
		{"missing posting allocation", func(keys []BASScalarKey) []BASScalarKey {
			keys[6].PostingAllocationSequence = modernNominationNumber(3)
			return keys
		}},
		{"missing own sequence", func(keys []BASScalarKey) []BASScalarKey { keys[6].Sequence = nil; return keys }},
		{"mismatched own parent", func(keys []BASScalarKey) []BASScalarKey { keys[6].ID = "payment/two"; return keys }},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := nominateModernBASCandidates(test.change(modernNominationFixture()))
			if got != nil || !errors.Is(err, ErrRepository) {
				t.Fatalf("broken candidate links = %#v, %v", got, err)
			}
		})
	}
}

func TestPurchaseBASModernNominationInvalidReadInputsReturnNoDiscovery(t *testing.T) {
	var repo *Repository
	for _, allowance := range []int64{-1, 0, 1000} {
		got, err := repo.ReadModernBASCandidates(context.Background(), modernNominationOrg, BASScalarSummary{OrganisationID: modernNominationOrg}, 0, 0, allowance)
		if got != nil || !errors.Is(err, ErrRepository) {
			t.Fatalf("nil repository allowance %d = %#v, %v", allowance, got, err)
		}
	}
}
