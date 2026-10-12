//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func insertCoverageOpeningDraft(t *testing.T, f *postingFixture, conversionID, sourceHashByte string) {
	t.Helper()
	if len(sourceHashByte) != 1 {
		t.Fatalf("source hash marker must be one byte, got %q", sourceHashByte)
	}
	if _, err := f.executor.ExecContext(context.Background(), `
		INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,version,created_at)
		VALUES(?,?,'2026-08-10','DRAFT',?,1,?)`, conversionID, postingOrg,
		strings.Repeat(sourceHashByte, 64), f.now.Format("2006-01-02T15:04:05.999999999Z07:00")); err != nil {
		t.Fatalf("insert legal DRAFT opening conversion %s: %v", conversionID, err)
	}
}

// postCoverageOrdinaryOpening completes a pre-inserted DRAFT conversion using
// the same guarded opening-item, journal-post and DRAFT-to-POSTED transitions
// as the public repository flow. The test preinserts the target header so
// MarkReplaced can satisfy its ownership guard; OpeningConversionRepository.Post
// owns that header insert and therefore cannot be used for this target. The
// conversion deliberately has no purchase_opening_payables owner row.
func postCoverageOrdinaryOpening(t *testing.T, f *postingFixture, conversionID, equityAccountID string, linkedDocument ...*tammyv1.Document) *tammyv1.Journal {
	t.Helper()
	ctx := context.Background()
	if len(linkedDocument) > 1 || (len(linkedDocument) == 1 && linkedDocument[0] == nil) {
		t.Fatal("ordinary opening accepts at most one non-nil linked document")
	}
	debitItemID, _ := f.id()
	creditItemID, _ := f.id()
	var debitItemResult sql.Result
	var err error
	if len(linkedDocument) == 1 {
		document := linkedDocument[0]
		debitItemResult, err = f.executor.ExecContext(ctx, `
			INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,
				source_type,source_id,source_revision,source_content_hash)
			VALUES(?,?,?,'ORDINARY',11000,0,'AUD','document',?,?,?)`, debitItemID, conversionID, postingExpense,
			document.Id, document.Version, document.Sha256)
	} else {
		debitItemResult, err = f.executor.ExecContext(ctx, `
			INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code)
			VALUES(?,?,?,'ORDINARY',11000,0,'AUD')`, debitItemID, conversionID, postingExpense)
	}
	if err != nil {
		t.Fatalf("insert ordinary debit item for conversion %s: %v", conversionID, err)
	}
	if affected, err := debitItemResult.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("insert ordinary debit item for conversion %s affected %d rows: %v", conversionID, affected, err)
	}
	if _, err := f.executor.ExecContext(ctx, `
		INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code)
		VALUES(?,?,?,'ORDINARY',0,11000,'AUD')`, creditItemID, conversionID, equityAccountID); err != nil {
		t.Fatalf("insert ordinary credit item for conversion %s: %v", conversionID, err)
	}

	journal := f.seedPriorJournal(conversionID, tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION,
		postingExpense, equityAccountID)
	result, err := f.executor.ExecContext(ctx, `
		UPDATE opening_conversions SET state='POSTED',journal_id=?,financial_revision=?
		WHERE id=? AND state='DRAFT'`, journal.Id, journal.FinancialRevision, conversionID)
	if err != nil {
		t.Fatalf("post owned journal on conversion %s: %v", conversionID, err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("DRAFT-to-POSTED transition for conversion %s affected %d rows: %v", conversionID, affected, err)
	}
	return journal
}

