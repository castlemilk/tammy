//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/accounting"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

type purchaseBASScalarSQLReader interface {
	app.CommandSQLExecutor
}

func purchaseBASScalarText(t *testing.T, executor purchaseBASScalarSQLReader, query string, args ...any) string {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var value string
	if !rows.Next() || rows.Scan(&value) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("invalid text scalar: %s", query)
	}
	return value
}

func purchaseBASScalarFindKey(t *testing.T, keys []purchases.BASScalarKey, kind purchases.BASScalarKeyKind, id string) purchases.BASScalarKey {
	t.Helper()
	var found *purchases.BASScalarKey
	for index := range keys {
		if keys[index].Kind == kind && keys[index].ID == id {
			if found != nil {
				t.Fatalf("duplicate BAS scalar key for kind=%d id=%s", kind, id)
			}
			found = &keys[index]
		}
	}
	if found == nil {
		t.Fatalf("missing BAS scalar key for kind=%d id=%s", kind, id)
	}
	return *found
}

func purchaseBASScalarRequireText(t *testing.T, got *string, want string, field string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s=%v, want %q", field, got, want)
	}
}

func purchaseBASScalarRequireUint(t *testing.T, got *uint64, want uint64, field string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s=%v, want %d", field, got, want)
	}
}

func purchaseBASScalarIndependentBytes(keys []purchases.BASScalarKey) int64 {
	var total int64
	addText := func(value string) { total += int64(len(value)) }
	addOptionalText := func(value *string) {
		if value != nil {
			addText(*value)
		}
	}
	total += int64(len(keys)) * 512
	for _, key := range keys {
		addText(key.OrganisationID)
		addText(key.ID)
		for _, value := range []*string{
			key.PostingID, key.DocumentID, key.OpeningPayableID, key.ConversionID, key.PaymentID,
			key.AccountID, key.JournalID, key.EvidenceID, key.DeferralID, key.OperationKey, key.RelatedID,
			key.ScopeKind, key.ScopeID, key.SourceKind, key.SourceType, key.SourceID, key.DueDateSourceID,
			key.ClaimEvidenceSourceID, key.ActualPaymentDate,
		} {
			addOptionalText(value)
		}
		total += int64(len(key.SourceHash) + len(key.RecordHash) + len(key.ReviewedSnapshotHash) +
			len(key.DueDateSourceHash) + len(key.ClaimEvidenceSourceHash))
	}
	return total
}

func purchaseBASScalarSQLRootCount(t *testing.T, executor purchaseBASScalarSQLReader) int64 {
	t.Helper()
	return postScalar(t, executor, `SELECT count(*) FROM (
		SELECT id FROM purchase_postings_v1 WHERE organisation_id=?
		UNION ALL
		SELECT d.id FROM documents d WHERE d.organisation_id=? AND NOT EXISTS(
			SELECT 1 FROM purchase_postings_v1 p WHERE p.organisation_id=d.organisation_id AND p.document_id=d.id
		)
		UNION ALL
		SELECT p.id FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id
		WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')
	)`, postingOrg, postingOrg, postingOrg)
}

// These are scalar-only table counts over the valid writer-created fixture.
// The replacement DRAFT conversion is intentional context; it is retained as
// a conversion key but is not a current purchase root or receipt.
func purchaseBASScalarSQLFamilyCounts(t *testing.T, executor purchaseBASScalarSQLReader) [purchases.BASScalarFamilyCount]int64 {
	t.Helper()
	queries := []string{
		`SELECT count(*) FROM purchase_postings_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_posting_allocations_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_payments_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_payment_allocations_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_tax_evidence_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_accounting_events_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_gst_deferrals_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_gst_deferral_portions_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_gst_attributions_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM documents WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_legacy_reviews_v1 WHERE organisation_id=?`,
		`SELECT count(*) FROM purchase_opening_payables p JOIN opening_conversions c ON c.id=p.conversion_id WHERE c.organisation_id=? AND c.state IN ('POSTED','REPLACED')`,
		`SELECT count(*) FROM opening_conversions WHERE organisation_id=?`,
		`SELECT count(*) FROM opening_items i JOIN opening_conversions c ON c.id=i.conversion_id WHERE c.organisation_id=?`,
		`SELECT count(*) FROM journals WHERE organisation_id=? AND source_type='MANUAL'`,
	}
	var counts [purchases.BASScalarFamilyCount]int64
	for index, query := range queries {
		counts[index] = postScalar(t, executor, query, postingOrg)
	}
	return counts
}

