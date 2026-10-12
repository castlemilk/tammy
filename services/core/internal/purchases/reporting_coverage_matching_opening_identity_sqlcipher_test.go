//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"crypto/sha256"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func TestPurchaseCoverageMatchesOpeningPayablesByExactItemIdentity(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	document := f.document

	var payableAccountID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg).Scan(&payableAccountID); err != nil {
		t.Fatalf("find the organisation's payable control account: %v", err)
	}
	conversionID, _ := f.id()
	insertCoverageOpeningDraft(t, f, conversionID, "e")

	// This ordinary debit item carries an exact document source edge, but it
	// does not have a purchase_opening_payables owner row with the same ID.
	ordinaryItemID, _ := f.id()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,
			source_type,source_id,source_revision,source_content_hash)
		VALUES(?,?,?,'ORDINARY',11000,0,'AUD','document',?,?,?)`,
		ordinaryItemID, conversionID, postingExpense, document.Id, document.Version, document.Sha256); err != nil {
		t.Fatalf("insert the valid document-linked ordinary opening item: %v", err)
	}

	// A separate supplier payable shares the conversion, but its retained
	// OpeningBalanceInput has no document source. Conversion membership alone
	// must not make this payable a match for the ordinary item above.
	payableID, _ := f.id()
	input := &tammyv1.OpeningBalanceInput{
		ClientLineId: payableID, AccountId: payableAccountID,
		Kind:          tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM,
		LedgerBalance: postMoney(11000), OriginalIssueDate: postDate(10), OriginalDueDate: postDate(20),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,
			original_issue_date,original_due_date)
		VALUES(?,?,?,'SUPPLIER_OPEN_ITEM',0,11000,'AUD','2026-08-10','2026-08-20')`,
		payableID, conversionID, payableAccountID); err != nil {
		t.Fatalf("insert the valid sibling supplier payable item: %v", err)
	}
	openings, err := purchases.NewOpeningRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := openings.RecordOpeningPayable(ctx, conversionID, input, f.now); err != nil {
		t.Fatalf("record the sibling payable through OpeningRepository: %v", err)
	}

	journal := f.seedPriorJournal(conversionID, tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION,
		postingExpense, payableAccountID)
	result, err := tx.ExecContext(ctx, `
		UPDATE opening_conversions SET state='POSTED',journal_id=?,financial_revision=?
		WHERE id=? AND state='DRAFT'`, journal.Id, journal.FinancialRevision, conversionID)
	if err != nil {
		t.Fatalf("post the guarded opening conversion: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("post opening conversion affected %d rows: %v", affected, err)
	}

	candidateBytes, err := (proto.MarshalOptions{Deterministic: true}).Marshal(document.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	reviewedHash := sha256.Sum256(candidateBytes)
	matureMatches, err := openings.MatchingPriorSources(ctx, postingOrg, document.Id, document.Sha256, reviewedHash[:])
	if err != nil {
		t.Fatalf("read mature exact-item prior-source matches: %v", err)
	}
	if len(matureMatches) != 0 {
		t.Fatalf("the unlinked sibling payable unexpectedly matches the document: %+v", matureMatches)
	}

	beforeChanges := postScalar(t, tx, `SELECT total_changes()`)
	capture := captureCoverage(t, f.repo(tx), ctx)
	if got := postScalar(t, tx, `SELECT total_changes()`); got != beforeChanges {
		t.Fatalf("coverage capture wrote %d rows", got-beforeChanges)
	}
	var capturedDocument *purchases.LegacyPurchaseReportingRecord
	for _, source := range capture.LegacySources {
		if source != nil && source.Document != nil && source.Document.Id == document.Id {
			capturedDocument = source
			break
		}
	}
	if capturedDocument == nil {
		t.Fatal("capture omitted the document source root")
	}
	if len(capturedDocument.MatchingOpeningPayableIDs) != 0 {
		t.Fatalf("capture attributed same-conversion sibling payables to the document: got %v, want no exact item match", capturedDocument.MatchingOpeningPayableIDs)
	}

	rows := coverageRawRows(capture)
	ordinary := retainedRowForText(t, rows, "opening_items", "id", ordinaryItemID)
	for column, want := range map[string]string{"source_type": "document", "source_id": document.Id} {
		value, present := retainedValue(ordinary, column)
		if !present || value.Kind != "TEXT" || value.Text != want {
			t.Fatalf("ordinary item %s %s=%#v, want exact text %q", ordinaryItemID, column, value, want)
		}
	}
	ordinaryRevision, present := retainedValue(ordinary, "source_revision")
	if !present || ordinaryRevision.Kind != "INTEGER" || ordinaryRevision.Integer != int64(document.Version) {
		t.Fatalf("ordinary item source revision=%#v, want %d", ordinaryRevision, document.Version)
	}
	ordinaryHash, present := retainedValue(ordinary, "source_content_hash")
	if !present || ordinaryHash.Kind != "BLOB" || string(ordinaryHash.Bytes) != string(document.Sha256) {
		t.Fatal("capture changed the ordinary item's exact original document hash")
	}
	payable := retainedRowForText(t, rows, "opening_items", "id", payableID)
	for _, column := range []string{"source_type", "source_id", "source_revision", "source_content_hash"} {
		value, present := retainedValue(payable, column)
		if !present || value.Kind != "NULL" {
			t.Fatalf("unlinked sibling payable synthesized %s provenance: %#v", column, value)
		}
	}
}
