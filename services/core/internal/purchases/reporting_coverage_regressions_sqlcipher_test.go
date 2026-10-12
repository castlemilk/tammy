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

func TestPurchaseCoverageRetainsLowValueRuleProofAndOriginalDocuments(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	supportBytes := []byte("reviewed low-value acquisition support")
	support := newPurchaseReportingSupportDocument(t, f, "LOW-VALUE-SUPPORT", supportBytes)
	f.addDocument("Low-value supplier", "LOW-VALUE-COVERAGE", 7500, 750)
	invoice := proto.Clone(f.document).(*tammyv1.Document)
	invoiceBytes := []byte("original purchase " + invoice.Id)
	review := lowValueReview(t, f)
	review.ExceptionReview.SupportingRecords = append(review.ExceptionReview.SupportingRecords, &tammyv1.PurchaseRetainedSourceRef{
		SourceId: support.Id, SourceVersion: support.Version, SourceHash: append([]byte(nil), support.Sha256...),
	})
	f.request.Evidence = review
	repo := f.repo(tx)
	retainExceptionRulesIfSupported(t, repo)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatalf("post low-value purchase with reviewed support: %v", err)
	}

	modern := capturePostings(t, repo, ctx, postingOrg)
	modernRule := retainedRowForText(t, modern, "rule_bundles", "id", "019a4210-0000-7000-8000-000000000075")
	stateBefore := coverageStateCounts(t, tx)
	changesBefore := postScalar(t, tx, `SELECT total_changes()`)
	coverage := captureCoverage(t, repo, ctx)
	if len(coverage.Postings) != 1 || coverage.Postings[0].Posting == nil || coverage.Postings[0].Posting.DocumentId != invoice.Id || len(coverage.Postings[0].Evidences) != 1 {
		t.Fatalf("complete capture lost the modern low-value purchase or its evidence: %+v", coverage.Postings)
	}
	retainedReview := coverage.Postings[0].Evidences[0].Review
	if !proto.Equal(retainedReview, review) || retainedReview.ExceptionReview == nil ||
		retainedReview.ExceptionReview.Rule == nil || retainedReview.ExceptionReview.Rule.SourceId != "019a4210-0000-7000-8000-000000000075" ||
		retainedReview.ExceptionReview.Rule.SourceVersion != 1 ||
		!bytes.Equal(retainedReview.ExceptionReview.Rule.SourceHash, review.ExceptionReview.Rule.SourceHash) ||
		len(retainedReview.ExceptionReview.SupportingRecords) != 2 ||
		retainedReview.ExceptionReview.SupportingRecords[0].SourceId != invoice.Id ||
		retainedReview.ExceptionReview.SupportingRecords[1].SourceId != support.Id {
		t.Fatalf("complete capture changed low-value rule/support provenance: %+v", retainedReview)
	}

	fullRows := coverageRawRows(coverage)
	fullRule := retainedRowForText(t, fullRows, "rule_bundles", "id", "019a4210-0000-7000-8000-000000000075")
	if !reflect.DeepEqual(modernRule, fullRule) {
		t.Fatalf("complete capture did not retain the exact global rule row from the modern source capture:\nmodern=%+v\nfull=%+v", modernRule, fullRule)
	}
	if organisation, ok := retainedValue(fullRule, "organisation_id"); !ok || organisation.Kind != "NULL" {
		t.Fatalf("low-value rule lost its global NULL organisation provenance: %+v", organisation)
	}
	for column, want := range map[string]string{
		"bundle_type":     "AU_GST_PURCHASE_TAX_INVOICE_EXCEPTION",
		"version":         "au-gst-tax-invoice-low-value-v1",
		"semantic_sha256": "11cc08f62cb53e5e896e0af18ad2056f1bf0a572a4cc2b98c342de6ba7398157",
		"effective_from":  "2025-11-01",
	} {
		value, ok := retainedValue(fullRule, column)
		if !ok || value.Kind != "TEXT" || value.Text != want {
			t.Fatalf("retained global low-value rule %s=%+v, want %q", column, value, want)
		}
	}
	if rules, ok := retainedValue(fullRule, "rules_proto"); !ok || rules.Kind != "BLOB" || len(rules.Bytes) == 0 {
		t.Fatalf("retained global low-value rule omitted its full rule body: %+v", rules)
	}
	if effectiveTo, ok := retainedValue(fullRule, "effective_to"); !ok || effectiveTo.Kind != "NULL" {
		t.Fatalf("retained global low-value rule changed its open-ended interval: %+v", effectiveTo)
	}

	for _, document := range []*tammyv1.Document{invoice, support} {
		found := false
		for _, record := range coverage.Documents {
			if record.Document != nil && record.Document.Id == document.Id && proto.Equal(record.Document, document) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("complete capture omitted exact retained document %s", document.Id)
		}
	}
	for _, expected := range []struct {
		id       string
		original []byte
		hash     []byte
	}{{invoice.Id, invoiceBytes, invoice.Sha256}, {support.Id, supportBytes, support.Sha256}} {
		row := retainedRowForText(t, fullRows, "documents", "id", expected.id)
		original, ok := retainedValue(row, "original_bytes")
		if !ok || original.Kind != "BLOB" || !bytes.Equal(original.Bytes, expected.original) {
			t.Fatalf("complete capture changed original bytes for document %s: %+v", expected.id, original)
		}
		hash, ok := retainedValue(row, "sha256")
		if !ok || hash.Kind != "BLOB" || !bytes.Equal(hash.Bytes, expected.hash) {
			t.Fatalf("complete capture changed original hash for document %s: %+v", expected.id, hash)
		}
	}
	assertEveryCapturedTableHasCompleteSchema(t, tx, fullRows)
	if !reflect.DeepEqual(stateBefore, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != changesBefore {
		t.Fatal("complete low-value purchase capture changed retained state")
	}

	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	partial, budgetErr := f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, coverage.PayloadBytes-1)
	assertPurchaseCoverageBound(t, budgetErr, "capture bytes", coverage.PayloadBytes-1)
	if partial != nil || probe.bodyReads != 0 {
		t.Fatalf("one-byte-short low-value capture returned partial=%v or hydrated %d proof bodies before the shared budget bound", partial != nil, probe.bodyReads)
	}
	exact, err := f.repo(tx).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, coverage.PayloadBytes)
	if err != nil || exact == nil || exact.PayloadBytes != coverage.PayloadBytes ||
		!bytes.Equal(exact.Fingerprint, coverage.Fingerprint) || !reflect.DeepEqual(exact.Rows, coverage.Rows) {
		t.Fatalf("exact low-value capture payload budget was not stable: exact=%v err=%v", exact, err)
	}
	if !reflect.DeepEqual(stateBefore, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != changesBefore {
		t.Fatal("low-value byte-bound capture changed retained state")
	}
}

