//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Characterization oracles for the BAS candidate-mapping dependency. These
// assert writer-backed identity and current full-reader behavior only. They do
// not define a semantic candidate-admission policy or derive eligibility from
// physical scalar keys.
package purchases_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

func purchaseLegacyOracleReadBaseline(t *testing.T, tx *sqlcipher.Transaction, stage string) map[string]any {
	t.Helper()
	if !tx.Authenticated() {
		t.Fatalf("%s: caller transaction was not authenticated before readers", stage)
	}
	state := purchaseBASScalarState(t, tx)
	if !tx.Authenticated() {
		t.Fatalf("%s: caller transaction lost authentication while taking the read baseline", stage)
	}
	return state
}

func purchaseLegacyOracleRequireReadOnly(t *testing.T, tx *sqlcipher.Transaction, before map[string]any, stage string) {
	t.Helper()
	if !tx.Authenticated() {
		t.Fatalf("%s: caller transaction lost authentication after readers", stage)
	}
	after := purchaseBASScalarState(t, tx)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("%s: readers changed caller-owned SQL state: before=%v after=%v", stage, before, after)
	}
	if !tx.Authenticated() {
		t.Fatalf("%s: caller transaction lost authentication while checking read-only state", stage)
	}
}

func purchaseLegacyOracleKeys(t *testing.T, f *postingFixture) []purchases.BASScalarKey {
	t.Helper()
	ctx := context.Background()
	repo := f.repo(f.executor)
	summary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("summarize writer-backed BAS scalar keys: summary=%#v err=%v", summary, err)
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("read reserved writer-backed BAS scalar keys: keys=%d summary=%#v err=%v", len(keys), summary, err)
	}
	return keys
}

func purchaseLegacyOracleWorkpaperReview(t *testing.T, snapshot *purchases.BASSnapshot, reviewID string) *tammyv1.BasPurchaseReview {
	t.Helper()
	for _, row := range snapshot.Reviews {
		if row != nil && row.LegacyReview != nil && row.LegacyReview.Id == reviewID {
			return row
		}
	}
	t.Fatalf("workpaper omitted retained legacy review %s", reviewID)
	return nil
}

func purchaseLegacyOracleRequireReviewHistory(t *testing.T, got, want []*tammyv1.LegacyPurchaseReview) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("retained legacy review count=%d, want %d", len(got), len(want))
	}
	for index, record := range want {
		if got[index] == nil || !proto.Equal(got[index], record) {
			t.Fatalf("retained legacy review[%d]=%+v, want immutable writer record %+v", index, got[index], record)
		}
	}
}

func purchaseLegacyOracleRequireWorkpaperReview(t *testing.T, snapshot *purchases.BASSnapshot, record *tammyv1.LegacyPurchaseReview, status tammyv1.PurchaseReviewStatus, unclaimedGST int64) {
	t.Helper()
	row := purchaseLegacyOracleWorkpaperReview(t, snapshot, record.Id)
	legacy := row.LegacyReview
	if row.Status != status || row.UnclaimedGst == nil || row.UnclaimedGst.MinorUnits != unclaimedGST ||
		legacy == nil || legacy.Id != record.Id || legacy.SourceRevision != record.SourceRevision ||
		!bytes.Equal(legacy.SourceHash, record.SourceHash) || legacy.Reason != record.Review.Reason ||
		legacy.Disposition != record.Review.Disposition || !proto.Equal(legacy.Source, record.Review.Source) ||
		!proto.Equal(legacy.AffectedPeriod, record.Review.AffectedPeriod) ||
		!proto.Equal(legacy.ReportingBalanceDate, record.Review.ReportingBalanceDate) ||
		row.SourceRevision != record.SourceRevision || !bytes.Equal(row.SourceHash, record.SourceHash) || row.Reason != record.Review.Reason {
		t.Fatalf("workpaper did not retain exact review %s with status=%s and unclaimed GST=%d: row=%+v", record.Id, status, unclaimedGST, row)
	}
}

func purchaseLegacyOracleRequirePaymentReceiptKey(t *testing.T, keys []purchases.BASScalarKey, payment *tammyv1.Journal, sourceID string) {
	t.Helper()
	if payment == nil {
		t.Fatal("writer-backed payment journal is nil")
	}
	key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarManualJournal, payment.Id)
	if key.ID != payment.Id || !key.IsReceipt {
		t.Fatalf("manual cash payment did not map to its exact receipt identity: payment=%s key=%#v", payment.Id, key)
	}
	purchaseBASScalarRequireText(t, key.SourceID, sourceID, "manual payment source document id")
}

