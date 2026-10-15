package purchases

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestLegacyCurrentnessSupportingPaymentRequiresExactSourceAncestry(t *testing.T) {
	const org = "018f0000-0000-7000-8000-000000001001"
	const root = "018f0000-0000-7000-8000-000000001111"
	const middle = "018f0000-0000-7000-8000-000000001112"
	const leaf = "018f0000-0000-7000-8000-000000001113"
	const unrelated = "018f0000-0000-7000-8000-000000001114"
	for _, name := range []string{"exact source ancestry", "unrelated captured chain", "missing parent", "foreign intermediate", "nonzero kind intermediate", "nonreciprocal parent", "wrong parent revision", "cycle", "wrong support hash"} {
		t.Run(name, func(t *testing.T) {
			// This graph-only oracle does not claim financial validity. Native
			// writer tests establish the corresponding complete financial proofs.
			proofs := map[string]*accounting.JournalReportingRecord{
				root: {SourceKind: 0, SourceType: "MANUAL", SourceID: "source-a", SourceRevision: 1, State: "REVERSED",
					Journal: &tammyv1.Journal{Id: root, OrganisationId: org, Version: 2, State: tammyv1.JournalState_JOURNAL_STATE_REVERSED, ReversedByJournalId: proto.String(middle)}},
				middle: {SourceKind: 0, SourceType: "REVERSAL", SourceID: root, SourceRevision: 1, State: "REVERSED",
					Journal: &tammyv1.Journal{Id: middle, OrganisationId: org, Version: 2, State: tammyv1.JournalState_JOURNAL_STATE_REVERSED, ReversalOfJournalId: proto.String(root), ReversedByJournalId: proto.String(leaf)}},
				leaf: {SourceKind: 0, SourceType: "REVERSAL", SourceID: middle, SourceRevision: 1, State: "POSTED",
					Journal: &tammyv1.Journal{Id: leaf, OrganisationId: org, Version: 1, State: tammyv1.JournalState_JOURNAL_STATE_POSTED, ReversalOfJournalId: proto.String(middle)}},
				unrelated: {SourceKind: 0, SourceType: "MANUAL", SourceID: "source-b", SourceRevision: 1, State: "POSTED",
					Journal: &tammyv1.Journal{Id: unrelated, OrganisationId: org, Version: 1, State: tammyv1.JournalState_JOURNAL_STATE_POSTED}},
			}
			encoded, err := purchaseBytes(proofs[leaf].Journal)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(encoded)
			ref := &tammyv1.PurchaseRetainedSourceRef{SourceId: leaf, SourceVersion: 1, SourceHash: append([]byte(nil), digest[:]...)}
			record := &tammyv1.LegacyPurchaseReview{OrganisationId: org, Review: &tammyv1.LegacyPurchaseReviewInput{SupportingPayments: []*tammyv1.PurchaseRetainedSourceRef{ref}}}
			prior := map[string]struct{}{root: {}}
			switch name {
			case "unrelated captured chain":
				prior = map[string]struct{}{unrelated: {}}
			case "missing parent":
				delete(proofs, middle)
			case "foreign intermediate":
				proofs[middle].Journal.OrganisationId = unrelated
			case "nonzero kind intermediate":
				proofs[middle].SourceKind = 6
			case "nonreciprocal parent":
				proofs[root].Journal.ReversedByJournalId = proto.String(unrelated)
			case "wrong parent revision":
				proofs[middle].SourceRevision = 2
			case "cycle":
				proofs[middle].SourceID = leaf
				proofs[middle].Journal.ReversalOfJournalId = proto.String(leaf)
			case "wrong support hash":
				ref.SourceHash[0] ^= 1
			}
			err = verifyCoverageSupportingPayments(record, prior, proofs, nil)
			if name == "exact source ancestry" {
				if err != nil {
					t.Fatalf("exact source-specific reversal ancestry rejected: %v", err)
				}
			} else if !errors.Is(err, ErrRepository) {
				t.Fatalf("%s support accepted without exact source ancestry/hash: %v", name, err)
			}
		})
	}
}
