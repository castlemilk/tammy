//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseReportingCaptureRejectsExtraRetainedAllocationWithoutPartialResult(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posted := postReportingPurchase(t, f, "ALLOCATION-CLOSURE", postDate(10), 0, 0)
	baseline := capturePostings(t, f.repo(tx), ctx, postingOrg)
	if len(baseline.Postings) != 1 || len(baseline.Postings[0].Posting.Allocations) != 1 {
		t.Fatal("allocation closure fixture did not establish a successful complete read")
	}
	// Immutable history guards permit appending a distinct sequence. Its raw
	// row is legal SQL but disagrees with the closed retained posting protobuf.
	if _, err := tx.ExecContext(ctx, `INSERT INTO purchase_posting_allocations_v1(
		posting_id,organisation_id,sequence,account_id,account_version,gross_minor,
		supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,
		cash_flow_classification,noncreditable_reason,allocation_proto)
		SELECT posting_id,organisation_id,2,account_id,account_version,gross_minor,
		supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,
		cash_flow_classification,noncreditable_reason,allocation_proto
		FROM purchase_posting_allocations_v1 WHERE posting_id=? AND sequence=1`, posted.Posting.Id); err != nil {
		t.Fatal(err)
	}
	partial, err := f.repo(tx).CapturePostingsForReporting(ctx, postingOrg)
	if partial != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("extra retained allocation returned partial=%v error=%v", partial != nil, err)
	}
}

func TestPurchaseReportingCaptureUsesCallerTransactionAndOuterRollbackRestoresBaseline(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	seedTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer seedTx.Rollback()

	f := newPostingFixture(t, seedTx, tammyv1.GstBasis_GST_BASIS_CASH)
	seedCounts := reportingStateCounts(t, seedTx)
	baseline := capturePostings(t, f.repo(seedTx), ctx, postingOrg)
	if len(baseline.Postings) != 0 || len(baseline.Documents) != 0 || len(baseline.Journals) != 0 {
		t.Fatalf("empty posting census returned roots before posting: postings=%d documents=%d journals=%d", len(baseline.Postings), len(baseline.Documents), len(baseline.Journals))
	}
	if got := reportingStateCounts(t, seedTx); !reflect.DeepEqual(got, seedCounts) {
		t.Fatalf("baseline capture changed retained state: before=%v after=%v", seedCounts, got)
	}
	if err := seedTx.Commit(); err != nil {
		t.Fatal(err)
	}

	outerTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer outerTx.Rollback()
	f.executor = outerTx
	response, err := f.repo(outerTx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatalf("post reviewed purchase in caller transaction: %v", err)
	}
	if response == nil || response.Posting == nil {
		t.Fatal("post reviewed purchase returned no posting")
	}
	postCounts := reportingStateCounts(t, outerTx)
	captured := capturePostings(t, f.repo(outerTx), ctx, postingOrg)
	if len(captured.Postings) != 1 || len(captured.Documents) != 1 || len(captured.Journals) != 1 {
		t.Fatalf("capture did not see caller's uncommitted posting: postings=%d documents=%d journals=%d", len(captured.Postings), len(captured.Documents), len(captured.Journals))
	}
	if !proto.Equal(captured.Postings[0].Posting, response.Posting) {
		t.Fatal("capture did not return the posting written in the caller transaction")
	}
	if bytes.Equal(captured.Fingerprint, baseline.Fingerprint) {
		t.Fatal("capture fingerprint did not reflect the caller's uncommitted posting")
	}
	if got := reportingStateCounts(t, outerTx); !reflect.DeepEqual(got, postCounts) {
		t.Fatalf("capture wrote rows or changed revisions in the caller transaction: before=%v after=%v", postCounts, got)
	}
	if err := outerTx.Rollback(); err != nil {
		t.Fatalf("rollback caller transaction: %v", err)
	}

	readTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	f.executor = readTx
	afterRollback := capturePostings(t, f.repo(readTx), ctx, postingOrg)
	if len(afterRollback.Postings) != 0 || len(afterRollback.Documents) != 0 || len(afterRollback.Journals) != 0 {
		t.Fatalf("posting survived caller rollback: postings=%d documents=%d journals=%d", len(afterRollback.Postings), len(afterRollback.Documents), len(afterRollback.Journals))
	}
	if !bytes.Equal(afterRollback.Fingerprint, baseline.Fingerprint) || !reflect.DeepEqual(afterRollback.Rows, baseline.Rows) {
		t.Fatal("capture after rollback did not restore the exact baseline fingerprint and retained rows")
	}
	if got := reportingStateCounts(t, readTx); !reflect.DeepEqual(got, seedCounts) {
		t.Fatalf("capture after rollback changed seed state or revisions: before=%v after=%v", seedCounts, got)
	}
}