func purchaseLegacyOracleRequireReviewKey(t *testing.T, keys []purchases.BASScalarKey, record *tammyv1.LegacyPurchaseReview) {
	t.Helper()
	if record == nil || record.Review == nil || record.Review.Source == nil || record.Review.Source.GetDocument() == nil {
		t.Fatal("writer-backed review did not retain a document source snapshot")
	}
	source := record.Review.Source.GetDocument()
	key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarLegacyReview, record.Id)
	if key.ID != record.Id {
		t.Fatalf("legacy review key changed immutable ID: key=%#v record=%#v", key, record)
	}
	purchaseBASScalarRequireUint(t, key.AccountingVersion, record.SourceRevision, "legacy review accounting version")
	purchaseBASScalarRequireText(t, key.DocumentID, source.DocumentId, "legacy review document id")
	purchaseBASScalarRequireUint(t, key.SourceVersion, source.ReviewedVersion, "legacy review source version")
	if !bytes.Equal(key.SourceHash, source.OriginalContentHash) ||
		!bytes.Equal(key.ReviewedSnapshotHash, source.ReviewedSnapshotHash) ||
		!bytes.Equal(key.RecordHash, record.SourceHash) ||
		bytes.Equal(key.SourceHash, key.RecordHash) {
		t.Fatalf("legacy source, reviewed-snapshot, and immutable result hashes were conflated: key=%#v record=%#v", key, record)
	}
}