func TestPurchaseBASScalarKeysRetainExactModernPhysicalFamilies(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	fixture, commandRepository, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferral, err := deferCommand(t, commandRepository, deferRequest(t, fixture, state, portion))
	if err != nil {
		t.Fatalf("retain source deferral: %v", err)
	}
	claim := claimRequest(t, fixture, deferral.Accounting, portion)
	claim.DeferralId = &deferral.Deferral.Id
	attributed, err := attributeCommand(t, commandRepository, claim)
	if err != nil {
		t.Fatalf("attribute deferred source portion: %v", err)
	}

	wantRoots := purchaseBASScalarSQLRootCount(t, tx)
	wantDocuments := postScalar(t, tx, `SELECT count(*) FROM documents WHERE organisation_id=?`, postingOrg)
	wantFamilies := [purchases.BASScalarFamilyCount]int64{1, 1, 1, 1, 2, 5, 1, 1, 1, wantDocuments}
	if wantRoots != 2 || wantDocuments < 2 {
		t.Fatalf("actual modern fixture roots/documents=%d/%d; want posted purchase plus separate due-date support document", wantRoots, wantDocuments)
	}

	repository := fixture.repo(tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil || summary.RootCount != wantRoots || summary.ReceiptCount != 1 ||
		summary.ScalarKeyCount <= 0 || summary.ScalarBytes <= 0 || summary.FamilyCounts != wantFamilies {
		t.Fatalf("modern scalar summary=%#v err=%v; want root/receipt/families=%d/1/%v", summary, err, wantRoots, wantFamilies)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount || purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
		t.Fatalf("modern scalar key count/independent byte total=%d/%d err=%v; summary=%#v",
			len(keys), purchaseBASScalarIndependentBytes(keys), err, summary)
	}

	postingID := portion.GetPayment().PostingId
	paymentID := portion.GetPayment().PaymentId
	posting := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPosting, postingID)
	if !posting.IsRoot {
		t.Fatal("posted purchase key lost its current-root flag")
	}
	postingDocumentID := purchaseBASScalarText(t, tx, `SELECT document_id FROM purchase_postings_v1 WHERE id=?`, postingID)
	purchaseBASScalarRequireText(t, posting.DocumentID, postingDocumentID, "posting document id")

	allocationRows, err := tx.QueryContext(ctx, `SELECT sequence,account_id,account_version FROM purchase_posting_allocations_v1 WHERE posting_id=? ORDER BY sequence`, postingID)
	if err != nil {
		t.Fatal(err)
	}
	postingAllocations := 0
	for allocationRows.Next() {
		var sequence, accountVersion int64
		var accountID string
		if err := allocationRows.Scan(&sequence, &accountID, &accountVersion); err != nil {
			t.Fatal(err)
		}
		key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPostingAllocation, postingID)
		purchaseBASScalarRequireUint(t, key.Sequence, uint64(sequence), "posting allocation primary-key sequence")
		purchaseBASScalarRequireText(t, key.AccountID, accountID, "posting allocation account id")
		purchaseBASScalarRequireUint(t, key.AccountVersion, uint64(accountVersion), "posting allocation account version")
		postingAllocations++
	}
	if err := allocationRows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = allocationRows.Close()
	if postingAllocations != 1 {
		t.Fatalf("SQL posting allocation count=%d, want one actual writer allocation", postingAllocations)
	}

	payment := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPayment, paymentID)
	purchaseBASScalarRequireText(t, payment.PostingID, postingID, "payment posting id")
	paymentAllocationRows, err := tx.QueryContext(ctx, `SELECT sequence,posting_allocation_sequence FROM purchase_payment_allocations_v1 WHERE payment_id=? ORDER BY sequence`, paymentID)
	if err != nil {
		t.Fatal(err)
	}
	paymentAllocations := 0
	for paymentAllocationRows.Next() {
		var sequence, postingSequence int64
		if err := paymentAllocationRows.Scan(&sequence, &postingSequence); err != nil {
			t.Fatal(err)
		}
		key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarPaymentAllocation, paymentID)
		purchaseBASScalarRequireUint(t, key.Sequence, uint64(sequence), "payment allocation primary-key sequence")
		purchaseBASScalarRequireUint(t, key.PostingAllocationSequence, uint64(postingSequence), "payment allocation posting sequence")
		purchaseBASScalarRequireText(t, key.PostingID, postingID, "payment allocation posting id")
		paymentAllocations++
	}
	if err := paymentAllocationRows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = paymentAllocationRows.Close()
	if paymentAllocations != 1 {
		t.Fatalf("SQL payment allocation count=%d, want one actual writer allocation", paymentAllocations)
	}

	firstEvidenceID := purchaseBASScalarText(t, tx, `SELECT id FROM purchase_tax_evidence_v1 WHERE posting_id=? ORDER BY accounting_version LIMIT 1`, postingID)
	firstEvidence := purchaseBASScalarFindKey(t, keys, purchases.BASScalarEvidence, firstEvidenceID)
	if firstEvidence.SourceHash != nil || firstEvidence.SourceVersion != nil || len(firstEvidence.RecordHash) != 32 {
		t.Fatalf("nullable evidence source tuple was synthesized or its independent record hash lost: %#v", firstEvidence)
	}
	firstEvidenceRecordHashRows, err := tx.QueryContext(ctx, `SELECT record_hash FROM purchase_tax_evidence_v1 WHERE id=?`, firstEvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	var firstEvidenceRecordHash []byte
	if !firstEvidenceRecordHashRows.Next() || firstEvidenceRecordHashRows.Scan(&firstEvidenceRecordHash) != nil {
		t.Fatal("first evidence SQL record hash missing")
	}
	_ = firstEvidenceRecordHashRows.Close()
	if !bytes.Equal(firstEvidence.RecordHash, firstEvidenceRecordHash) {
		t.Fatal("evidence record hash changed from its exact independent SQL column")
	}

	deferralPortion := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDeferralPortion, deferral.Deferral.Id)
	purchaseBASScalarRequireUint(t, deferralPortion.Sequence, 1, "deferral portion primary-key sequence")
	purchaseBASScalarRequireText(t, deferralPortion.DeferralID, deferral.Deferral.Id, "deferral portion deferral id")
	purchaseBASScalarRequireText(t, deferralPortion.PaymentID, paymentID, "deferral portion payment id")
	purchaseBASScalarRequireUint(t, deferralPortion.AllocationSequence, 1, "deferral portion allocation sequence")
	purchaseBASScalarRequireText(t, deferralPortion.DueDateSourceID, portion.DueDateEvidence.SourceId, "independent due-date source id")
	purchaseBASScalarRequireUint(t, deferralPortion.DueDateSourceVersion, portion.DueDateEvidence.SourceVersion, "independent due-date source version")
	if !bytes.Equal(deferralPortion.DueDateSourceHash, portion.DueDateEvidence.SourceHash) ||
		bytes.Equal(deferralPortion.DueDateSourceHash, deferralPortion.SourceHash) {
		t.Fatal("due-date source hash was lost, merged into the allocation source, or synthesized")
	}

	eventKeys := make(map[string]purchases.BASScalarKey)
	for _, key := range keys {
		if key.Kind == purchases.BASScalarEvent {
			eventKeys[key.ID] = key
		}
	}
	eventRows, err := tx.QueryContext(ctx, `SELECT operation_key,scope_kind,scope_id,accounting_version FROM purchase_accounting_events_v1 WHERE organisation_id=?`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	eventCount := 0
	for eventRows.Next() {
		var operation, scopeKind, scopeID string
		var version int64
		if err := eventRows.Scan(&operation, &scopeKind, &scopeID, &version); err != nil {
			t.Fatal(err)
		}
		key, ok := eventKeys[operation]
		if !ok || key.OperationKey == nil || *key.OperationKey != operation || key.ScopeKind == nil || *key.ScopeKind != scopeKind ||
			key.ScopeID == nil || *key.ScopeID != scopeID || key.AccountingVersion == nil || *key.AccountingVersion != uint64(version) {
			t.Fatalf("event scalar key lost exact operation/scope/version tuple for %s: %#v", operation, key)
		}
		eventCount++
	}
	if err := eventRows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = eventRows.Close()
	if eventCount != 5 || len(eventKeys) != eventCount {
		t.Fatalf("exact SQL event tuples=%d and projected tuples=%d, want five", eventCount, len(eventKeys))
	}

	postedDocumentKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, postingDocumentID)
	dueDateDocumentKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, portion.DueDateEvidence.SourceId)
	if postedDocumentKey.IsRoot || !dueDateDocumentKey.IsRoot {
		t.Fatalf("posted/support document root flags=%t/%t, want false/true", postedDocumentKey.IsRoot, dueDateDocumentKey.IsRoot)
	}
	attribution := purchaseBASScalarFindKey(t, keys, purchases.BASScalarAttribution, attributed.Attributions[0].Id)
	purchaseBASScalarRequireText(t, attribution.PostingID, postingID, "attribution posting id")
	purchaseBASScalarRequireText(t, attribution.DocumentID, postingDocumentID, "attribution document id")
	if len(attribution.RecordHash) != 32 || attribution.SourceHash != nil {
		t.Fatalf("attribution record hash length=%d, source hash present=%t; record proof must remain separate from source provenance", len(attribution.RecordHash), attribution.SourceHash != nil)
	}
}

