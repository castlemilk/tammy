//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseCoverageRetainsHistoricalSupportedPaymentAfterReversal(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	documentID := f.document.Id
	payment := seedCoverageLegacyCashPayment(t, f, documentID, "1016")
	paymentHash := coverageMessageHash(t, payment)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT)
	request.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{
		SourceId: payment.Id, SourceVersion: payment.Version, SourceHash: paymentHash,
	}}
	review, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil {
		t.Fatalf("retain supported manual payment before reversal: %v", err)
	}
	before := captureCoverage(t, f.repo(tx), ctx)
	if before.ReceiptCount != 1 {
		t.Fatalf("posted manual settlement receipt count=%d, want 1", before.ReceiptCount)
	}
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	reversalID, _ := f.id()
	lineIDs := make([]string, len(payment.Lines))
	for index := range lineIDs {
		lineIDs[index], _ = f.id()
	}
	reversed, reversal, err := journalRepository.Reverse(ctx, payment.Id, payment.Version, postDate(11),
		"Reverse the retained legacy settlement", reversalID, lineIDs, f.now)
	if err != nil || reversed == nil || reversal == nil {
		t.Fatalf("reverse supported legacy payment: original=%v reversal=%v err=%v", reversed, reversal, err)
	}
	counts := coverageStateCounts(t, tx)
	changes := postScalar(t, tx, "SELECT total_changes()")
	after := captureCoverage(t, f.repo(tx), ctx)
	legacy := coverageLegacyDocument(t, after, documentID)
	if len(legacy.Reviews) != 1 || !proto.Equal(legacy.Reviews[0], review.Review) {
		t.Fatalf("reversal erased or rewrote immutable supported-payment review: %+v", legacy)
	}
	ref := legacy.Reviews[0].Review.SupportingPayments[0]
	if ref.SourceId != payment.Id || ref.SourceVersion != payment.Version || !bytes.Equal(ref.SourceHash, paymentHash) {
		t.Fatalf("historical payment identity was replaced with current reversal identity: %+v", ref)
	}
	if after.ReceiptCount != 0 || len(legacy.PaymentJournalIDs) != 0 || !coverageHasID(legacy.PriorJournalIDs, payment.Id) {
		t.Fatalf("reversed settlement or lineage reversal became a current receipt: count=%d source=%+v", after.ReceiptCount, legacy)
	}
	originalProof := coverageJournal(t, after, payment.Id)
	reversalProof := coverageJournal(t, after, reversalID)
	if !proto.Equal(originalProof.Journal, reversed) || !proto.Equal(reversalProof.Journal, reversal) ||
		originalProof.Journal.GetReversedByJournalId() != reversalID || reversalProof.Journal.GetReversalOfJournalId() != payment.Id ||
		bytes.Equal(coverageMessageHash(t, originalProof.Journal), paymentHash) || bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("current reciprocal journal proofs were omitted or substituted for the original supported-payment hash")
	}
	if postScalar(t, tx, "SELECT total_changes()") != changes {
		t.Fatal("historical reversal capture wrote rows")
	}
	if got := coverageStateCounts(t, tx); !reflect.DeepEqual(got, counts) {
		t.Fatalf("historical reversal capture changed retained counts/revisions: before=%v after=%v", counts, got)
	}
}