func TestBASCandidateLegacyIdentityOracle(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	currentDocument := proto.Clone(f.document).(*tammyv1.Document)
	const sourceGST = int64(1000)
	if currentDocument.Candidate.Gst == nil || currentDocument.Candidate.Gst.MinorUnits != sourceGST {
		t.Fatal("legacy source fixture must retain 1,000 cents of reviewed GST")
	}
	sourceID := currentDocument.Id
	reviewRequest := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	if reviewRequest.Review.Source.GetDocument().DocumentId != sourceID {
		t.Fatal("review fixture changed the physical source identity")
	}
	legacyReviews := legacyRepo(t, f, tx)
	reviewResult, err := legacyReviews.ResolveLegacy(ctx, reviewRequest)
	if err != nil || reviewResult == nil || reviewResult.Review == nil {
		t.Fatalf("retain first source-bound legacy decision: result=%v err=%v", reviewResult, err)
	}
	secondRequest := electNextLegacy(t, f, reviewRequest)
	secondRequest.Review.Reason = "A second consistent review confirms the same unpaid source facts"
	secondResult, err := legacyReviews.ResolveLegacy(ctx, secondRequest)
	if err != nil || secondResult == nil || secondResult.Review == nil {
		t.Fatalf("retain second source-bound legacy decision: result=%v err=%v", secondResult, err)
	}
	reviewRecords := []*tammyv1.LegacyPurchaseReview{reviewResult.Review, secondResult.Review}
	if reviewRecords[0].Id == reviewRecords[1].Id || reviewRecords[0].SourceRevision != 1 || reviewRecords[1].SourceRevision != reviewRecords[0].SourceRevision+1 ||
		bytes.Equal(reviewRecords[0].SourceHash, reviewRecords[1].SourceHash) ||
		!proto.Equal(reviewRecords[0].Review.Source, reviewRecords[1].Review.Source) ||
		!proto.Equal(reviewRecords[0].Review.AffectedPeriod, reviewRecords[1].Review.AffectedPeriod) ||
		!proto.Equal(reviewRecords[0].Review.ReportingBalanceDate, reviewRecords[1].Review.ReportingBalanceDate) {
		t.Fatalf("second writer review was not a distinct immutable record for the same source and scope: first=%+v second=%+v", reviewRecords[0], reviewRecords[1])
	}

	repo := f.repo(tx)
	period := reviewRequest.Review.AffectedPeriod
	balance := reviewRequest.Review.ReportingBalanceDate
	initialReadBaseline := purchaseLegacyOracleReadBaseline(t, tx, "applicable legacy readers")
	beforeKeys := purchaseLegacyOracleKeys(t, f)
	beforeSummary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || beforeSummary == nil {
		t.Fatalf("summarize initial source roots: %#v %v", beforeSummary, err)
	}
	beforeDocumentKey := purchaseBASScalarFindKey(t, beforeKeys, purchases.BASScalarDocument, sourceID)
	if !beforeDocumentKey.IsRoot || beforeDocumentKey.IsReceipt {
		t.Fatalf("unposted reviewed document root flags=%t/%t", beforeDocumentKey.IsRoot, beforeDocumentKey.IsReceipt)
	}
	for _, record := range reviewRecords {
		purchaseLegacyOracleRequireReviewKey(t, beforeKeys, record)
	}
	if beforeSummary.FamilyCounts[purchases.BASScalarLegacyReview-1] != int64(len(reviewRecords)) {
		t.Fatalf("scalar review family count=%d, want %d immutable writer reviews", beforeSummary.FamilyCounts[purchases.BASScalarLegacyReview-1], len(reviewRecords))
	}

	currentValues, err := repo.LegacyCandidates(ctx, postingOrg, period, balance)
	if err != nil {
		t.Fatal(err)
	}
	current := legacyCandidate(t, currentValues, sourceID)
	currentSource := legacyDocumentSource(currentDocument)
	if !current.Excluded || current.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR ||
		!proto.Equal(current.Source, currentSource) {
		t.Fatalf("applicable unpaid review changed root identity or failed to classify: candidate=%+v", current)
	}
	purchaseLegacyOracleRequireReviewHistory(t, current.Reviews, reviewRecords)
	for _, record := range reviewRecords {
		if !purchases.LegacyReviewApplies(record, currentSource, period, balance) {
			t.Fatalf("writer-backed review %s did not apply to its exact current source snapshot", record.Id)
		}
	}
	otherSnapshot := proto.Clone(currentSource).(*tammyv1.LegacyPurchaseSourceRef)
	otherSnapshot.GetDocument().ReviewedSnapshotHash = bytes.Repeat([]byte{0xa5}, 32)
	if purchases.LegacyReviewApplies(reviewResult.Review, otherSnapshot, period, balance) {
		t.Fatal("review applied to a different reviewed-document snapshot")
	}
	coverage := captureCoverage(t, repo, ctx)
	coverageSource := coverageLegacyDocument(t, coverage, sourceID)
	if !coverageSource.CurrentRoot || !proto.Equal(coverageSource.Source, currentSource) {
		t.Fatalf("complete coverage changed the current source root: %+v", coverageSource)
	}
	purchaseLegacyOracleRequireReviewHistory(t, coverageSource.Reviews, reviewRecords)

	applicableWorkpaper, err := repo.CaptureBAS(ctx, postingOrg, period, balance, f.now)
	if err != nil || applicableWorkpaper == nil {
		t.Fatalf("capture applicable legacy workpaper: snapshot=%v err=%v", applicableWorkpaper, err)
	}
	for _, record := range reviewRecords {
		purchaseLegacyOracleRequireWorkpaperReview(t, applicableWorkpaper, record,
			tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR, 0)
	}
	if applicableWorkpaper.LegacyCandidateGST == nil || applicableWorkpaper.LegacyCandidateGST.MinorUnits != 0 {
		t.Fatalf("consistent unpaid history added legacy candidate GST: %+v", applicableWorkpaper.LegacyCandidateGST)
	}
	purchaseLegacyOracleRequireReadOnly(t, tx, initialReadBaseline, "applicable legacy readers")

	// The same physical source retains both immutable reviews when the selected
	// period and balance move outside their scope. Full-reader classification
	// changes while the writer-created scalar census stays identical.
	nextPeriod := &tammyv1.PurchaseClaimPeriod{
		PeriodStart: &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 1},
		PeriodEnd:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	}
	staleReadBaseline := purchaseLegacyOracleReadBaseline(t, tx, "stale legacy readers")
	staleKeys := purchaseLegacyOracleKeys(t, f)
	staleSummary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || staleSummary == nil {
		t.Fatalf("summarize source roots after period and balance change: %#v %v", staleSummary, err)
	}
	if !reflect.DeepEqual(beforeKeys, staleKeys) || !reflect.DeepEqual(beforeSummary, staleSummary) {
		t.Fatalf("period and balance selection changed the physical scalar census: keys equal=%t summaries equal=%t", reflect.DeepEqual(beforeKeys, staleKeys), reflect.DeepEqual(beforeSummary, staleSummary))
	}
	staleValues, err := repo.LegacyCandidates(ctx, postingOrg, nextPeriod, nextPeriod.PeriodEnd)
	if err != nil {
		t.Fatal(err)
	}
	stale := legacyCandidate(t, staleValues, sourceID)
	if stale.Excluded || stale.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY ||
		!proto.Equal(stale.Source, currentSource) {
		t.Fatalf("stale review removed or replaced its physical root: %+v", stale)
	}
	purchaseLegacyOracleRequireReviewHistory(t, stale.Reviews, reviewRecords)
	staleCoverage := captureCoverage(t, repo, ctx)
	staleCoverageSource := coverageLegacyDocument(t, staleCoverage, sourceID)
	if !staleCoverageSource.CurrentRoot || !proto.Equal(staleCoverageSource.Source, currentSource) {
		t.Fatalf("scope-only change lost the current root or source snapshot: %+v", staleCoverageSource)
	}
	purchaseLegacyOracleRequireReviewHistory(t, staleCoverageSource.Reviews, reviewRecords)
	staleWorkpaper, err := repo.CaptureBAS(ctx, postingOrg, nextPeriod, nextPeriod.PeriodEnd, f.now)
	if err != nil || staleWorkpaper == nil {
		t.Fatalf("capture stale legacy workpaper: snapshot=%v err=%v", staleWorkpaper, err)
	}
	for _, record := range reviewRecords {
		purchaseLegacyOracleRequireWorkpaperReview(t, staleWorkpaper, record,
			tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY, sourceGST)
	}
	purchaseLegacyOracleRequireReadOnly(t, tx, staleReadBaseline, "stale legacy readers")

	// A later-discovered cash payment contradicts both unpaid reviews. The
	// writer adds a receipt key while preserving the document root and both
	// review records; only the full-reader decision classification changes.
	payment := seedLegacyCashPayment(t, f, sourceID)
	postPaymentReadBaseline := purchaseLegacyOracleReadBaseline(t, tx, "post-payment legacy readers")
	afterPaymentKeys := purchaseLegacyOracleKeys(t, f)
	afterSummary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || afterSummary == nil {
		t.Fatalf("summarize source after payment: %#v %v", afterSummary, err)
	}
	afterDocumentKey := purchaseBASScalarFindKey(t, afterPaymentKeys, purchases.BASScalarDocument, sourceID)
	if !afterDocumentKey.IsRoot || afterDocumentKey.ID != beforeDocumentKey.ID ||
		afterDocumentKey.ObjectVersion == nil || beforeDocumentKey.ObjectVersion == nil ||
		*afterDocumentKey.ObjectVersion != *beforeDocumentKey.ObjectVersion ||
		!bytes.Equal(afterDocumentKey.SourceHash, beforeDocumentKey.SourceHash) ||
		afterSummary.RootCount != beforeSummary.RootCount || afterSummary.ReceiptCount != beforeSummary.ReceiptCount+1 {
		t.Fatalf("payment changed the source root identity or escaped the separate receipt census: before=%#v after=%#v summaries=%#v/%#v", beforeDocumentKey, afterDocumentKey, beforeSummary, afterSummary)
	}
	for _, record := range reviewRecords {
		purchaseLegacyOracleRequireReviewKey(t, afterPaymentKeys, record)
	}
	purchaseLegacyOracleRequirePaymentReceiptKey(t, afterPaymentKeys, payment, sourceID)

	contradictoryValues, err := repo.LegacyCandidates(ctx, postingOrg, period, balance)
	if err != nil {
		t.Fatal(err)
	}
	contradictory := legacyCandidate(t, contradictoryValues, sourceID)
	if contradictory.Excluded || contradictory.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY ||
		!proto.Equal(contradictory.Source, currentSource) {
		t.Fatalf("new payment did not remain a body-level contradiction on the same source: %+v", contradictory)
	}
	purchaseLegacyOracleRequireReviewHistory(t, contradictory.Reviews, reviewRecords)
	for _, record := range reviewRecords {
		if !purchases.LegacyReviewApplies(record, contradictory.Source, period, balance) {
			t.Fatalf("new payment unexpectedly changed source/scope applicability of review %s", record.Id)
		}
	}
	coverageAfterPayment := captureCoverage(t, repo, ctx)
	sourceAfterPayment := coverageLegacyDocument(t, coverageAfterPayment, sourceID)
	if !sourceAfterPayment.CurrentRoot || !coverageHasID(sourceAfterPayment.PaymentJournalIDs, payment.Id) ||
		!proto.Equal(sourceAfterPayment.Source, currentSource) {
		t.Fatalf("complete coverage lost the current root or newly discovered payment: %+v", sourceAfterPayment)
	}
	purchaseLegacyOracleRequireReviewHistory(t, sourceAfterPayment.Reviews, reviewRecords)
	contradictoryWorkpaper, err := repo.CaptureBAS(ctx, postingOrg, period, balance, f.now)
	if err != nil || contradictoryWorkpaper == nil {
		t.Fatalf("capture contradictory legacy workpaper: snapshot=%v err=%v", contradictoryWorkpaper, err)
	}
	for _, record := range reviewRecords {
		purchaseLegacyOracleRequireWorkpaperReview(t, contradictoryWorkpaper, record,
			tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_UNRESOLVED_LEGACY, sourceGST)
	}
	purchaseLegacyOracleRequireReadOnly(t, tx, postPaymentReadBaseline, "post-payment legacy readers")
}