func postCoveragePurchaseOpening(t *testing.T, f *postingFixture, conversionID string) (string, *tammyv1.Journal) {
	t.Helper()
	ctx := context.Background()
	rows, err := f.executor.QueryContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var payableAccountID string
	if !rows.Next() || rows.Scan(&payableAccountID) != nil {
		_ = rows.Close()
		t.Fatal("missing payable control account")
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}

	itemID, _ := f.id()
	input := &tammyv1.OpeningBalanceInput{
		ClientLineId: itemID, AccountId: payableAccountID,
		Kind:          tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM,
		LedgerBalance: postMoney(11000), OriginalIssueDate: postDate(10), OriginalDueDate: postDate(20),
		OutstandingGst: postMoney(1000), PriorGstAttributed: postMoney(0),
	}
	if _, err := f.executor.ExecContext(ctx, `
		INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,
			original_issue_date,original_due_date,outstanding_gst_minor,prior_gst_attributed_minor)
		VALUES(?,?,?,'SUPPLIER_OPEN_ITEM',0,11000,'AUD','2026-08-10','2026-08-20',1000,0)`,
		itemID, conversionID, payableAccountID); err != nil {
		t.Fatalf("insert purchase payable item: %v", err)
	}
	openings, err := purchases.NewOpeningRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	if err := openings.RecordOpeningPayable(ctx, conversionID, input, f.now); err != nil {
		t.Fatalf("record purchase payable: %v", err)
	}
	journal := f.seedPriorJournal(conversionID, tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION,
		postingExpense, payableAccountID)
	result, err := f.executor.ExecContext(ctx, `
		UPDATE opening_conversions SET state='POSTED',journal_id=?,financial_revision=?
		WHERE id=? AND state='DRAFT'`, journal.Id, journal.FinancialRevision, conversionID)
	if err != nil {
		t.Fatalf("post owned payable opening journal: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("post purchase opening conversion affected %d rows: %v", affected, err)
	}
	return itemID, journal
}

func TestPurchaseCoverageRetainsOpeningReplacementPredecessorOfPayableRoot(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	f.addDocument("Context Supplier", "OPENING-CONTEXT", 10000, 1000)
	linkedDocument := f.document

	// Post a non-payable ordinary conversion first. The later purchase-owned
	// conversion replaces it, so a census rooted at the payable must walk the
	// incoming replacement edge to retain this earlier accounting context.
	equityAccountID, _ := f.id()
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	equity := &tammyv1.Account{Id: equityAccountID, OrganisationId: postingOrg, Version: 1,
		Code: "EQ-COVERAGE-PREDECESSOR", Name: "Coverage replacement predecessor equity",
		Type:                 tammyv1.AccountType_ACCOUNT_TYPE_EQUITY,
		NormalBalance:        tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT,
		Status:               tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation:          tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "custom.equity", CashFlowClassification: "financing"}
	if err := accounts.Create(ctx, equity, f.now); err != nil {
		t.Fatalf("create ordinary credit account: %v", err)
	}

	predecessorID, _ := f.id()
	insertCoverageOpeningDraft(t, f, predecessorID, "a")
	predecessorJournal := postCoverageOrdinaryOpening(t, f, predecessorID, equityAccountID, linkedDocument)
	payableConversionID, _ := f.id()
	insertCoverageOpeningDraft(t, f, payableConversionID, "b")

	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	reversalID, _ := f.id()
	reversalLineIDs := make([]string, len(predecessorJournal.Lines))
	for index := range reversalLineIDs {
		reversalLineIDs[index], _ = f.id()
	}
	reversed, reversal, err := journalRepository.Reverse(ctx, predecessorJournal.Id, predecessorJournal.Version,
		postDate(11), "Replace earlier ordinary opening conversion", reversalID, reversalLineIDs, f.now)
	if err != nil || reversed == nil || reversal == nil || reversed.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED ||
		reversed.ReversedByJournalId == nil || *reversed.ReversedByJournalId != reversalID ||
		reversal.ReversalOfJournalId == nil || *reversal.ReversalOfJournalId != predecessorJournal.Id {
		t.Fatalf("reverse opening predecessor: original=%v reversal=%v err=%v", reversed, reversal, err)
	}
	conversionRepository, err := accounting.NewOpeningConversionRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversionRepository.MarkReplaced(ctx, predecessorID, 1, payableConversionID); err != nil {
		t.Fatalf("mark non-payable predecessor replaced by payable target: %v", err)
	}
	rootPayableID, payableJournal := postCoveragePurchaseOpening(t, f, payableConversionID)

	beforeChanges := postScalar(t, tx, `SELECT total_changes()`)
	capture := captureCoverage(t, f.repo(tx), ctx)
	if got := postScalar(t, tx, `SELECT total_changes()`); got != beforeChanges {
		t.Fatalf("coverage capture wrote %d rows while reading predecessor context", got-beforeChanges)
	}
	if capture.ReceiptCount != 0 {
		t.Fatalf("opening replacement journals became cash receipts: got %d", capture.ReceiptCount)
	}

	rows := coverageRawRows(capture)
	predecessor := retainedRowForText(t, rows, "opening_conversions", "id", predecessorID)
	state, stateOK := retainedValue(predecessor, "state")
	replacedBy, replacementOK := retainedValue(predecessor, "replaced_by_id")
	if !stateOK || state.Kind != "TEXT" || state.Text != "REPLACED" || !replacementOK ||
		replacedBy.Kind != "TEXT" || replacedBy.Text != payableConversionID ||
		countCoverageRows(rows, "opening_items", "conversion_id", predecessorID) != 2 {
		t.Fatalf("payable-root capture omitted predecessor conversion/items: row=%+v state=%#v replacedBy=%#v", predecessor, state, replacedBy)
	}
	if countCoverageRows(rows, "opening_conversions", "id", payableConversionID) != 1 ||
		countCoverageRows(rows, "purchase_opening_payables", "conversion_id", payableConversionID) != 1 ||
		countCoverageRows(rows, "purchase_opening_payables", "conversion_id", predecessorID) != 0 {
		t.Fatal("predecessor traversal changed the single purchase-payable root")
	}
	openingRoots := 0
	for _, source := range capture.LegacySources {
		if source == nil || source.Source.GetOpeningPayable() == nil {
			continue
		}
		openingRoots++
		if source.Source.GetOpeningPayable().OpeningPayableId != rootPayableID || source.Opening == nil ||
			source.Opening.ConversionID != payableConversionID {
			t.Fatalf("predecessor conversion was exposed as an invented payable source: %+v", source)
		}
	}
	if openingRoots != 1 {
		t.Fatalf("capture returned %d opening roots, want only the payable target", openingRoots)
	}
	var linkedSourceFound bool
	for _, source := range capture.LegacySources {
		if source == nil || source.Document == nil || source.Document.Id != linkedDocument.Id {
			continue
		}
		linkedSourceFound = true
		if len(source.MatchingOpeningPayableIDs) != 0 {
			t.Fatalf("ordinary source-linked opening item was promoted to payable identity: %v", source.MatchingOpeningPayableIDs)
		}
	}
	if !linkedSourceFound {
		t.Fatal("capture omitted the document linked from the ordinary opening predecessor")
	}

	expectedJournals := map[string]struct {
		sourceType, sourceID string
		state                tammyv1.JournalState
		version              uint64
	}{
		predecessorJournal.Id: {"OPENING", predecessorID, tammyv1.JournalState_JOURNAL_STATE_REVERSED, 2},
		reversalID:            {"REVERSAL", predecessorJournal.Id, tammyv1.JournalState_JOURNAL_STATE_POSTED, 1},
		payableJournal.Id:     {"OPENING", payableConversionID, tammyv1.JournalState_JOURNAL_STATE_POSTED, 1},
	}
	if len(capture.Journals) != len(expectedJournals) {
		t.Fatalf("captured %d journal proofs, want %d unique predecessor/root proofs", len(capture.Journals), len(expectedJournals))
	}
	for journalID, expected := range expectedJournals {
		proof := coverageJournal(t, capture, journalID)
		if proof.SourceType != expected.sourceType || proof.SourceID != expected.sourceID || proof.SourceKind != 0 ||
			proof.SourceRevision != 1 || proof.Journal.State != expected.state || proof.Journal.Version != expected.version ||
			len(proof.ContentHash) != 32 || countCoverageJournals(capture, journalID) != 1 {
			t.Fatalf("journal %s predecessor/root proof was omitted, duplicated or changed: %+v", journalID, proof)
		}
	}
}

func TestPurchaseCoverageFollowsOnlyReachableOpeningReplacementChain(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	// This conversion is the one real purchase-owned opening root.
	rootPayableID := legacyOpeningFixture(t, f)
	openings, err := purchases.NewOpeningRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	rootPayable, err := openings.GetOpeningPayable(ctx, postingOrg, rootPayableID)
	if err != nil || rootPayable == nil || rootPayable.State != "POSTED" {
		t.Fatalf("read POSTED purchase opening root: opening=%+v err=%v", rootPayable, err)
	}

	// Create an ordinary credit account used only by the two non-payable
	// replacement conversions. All account writes use the accounting owner.
	equityAccountID, _ := f.id()
	accounts, err := accounting.NewAccountRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	equity := &tammyv1.Account{Id: equityAccountID, OrganisationId: postingOrg, Version: 1,
		Code: "EQ-COVERAGE-CHAIN", Name: "Coverage ordinary opening equity",
		Type:                 tammyv1.AccountType_ACCOUNT_TYPE_EQUITY,
		NormalBalance:        tammyv1.NormalBalance_NORMAL_BALANCE_CREDIT,
		Status:               tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE,
		Designation:          tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "custom.equity", CashFlowClassification: "financing"}
	if err := accounts.Create(ctx, equity, f.now); err != nil {
		t.Fatalf("create ordinary credit account: %v", err)
	}

	conversionRepository, err := accounting.NewOpeningConversionRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}

	// B must exist as a same-organisation DRAFT before the guarded A->B update.
	conversionB, _ := f.id()
	insertCoverageOpeningDraft(t, f, conversionB, "b")
	firstReversalID, _ := f.id()
	rootJournalRows, err := journalRepository.Get(ctx, rootPayable.JournalID)
	if err != nil || rootJournalRows == nil {
		t.Fatalf("read original purchase opening journal: journal=%v err=%v", rootJournalRows, err)
	}
	firstReversalLineIDs := make([]string, len(rootJournalRows.Lines))
	for index := range firstReversalLineIDs {
		firstReversalLineIDs[index], _ = f.id()
	}
	firstReversed, firstReversal, err := journalRepository.Reverse(ctx, rootPayable.JournalID, rootJournalRows.Version,
		postDate(11), "Retain original opening replacement history", firstReversalID, firstReversalLineIDs, f.now)
	if err != nil || firstReversed == nil || firstReversal == nil ||
		firstReversed.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED ||
		firstReversed.ReversedByJournalId == nil || *firstReversed.ReversedByJournalId != firstReversalID ||
		firstReversal.ReversalOfJournalId == nil || *firstReversal.ReversalOfJournalId != rootPayable.JournalID {
		t.Fatalf("reverse original opening journal: original=%v reversal=%v err=%v", firstReversed, firstReversal, err)
	}
	if _, err := conversionRepository.MarkReplaced(ctx, rootPayable.ConversionID, 1, conversionB); err != nil {
		t.Fatalf("mark purchase opening root replaced by B: %v", err)
	}
	journalB := postCoverageOrdinaryOpening(t, f, conversionB, equityAccountID)

	// B itself is a real POSTED opening conversion but intentionally has no
	// purchase_opening_payables row. C is inserted before the guarded B->C edge.
	conversionC, _ := f.id()
	insertCoverageOpeningDraft(t, f, conversionC, "c")
	secondReversalID, _ := f.id()
	secondReversalLineIDs := make([]string, len(journalB.Lines))
	for index := range secondReversalLineIDs {
		secondReversalLineIDs[index], _ = f.id()
	}
	reversedB, secondReversal, err := journalRepository.Reverse(ctx, journalB.Id, journalB.Version,
		postDate(12), "Replace ordinary opening B", secondReversalID, secondReversalLineIDs, f.now)
	if err != nil || reversedB == nil || secondReversal == nil ||
		reversedB.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED ||
		reversedB.ReversedByJournalId == nil || *reversedB.ReversedByJournalId != secondReversalID ||
		secondReversal.ReversalOfJournalId == nil || *secondReversal.ReversalOfJournalId != journalB.Id {
		t.Fatalf("reverse ordinary opening B before replacement: original=%v reversal=%v err=%v", reversedB, secondReversal, err)
	}
	if _, err := conversionRepository.MarkReplaced(ctx, conversionB, 1, conversionC); err != nil {
		t.Fatalf("mark ordinary opening B replaced by C: %v", err)
	}
	journalC := postCoverageOrdinaryOpening(t, f, conversionC, equityAccountID)

	// This valid but unrelated DRAFT conversion must not be pulled into the
	// source-owned transitive replacement closure.
	unrelatedDraft, _ := f.id()
	insertCoverageOpeningDraft(t, f, unrelatedDraft, "d")

	beforeChanges := postScalar(t, tx, `SELECT total_changes()`)
	capture := captureCoverage(t, f.repo(tx), ctx)
	if capture.ReceiptCount != 0 {
		t.Fatalf("opening replacement journal context became cash receipts: got %d", capture.ReceiptCount)
	}
	if got := postScalar(t, tx, `SELECT total_changes()`); got != beforeChanges {
		t.Fatalf("coverage capture wrote %d rows while reading replacement context", got-beforeChanges)
	}

	rows := coverageRawRows(capture)
	for _, conversion := range []struct {
		id, state, replacement string
		itemCount              int
	}{{rootPayable.ConversionID, "REPLACED", conversionB, 1}, {conversionB, "REPLACED", conversionC, 2}, {conversionC, "POSTED", "", 2}} {
		if countCoverageRows(rows, "opening_conversions", "id", conversion.id) != 1 {
			t.Fatalf("reachable conversion %s was not retained exactly once", conversion.id)
		}
		row := retainedRowForText(t, rows, "opening_conversions", "id", conversion.id)
		state, stateOK := retainedValue(row, "state")
		if !stateOK || state.Kind != "TEXT" || state.Text != conversion.state {
			t.Fatalf("conversion %s state=%#v, want raw %q", conversion.id, state, conversion.state)
		}
		replacement, replacementOK := retainedValue(row, "replaced_by_id")
		if conversion.replacement == "" {
			if !replacementOK || replacement.Kind != "NULL" {
				t.Fatalf("terminal conversion %s synthesized replacement edge: %#v", conversion.id, replacement)
			}
		} else if !replacementOK || replacement.Kind != "TEXT" || replacement.Text != conversion.replacement {
			t.Fatalf("conversion %s replacement edge=%#v, want %s", conversion.id, replacement, conversion.replacement)
		}
		if countCoverageRows(rows, "opening_items", "conversion_id", conversion.id) != conversion.itemCount {
			t.Fatalf("conversion %s ordinary/open-item rows were not retained in full", conversion.id)
		}
	}
	reachableConversions := map[string]struct{}{
		rootPayable.ConversionID: {}, conversionB: {}, conversionC: {},
	}
	for _, row := range capture.Rows {
		if row.Table != "opening_items" {
			continue
		}
		conversionID, conversionOK := retainedValue(row, "conversion_id")
		if !conversionOK || conversionID.Kind != "TEXT" {
			t.Fatalf("captured opening item lacks its conversion edge: %+v", row)
		}
		if _, reachable := reachableConversions[conversionID.Text]; !reachable {
			continue
		}
		accountID, accountOK := retainedValue(row, "account_id")
		if !accountOK || accountID.Kind != "TEXT" || countCoverageRows(rows, "accounts", "id", accountID.Text) != 1 {
			t.Fatalf("opening item for conversion %s lacks exactly one full account row: %+v", conversionID.Text, row)
		}
	}
	if countCoverageRows(rows, "opening_conversions", "id", unrelatedDraft) != 0 {
		t.Fatal("coverage swept an unrelated same-organisation DRAFT conversion")
	}

	// Only A is a purchase-owned source root. B and C belong in the raw
	// provenance closure, not as invented payables, legacy roots or receipts.
	if countCoverageRows(rows, "purchase_opening_payables", "conversion_id", rootPayable.ConversionID) != 1 ||
		countCoverageRows(rows, "purchase_opening_payables", "conversion_id", conversionB) != 0 ||
		countCoverageRows(rows, "purchase_opening_payables", "conversion_id", conversionC) != 0 {
		t.Fatal("replacement context gained a synthetic purchase opening payable")
	}
	openingRoots := 0
	for _, source := range capture.LegacySources {
		if source == nil || source.Source.GetOpeningPayable() == nil {
			continue
		}
		openingRoots++
		if source.Source.GetOpeningPayable().OpeningPayableId != rootPayableID || source.Opening == nil ||
			source.Opening.ConversionID != rootPayable.ConversionID {
			t.Fatalf("non-root replacement conversion was exposed as a legacy source: %+v", source)
		}
	}
	if openingRoots != 1 || coverageLegacyOpening(t, capture, rootPayableID).Opening.State != "REPLACED" {
		t.Fatalf("capture returned %d purchase-owned opening roots; want only A in REPLACED state", openingRoots)
	}

	// Accounting proofs are deduplicated even though A is reached as a purchase
	// root and its reversal plus B/C are discovered through linked context.
	expectedJournals := map[string]struct {
		sourceType, sourceID, reversalOf string
		state                            tammyv1.JournalState
		version                          uint64
	}{
		rootPayable.JournalID: {"OPENING", rootPayable.ConversionID, "", tammyv1.JournalState_JOURNAL_STATE_REVERSED, 2},
		firstReversalID:       {"REVERSAL", rootPayable.JournalID, rootPayable.JournalID, tammyv1.JournalState_JOURNAL_STATE_POSTED, 1},
		journalB.Id:           {"OPENING", conversionB, "", tammyv1.JournalState_JOURNAL_STATE_REVERSED, 2},
		secondReversalID:      {"REVERSAL", journalB.Id, journalB.Id, tammyv1.JournalState_JOURNAL_STATE_POSTED, 1},
		journalC.Id:           {"OPENING", conversionC, "", tammyv1.JournalState_JOURNAL_STATE_POSTED, 1},
	}
	if len(capture.Journals) != len(expectedJournals) {
		t.Fatalf("captured %d journal proofs, want %d unique reachable proofs", len(capture.Journals), len(expectedJournals))
	}
	for journalID, expected := range expectedJournals {
		if countCoverageJournals(capture, journalID) != 1 {
			t.Fatalf("reachable journal proof %s was omitted or duplicated", journalID)
		}
		proof := coverageJournal(t, capture, journalID)
		if proof.SourceType != expected.sourceType || proof.SourceKind != 0 || proof.SourceID != expected.sourceID ||
			proof.SourceRevision != 1 || proof.Journal.State != expected.state || proof.Journal.Version != expected.version ||
			len(proof.ContentHash) != 32 {
			t.Fatalf("journal %s proof lost exact source/state/hash: %+v", journalID, proof)
		}
		if expected.reversalOf == "" {
			if proof.Journal.ReversalOfJournalId != nil {
				t.Fatalf("non-reversal journal %s acquired a reversal parent: %+v", journalID, proof.Journal)
			}
		} else if proof.Journal.ReversalOfJournalId == nil || *proof.Journal.ReversalOfJournalId != expected.reversalOf {
			t.Fatalf("reversal proof %s lost its exact original journal edge: %+v", journalID, proof.Journal)
		}
		completeProof, err := journalRepository.GetForReporting(ctx, postingOrg, journalID)
		if err != nil || completeProof == nil || !bytes.Equal(completeProof.ContentHash, proof.ContentHash) {
			t.Fatalf("captured journal %s differs from accounting's complete proof: proof=%v err=%v", journalID, completeProof, err)
		}
		for _, line := range proof.Journal.Lines {
			if countCoverageRows(rows, "accounts", "id", line.AccountId) != 1 {
				t.Fatalf("journal %s line account %s was omitted or duplicated", journalID, line.AccountId)
			}
		}
	}
}