func TestPurchaseReportingFingerprintCommitsSourceOnlyAccountMetadata(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	response := postReportingPurchase(t, f, "RAW-ACCOUNT-PROVENANCE", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	if response == nil || response.Posting == nil {
		t.Fatalf("post provenance fixture returned no posting: response=%v", response)
	}
	repo := f.repo(tx)
	beforeCounts := reportingStateCounts(t, tx)
	before := capturePostings(t, repo, ctx, postingOrg)
	if len(before.Postings) != 1 || before.Postings[0].Accounting == nil {
		t.Fatalf("expected one complete posting before metadata update, got %d", len(before.Postings))
	}
	beforeAccount := retainedRowForText(t, before, "accounts", "code", "2000")
	oldUpdatedAt, ok := retainedValue(beforeAccount, "updated_at")
	if !ok || (oldUpdatedAt.Kind != "NULL" && oldUpdatedAt.Kind != "TEXT") {
		t.Fatalf("account raw row lacks its nullable updated_at provenance: %+v", oldUpdatedAt)
	}

	if _, err := tx.ExecContext(ctx, "SAVEPOINT purchase_reporting_provenance"); err != nil {
		t.Fatalf("savepoint before raw metadata mutation: %v", err)
	}
	newUpdatedAt := f.now.Add(time.Second).UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE accounts SET updated_at=? WHERE organisation_id=? AND code='2000'`, newUpdatedAt, postingOrg)
	if err != nil {
		t.Fatalf("update source-only account metadata: %v", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		t.Fatalf("account metadata update affected %d rows, error=%v; want one", changed, err)
	}
	after := capturePostings(t, repo, ctx, postingOrg)
	if len(after.Postings) != 1 || after.Postings[0].Accounting == nil {
		t.Fatalf("expected one complete posting after metadata update, got %d", len(after.Postings))
	}
	if !proto.Equal(before.Postings[0].Posting, after.Postings[0].Posting) || !proto.Equal(before.Postings[0].Accounting, after.Postings[0].Accounting) {
		t.Fatal("source-only account updated_at mutation changed reconstructed accounting state or posting proto")
	}
	if reflect.DeepEqual(before.Rows, after.Rows) || bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("raw account metadata change did not alter retained rows and capture fingerprint")
	}
	afterAccount := retainedRowForText(t, after, "accounts", "code", "2000")
	newRawUpdatedAt, ok := retainedValue(afterAccount, "updated_at")
	if !ok || newRawUpdatedAt.Kind != "TEXT" || newRawUpdatedAt.Text != newUpdatedAt ||
		(oldUpdatedAt.Kind == "TEXT" && newRawUpdatedAt.Text == oldUpdatedAt.Text) {
		t.Fatalf("capture did not retain the changed account updated_at: old=%+v new=%+v", oldUpdatedAt, newRawUpdatedAt)
	}
	if got := reportingStateCounts(t, tx); !reflect.DeepEqual(got, beforeCounts) {
		t.Fatalf("read-only capture changed retained rows or revisions after account update: before=%v after=%v", beforeCounts, got)
	}

	if _, err := tx.ExecContext(ctx, "ROLLBACK TO purchase_reporting_provenance"); err != nil {
		t.Fatalf("restore source-only account metadata: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "RELEASE purchase_reporting_provenance"); err != nil {
		t.Fatalf("release metadata savepoint: %v", err)
	}
	restored := capturePostings(t, repo, ctx, postingOrg)
	if !bytes.Equal(restored.Fingerprint, before.Fingerprint) || !reflect.DeepEqual(restored.Rows, before.Rows) {
		t.Fatal("restoring account metadata did not restore the exact original fingerprint and retained rows")
	}
}