func TestBASCandidateLegacyEvidenceAndPromotionOracle(t *testing.T) {
	t.Run("evidence-only link resolves without a second credit", func(t *testing.T) {
		ctx := context.Background()
		workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
		tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
		ownedDocument := proto.Clone(f.document).(*tammyv1.Document)
		postRequest := proto.Clone(f.request).(*tammyv1.PostReviewedPurchaseRequest)
		posted, err := f.repo(tx).PostReviewed(ctx, postRequest)
		if err != nil || posted == nil || posted.Posting == nil {
			t.Fatalf("post owned purchase before evidence link: posting=%v err=%v", posted, err)
		}
		journalCount := postScalar(t, tx, "SELECT count(*) FROM journals")

		f.addDocument("Additional transaction evidence", "EVIDENCE-LINK", 10000, 1000)
		evidenceDocument := proto.Clone(f.document).(*tammyv1.Document)
		request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_EVIDENCE_ONLY_LINKED_PURCHASE)
		sourceID := evidenceDocument.Id
		if request.Review.Source.GetDocument().DocumentId != sourceID {
			t.Fatal("evidence-only fixture changed the document source identity")
		}
		request.Review.LinkedPostingId = proto.String(posted.Posting.Id)
		result, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
		if err != nil || result == nil || result.Review == nil {
			t.Fatalf("retain evidence-only link: result=%v err=%v", result, err)
		}

		repo := f.repo(tx)
		readBaseline := purchaseLegacyOracleReadBaseline(t, tx, "evidence-only legacy readers")
		keys := purchaseLegacyOracleKeys(t, f)
		evidenceKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, sourceID)
		ownedKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, ownedDocument.Id)
		postingKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPosting, posted.Posting.Id)
		purchaseBASScalarRequireText(t, postingKey.DocumentID, ownedDocument.Id, "owned posting document id")
		if !evidenceKey.IsRoot || ownedKey.IsRoot || !postingKey.IsRoot || postingKey.IsReceipt {
			t.Fatalf("evidence, promoted document, or modern root identity changed: evidence=%#v owned=%#v posting=%#v", evidenceKey, ownedKey, postingKey)
		}
		purchaseLegacyOracleRequireReviewKey(t, keys, result.Review)

		values, err := repo.LegacyCandidates(ctx, postingOrg, request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
		if err != nil {
			t.Fatal(err)
		}
		candidate := legacyCandidate(t, values, sourceID)
		if !candidate.Excluded || !candidate.EvidenceOnly ||
			candidate.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR ||
			len(candidate.Reviews) != 1 || !proto.Equal(candidate.Reviews[0], result.Review) ||
			!proto.Equal(candidate.Source, legacyDocumentSource(evidenceDocument)) {
			t.Fatalf("evidence-only body decision lost its separate source identity: %+v", candidate)
		}
		links, err := repo.LegacyEvidenceForPosting(ctx, postingOrg, posted.Posting.Id,
			request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
		if err != nil || len(links) != 1 || links[0] == nil || len(links[0].Reviews) != 1 || !proto.Equal(links[0].Reviews[0], result.Review) {
			t.Fatalf("verified evidence-only link did not resolve to the owned posting: links=%v err=%v", links, err)
		}

		coverage := captureCoverage(t, repo, ctx)
		coverageEvidence := coverageLegacyDocument(t, coverage, sourceID)
		if !coverageEvidence.CurrentRoot || len(coverageEvidence.Reviews) != 1 ||
			!proto.Equal(coverageEvidence.Reviews[0], result.Review) ||
			!proto.Equal(coverageEvidence.Source, legacyDocumentSource(evidenceDocument)) {
			t.Fatalf("coverage did not retain current evidence root and immutable link: %+v", coverageEvidence)
		}
		workpaper, err := repo.CaptureBAS(ctx, postingOrg, request.Review.AffectedPeriod,
			request.Review.ReportingBalanceDate, f.now)
		if err != nil || workpaper == nil {
			t.Fatalf("capture evidence-only workpaper: snapshot=%v err=%v", workpaper, err)
		}
		row := purchaseLegacyOracleWorkpaperReview(t, workpaper, result.Review.Id)
		journalCountAfterReads := postScalar(t, tx, "SELECT count(*) FROM journals")
		if row.UnclaimedGst == nil || row.UnclaimedGst.MinorUnits != 0 ||
			row.Status != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_CLEAR ||
			journalCountAfterReads != journalCount {
			t.Fatalf("evidence-only proof created extra credit or accounting: row=%+v journals=%d/%d", row, journalCount, journalCountAfterReads)
		}
		purchaseLegacyOracleRequireReadOnly(t, tx, readBaseline, "evidence-only legacy readers")
	})

	t.Run("promoted review remains non-root lineage", func(t *testing.T) {
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
		request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
		sourceID := promotedDocument.Id
		result, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
		if err != nil || result == nil || result.Review == nil {
			t.Fatalf("retain pre-promotion review: result=%v err=%v", result, err)
		}
		f.document, f.request = promotedDocument, postRequest
		posted, err := f.repo(tx).PostReviewed(ctx, postRequest)
		if err != nil || posted == nil || posted.Posting == nil {
			t.Fatalf("promote the reviewed document: posting=%v err=%v", posted, err)
		}
		payment := seedLegacyCashPayment(t, f, sourceID)

		repo := f.repo(tx)
		readBaseline := purchaseLegacyOracleReadBaseline(t, tx, "promoted legacy readers")
		keys := purchaseLegacyOracleKeys(t, f)
		documentKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, sourceID)
		postingKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPosting, posted.Posting.Id)
		purchaseLegacyOracleRequireReviewKey(t, keys, result.Review)
		purchaseBASScalarRequireText(t, postingKey.DocumentID, sourceID, "promoted posting document id")
		if documentKey.IsRoot || !postingKey.IsRoot || postingKey.IsReceipt {
			t.Fatalf("promotion did not preserve the document as non-root lineage beside its posting root: document=%#v posting=%#v", documentKey, postingKey)
		}
		purchaseLegacyOracleRequirePaymentReceiptKey(t, keys, payment, sourceID)

		legacyValues, err := repo.LegacyCandidates(ctx, postingOrg,
			request.Review.AffectedPeriod, request.Review.ReportingBalanceDate)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range legacyValues {
			if value != nil && value.Source != nil && value.Source.GetDocument() != nil &&
				value.Source.GetDocument().DocumentId == sourceID {
				t.Fatalf("promoted document was rediscovered as an independent legacy root: %+v", value)
			}
		}
		coverage := captureCoverage(t, repo, ctx)
		lineage := coverageLegacyDocument(t, coverage, sourceID)
		currentSource := legacyDocumentSource(promotedDocument)
		if lineage.CurrentRoot || !proto.Equal(lineage.Source, currentSource) ||
			len(lineage.Reviews) != 1 || !proto.Equal(lineage.Reviews[0], result.Review) ||
			!coverageHasID(lineage.PaymentJournalIDs, payment.Id) ||
			len(coverage.Postings) != 1 || coverage.Postings[0].Posting == nil || coverage.Postings[0].Posting.Id != posted.Posting.Id {
			t.Fatalf("promoted review, current source snapshot, or new payment lineage was lost: %+v", lineage)
		}
		purchaseLegacyOracleRequireReadOnly(t, tx, readBaseline, "promoted legacy readers")
	})
}