func TestPurchaseBASScalarKeysRetainLegacyOpeningsAndUniqueManualReceipt(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	fixture := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	promotedDocument := proto.Clone(fixture.document).(*tammyv1.Document)
	postRequest := proto.Clone(fixture.request).(*tammyv1.PostReviewedPurchaseRequest)
	firstReviewRequest := legacyRequest(t, fixture, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_CONFIRMED_UNPAID_THROUGH_BALANCE)
	firstReview, err := legacyRepo(t, fixture, tx).ResolveLegacy(ctx, firstReviewRequest)
	if err != nil {
		t.Fatalf("retain first pre-promotion document review: %v", err)
	}
	secondReviewRequest := electNextLegacy(t, fixture, firstReviewRequest)
	secondReviewRequest.Review.Reason = "Re-reviewed the same retained source with complete prior history"
	secondReview, err := legacyRepo(t, fixture, tx).ResolveLegacy(ctx, secondReviewRequest)
	if err != nil {
		t.Fatalf("retain second immutable document review: %v", err)
	}
	fixture.document, fixture.request = promotedDocument, postRequest
	if _, err := fixture.repo(tx).PostReviewed(ctx, postRequest); err != nil {
		t.Fatalf("promote reviewed document through the real posting writer: %v", err)
	}

	noncashPrior := fixture.seedPriorJournal(promotedDocument.Id, tammyv1.JournalSource_JOURNAL_SOURCE_MANUAL, postingExpense, postingExpense)
	liveReceipt := seedCoverageCashReceiptWithTaxFactSource(t, fixture, promotedDocument.Id, promotedDocument.Id)
	historicalReceipt := seedCoverageLegacyCashPayment(t, fixture, promotedDocument.Id, "1012")
	journalRepository, err := accounting.NewJournalRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	reversalID, _ := fixture.id()
	reversalLineIDs := make([]string, len(historicalReceipt.Lines))
	for index := range reversalLineIDs {
		reversalLineIDs[index], _ = fixture.id()
	}
	if _, reversal, err := journalRepository.Reverse(ctx, historicalReceipt.Id, historicalReceipt.Version, postDate(11),
		"Reverse the historical supported settlement", reversalID, reversalLineIDs, fixture.now); err != nil || reversal == nil {
		t.Fatalf("retain actual payment reversal chain: reversal=%v err=%v", reversal, err)
	}

	fixture.date = &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 1}
	fixture.addDocumentReview("Opening evidence supplier", "OPENING-EVIDENCE", 10000, 1000, true)
	openingDocument := proto.Clone(fixture.document).(*tammyv1.Document)
	replacedOpeningID, replacedConversionID := coverageOpeningWithDocument(t, fixture, openingDocument, true)
	coverageReviewOpening(t, fixture, replacedOpeningID)
	postedOpeningID := legacyOpeningFixture(t, fixture)
	coverageReviewOpening(t, fixture, postedOpeningID)
	replacementConversionID := purchaseBASScalarText(t, tx, `SELECT replaced_by_id FROM opening_conversions WHERE id=?`, replacedConversionID)
	postedConversionID := purchaseBASScalarText(t, tx, `SELECT conversion_id FROM purchase_opening_payables WHERE id=?`, postedOpeningID)

	repository := fixture.repo(tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatalf("summarize legacy/opening scalar rows: %#v %v", summary, err)
	}
	wantFamilies := purchaseBASScalarSQLFamilyCounts(t, tx)
	wantRoots := purchaseBASScalarSQLRootCount(t, tx)
	if summary.RootCount != wantRoots || summary.ReceiptCount != 1 || summary.FamilyCounts != wantFamilies {
		t.Fatalf("legacy/opening scalar roots/receipts/families=%d/%d/%v; want %d/1/%v",
			summary.RootCount, summary.ReceiptCount, summary.FamilyCounts, wantRoots, wantFamilies)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount || purchaseBASScalarIndependentBytes(keys) != summary.ScalarBytes {
		t.Fatalf("legacy/opening scalar keys=%d independent bytes=%d err=%v summary=%#v",
			len(keys), purchaseBASScalarIndependentBytes(keys), err, summary)
	}
	coverage := captureCoverage(t, repository, ctx)
	if int64(coverage.ReceiptCount) != summary.ReceiptCount {
		t.Fatalf("independent real-writer receipt count=%d, scalar count=%d", coverage.ReceiptCount, summary.ReceiptCount)
	}

	postedDocumentKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, promotedDocument.Id)
	supportDocumentID := firstReview.Review.Review.SupportingEvidence[0].SourceId
	supportDocumentKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, supportDocumentID)
	if postedDocumentKey.IsRoot || !supportDocumentKey.IsRoot {
		t.Fatalf("promoted/support document root flags=%t/%t, want false/true", postedDocumentKey.IsRoot, supportDocumentKey.IsRoot)
	}

	firstReviewKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarLegacyReview, firstReview.Review.Id)
	secondReviewKey := purchaseBASScalarFindKey(t, keys, purchases.BASScalarLegacyReview, secondReview.Review.Id)
	purchaseBASScalarRequireText(t, firstReviewKey.DocumentID, promotedDocument.Id, "legacy review document id")
	purchaseBASScalarRequireUint(t, firstReviewKey.SourceVersion, promotedDocument.Version, "legacy review source version")
	if !bytes.Equal(firstReviewKey.SourceHash, promotedDocument.Sha256) ||
		!bytes.Equal(firstReviewKey.ReviewedSnapshotHash, firstReview.Review.Review.Source.GetDocument().ReviewedSnapshotHash) ||
		!bytes.Equal(firstReviewKey.RecordHash, firstReview.Review.SourceHash) ||
		bytes.Equal(firstReviewKey.SourceHash, firstReviewKey.RecordHash) ||
		secondReviewKey.SourceVersion == nil || *secondReviewKey.SourceVersion != promotedDocument.Version {
		t.Fatalf("review source/hash/snapshot/result-proof tuple was merged or changed: first=%#v second=%#v", firstReviewKey, secondReviewKey)
	}

	replacedPayable := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningPayable, replacedOpeningID)
	postedPayable := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningPayable, postedOpeningID)
	if !replacedPayable.IsRoot || !postedPayable.IsRoot {
		t.Fatal("POSTED/REPLACED opening payable keys lost their current-root flags")
	}
	purchaseBASScalarRequireText(t, replacedPayable.ConversionID, replacedConversionID, "replaced payable conversion id")
	purchaseBASScalarRequireText(t, postedPayable.ConversionID, postedConversionID, "posted payable conversion id")
	for _, value := range []struct {
		id, wantHash string
		version      uint64
	}{
		{replacedConversionID, strings.Repeat("a", 64), 2},
		{replacementConversionID, strings.Repeat("b", 64), 1},
		{postedConversionID, strings.Repeat("a", 64), 1},
	} {
		conversion := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningConversion, value.id)
		purchaseBASScalarRequireUint(t, conversion.ObjectVersion, value.version, "opening conversion object version")
		if !bytes.Equal(conversion.SourceHash, []byte(value.wantHash)) {
			t.Fatalf("opening conversion %s source hash=%q len=%d; want raw %d-byte ASCII hex text",
				value.id, conversion.SourceHash, len(conversion.SourceHash), len(value.wantHash))
		}
		if value.id == replacementConversionID && conversion.IsRoot {
			t.Fatal("replacement DRAFT conversion context was promoted to a root")
		}
	}
	for _, value := range []struct{ itemID, conversionID string }{
		{replacedOpeningID, replacedConversionID}, {postedOpeningID, postedConversionID},
	} {
		item := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningItem, value.itemID)
		purchaseBASScalarRequireText(t, item.ConversionID, value.conversionID, "opening item conversion id")
		payable := purchaseBASScalarFindKey(t, keys, purchases.BASScalarOpeningPayable, value.itemID)
		if payable.ID != value.itemID {
			t.Fatalf("opening payable key id=%s, want %s", payable.ID, value.itemID)
		}
	}

	receiptFlags := 0
	for _, key := range keys {
		if !key.IsReceipt {
			continue
		}
		receiptFlags++
		if key.ID != liveReceipt.Id && (key.JournalID == nil || *key.JournalID != liveReceipt.Id) {
			t.Fatalf("receipt flag points to %s, want the unique live manual journal %s", key.ID, liveReceipt.Id)
		}
	}
	if receiptFlags != 1 {
		t.Fatalf("projected receipt-flagged key count=%d, want one unique cash journal despite multiple tax/cash-flow source links", receiptFlags)
	}
	for _, excludedID := range []string{noncashPrior.Id, historicalReceipt.Id, reversalID} {
		purchaseBASScalarAssertJournalNotReceipt(t, keys, excludedID)
	}
}

func purchaseBASScalarAssertJournalNotReceipt(t *testing.T, keys []purchases.BASScalarKey, journalID string) {
	t.Helper()
	for _, key := range keys {
		if (key.ID == journalID || key.JournalID != nil && *key.JournalID == journalID) && key.IsReceipt {
			t.Fatalf("noncash/reversed journal %s was classified as a receipt: %#v", journalID, key)
		}
	}
}