func TestPurchaseCoverageFindsManualCashReceiptFromExpenseTaxFactSource(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	f.addDocumentReview("Needs-review receipt supplier", "NEEDS-REVIEW-RECEIPT", 10000, 1000, false)
	ownedDocument := proto.Clone(f.document).(*tammyv1.Document)
	documentOriginal := []byte("original purchase " + ownedDocument.Id)
	unrelatedJournalSourceID, _ := f.id()
	journal := seedCoverageCashReceiptWithTaxFactSource(t, f, ownedDocument.Id, unrelatedJournalSourceID)
	if journal.Lines[0].Debit.MinorUnits != 11000 || journal.Lines[1].Credit.MinorUnits != 11000 ||
		journal.Source != tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL {
		t.Fatalf("fixture is not a balanced MANUAL cash receipt: %+v", journal)
	}
	if unrelatedJournalSourceID == ownedDocument.Id {
		t.Fatalf("fixture journal source should be a distinct canonical raw ID: %q", unrelatedJournalSourceID)
	}

	stateBefore := coverageStateCounts(t, tx)
	changesBefore := postScalar(t, tx, `SELECT total_changes()`)
	coverage := captureCoverage(t, f.repo(tx), ctx)
	if coverage.ReceiptCount != 1 {
		t.Fatalf("receipt count=%d, want one source linked by an expense-line tax fact", coverage.ReceiptCount)
	}
	root := coverageLegacyDocument(t, coverage, ownedDocument.Id)
	if !root.CurrentRoot || root.Source != nil || root.Document == nil || root.Document.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW || len(root.Reviews) != 0 {
		t.Fatalf("needs-review source was not retained without forged review provenance: %+v", root)
	}
	if len(root.PaymentJournalIDs) != 1 || root.PaymentJournalIDs[0] != journal.Id || !coverageHasID(root.PriorJournalIDs, journal.Id) {
		t.Fatalf("tax-fact-linked manual receipt edges=%v prior=%v, want the exact journal on both source paths", root.PaymentJournalIDs, root.PriorJournalIDs)
	}

	var captured *accounting.JournalReportingRecord
	proofCount := 0
	for _, proof := range coverage.Journals {
		if proof != nil && proof.Journal != nil && proof.Journal.Id == journal.Id {
			captured = proof
			proofCount++
		}
	}
	repository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := repository.GetForReporting(ctx, postingOrg, journal.Id)
	if err != nil || expected == nil {
		t.Fatalf("read complete manual journal proof through the public reporting API: proof=%v err=%v", expected, err)
	}
	if proofCount != 1 || captured == nil || !proto.Equal(captured.Journal, journal) ||
		captured.SourceType != "MANUAL" || captured.SourceKind != 0 || captured.SourceID != unrelatedJournalSourceID || captured.SourceRevision != 1 || captured.State != "POSTED" ||
		!bytes.Equal(captured.ContentHash, expected.ContentHash) || captured.PayloadBytes != expected.PayloadBytes {
		t.Fatalf("complete capture changed the exact unrelated-source MANUAL journal proof: count=%d captured=%+v expected=%+v", proofCount, captured, expected)
	}
	if len(captured.Journal.Lines) != 2 || !proto.Equal(captured.Journal.Lines[0], journal.Lines[0]) || !proto.Equal(captured.Journal.Lines[1], journal.Lines[1]) {
		t.Fatalf("accounting-owned journal proof changed ordered lines, accounts, amounts, or tax references: %+v", captured.Journal.Lines)
	}

	rows := coverageRawRows(coverage)
	fact := retainedRowForText(t, rows, "tax_facts", "journal_line_id", journal.Lines[0].Id)
	for column, want := range map[string]string{"organisation_id": postingOrg, "source_type": "document", "source_id": ownedDocument.Id} {
		value, ok := retainedValue(fact, column)
		if !ok || value.Kind != "TEXT" || value.Text != want {
			t.Fatalf("expense-line fact source %s=%+v, want %q", column, value, want)
		}
	}
	if revision, ok := retainedValue(fact, "source_revision"); !ok || revision.Kind != "INTEGER" || revision.Integer != int64(ownedDocument.Version) {
		t.Fatalf("expense-line fact lost exact needs-review document revision: %+v", revision)
	}
	if hash, ok := retainedValue(fact, "source_content_hash"); !ok || hash.Kind != "BLOB" || !bytes.Equal(hash.Bytes, ownedDocument.Sha256) {
		t.Fatalf("expense-line fact lost exact needs-review document hash: %+v", hash)
	}
	for _, row := range rows.Rows {
		if row.Table != "tax_facts" {
			continue
		}
		line, ok := retainedValue(row, "journal_line_id")
		if ok && line.Kind == "TEXT" && line.Text == journal.Lines[1].Id {
			t.Fatalf("cash-credit line unexpectedly carries a tax fact: %+v", row)
		}
	}

	for _, expectedFlow := range []struct {
		line     string
		category string
		amount   int64
	}{{journal.Lines[0].Id, "NONCASH", 11000}, {journal.Lines[1].Id, "OPERATING", -11000}} {
		count := 0
		for _, row := range rows.Rows {
			if row.Table != "cash_flow_facts" {
				continue
			}
			line, lineOK := retainedValue(row, "journal_line_id")
			owner, ownerOK := retainedValue(row, "organisation_id")
			category, categoryOK := retainedValue(row, "category")
			amount, amountOK := retainedValue(row, "amount_minor")
			if lineOK && line.Kind == "TEXT" && line.Text == expectedFlow.line && ownerOK && owner.Kind == "TEXT" && owner.Text == postingOrg &&
				categoryOK && category.Kind == "TEXT" && category.Text == expectedFlow.category && amountOK && amount.Kind == "INTEGER" && amount.Integer == expectedFlow.amount {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("capture retained cash-flow proof for line %s %q %d %d times", expectedFlow.line, expectedFlow.category, expectedFlow.amount, count)
		}
	}
	for index, line := range captured.Journal.Lines {
		account := retainedRowForText(t, rows, "accounts", "id", line.AccountId)
		classification, classificationOK := retainedValue(account, "report_classification")
		wantClassification := "custom.account"
		if index == 1 {
			wantClassification = "balance_sheet.cash"
		}
		if !classificationOK || classification.Kind != "TEXT" || classification.Text != wantClassification {
			t.Fatalf("complete capture changed account %s reporting classification: %+v", line.AccountId, classification)
		}
		if index == 1 {
			cashFlowClass, ok := retainedValue(account, "cash_flow_classification")
			if !ok || cashFlowClass.Kind != "TEXT" || cashFlowClass.Text != "operating" {
				t.Fatalf("complete capture changed cash account flow classification: %+v", cashFlowClass)
			}
		}
	}
	documentRow := retainedRowForText(t, rows, "documents", "id", ownedDocument.Id)
	if original, ok := retainedValue(documentRow, "original_bytes"); !ok || original.Kind != "BLOB" || !bytes.Equal(original.Bytes, documentOriginal) {
		t.Fatalf("complete capture changed the needs-review original document: %+v", original)
	}
	assertEveryCapturedTableHasCompleteSchema(t, tx, rows)
	if !reflect.DeepEqual(stateBefore, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != changesBefore {
		t.Fatal("complete manual receipt capture changed retained state")
	}

	probe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	partial, budgetErr := f.repo(probe).CaptureCoverageForBASWithPayloadBudget(ctx, postingOrg, coverage.PayloadBytes-1)
	assertPurchaseCoverageBound(t, budgetErr, "capture bytes", coverage.PayloadBytes-1)
	if partial != nil || probe.bodyReads != 0 {
		t.Fatalf("one-byte-short manual receipt capture returned partial=%v or hydrated %d proof bodies before the shared budget bound", partial != nil, probe.bodyReads)
	}
	if !reflect.DeepEqual(stateBefore, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != changesBefore {
		t.Fatal("manual receipt byte-bound capture changed retained state")
	}
}

func TestPurchaseCoverageUsesCurrentReviewedDocumentIdentityWithoutReviewHistory(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	currentDocument := proto.Clone(f.document).(*tammyv1.Document)
	wantCurrentSource := legacyDocumentSource(currentDocument)
	repo := f.repo(tx)
	before := captureCoverage(t, repo, ctx)
	current := coverageLegacyDocument(t, before, currentDocument.Id)
	if !current.CurrentRoot || len(current.Reviews) != 0 || !proto.Equal(current.Source, wantCurrentSource) {
		t.Fatalf("reviewed document without legacy history lost its exact current identity: %+v", current)
	}
	documentRef := wantCurrentSource.GetDocument()
	if documentRef == nil || documentRef.DocumentId != currentDocument.Id || documentRef.ReviewedVersion != currentDocument.Version ||
		!bytes.Equal(documentRef.OriginalContentHash, currentDocument.Sha256) || len(documentRef.ReviewedSnapshotHash) != 32 {
		t.Fatalf("current source ref omitted exact version or distinct original/reviewed hashes: %+v", documentRef)
	}

	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	response, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil || response == nil || response.Review == nil {
		t.Fatalf("retain a real legacy decision for the current document identity: response=%v err=%v", response, err)
	}
	stateBeforeCapture := coverageStateCounts(t, tx)
	changesBeforeCapture := postScalar(t, tx, `SELECT total_changes()`)
	after := captureCoverage(t, f.repo(tx), ctx)
	withHistory := coverageLegacyDocument(t, after, currentDocument.Id)
	if !withHistory.CurrentRoot || !proto.Equal(withHistory.Source, wantCurrentSource) || len(withHistory.Reviews) != 1 ||
		!proto.Equal(withHistory.Reviews[0], response.Review) || !proto.Equal(withHistory.Reviews[0].Review.Source, wantCurrentSource) {
		t.Fatalf("legacy history replaced the current reviewed-document identity or changed its immutable decision: %+v", withHistory)
	}
	if bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("adding a real immutable legacy decision did not change the retained source fingerprint")
	}
	if !reflect.DeepEqual(stateBeforeCapture, coverageStateCounts(t, tx)) || postScalar(t, tx, `SELECT total_changes()`) != changesBeforeCapture {
		t.Fatal("capture of current and historical document identities changed retained state")
	}
}
