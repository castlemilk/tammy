//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseCoverageRejectsReciprocalReversalCycle(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	firstSource, _ := f.id()
	secondSource, _ := f.id()
	first := f.seedPriorJournal(firstSource, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		postingExpense, postingExpense, f.document.Id)
	second := f.seedPriorJournal(secondSource, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		postingExpense, postingExpense, f.document.Id)
	if first.Id == second.Id || first.Id == firstSource || second.Id == secondSource {
		t.Fatal("cycle fixture must use distinct journal and raw source identities")
	}

	// This test models a corrupted persisted graph. Ordinary repository writes
	// cannot create these reciprocal reversal edges; remove only the write-time
	// immutability/completion triggers needed to represent that hostile state.
	for _, trigger := range []string{
		"journals_posted_immutable",
		"posted_journal_update_guard",
		"reversal_completion_links_direct_reversal",
	} {
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trigger); err != nil {
			t.Fatalf("prepare corrupt reversal-cycle fixture by removing %s: %v", trigger, err)
		}
	}
	for _, edge := range []struct {
		journalID string
		otherID   string
	}{{first.Id, second.Id}, {second.Id, first.Id}} {
		if _, err := tx.ExecContext(ctx, `UPDATE journals
			SET version=2, source_type='REVERSAL', source_id=?, source_revision=1,
			    state='REVERSED', reversal_of_journal_id=?, reversed_by_journal_id=?
			WHERE id=?`, edge.otherID, edge.otherID, edge.otherID, edge.journalID); err != nil {
			t.Fatalf("seed reciprocal reversal-cycle edge %s -> %s: %v", edge.journalID, edge.otherID, err)
		}
	}

	capture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	if capture != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("complete coverage accepted a closed reversal cycle or returned partial data: capture=%v err=%v", capture, err)
	}
}

func TestPurchaseCoverageKeepsHistoricalPaymentReviewAfterCashClassificationChange(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	documentID := f.document.Id
	payment := seedCoverageLegacyCashPayment(t, f, documentID, "1017")
	paymentHash := coverageMessageHash(t, payment)
	request := legacyRequest(t, f,
		tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT)
	request.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{
		SourceId: payment.Id, SourceVersion: payment.Version, SourceHash: paymentHash,
	}}
	response, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil || response == nil || response.Review == nil {
		t.Fatalf("retain a real supported-payment review: response=%v err=%v", response, err)
	}

	beforeCounts := coverageStateCounts(t, tx)
	beforeChanges := postScalar(t, tx, `SELECT total_changes()`)
	before := captureCoverage(t, f.repo(tx), ctx)
	if before.ReceiptCount != 1 {
		t.Fatalf("cash-classified manual payment receipt count=%d, want 1", before.ReceiptCount)
	}
	beforeSource := coverageLegacyDocument(t, before, documentID)
	if len(beforeSource.Reviews) != 1 || !proto.Equal(beforeSource.Reviews[0], response.Review) {
		t.Fatalf("capture omitted the exact historical payment review: %+v", beforeSource.Reviews)
	}
	if !reflect.DeepEqual(beforeCounts, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != beforeChanges {
		t.Fatal("pre-change coverage capture changed retained state")
	}

	var cashAccountID string
	for _, line := range payment.Lines {
		if line != nil && line.Credit != nil && line.Credit.MinorUnits > 0 {
			cashAccountID = line.AccountId
			break
		}
	}
	if cashAccountID == "" {
		t.Fatal("cash receipt fixture has no credit account")
	}
	accountRepository, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	cashAccount, err := accountRepository.Get(ctx, cashAccountID)
	if err != nil || cashAccount == nil || cashAccount.ReportClassification != "balance_sheet.cash" {
		t.Fatalf("load the real cash account before reclassification: account=%v err=%v", cashAccount, err)
	}
	expectedVersion := cashAccount.Version
	cashAccount.Version++
	cashAccount.ReportClassification = "balance_sheet.other"
	if err := accountRepository.Update(ctx, expectedVersion, cashAccount, f.now.Add(time.Minute)); err != nil {
		t.Fatalf("legally change current cash-account reporting classification: %v", err)
	}

	afterCounts := coverageStateCounts(t, tx)
	afterChanges := postScalar(t, tx, `SELECT total_changes()`)
	after := captureCoverage(t, f.repo(tx), ctx)
	if after.ReceiptCount != 0 {
		t.Fatalf("reclassified manual journal remained a current cash receipt: count=%d", after.ReceiptCount)
	}
	afterSource := coverageLegacyDocument(t, after, documentID)
	if len(afterSource.Reviews) != 1 || !proto.Equal(afterSource.Reviews[0], response.Review) ||
		len(afterSource.Reviews[0].Review.SupportingPayments) != 1 {
		t.Fatalf("current account metadata invalidated or rewrote the immutable payment review: %+v", afterSource.Reviews)
	}
	ref := afterSource.Reviews[0].Review.SupportingPayments[0]
	if ref.SourceId != payment.Id || ref.SourceVersion != payment.Version || !bytes.Equal(ref.SourceHash, paymentHash) {
		t.Fatalf("historical payment identity changed after account reclassification: %+v", ref)
	}
	if len(afterSource.PaymentJournalIDs) != 0 || !coverageHasID(afterSource.PriorJournalIDs, payment.Id) {
		t.Fatalf("reclassified support journal was not retained solely as prior source evidence: payments=%v prior=%v",
			afterSource.PaymentJournalIDs, afterSource.PriorJournalIDs)
	}
	if bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("cash-account classification change did not update the complete source fingerprint")
	}
	accountRow := retainedRowForText(t, coverageRawRows(after), "accounts", "id", cashAccountID)
	classification, ok := retainedValue(accountRow, "report_classification")
	if !ok || classification.Kind != "TEXT" || classification.Text != "balance_sheet.other" {
		t.Fatalf("complete capture omitted the current account classification: %+v", classification)
	}
	if !reflect.DeepEqual(afterCounts, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != afterChanges {
		t.Fatal("post-change coverage capture changed retained state")
	}
}
