//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func captureCoverage(t *testing.T, repo *purchases.Repository, ctx context.Context) *purchases.PurchaseCoverageReportingCapture {
	t.Helper()
	capture, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
	if err != nil || capture == nil {
		t.Fatalf("capture complete purchase coverage: capture=%v err=%v", capture, err)
	}
	return capture
}

func seedCoverageLegacyCashPayment(t *testing.T, f *postingFixture, source, cashCode string) *tammyv1.Journal {
	t.Helper()
	ctx := context.Background()
	cashID, _ := f.id()
	accounts, err := accounting.NewAccountRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	cash := &tammyv1.Account{Id: cashID, OrganisationId: postingOrg, Version: 1, Code: cashCode, Name: "Coverage historical cash", Type: tammyv1.AccountType_ACCOUNT_TYPE_ASSET,
		NormalBalance: tammyv1.NormalBalance_NORMAL_BALANCE_DEBIT, Status: tammyv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, Designation: tammyv1.AccountDesignation_ACCOUNT_DESIGNATION_ORDINARY,
		ReportClassification: "balance_sheet.cash", CashFlowClassification: "operating"}
	if err := accounts.Create(ctx, cash, f.now); err != nil {
		t.Fatal(err)
	}
	writer, err := accounting.NewJournalRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := writer.ReserveFinancialRevision(ctx, nil, f.now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := f.id()
	debitID, _ := f.id()
	creditID, _ := f.id()
	journal := &tammyv1.Journal{Id: id, OrganisationId: postingOrg, Version: 1, PostingDate: postDate(10), Memo: "Coverage linked historical payment", Source: tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL,
		State: tammyv1.JournalState_JOURNAL_STATE_POSTED, FinancialRevision: revision, PostedAt: timestamppb.New(f.now), TotalDebits: postMoney(11000), TotalCredits: postMoney(11000),
		Lines: []*tammyv1.JournalLine{{Id: debitID, JournalId: id, Sequence: 1, AccountId: postingExpense, Debit: postMoney(11000), Credit: postMoney(0)},
			{Id: creditID, JournalId: id, Sequence: 2, AccountId: cashID, Debit: postMoney(0), Credit: postMoney(11000)}}}
	flows := map[string][]accounting.CashFlowComponent{debitID: {{Category: accounting.CashFlowNoncash, AmountMinor: 11000}}, creditID: {{Category: accounting.CashFlowOperating, AmountMinor: -11000}}}
	sourceRevision := uint64(postScalar(t, f.executor, `SELECT COALESCE(MAX(source_revision),0)+1 FROM journals WHERE organisation_id=? AND source_type='MANUAL' AND source_id=?`, postingOrg, source))
	if err := writer.Post(ctx, journal, "MANUAL", source, sourceRevision, nil, flows, f.now); err != nil {
		t.Fatal(err)
	}
	if got := postScalar(t, f.executor, `SELECT SUM(debit_minor-credit_minor) FROM journal_lines WHERE journal_id=? AND account_id=?`, id, cashID); got != -11000 {
		t.Fatalf("cash-code %s receipt did not retain a real cash credit: %d", cashCode, got)
	}
	return journal
}

func coverageLegacyDocument(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, id string) *purchases.LegacyPurchaseReportingRecord {
	t.Helper()
	for _, source := range capture.LegacySources {
		if source != nil && source.Document != nil && source.Document.Id == id {
			return source
		}
	}
	t.Fatalf("coverage omitted legacy document source %s", id)
	return nil
}

func coverageLegacyOpening(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, id string) *purchases.LegacyPurchaseReportingRecord {
	t.Helper()
	for _, source := range capture.LegacySources {
		if source != nil && source.Source.GetOpeningPayable() != nil && source.Source.GetOpeningPayable().OpeningPayableId == id {
			return source
		}
	}
	t.Fatalf("coverage omitted opening payable source %s", id)
	return nil
}

func coverageHasID(values []string, id string) bool {
	for _, value := range values {
		if value == id {
			return true
		}
	}
	return false
}

func coverageRawRows(capture *purchases.PurchaseCoverageReportingCapture) *purchases.PostingReportingCapture {
	return &purchases.PostingReportingCapture{Rows: capture.Rows}
}

func coverageStateCounts(t *testing.T, executor app.CommandSQLExecutor) map[string]any {
	t.Helper()
	counts := reportingStateCounts(t, executor)
	for _, query := range []string{
		`SELECT count(*) FROM opening_conversions`,
		`SELECT count(*) FROM opening_items`,
		`SELECT count(*) FROM purchase_opening_payables`,
		`SELECT count(*) FROM purchase_legacy_reviews_v1`,
	} {
		counts[query] = postScalar(t, executor, query)
	}
	return counts
}

func coverageOpeningWithDocument(t *testing.T, f *postingFixture, document *tammyv1.Document, replaced bool) (itemID, conversionID string) {
	t.Helper()
	ctx := context.Background()
	rows, err := f.executor.QueryContext(ctx, `SELECT id FROM accounts WHERE organisation_id=? AND code='2000'`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var payableID string
	if !rows.Next() || rows.Scan(&payableID) != nil {
		_ = rows.Close()
		t.Fatal("missing payable control")
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}

	conversionID, _ = f.id()
	itemID, _ = f.id()
	journal := f.seedPriorJournal(conversionID, tammyv1.JournalSource_JOURNAL_SOURCE_OPENING_CONVERSION, postingExpense, payableID)
	if _, err := f.executor.ExecContext(ctx, `INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,version,created_at) VALUES(?,?,'2026-08-10','DRAFT',?,1,?)`, conversionID, postingOrg, strings.Repeat("a", 64), f.now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	input := &tammyv1.OpeningBalanceInput{
		ClientLineId:  itemID,
		AccountId:     payableID,
		Kind:          tammyv1.OpeningBalanceKind_OPENING_BALANCE_KIND_SUPPLIER_OPEN_ITEM,
		LedgerBalance: postMoney(11000),
		Source: &tammyv1.SourceRef{Type: "document", Id: document.Id, Revision: uint64(document.Version),
			ContentHash: append([]byte(nil), document.Sha256...)},
		OriginalIssueDate: postDate(10), OriginalDueDate: postDate(20),
		OutstandingGst: postMoney(1000), PriorGstAttributed: postMoney(0),
	}
	if _, err := f.executor.ExecContext(ctx, `INSERT INTO opening_items(id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,source_type,source_id,source_revision,source_content_hash,original_issue_date,original_due_date,outstanding_gst_minor,prior_gst_attributed_minor) VALUES(?,?,?,'SUPPLIER_OPEN_ITEM',0,11000,'AUD','document',?,2,?,'2026-08-10','2026-08-20',1000,0)`, itemID, conversionID, payableID, document.Id, document.Sha256); err != nil {
		t.Fatal(err)
	}
	opening, err := purchases.NewOpeningRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	if err := opening.RecordOpeningPayable(ctx, conversionID, input, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.executor.ExecContext(ctx, `UPDATE opening_conversions SET state='POSTED',journal_id=?,financial_revision=? WHERE id=?`, journal.Id, journal.FinancialRevision, conversionID); err != nil {
		t.Fatal(err)
	}
	if replaced {
		replacementID, _ := f.id()
		if _, err := f.executor.ExecContext(ctx, `INSERT INTO opening_conversions(id,organisation_id,conversion_date,state,source_sha256,version,created_at) VALUES(?,?,'2026-08-10','DRAFT',?,1,?)`, replacementID, postingOrg, strings.Repeat("b", 64), f.now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		journalRepository, err := accounting.NewJournalRepository(f.executor)
		if err != nil {
			t.Fatal(err)
		}
		reversalID, _ := f.id()
		reversalLineIDs := make([]string, len(journal.Lines))
		for index := range reversalLineIDs {
			reversalLineIDs[index], _ = f.id()
		}
		reversed, _, err := journalRepository.Reverse(ctx, journal.Id, journal.Version, postDate(11),
			"Replace opening source evidence", reversalID, reversalLineIDs, f.now)
		if err != nil || reversed == nil || reversed.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED ||
			reversed.Version != 2 || reversed.ReversedByJournalId == nil || *reversed.ReversedByJournalId != reversalID {
			t.Fatalf("reverse opening conversion journal before replacement: journal=%v err=%v", reversed, err)
		}
		secondReversalID, _ := f.id()
		secondReversalLineIDs := make([]string, len(reversalLineIDs))
		for index := range secondReversalLineIDs {
			secondReversalLineIDs[index], _ = f.id()
		}
		reversedFirst, finalReversal, err := journalRepository.Reverse(ctx, reversalID, 1, postDate(12),
			"Reverse the replacement reversal", secondReversalID, secondReversalLineIDs, f.now)
		if err != nil || reversedFirst == nil || finalReversal == nil || reversedFirst.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED ||
			reversedFirst.Version != 2 || reversedFirst.ReversedByJournalId == nil || *reversedFirst.ReversedByJournalId != secondReversalID ||
			finalReversal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || finalReversal.ReversalOfJournalId == nil ||
			*finalReversal.ReversalOfJournalId != reversalID {
			t.Fatalf("retain transitive replacement reversal chain: first=%v final=%v err=%v", reversedFirst, finalReversal, err)
		}
		conversionRepository, err := accounting.NewOpeningConversionRepository(f.executor)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conversionRepository.MarkReplaced(ctx, conversionID, 1, replacementID); err != nil {
			t.Fatalf("mark legally reversed opening conversion replaced: %v", err)
		}
	}
	return itemID, conversionID
}

func coverageReviewOpening(t *testing.T, f *postingFixture, itemID string) *tammyv1.LegacyPurchaseReview {
	t.Helper()
	ctx := context.Background()
	opening, err := purchases.NewOpeningRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	value, err := opening.GetOpeningPayable(ctx, postingOrg, itemID)
	if err != nil {
		t.Fatal(err)
	}
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	request.Review.Source = value.LegacySource()
	response, err := legacyRepo(t, f, f.executor).ResolveLegacy(ctx, request)
	if err != nil || response == nil || response.Review == nil {
		t.Fatalf("retain opening review: response=%v err=%v", response, err)
	}
	return response.Review
}

func TestPurchaseCoverageCaptureRetainsEveryFamilyAndExplicitLineage(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	repo := f.repo(tx)

	// This reviewed source has both an ordinary prior journal and a supported
	// manual cash settlement. The review retains exact document, payment and
	// support-document edges.
	f.date = &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 10}
	f.addDocument("Old legacy supplier", "LEGACY-OLD", 10000, 1000)
	oldLegacy := proto.Clone(f.document).(*tammyv1.Document)
	prior := f.seedPriorJournal(oldLegacy.Id, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
	legacyPayment := seedCoverageLegacyCashPayment(t, f, oldLegacy.Id, "1011")
	legacyRequestValue := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_UNSUPPORTED_EXISTING_SETTLEMENT)
	legacyPaymentHash := coverageMessageHash(t, legacyPayment)
	legacyRequestValue.Review.SupportingPayments = []*tammyv1.PurchaseRetainedSourceRef{{
		SourceId: legacyPayment.Id, SourceVersion: legacyPayment.Version,
		SourceHash: legacyPaymentHash,
	}}
	legacyResult, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, legacyRequestValue)
	if err != nil {
		t.Fatalf("retain explicit legacy settlement review: %v", err)
	}
	secondLegacyRequest := electNextLegacy(t, f, legacyRequestValue)
	secondLegacyRequest.Review.Reason = "Re-reviewed the same retained settlement with complete prior history"
	secondLegacyResult, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, secondLegacyRequest)
	if err != nil {
		t.Fatalf("retain second immutable legacy review revision: %v", err)
	}
	legacySupportID := legacyResult.Review.Review.SupportingEvidence[0].SourceId

	// Complete discovery must preserve old, undated and future documents.
	f.date = nil
	f.addDocumentReview("Undated legacy supplier", "LEGACY-UNDATED", 10000, 1000, false)
	undated := proto.Clone(f.document).(*tammyv1.Document)
	documentsRepo, err := documents.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	undatedCandidate := proto.Clone(undated.Candidate).(*tammyv1.DocumentCandidate)
	undatedCandidate.DocumentDate = nil
	undated, err = documentsRepo.SaveReview(ctx, undated.Id, undated.Version, undatedCandidate, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.date = &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 10}
	f.addDocument("Future legacy supplier", "LEGACY-FUTURE", 10000, 1000)
	futureLegacy := proto.Clone(f.document).(*tammyv1.Document)
	f.date = &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 1}
	f.addDocumentReview("Needs review supplier", "LEGACY-INTAKE", 10000, 1000, false)
	needsReview := proto.Clone(f.document).(*tammyv1.Document)
	needsReviewPrior := f.seedPriorJournal(needsReview.Id, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
	needsReviewPayment := seedCoverageLegacyCashPayment(t, f, needsReview.Id, "1012")

	oldPosting := postReportingPurchase(t, f, "MODERN-OLD", &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 10}, 10000, 1000)
	modernPayment, err := repo.RecordPayment(ctx, paymentRequest(t, f, oldPosting.Accounting.AccountingVersion, 11000, paymentDate(8, 20)))
	if err != nil {
		t.Fatalf("record modern reporting receipt: %v", err)
	}
	futurePosting := postReportingPurchase(t, f, "MODERN-FUTURE", &tammyv1.CivilDate{Year: 2027, Month: 1, Day: 10}, 10000, 1000)

	replacedOpeningID, _ := coverageOpeningWithDocument(t, f, oldLegacy, true)
	replacedOpeningReview := coverageReviewOpening(t, f, replacedOpeningID)
	postedOpeningID := legacyOpeningFixture(t, f)
	postedOpeningReview := coverageReviewOpening(t, f, postedOpeningID)

	beforeChanges := postScalar(t, tx, `SELECT total_changes()`)
	beforeCounts := coverageStateCounts(t, tx)
	capture := captureCoverage(t, repo, ctx)
	if len(capture.Postings) != 2 || capture.ReceiptCount != 3 {
		t.Fatalf("modern roots/combined receipts=%d/%d, want 2 modern roots and one modern plus two legacy receipts", len(capture.Postings), capture.ReceiptCount)
	}
	if len(capture.Fingerprint) != sha256.Size || capture.PayloadBytes <= 0 || capture.PayloadBytes > 8*1024*1024 {
		t.Fatalf("combined fingerprint/payload=%x/%d", capture.Fingerprint, capture.PayloadBytes)
	}
	if !proto.Equal(capture.Postings[0].Posting, oldPosting.Posting) && !proto.Equal(capture.Postings[1].Posting, oldPosting.Posting) {
		t.Fatal("old modern posting was not retained")
	}
	if !proto.Equal(capture.Postings[0].Posting, futurePosting.Posting) && !proto.Equal(capture.Postings[1].Posting, futurePosting.Posting) {
		t.Fatal("future modern posting was filtered")
	}

	oldSource := coverageLegacyDocument(t, capture, oldLegacy.Id)
	if !oldSource.CurrentRoot || oldSource.Document == nil || oldSource.Document.Candidate.DocumentDate.Month != 6 || !coverageHasID(oldSource.PriorJournalIDs, prior.Id) || !coverageHasID(oldSource.PriorJournalIDs, legacyPayment.Id) || !coverageHasID(oldSource.PaymentJournalIDs, legacyPayment.Id) {
		t.Fatalf("legacy root/prior/payment provenance incomplete: %+v", oldSource)
	}
	if len(oldSource.Reviews) != 2 || oldSource.Reviews[0].SourceRevision != 1 || oldSource.Reviews[1].SourceRevision != 2 ||
		!proto.Equal(oldSource.Reviews[0], legacyResult.Review) || !proto.Equal(oldSource.Reviews[1], secondLegacyResult.Review) ||
		!proto.Equal(oldSource.Source, legacyResult.Review.Review.Source) {
		t.Fatalf("legacy reviews are incomplete, unordered, or changed today's source projection: %+v", oldSource.Reviews)
	}
	assertCoverageEventPresent(t, capture, "DOCUMENT", oldLegacy.Id)
	if countCoverageRows(coverageRawRows(capture), "purchase_accounting_events_v1", "scope_id", oldLegacy.Id) != 2 {
		t.Fatal("both immutable legacy accounting event revisions were not retained")
	}
	if !coverageHasID(oldSource.MatchingOpeningPayableIDs, replacedOpeningID) {
		t.Fatalf("document-to-replaced-opening source edge omitted: %v", oldSource.MatchingOpeningPayableIDs)
	}
	if got := coverageLegacyDocument(t, capture, undated.Id); !got.CurrentRoot || got.Document.Candidate.DocumentDate != nil {
		t.Fatalf("undated legacy source filtered or assigned a date: %+v", got)
	}
	if got := coverageLegacyDocument(t, capture, futureLegacy.Id); !got.CurrentRoot || got.Document.Candidate.DocumentDate.Year != 2027 {
		t.Fatalf("future legacy source filtered or rewritten: %+v", got)
	}
	needsReviewSource := coverageLegacyDocument(t, capture, needsReview.Id)
	if !needsReviewSource.CurrentRoot || needsReviewSource.Document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW || needsReviewSource.Source != nil || len(needsReviewSource.Reviews) != 0 {
		t.Fatalf("unreviewed intake was omitted or given forged legacy review provenance: %+v", needsReviewSource)
	}
	if !coverageHasID(needsReviewSource.PriorJournalIDs, needsReviewPrior.Id) ||
		!coverageHasID(needsReviewSource.PriorJournalIDs, needsReviewPayment.Id) ||
		!coverageHasID(needsReviewSource.PaymentJournalIDs, needsReviewPayment.Id) {
		t.Fatalf("unreviewed document ID was not used to discover its explicit prior and payment journals: prior=%v payments=%v", needsReviewSource.PriorJournalIDs, needsReviewSource.PaymentJournalIDs)
	}
	postedOpening := coverageLegacyOpening(t, capture, postedOpeningID)
	replacedOpening := coverageLegacyOpening(t, capture, replacedOpeningID)
	if !postedOpening.CurrentRoot || postedOpening.Opening == nil || postedOpening.Opening.State != "POSTED" || len(postedOpening.Reviews) != 1 || !proto.Equal(postedOpening.Reviews[0], postedOpeningReview) {
		t.Fatalf("POSTED opening provenance/review incomplete: %+v", postedOpening)
	}
	assertCoverageEventPresent(t, capture, "OPENING_PAYABLE", postedOpeningID)
	if !replacedOpening.CurrentRoot || replacedOpening.Opening == nil || replacedOpening.Opening.State != "REPLACED" || len(replacedOpening.Reviews) != 1 || !proto.Equal(replacedOpening.Reviews[0], replacedOpeningReview) {
		t.Fatalf("REPLACED opening provenance/review incomplete: %+v", replacedOpening)
	}
	assertCoverageEventPresent(t, capture, "OPENING_PAYABLE", replacedOpeningID)
	if len(postedOpening.Source.GetOpeningPayable().SourceHash) != sha256.Size || len(replacedOpening.Source.GetOpeningPayable().SourceHash) != sha256.Size ||
		bytes.Equal(postedOpening.Source.GetOpeningPayable().SourceHash, postedOpening.Opening.ConversionHash) {
		t.Fatal("computed opening provenance hashes were omitted")
	}

	rows := coverageRawRows(capture)
	openingRow := retainedRowForText(t, rows, "opening_items", "id", postedOpeningID)
	for _, column := range []string{"source_type", "source_id", "source_revision", "source_content_hash"} {
		value, ok := retainedValue(openingRow, column)
		if !ok || value.Kind != "NULL" {
			t.Fatalf("opening SQL NULL provenance %s was synthesized: %#v", column, value)
		}
	}
	replacedOpeningRow := retainedRowForText(t, rows, "opening_items", "id", replacedOpeningID)
	replacedSourceType, sourceTypeOK := retainedValue(replacedOpeningRow, "source_type")
	replacedSourceID, sourceIDOK := retainedValue(replacedOpeningRow, "source_id")
	replacedSourceRevision, sourceRevisionOK := retainedValue(replacedOpeningRow, "source_revision")
	replacedSourceHash, sourceHashOK := retainedValue(replacedOpeningRow, "source_content_hash")
	if !sourceTypeOK || replacedSourceType.Kind != "TEXT" || replacedSourceType.Text != "document" ||
		!sourceIDOK || replacedSourceID.Kind != "TEXT" || replacedSourceID.Text != oldLegacy.Id ||
		!sourceRevisionOK || replacedSourceRevision.Kind != "INTEGER" || replacedSourceRevision.Integer != 2 ||
		!sourceHashOK || replacedSourceHash.Kind != "BLOB" || !bytes.Equal(replacedSourceHash.Bytes, oldLegacy.Sha256) {
		t.Fatalf("replaced opening's present raw document provenance changed: type=%#v id=%#v revision=%#v hash=%#v", replacedSourceType, replacedSourceID, replacedSourceRevision, replacedSourceHash)
	}
	needsReviewRow := retainedRowForText(t, rows, "documents", "id", needsReview.Id)
	if reviewedAt, ok := retainedValue(needsReviewRow, "reviewed_at"); !ok || reviewedAt.Kind != "NULL" {
		t.Fatalf("NEEDS_REVIEW source acquired a synthetic review timestamp: %#v", reviewedAt)
	}
	if len(oldSource.Reviews[0].Review.SupportingEvidence) != 1 || oldSource.Reviews[0].Review.SupportingEvidence[0].SourceId != legacySupportID {
		t.Fatal("legacy evidence edge changed")
	}
	if len(oldSource.Reviews[0].Review.SupportingPayments) != 1 || oldSource.Reviews[0].Review.SupportingPayments[0].SourceId != legacyPayment.Id ||
		oldSource.Reviews[0].Review.SupportingPayments[0].SourceVersion != legacyPayment.Version ||
		!bytes.Equal(oldSource.Reviews[0].Review.SupportingPayments[0].SourceHash, legacyPaymentHash) {
		t.Fatal("legacy support-payment edge lost exact ID/version/hash")
	}
	if !coverageDocumentPresent(capture, legacySupportID) {
		t.Fatalf("supporting evidence document %s was not hydrated", legacySupportID)
	}
	retainedSupport := coverageCapturedDocument(t, capture, legacySupportID)
	if retainedSupport.Version != legacyResult.Review.Review.SupportingEvidence[0].SourceVersion ||
		!bytes.Equal(retainedSupport.Sha256, legacyResult.Review.Review.SupportingEvidence[0].SourceHash) {
		t.Fatal("supporting evidence edge does not match its exact retained document identity")
	}
	if !coverageDocumentPresent(capture, oldPosting.Posting.DocumentId) || !coverageDocumentPresent(capture, futurePosting.Posting.DocumentId) {
		t.Fatal("modern posting document edge was not hydrated")
	}
	legacyJournal := coverageJournal(t, capture, legacyPayment.Id)
	if legacyJournal.SourceType != "MANUAL" || legacyJournal.SourceKind != 0 || legacyJournal.SourceID != oldLegacy.Id ||
		!proto.Equal(legacyJournal.Journal, legacyPayment) ||
		!bytes.Equal(coverageMessageHash(t, legacyJournal.Journal), legacyPaymentHash) || len(legacyJournal.ContentHash) != sha256.Size ||
		bytes.Equal(legacyJournal.ContentHash, legacyPaymentHash) {
		t.Fatalf("legacy manual receipt source edge/hash changed: %+v", legacyJournal)
	}
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	accountingProof, err := journalRepository.GetForReporting(ctx, postingOrg, legacyPayment.Id)
	if err != nil || accountingProof == nil || !bytes.Equal(legacyJournal.ContentHash, accountingProof.ContentHash) {
		t.Fatalf("captured legacy receipt did not preserve accounting's complete SQL journal proof: proof=%v err=%v", accountingProof, err)
	}
	needsReviewReceipt := coverageJournal(t, capture, needsReviewPayment.Id)
	if needsReviewReceipt.SourceType != "MANUAL" || needsReviewReceipt.SourceKind != 0 || needsReviewReceipt.SourceID != needsReview.Id {
		t.Fatalf("unreviewed document manual receipt source edge changed: %+v", needsReviewReceipt)
	}
	modernRoot := coveragePosting(t, capture, oldPosting.Posting.Id)
	if len(modernRoot.Payments) != 1 || modernRoot.Payments[0].Id != modernPayment.Payment.Id {
		t.Fatal("modern payment history was not retained")
	}
	modernJournal := coverageJournal(t, capture, modernPayment.Payment.JournalId)
	if modernJournal.SourceType != "BANKING" || modernJournal.SourceKind != 7 || modernJournal.SourceID != modernPayment.Payment.Id {
		t.Fatalf("modern BANKING source tuple changed: %+v", modernJournal)
	}
	if countCoverageJournals(capture, legacyPayment.Id) != 1 || countCoverageRows(rows, "documents", "id", legacySupportID) != 1 ||
		countCoverageRows(rows, "accounts", "code", "2000") != 1 || coverageHasID(oldSource.PaymentJournalIDs, prior.Id) {
		t.Fatal("shared journal proof/document/account rows were duplicated or a prior-only journal was counted as a receipt")
	}
	priorJournal := coverageJournal(t, capture, prior.Id)
	if priorJournal.SourceType != "MANUAL" || priorJournal.SourceKind != 0 || priorJournal.SourceID != oldLegacy.Id {
		t.Fatalf("prior manual journal source edge changed: %+v", priorJournal)
	}
	for _, opening := range []*purchases.LegacyPurchaseReportingRecord{postedOpening, replacedOpening} {
		journal := coverageJournal(t, capture, opening.Opening.JournalID)
		if journal.SourceType != "OPENING" || journal.SourceKind != 0 || journal.SourceID != opening.Opening.ConversionID || journal.SourceRevision != 1 {
			t.Fatalf("opening conversion journal edge changed: %+v", journal)
		}
	}
	openingJournal := coverageJournal(t, capture, replacedOpening.Opening.JournalID)
	if openingJournal.Journal.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED || openingJournal.Journal.Version != 2 || openingJournal.Journal.ReversedByJournalId == nil {
		t.Fatalf("REPLACED opening journal lacks its real reversal link: %+v", openingJournal.Journal)
	}
	firstReversalID := *openingJournal.Journal.ReversedByJournalId
	firstReversal := coverageJournal(t, capture, firstReversalID)
	if firstReversal.SourceType != "REVERSAL" || firstReversal.SourceKind != 0 || firstReversal.SourceID != openingJournal.Journal.Id || firstReversal.SourceRevision != 1 ||
		firstReversal.Journal.State != tammyv1.JournalState_JOURNAL_STATE_REVERSED || firstReversal.Journal.Version != 2 ||
		firstReversal.Journal.ReversalOfJournalId == nil || *firstReversal.Journal.ReversalOfJournalId != openingJournal.Journal.Id ||
		firstReversal.Journal.ReversedByJournalId == nil {
		t.Fatalf("replacement opening's first reversal proof is incomplete: %+v", firstReversal)
	}
	finalReversalID := *firstReversal.Journal.ReversedByJournalId
	finalReversal := coverageJournal(t, capture, finalReversalID)
	if finalReversal.SourceType != "REVERSAL" || finalReversal.SourceKind != 0 || finalReversal.SourceID != firstReversal.Journal.Id || finalReversal.SourceRevision != 1 ||
		finalReversal.Journal.State != tammyv1.JournalState_JOURNAL_STATE_POSTED || finalReversal.Journal.ReversalOfJournalId == nil ||
		*finalReversal.Journal.ReversalOfJournalId != firstReversal.Journal.Id {
		t.Fatalf("replacement opening's final posted reversal proof is incomplete: %+v", finalReversal)
	}
	for _, journal := range []*accounting.JournalReportingRecord{openingJournal, firstReversal, finalReversal} {
		for _, line := range journal.Journal.Lines {
			if !coverageRawRowHasText(rows, "cash_flow_facts", "journal_line_id", line.Id) || !coverageRawRowHasText(rows, "accounts", "id", line.AccountId) {
				t.Fatalf("reversal closure omitted cash-flow/account proof for journal %s line %s", journal.Journal.Id, line.Id)
			}
		}
	}
	if oldPosting.Posting.JournalId == nil {
		t.Fatal("modern posting lost its payable journal identity")
	}
	postingJournal := coverageJournal(t, capture, *oldPosting.Posting.JournalId)
	if postingJournal.SourceType != "PAYABLE" || postingJournal.SourceKind != 6 || postingJournal.SourceID != oldPosting.Posting.Id {
		t.Fatalf("modern payable journal edge changed: %+v", postingJournal)
	}
	for _, journal := range capture.Journals {
		if journal == nil || journal.Journal == nil {
			t.Fatal("nil captured journal")
		}
		for _, line := range journal.Journal.Lines {
			if !coverageRawRowHasText(rows, "accounts", "id", line.AccountId) {
				t.Fatalf("journal %s account edge %s has no raw account row", journal.Journal.Id, line.AccountId)
			}
		}
	}
	if got := coverageStateCounts(t, tx); !reflect.DeepEqual(got, beforeCounts) {
		t.Fatalf("coverage capture wrote retained rows/revisions: before=%v after=%v", beforeCounts, got)
	}
	if got := postScalar(t, tx, `SELECT total_changes()`); got != beforeChanges {
		t.Fatalf("coverage capture executed %d SQLite row changes; want read-only capture", got-beforeChanges)
	}
	assertEveryCapturedTableHasCompleteSchema(t, tx, rows)
	assertCapturedTableSet(t, rows,
		"accounts", "cash_flow_facts", "command_idempotency_v1", "documents", "financial_revision_claims",
		"opening_conversions", "opening_items", "purchase_accounting_events_v1", "purchase_legacy_reviews_v1",
		"purchase_opening_payables", "purchase_payment_allocations_v1", "purchase_payments_v1",
		"purchase_posting_allocations_v1", "purchase_postings_v1", "purchase_tax_evidence_v1",
	)
	reviewRow := retainedRowForText(t, rows, "purchase_legacy_reviews_v1", "id", legacyResult.Review.Id)
	reviewProto, reviewProtoOK := retainedValue(reviewRow, "review_proto")
	reviewRecordHash, reviewHashOK := retainedValue(reviewRow, "record_hash")
	if !reviewProtoOK || reviewProto.Kind != "BLOB" || len(reviewProto.Bytes) == 0 ||
		!reviewHashOK || reviewRecordHash.Kind != "BLOB" || !bytes.Equal(reviewProto.Bytes, mustCoverageMarshal(t, legacyResult.Review)) ||
		!bytes.Equal(reviewRecordHash.Bytes, legacyResult.Review.SourceHash) {
		t.Fatalf("captured legacy review omitted its immutable raw review body/hash: proto=%#v hash=%#v", reviewProto, reviewRecordHash)
	}
	secondReviewRow := retainedRowForText(t, rows, "purchase_legacy_reviews_v1", "id", secondLegacyResult.Review.Id)
	secondReviewProto, secondReviewProtoOK := retainedValue(secondReviewRow, "review_proto")
	secondReviewRecordHash, secondReviewHashOK := retainedValue(secondReviewRow, "record_hash")
	if !secondReviewProtoOK || secondReviewProto.Kind != "BLOB" || len(secondReviewProto.Bytes) == 0 ||
		!secondReviewHashOK || secondReviewRecordHash.Kind != "BLOB" || !bytes.Equal(secondReviewProto.Bytes, mustCoverageMarshal(t, secondLegacyResult.Review)) ||
		!bytes.Equal(secondReviewRecordHash.Bytes, secondLegacyResult.Review.SourceHash) {
		t.Fatalf("captured second legacy review omitted its immutable raw body/hash: proto=%#v hash=%#v", secondReviewProto, secondReviewRecordHash)
	}
	legacyOperationKey := legacyRequestValue.CommandContext.IdempotencyKey
	eventRow := retainedRowForText(t, rows, "purchase_accounting_events_v1", "operation_key", legacyOperationKey)
	eventScopeKind, eventScopeKindOK := retainedValue(eventRow, "scope_kind")
	eventScopeID, eventScopeIDOK := retainedValue(eventRow, "scope_id")
	eventResult, eventResultOK := retainedValue(eventRow, "result_proto")
	eventResultHash, eventHashOK := retainedValue(eventRow, "result_sha256")
	if !eventScopeKindOK || eventScopeKind.Kind != "TEXT" || eventScopeKind.Text != "DOCUMENT" ||
		!eventScopeIDOK || eventScopeID.Kind != "TEXT" || eventScopeID.Text != oldLegacy.Id ||
		!eventResultOK || eventResult.Kind != "BLOB" || !bytes.Equal(eventResult.Bytes, mustCoverageMarshal(t, legacyResult)) ||
		!eventHashOK || eventResultHash.Kind != "BLOB" || !bytes.Equal(eventResultHash.Bytes, coverageMessageHash(t, legacyResult)) {
		t.Fatalf("captured legacy accounting event omitted its raw decision/event proof: kind=%#v id=%#v result=%#v hash=%#v", eventScopeKind, eventScopeID, eventResult, eventResultHash)
	}
	secondLegacyOperationKey := secondLegacyRequest.CommandContext.IdempotencyKey
	secondEventRow := retainedRowForText(t, rows, "purchase_accounting_events_v1", "operation_key", secondLegacyOperationKey)
	secondEventScopeKind, secondEventScopeKindOK := retainedValue(secondEventRow, "scope_kind")
	secondEventScopeID, secondEventScopeIDOK := retainedValue(secondEventRow, "scope_id")
	secondEventResult, secondEventResultOK := retainedValue(secondEventRow, "result_proto")
	secondEventResultHash, secondEventHashOK := retainedValue(secondEventRow, "result_sha256")
	if !secondEventScopeKindOK || secondEventScopeKind.Kind != "TEXT" || secondEventScopeKind.Text != "DOCUMENT" ||
		!secondEventScopeIDOK || secondEventScopeID.Kind != "TEXT" || secondEventScopeID.Text != oldLegacy.Id ||
		!secondEventResultOK || secondEventResult.Kind != "BLOB" || !bytes.Equal(secondEventResult.Bytes, mustCoverageMarshal(t, secondLegacyResult)) ||
		!secondEventHashOK || secondEventResultHash.Kind != "BLOB" || !bytes.Equal(secondEventResultHash.Bytes, coverageMessageHash(t, secondLegacyResult)) {
		t.Fatalf("captured second legacy event revision omitted its raw proof: kind=%#v id=%#v result=%#v hash=%#v", secondEventScopeKind, secondEventScopeID, secondEventResult, secondEventResultHash)
	}
	electionRow := retainedRowForText(t, rows, "command_idempotency_v1", "operation_key", legacyOperationKey)
	electionCommand, electionCommandOK := retainedValue(electionRow, "fully_qualified_rpc_name")
	electionOutcome, electionOutcomeOK := retainedValue(electionRow, "outcome")
	if !electionCommandOK || electionCommand.Kind != "TEXT" || electionCommand.Text != "tammy.v1.PurchaseService.ResolveLegacyPurchaseReview" ||
		!electionOutcomeOK || electionOutcome.Kind != "TEXT" || electionOutcome.Text != "ELECTED" {
		t.Fatalf("captured legacy review election is incomplete: command=%#v outcome=%#v", electionCommand, electionOutcome)
	}
	revisionClaim := retainedRowForText(t, rows, "financial_revision_claims", "operation_key", legacyOperationKey)
	claimRevision, claimRevisionOK := retainedValue(revisionClaim, "financial_revision")
	if !claimRevisionOK || claimRevision.Kind != "INTEGER" || claimRevision.Integer <= 0 {
		t.Fatalf("legacy event operation key %s lacks its financial revision claim: %#v", legacyOperationKey, claimRevision)
	}
	secondElectionRow := retainedRowForText(t, rows, "command_idempotency_v1", "operation_key", secondLegacyOperationKey)
	if outcome, ok := retainedValue(secondElectionRow, "outcome"); !ok || outcome.Kind != "TEXT" || outcome.Text != "ELECTED" {
		t.Fatalf("second legacy review command election is missing or changed: %#v", outcome)
	}
	secondRevisionClaim := retainedRowForText(t, rows, "financial_revision_claims", "operation_key", secondLegacyOperationKey)
	if revision, ok := retainedValue(secondRevisionClaim, "financial_revision"); !ok || revision.Kind != "INTEGER" || revision.Integer <= 0 {
		t.Fatalf("second legacy event operation key %s lacks its revision claim: %#v", secondLegacyOperationKey, revision)
	}

	unchangedMoney := paymentBalance(t, tx, "1150")
	if _, err := tx.ExecContext(ctx, "SAVEPOINT purchase_coverage_metadata"); err != nil {
		t.Fatal(err)
	}
	newUpdatedAt := f.now.Add(time.Second).UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET updated_at=? WHERE organisation_id=? AND code='2000'`, newUpdatedAt, postingOrg); err != nil {
		t.Fatal(err)
	}
	changedMetadata := captureCoverage(t, repo, ctx)
	if bytes.Equal(changedMetadata.Fingerprint, capture.Fingerprint) || reflect.DeepEqual(changedMetadata.Rows, capture.Rows) ||
		!proto.Equal(coveragePosting(t, changedMetadata, oldPosting.Posting.Id).Accounting, modernRoot.Accounting) || paymentBalance(t, tx, "1150") != unchangedMoney {
		t.Fatal("raw account metadata change failed to change fingerprint, or changed accounting money")
	}
	if _, err := tx.ExecContext(ctx, "ROLLBACK TO purchase_coverage_metadata"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "RELEASE purchase_coverage_metadata"); err != nil {
		t.Fatal(err)
	}
	restored := captureCoverage(t, repo, ctx)
	if !bytes.Equal(restored.Fingerprint, capture.Fingerprint) || !reflect.DeepEqual(restored.Rows, capture.Rows) {
		t.Fatal("metadata rollback did not restore exact capture fingerprint and rows")
	}
	if got := captureCoverage(t, repo, ctx); !bytes.Equal(got.Fingerprint, restored.Fingerprint) || !reflect.DeepEqual(got.Rows, restored.Rows) {
		t.Fatal("repeated complete capture was nondeterministic")
	}

	captureTime := f.now
	f.now = time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	afterFutureDocumentDate := captureCoverage(t, repo, ctx)
	if !bytes.Equal(afterFutureDocumentDate.Fingerprint, capture.Fingerprint) || !reflect.DeepEqual(afterFutureDocumentDate.Rows, capture.Rows) {
		t.Fatal("advancing the capture clock past a future-held document changed complete raw rows or fingerprint")
	}
	f.now = captureTime

	exactBudget, err := repo.CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, capture.PayloadBytes)
	if err != nil || exactBudget == nil || exactBudget.PayloadBytes != capture.PayloadBytes || !bytes.Equal(exactBudget.Fingerprint, capture.Fingerprint) {
		t.Fatalf("exact shared payload budget rejected or changed capture: capture=%v err=%v", exactBudget, err)
	}
	partial, err := repo.CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, capture.PayloadBytes-1)
	assertPurchaseSourceBound(t, err, capture.PayloadBytes-1)
	if partial != nil {
		t.Fatal("one-byte-short shared budget returned partial coverage")
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	read, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	restarted := captureCoverage(t, f.repo(read), ctx)
	assertCoverageStableAfterReopen(t, capture, restarted)
}

func coverageProtoListEqual[T proto.Message](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !proto.Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}

func assertCoverageStableAfterReopen(t *testing.T, before, after *purchases.PurchaseCoverageReportingCapture) {
	t.Helper()
	if before.OrganisationID != after.OrganisationID || before.ReceiptCount != after.ReceiptCount || before.PayloadBytes != after.PayloadBytes ||
		!bytes.Equal(before.Fingerprint, after.Fingerprint) || !reflect.DeepEqual(before.Rows, after.Rows) ||
		len(before.Postings) != len(after.Postings) || len(before.LegacySources) != len(after.LegacySources) ||
		len(before.Documents) != len(after.Documents) || len(before.Journals) != len(after.Journals) {
		t.Fatal("retained coverage bytes, proof hash, counts or ordering changed after encrypted reopen")
	}
	// Protobuf messages have private caches populated by marshal/hash operations.
	// Compare their complete messages with proto.Equal, independently of caches.
	for index, left := range before.Postings {
		right := after.Postings[index]
		if !proto.Equal(left.Posting, right.Posting) || !proto.Equal(left.Accounting, right.Accounting) ||
			!coverageProtoListEqual(left.Payments, right.Payments) || !coverageProtoListEqual(left.Evidences, right.Evidences) ||
			!coverageProtoListEqual(left.Claims.Attributions, right.Claims.Attributions) ||
			!coverageProtoListEqual(left.Claims.Deferrals, right.Claims.Deferrals) || len(left.Claims.DeferralPortions) != len(right.Claims.DeferralPortions) {
			t.Fatalf("modern purchase history %d changed after encrypted reopen", index)
		}
		for portionIndex, portion := range left.Claims.DeferralPortions {
			other := right.Claims.DeferralPortions[portionIndex]
			if portion.DeferralID != other.DeferralID || portion.Sequence != other.Sequence || !proto.Equal(portion.Portion, other.Portion) ||
				!proto.Equal(portion.DeferredGST, other.DeferredGST) || !proto.Equal(portion.UnclaimedGST, other.UnclaimedGST) {
				t.Fatalf("purchase deferral portion %d/%d changed after encrypted reopen", index, portionIndex)
			}
		}
	}
	for index, left := range before.LegacySources {
		right := after.LegacySources[index]
		if !proto.Equal(left.Source, right.Source) || !proto.Equal(left.Document, right.Document) ||
			!coverageProtoListEqual(left.Reviews, right.Reviews) || left.CurrentRoot != right.CurrentRoot ||
			!reflect.DeepEqual(left.PriorJournalIDs, right.PriorJournalIDs) || !reflect.DeepEqual(left.PaymentJournalIDs, right.PaymentJournalIDs) ||
			!reflect.DeepEqual(left.MatchingOpeningPayableIDs, right.MatchingOpeningPayableIDs) || (left.Opening == nil) != (right.Opening == nil) {
			t.Fatalf("legacy purchase history %d changed after encrypted reopen", index)
		}
		if left.Opening != nil {
			leftOpening, rightOpening := *left.Opening, *right.Opening
			if !proto.Equal(leftOpening.Input, rightOpening.Input) {
				t.Fatalf("opening input %d changed after encrypted reopen", index)
			}
			leftOpening.Input, rightOpening.Input = nil, nil
			if !reflect.DeepEqual(leftOpening, rightOpening) {
				t.Fatalf("opening provenance %d changed after encrypted reopen", index)
			}
		}
	}
	for index, left := range before.Documents {
		if !proto.Equal(left.Document, after.Documents[index].Document) {
			t.Fatalf("document %d changed after encrypted reopen", index)
		}
	}
	for index, left := range before.Journals {
		right := after.Journals[index]
		if !proto.Equal(left.Journal, right.Journal) {
			t.Fatalf("complete journal %d changed after encrypted reopen", index)
		}
		leftProof, rightProof := *left, *right
		leftProof.Journal, rightProof.Journal = nil, nil
		if !reflect.DeepEqual(leftProof, rightProof) {
			t.Fatalf("journal provenance %d changed after encrypted reopen", index)
		}
	}
}

func TestPurchaseCoverageCaptureRetainsPromotedReviewAfterNewCashPayment(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	promotedDocument := proto.Clone(f.document).(*tammyv1.Document)
	postRequest := proto.Clone(f.request).(*tammyv1.PostReviewedPurchaseRequest)
	reviewRequest := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	reviewResponse, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, reviewRequest)
	if err != nil {
		t.Fatalf("retain pre-promotion review: %v", err)
	}
	f.document, f.request = promotedDocument, postRequest
	posted, err := f.repo(tx).PostReviewed(ctx, postRequest)
	if err != nil {
		t.Fatalf("post source with explicit retained unpaid decision: %v", err)
	}
	// This newly discovered manual cash journal contradicts the earlier unpaid
	// decision. Coverage still needs the complete immutable review and receipt.
	newCash := seedLegacyCashPayment(t, f, promotedDocument.Id)
	readBack, err := legacyRepo(t, f, tx).ReadLegacy(ctx, postingOrg, reviewResponse.Review.Review.Source)
	if err != nil || len(readBack) != 1 || !proto.Equal(readBack[0], reviewResponse.Review) {
		t.Fatalf("stale historical review was not readable: reviews=%v err=%v", readBack, err)
	}

	capture := captureCoverage(t, f.repo(tx), ctx)
	legacy := coverageLegacyDocument(t, capture, promotedDocument.Id)
	if legacy.CurrentRoot || legacy.Document == nil || len(legacy.Reviews) != 1 || !proto.Equal(legacy.Reviews[0], reviewResponse.Review) {
		t.Fatalf("promoted source lost its non-root legacy history: %+v", legacy)
	}
	if !coverageHasID(legacy.PaymentJournalIDs, newCash.Id) || capture.ReceiptCount != 1 {
		t.Fatalf("new contradictory payment was omitted or double-counted: paymentIDs=%v receipts=%d", legacy.PaymentJournalIDs, capture.ReceiptCount)
	}
	if len(capture.Postings) != 1 || capture.Postings[0].Posting.Id != posted.Posting.Id {
		t.Fatal("promoted document was lost from the modern posting roots")
	}
	assertCoverageEventPresent(t, capture, "DOCUMENT", promotedDocument.Id)
	for _, source := range capture.LegacySources {
		if source.Source.GetDocument() != nil && source.Source.GetDocument().DocumentId == promotedDocument.Id && source.CurrentRoot {
			t.Fatal("posted document was double-counted as a current legacy root")
		}
	}
}

func TestPurchaseCoverageCaptureRetainsManualPaymentLineageForPromotedDocumentWithoutReview(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	document := proto.Clone(f.document).(*tammyv1.Document)
	request := proto.Clone(f.request).(*tammyv1.PostReviewedPurchaseRequest)
	posted, err := f.repo(tx).PostReviewed(ctx, request)
	if err != nil {
		t.Fatalf("post document without a prior legacy review: %v", err)
	}
	manualPayment := seedCoverageLegacyCashPayment(t, f, document.Id, "1013")
	if got := postScalar(t, tx, `SELECT count(*) FROM purchase_legacy_reviews_v1`); got != 0 {
		t.Fatalf("fixture unexpectedly retained a legacy review before capture: %d", got)
	}

	capture := captureCoverage(t, f.repo(tx), ctx)
	lineage := coverageLegacyDocument(t, capture, document.Id)
	if lineage.CurrentRoot || len(lineage.Reviews) != 0 || lineage.Document == nil || lineage.Document.Id != document.Id {
		t.Fatalf("promoted document without review was omitted or counted as a current legacy root: %+v", lineage)
	}
	if !coverageHasID(lineage.PriorJournalIDs, manualPayment.Id) || !coverageHasID(lineage.PaymentJournalIDs, manualPayment.Id) || capture.ReceiptCount != 1 {
		t.Fatalf("explicit manual payment lineage or unique receipt count changed: prior=%v payments=%v receipts=%d", lineage.PriorJournalIDs, lineage.PaymentJournalIDs, capture.ReceiptCount)
	}
	manualProof := coverageJournal(t, capture, manualPayment.Id)
	if manualProof.SourceType != "MANUAL" || manualProof.SourceKind != 0 || manualProof.SourceID != document.Id ||
		!proto.Equal(manualProof.Journal, manualPayment) {
		t.Fatalf("manual payment's exact document source tuple changed: %+v", manualProof)
	}
	if posted == nil || posted.Posting == nil || len(capture.Postings) != 1 || capture.Postings[0].Posting.Id != posted.Posting.Id ||
		capture.Postings[0].Posting.DocumentId != document.Id {
		t.Fatal("modern posting root was lost while retaining the separate manual payment lineage")
	}
	lineageCount, currentRootCount := 0, 0
	for _, source := range capture.LegacySources {
		if source == nil || source.Document == nil || source.Document.Id != document.Id {
			continue
		}
		lineageCount++
		if source.CurrentRoot {
			currentRootCount++
		}
	}
	if lineageCount != 1 || currentRootCount != 0 || countCoverageJournals(capture, manualPayment.Id) != 1 {
		t.Fatalf("promoted payment lineage was duplicated: source rows=%d current roots=%d manual journal proofs=%d", lineageCount, currentRootCount, countCoverageJournals(capture, manualPayment.Id))
	}
}

type coverageCaptureProbe struct {
	app.CommandSQLExecutor
	documentSizePreflighted  bool
	originalBytesHydrated    bool
	hydrationBeforePreflight bool
	bodyOutputColumns        []string
}

func (probe *coverageCaptureProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	lower := strings.ToLower(query)
	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err != nil || rows == nil {
		return rows, err
	}
	columns, err := rows.Columns()
	if err != nil {
		return rows, err
	}
	bodyOutput := false
	preflightedBeforeQuery := probe.documentSizePreflighted
	originalBytesColumn := false
	for _, column := range columns {
		normalized := strings.ToLower(strings.TrimSpace(column))
		if purchaseCoverageBodyResultColumn(column) {
			bodyOutput = true
			probe.bodyOutputColumns = append(probe.bodyOutputColumns, normalized)
		}
		if normalized == "original_bytes" {
			originalBytesColumn = true
		}
	}
	if !bodyOutput && strings.Contains(lower, "length(") && strings.Contains(lower, "original_bytes") {
		// Actual fixed-shape size preflights return scalar columns only. If any
		// body result column is selected, even alongside a length expression,
		// the query cannot establish preflight-before-hydration ordering.
		probe.documentSizePreflighted = true
	}
	if originalBytesColumn {
		probe.originalBytesHydrated = true
		if !preflightedBeforeQuery {
			probe.hydrationBeforePreflight = true
		}
	}
	return rows, nil
}

func TestPurchaseCoverageCapturePreflightsOversizedLinkedDocumentBeforeOriginalByteHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	legacySource := proto.Clone(f.document).(*tammyv1.Document)
	prior := f.seedPriorJournal(legacySource.Id, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
	oversized := bytes.Repeat([]byte{'x'}, 8*1024*1024+1)
	largeSupport := newPurchaseReportingSupportDocument(t, f, "OVERSIZED-COVERAGE-SUPPORT", oversized)
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	request.Review.SupportingEvidence = []*tammyv1.PurchaseRetainedSourceRef{{
		SourceId: largeSupport.Id, SourceVersion: largeSupport.Version, SourceHash: append([]byte(nil), largeSupport.Sha256...),
	}}
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request); err != nil {
		t.Fatalf("retain valid oversized support-source fixture: %v", err)
	}
	_ = prior
	probe := &coverageCaptureProbe{CommandSQLExecutor: tx}
	capture, err := f.repo(probe).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseSourceBound(t, err, 8*1024*1024)
	if capture != nil || !probe.documentSizePreflighted || probe.originalBytesHydrated || probe.hydrationBeforePreflight {
		t.Fatalf("oversized linked document was not rejected before original-byte hydration: capture=%v preflight=%t original_bytes=%t before=%t body-columns=%v", capture != nil, probe.documentSizePreflighted, probe.originalBytesHydrated, probe.hydrationBeforePreflight, probe.bodyOutputColumns)
	}
}

func TestPurchaseCoverageCaptureUsesCallerTransactionRollbackAndCancellation(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	seedTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := newPostingFixture(t, seedTx, tammyv1.GstBasis_GST_BASIS_CASH)
	seedCounts := coverageStateCounts(t, seedTx)
	baseline := captureCoverage(t, f.repo(seedTx), ctx)
	if len(baseline.Postings) != 0 || baseline.ReceiptCount != 0 {
		t.Fatal("seed coverage unexpectedly included a posted purchase or receipt")
	}
	if err := seedTx.Commit(); err != nil {
		t.Fatal(err)
	}

	outerTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.executor = outerTx
	posted := postReportingPurchase(t, f, "ROLLBACK-COVERAGE", postDate(10), 10000, 1000)
	openingID := legacyOpeningFixture(t, f)
	inside := captureCoverage(t, f.repo(outerTx), ctx)
	if len(inside.Postings) != 1 || posted.Posting == nil || len(inside.LegacySources) < 2 || coverageLegacyOpening(t, inside, openingID) == nil {
		t.Fatal("capture did not read caller-owned uncommitted sources from all families")
	}
	if got := coverageStateCounts(t, outerTx); got["SELECT count(*) FROM purchase_opening_payables"] != int64(1) {
		t.Fatalf("fixture did not retain uncommitted opening source: %v", got)
	}
	if err := outerTx.Rollback(); err != nil {
		t.Fatal(err)
	}

	readTx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	f.executor = readTx
	afterRollback := captureCoverage(t, f.repo(readTx), ctx)
	if !bytes.Equal(afterRollback.Fingerprint, baseline.Fingerprint) || !reflect.DeepEqual(afterRollback.Rows, baseline.Rows) ||
		!reflect.DeepEqual(coverageStateCounts(t, readTx), seedCounts) {
		t.Fatal("outer rollback did not restore exact complete coverage baseline")
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	canceling := &cancelPurchaseReportingExecutor{CommandSQLExecutor: readTx, cancel: cancel}
	partial, err := f.repo(canceling).CaptureCoverageForBAS(cancelCtx, postingOrg)
	if partial != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled complete capture returned partial=%v err=%v", partial, err)
	}
}

func mustCoverageMarshal(t *testing.T, message proto.Message) []byte {
	t.Helper()
	content, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func coverageMessageHash(t *testing.T, message proto.Message) []byte {
	t.Helper()
	digest := sha256.Sum256(mustCoverageMarshal(t, message))
	return append([]byte(nil), digest[:]...)
}

func coverageDocumentPresent(capture *purchases.PurchaseCoverageReportingCapture, id string) bool {
	for _, document := range capture.Documents {
		if document.Document != nil && document.Document.Id == id {
			return true
		}
	}
	return false
}

func coverageCapturedDocument(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, id string) *tammyv1.Document {
	t.Helper()
	for _, document := range capture.Documents {
		if document.Document != nil && document.Document.Id == id {
			return document.Document
		}
	}
	t.Fatalf("capture omitted hydrated document %s", id)
	return nil
}

func coverageJournal(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, id string) *accounting.JournalReportingRecord {
	t.Helper()
	for _, journal := range capture.Journals {
		if journal != nil && journal.Journal != nil && journal.Journal.Id == id {
			return journal
		}
	}
	t.Fatalf("capture omitted journal %s", id)
	return nil
}

func coveragePosting(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, id string) purchases.PostingReportingRecord {
	t.Helper()
	for _, posting := range capture.Postings {
		if posting.Posting != nil && posting.Posting.Id == id {
			return posting
		}
	}
	t.Fatalf("capture omitted modern posting %s", id)
	return purchases.PostingReportingRecord{}
}

func coverageRawRowHasText(capture *purchases.PostingReportingCapture, table, column, value string) bool {
	for _, row := range capture.Rows {
		if row.Table != table {
			continue
		}
		cell, ok := retainedValue(row, column)
		if ok && cell.Kind == "TEXT" && cell.Text == value {
			return true
		}
	}
	return false
}

func countCoverageRows(capture *purchases.PostingReportingCapture, table, column, value string) int {
	count := 0
	for _, row := range capture.Rows {
		if row.Table != table {
			continue
		}
		cell, ok := retainedValue(row, column)
		if ok && cell.Kind == "TEXT" && cell.Text == value {
			count++
		}
	}
	return count
}

func countCoverageJournals(capture *purchases.PurchaseCoverageReportingCapture, id string) int {
	count := 0
	for _, journal := range capture.Journals {
		if journal != nil && journal.Journal != nil && journal.Journal.Id == id {
			count++
		}
	}
	return count
}

func assertCoverageEventPresent(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, kind, id string) {
	t.Helper()
	for _, row := range capture.Rows {
		if row.Table != "purchase_accounting_events_v1" {
			continue
		}
		scopeKind, kindOK := retainedValue(row, "scope_kind")
		scopeID, idOK := retainedValue(row, "scope_id")
		resultHash, hashOK := retainedValue(row, "result_sha256")
		if kindOK && scopeKind.Kind == "TEXT" && scopeKind.Text == kind && idOK && scopeID.Kind == "TEXT" && scopeID.Text == id &&
			hashOK && resultHash.Kind == "BLOB" && len(resultHash.Bytes) == sha256.Size {
			return
		}
	}
	t.Fatalf("capture omitted the verified %s legacy accounting event for %s", kind, id)
}
