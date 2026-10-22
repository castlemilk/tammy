//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
)

// Native regressions retain the writer-backed ownership, scope and canonical
// capture controls used to reproduce the original omissions.

func movePurchaseOpeningOwnerGraphOutOfTarget(t *testing.T, f *postingFixture) {
	t.Helper()
	movePurchaseModernOwnerGraphOutOfTarget(t, f)
	const foreignOwner = purchaseScalarRootOwnerForeignUUID
	if postScalar(t, f.executor, `SELECT count(*) FROM opening_conversions WHERE organisation_id=?`, postingOrg) > 0 {
		mutatePurchaseBASScalar(t, f, "opening_conversions",
			`UPDATE opening_conversions SET organisation_id=? WHERE organisation_id=?`, foreignOwner, postingOrg)
	}
	if postScalar(t, f.executor, `SELECT count(*) FROM purchase_legacy_reviews_v1 WHERE organisation_id=?`, postingOrg) > 0 {
		mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
			`UPDATE purchase_legacy_reviews_v1 SET organisation_id=? WHERE organisation_id=?`, foreignOwner, postingOrg)
	}
}

func addUnlinkedDraftPayable(t *testing.T, tx *sqlcipher.Transaction, f *postingFixture,
	rootPayableID, rootConversionID string,
) (draftPayableID, draftConversionID string) {
	t.Helper()
	ctx := context.Background()
	draftConversionID, _ = f.id()
	insertCoverageOpeningDraft(t, f, draftConversionID, "c")
	// Establish foreign conversion ownership before attaching the foreign
	// account item, so the DRAFT's references are internally owner-consistent.
	mutatePurchaseBASScalar(t, f, "opening_conversions",
		`UPDATE opening_conversions SET organisation_id=? WHERE id=?`, purchaseScalarRootOwnerForeignUUID, draftConversionID)
	draftPayableID, _ = f.id()
	rows, err := tx.QueryContext(ctx, `SELECT retained_input_proto FROM purchase_opening_payables WHERE id=?`, rootPayableID)
	if err != nil {
		t.Fatalf("read writer-created payable input: %v", err)
	}
	var encoded []byte
	if !rows.Next() || rows.Scan(&encoded) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatal("read unique writer-created payable input")
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	input := &tammyv1.OpeningBalanceInput{}
	if err := proto.Unmarshal(encoded, input); err != nil {
		t.Fatalf("decode writer-created payable input: %v", err)
	}
	input.ClientLineId = draftPayableID
	opening, err := purchases.NewOpeningRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := opening.RecordOpeningPayable(ctx, draftConversionID, input, f.now); err != nil {
		t.Fatalf("write independent DRAFT payable through OpeningRepository: %v", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO opening_items(
			id,conversion_id,account_id,item_kind,debit_minor,credit_minor,currency_code,
			source_type,source_id,source_revision,source_content_hash,original_issue_date,
			original_due_date,outstanding_gst_minor,prior_gst_attributed_minor,
			latest_statement_date,latest_statement_balance_minor
		)
		SELECT ?,?,account_id,item_kind,debit_minor,credit_minor,currency_code,
			source_type,source_id,source_revision,source_content_hash,original_issue_date,
			original_due_date,outstanding_gst_minor,prior_gst_attributed_minor,
			latest_statement_date,latest_statement_balance_minor
		FROM opening_items WHERE id=? AND conversion_id=?`,
		draftPayableID, draftConversionID, rootPayableID, rootConversionID)
	if err != nil {
		t.Fatalf("add paired item to independent DRAFT: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("independent DRAFT item rows=%d err=%v; want one", affected, err)
	}
	return draftPayableID, draftConversionID
}

func assertPurchaseOpeningNoTargetTextOwner(t *testing.T, tx *sqlcipher.Transaction) {
	t.Helper()
	for _, table := range []string{
		"documents", "accounts", "journals", "purchase_postings_v1", "purchase_posting_allocations_v1",
		"purchase_payments_v1", "purchase_payment_allocations_v1", "purchase_tax_evidence_v1",
		"purchase_accounting_events_v1", "purchase_gst_deferrals_v1", "purchase_gst_deferral_portions_v1",
		"purchase_gst_attributions_v1", "opening_conversions", "purchase_legacy_reviews_v1",
	} {
		if count := postScalar(t, tx, "SELECT count(*) FROM "+table+
			" WHERE typeof(organisation_id)='text' AND organisation_id=?", postingOrg); count != 0 {
			t.Fatalf("%s retained %d target TEXT owners; want all writer graph owners foreign before the selected BLOB probe", table, count)
		}
	}
}

func assertPurchaseOpeningEmptyCapture(t *testing.T, capture *purchases.PurchaseCoverageReportingCapture, err error) {
	t.Helper()
	if err != nil || capture == nil || capture.RootCount != 0 || capture.ReceiptCount != 0 ||
		len(capture.Postings) != 0 || len(capture.LegacySources) != 0 || len(capture.Documents) != 0 ||
		len(capture.Journals) != 0 || len(capture.Rows) != 0 || capture.PayloadBytes != 0 || len(capture.Fingerprint) != 32 {
		t.Fatalf("expected successful empty full coverage capture, got capture=%#v err=%v", capture, err)
	}
}

func TestPurchaseOpeningReaderRetainsCanonicalReplacementAndReview(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	// Add the document explicitly so this writer test does not depend on the
	// base fixture having populated f.document.
	f.addDocument("Canonical opening supplier", "OPENING-CANONICAL", 10000, 1000)
	payableID, conversionID := coverageOpeningWithDocument(t, f, f.document, true)
	review := coverageReviewOpening(t, f, payableID)
	capture := captureCoverage(t, f.repo(tx), ctx)
	if hex.EncodeToString(capture.Fingerprint) != "1a550ad37a5406c66c59c810403ba8be7000870c001d7c7d55bd6bffb528a853" ||
		capture.PayloadBytes != 22811 || capture.RootCount != 4 || capture.ReceiptCount != 0 {
		t.Fatalf("canonical opening capture changed: hash=%x bytes=%d roots=%d receipts=%d", capture.Fingerprint,
			capture.PayloadBytes, capture.RootCount, capture.ReceiptCount)
	}
	opening := coverageLegacyOpening(t, capture, payableID)
	if len(opening.Reviews) != 1 || !proto.Equal(opening.Reviews[0], review) ||
		countCoverageRows(coverageRawRows(capture), "opening_conversions", "id", conversionID) != 1 ||
		countCoverageRows(coverageRawRows(capture), "purchase_opening_payables", "id", payableID) != 1 {
		t.Fatalf("canonical TEXT replacement/opening review was not retained: %#v", opening)
	}
}

func TestPurchaseBASScalarAndCoverageRejectPayableRootExactTargetBLOB(t *testing.T) {
	for _, state := range []string{"POSTED", "REPLACED"} {
		t.Run(state, func(t *testing.T) { testPurchaseBASRejectPayableRootBLOB(t, state) })
	}
}

func testPurchaseBASRejectPayableRootBLOB(t *testing.T, state string) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	f.addDocument("BLOB opening supplier", "OPENING-BLOB-ROOT", 10000, 1000)
	sourceDocumentID := f.document.Id
	payableID, conversionID := coverageOpeningWithDocument(t, f, f.document, state == "REPLACED")
	coverageReviewOpening(t, f, payableID)

	// Establish a genuine zero-target scalar/full-capture baseline: every
	// source document, account, journal, legacy review, event and other root or
	// replacement conversion is foreign TEXT before this one payable-backed
	// POSTED/REPLACED conversion becomes target-byte BLOB.
	movePurchaseOpeningOwnerGraphOutOfTarget(t, f)
	assertPurchaseOpeningNoTargetTextOwner(t, tx)
	if sourceType := purchaseBASScalarText(t, tx,
		`SELECT typeof(organisation_id) FROM documents WHERE id=?`, sourceDocumentID); sourceType != "text" {
		t.Fatalf("source document owner storage %q, want foreign TEXT", sourceType)
	}
	if purchaseBASScalarText(t, tx,
		`SELECT source_type FROM opening_items WHERE id=?`, payableID) != "document" ||
		purchaseBASScalarText(t, tx,
			`SELECT source_id FROM opening_items WHERE id=?`, payableID) != sourceDocumentID {
		t.Fatal("opening item source reference changed while moving its owners foreign")
	}
	if purchaseBASScalarText(t, tx, `SELECT a.organisation_id FROM accounts a
		JOIN opening_items i ON i.account_id=a.id WHERE i.id=?`, payableID) != purchaseScalarRootOwnerForeignUUID ||
		purchaseBASScalarText(t, tx, `SELECT j.organisation_id FROM journals j
		JOIN opening_conversions c ON c.journal_id=j.id WHERE c.id=?`, conversionID) != purchaseScalarRootOwnerForeignUUID {
		t.Fatal("opening account/journal reference parents were not retained as foreign TEXT owners")
	}
	purchaseModernOwnerAssertEmptyScalarReservation(t, ctx, f, tx)
	baselineCapture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseOpeningEmptyCapture(t, baselineCapture, err)

	if actualState := purchaseBASScalarText(t, tx, `SELECT state FROM opening_conversions WHERE id=?`, conversionID); actualState != state {
		t.Fatalf("selected root state=%q, want writer-created %s", actualState, state)
	}
	mutatePurchaseBASScalar(t, f, "opening_conversions",
		`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, conversionID)
	purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", conversionID, []byte(postingOrg))
	if count := postScalar(t, tx, `SELECT count(*) FROM opening_conversions
		WHERE typeof(organisation_id)='blob' AND organisation_id=CAST(? AS BLOB)`, postingOrg); count != 1 {
		t.Fatalf("payable-root fixture has %d exact-target BLOB conversion owners; want only the selected root", count)
	}
	readState := purchaseBASScalarState(t, tx)
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)

	scalarProbe := &purchaseBASScalarProbe{Transaction: tx}
	summary, scalarErr := f.repo(scalarProbe).SummarizeBASKeys(ctx, postingOrg)
	if summary != nil || !errors.Is(scalarErr, purchases.ErrRepository) ||
		scalarProbe.keyReads != 0 || scalarProbe.bodyReads != 0 {
		t.Errorf("BLOB payable-root summary=%#v err=%v keyReads=%d bodyReads=%d; want early ErrRepository",
			summary, scalarErr, scalarProbe.keyReads, scalarProbe.bodyReads)
	}

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, preflightErr := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	assertPurchaseRootOwnerRejected(t, preflight != nil, preflightErr, preflightProbe.bodyReads, "payable-root coverage preflight")

	captureProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	capture, captureErr := f.repo(captureProbe).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseRootOwnerRejected(t, capture != nil, captureErr, captureProbe.bodyReads, "payable-root full capture")
	assertPurchaseRootOwnerNoMutation(t, tx, readState, triggers)
}

func TestPurchaseBASScalarAndCoverageRejectDirectBLOBReviewOwnerAfterForeignGraph(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	// legacyRequest captures the source before adding its support document; add
	// the source explicitly and retain that exact writer-created document ID.
	f.addDocument("Legacy review supplier", "LEGACY-OWNER-BLOB", 10000, 1000)
	sourceDocumentID := f.document.Id
	request := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	response, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, request)
	if err != nil || response == nil || response.Review == nil {
		t.Fatalf("create canonical legacy review through ResolveLegacy: response=%v err=%v", response, err)
	}

	// The scalar has no target TEXT reference edge after all documents,
	// support/account/journal/event rows and review owners move foreign. Retain
	// the writer's document parent key, then drift only the review owner.
	movePurchaseOpeningOwnerGraphOutOfTarget(t, f)
	assertPurchaseOpeningNoTargetTextOwner(t, tx)
	var sourceKind, documentID, documentIDType string
	rows, err := tx.QueryContext(ctx, `SELECT source_kind,typeof(document_id),document_id
		FROM purchase_legacy_reviews_v1 WHERE id=?`, response.Review.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() || rows.Scan(&sourceKind, &documentIDType, &documentID) != nil || rows.Next() || rows.Err() != nil {
		_ = rows.Close()
		t.Fatalf("read retained legacy review source link %s", response.Review.Id)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if sourceKind != "DOCUMENT" || documentIDType != "text" || documentID != sourceDocumentID {
		t.Fatalf("foreign-owner review parent fields=%q/%q/%q; want unchanged DOCUMENT/TEXT/%s",
			sourceKind, documentIDType, documentID, sourceDocumentID)
	}
	if owner := purchaseBASScalarText(t, tx,
		`SELECT typeof(organisation_id)||':'||organisation_id FROM documents WHERE id=?`, sourceDocumentID); owner != "text:"+purchaseScalarRootOwnerForeignUUID {
		t.Fatalf("legacy review's source-document parent owner=%q; want foreign TEXT", owner)
	}
	purchaseModernOwnerAssertEmptyScalarReservation(t, ctx, f, tx)
	baselineCapture, err := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseOpeningEmptyCapture(t, baselineCapture, err)

	mutatePurchaseBASScalar(t, f, "purchase_legacy_reviews_v1",
		`UPDATE purchase_legacy_reviews_v1 SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, response.Review.Id)
	purchaseRootOwnerAssertStoredBLOB(t, tx, "purchase_legacy_reviews_v1", response.Review.Id, []byte(postingOrg))
	if count := postScalar(t, tx, `SELECT count(*) FROM purchase_legacy_reviews_v1
		WHERE typeof(organisation_id)='blob' AND organisation_id=CAST(? AS BLOB)`, postingOrg); count != 1 {
		t.Fatalf("legacy review fixture has %d exact-target BLOB owners; want only the selected review", count)
	}
	if got := purchaseBASScalarText(t, tx,
		`SELECT source_kind||':'||typeof(document_id)||':'||document_id FROM purchase_legacy_reviews_v1 WHERE id=?`, response.Review.Id); got != "DOCUMENT:text:"+sourceDocumentID {
		t.Fatalf("direct review-owner drift changed retained source parent refs: %q", got)
	}
	readState := purchaseBASScalarState(t, tx)
	triggers := purchaseRootOwnerTriggerDefinitions(t, tx)

	scalarProbe := &purchaseBASScalarProbe{Transaction: tx}
	summary, scalarErr := f.repo(scalarProbe).SummarizeBASKeys(ctx, postingOrg)
	if summary != nil || !errors.Is(scalarErr, purchases.ErrRepository) ||
		scalarProbe.keyReads != 0 || scalarProbe.bodyReads != 0 {
		t.Errorf("direct-BLOB legacy review summary=%#v err=%v keyReads=%d bodyReads=%d; want early ErrRepository",
			summary, scalarErr, scalarProbe.keyReads, scalarProbe.bodyReads)
	}

	preflightProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	preflight, preflightErr := f.repo(preflightProbe).PreflightCoverageForBASWithPayloadBudget(
		ctx, postingOrg, purchaseCoverageDraftPayloadLimit)
	assertPurchaseRootOwnerRejected(t, preflight != nil, preflightErr, preflightProbe.bodyReads, "legacy-review coverage preflight")

	captureProbe := &purchaseCoverageHydrationProbe{CommandSQLExecutor: tx}
	capture, captureErr := f.repo(captureProbe).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseRootOwnerRejected(t, capture != nil, captureErr, captureProbe.bodyReads, "legacy-review full capture")
	assertPurchaseRootOwnerNoMutation(t, tx, readState, triggers)
}

func TestPurchaseOpeningReaderIgnoresForeignReferenceUnlinkedDraftOwners(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()

	f.addDocument("Unlinked DRAFT source", "OPENING-DRAFT-OUTSCOPE", 10000, 1000)
	payableID, conversionID := coverageOpeningWithDocument(t, f, f.document, true)
	coverageReviewOpening(t, f, payableID)
	movePurchaseOpeningOwnerGraphOutOfTarget(t, f)
	assertPurchaseOpeningNoTargetTextOwner(t, tx)
	purchaseModernOwnerAssertEmptyScalarReservation(t, ctx, f, tx)
	beforeCapture := captureCoverage(t, f.repo(tx), ctx)
	assertPurchaseOpeningEmptyCapture(t, beforeCapture, nil)
	beforeSummary, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	if err != nil || beforeSummary == nil {
		t.Fatalf("reserve empty payable-root baseline: %#v %v", beforeSummary, err)
	}

	draftPayableID, targetBlobDraftID := addUnlinkedDraftPayable(t, tx, f, payableID, conversionID)
	if postScalar(t, tx, `SELECT count(*) FROM purchase_opening_payables p JOIN opening_items i
		ON i.id=p.id AND i.conversion_id=p.conversion_id WHERE p.id=? AND p.conversion_id=?`,
		draftPayableID, targetBlobDraftID) != 1 {
		t.Fatal("target-BLOB DRAFT control is not backed by its matching payable and item")
	}
	mutatePurchaseBASScalar(t, f, "opening_conversions",
		`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, postingOrg, targetBlobDraftID)
	otherBlobDraftID, _ := f.id()
	insertCoverageOpeningDraft(t, f, otherBlobDraftID, "e")
	mutatePurchaseBASScalar(t, f, "opening_conversions",
		`UPDATE opening_conversions SET organisation_id=CAST(? AS BLOB) WHERE id=?`, purchaseScalarRootOwnerForeignUUID, otherBlobDraftID)
	purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", targetBlobDraftID, []byte(postingOrg))
	purchaseRootOwnerAssertStoredBLOB(t, tx, "opening_conversions", otherBlobDraftID, []byte(purchaseScalarRootOwnerForeignUUID))
	assertPurchaseOpeningNoTargetTextOwner(t, tx)
	if owner := purchaseBASScalarText(t, tx, `SELECT a.organisation_id FROM opening_items i
		JOIN accounts a ON a.id=i.account_id WHERE i.conversion_id=?`, targetBlobDraftID); owner != purchaseScalarRootOwnerForeignUUID {
		t.Fatalf("unlinked DRAFT account parent owner=%q; want foreign TEXT", owner)
	}
	if owner := purchaseBASScalarText(t, tx, `SELECT d.organisation_id FROM opening_items i
		JOIN documents d ON d.id=i.source_id WHERE i.conversion_id=?`, targetBlobDraftID); owner != purchaseScalarRootOwnerForeignUUID {
		t.Fatalf("unlinked DRAFT source-document parent owner=%q; want foreign TEXT", owner)
	}

	afterSummary, err := f.repo(tx).SummarizeBASKeys(ctx, postingOrg)
	if err != nil || afterSummary == nil || !reflect.DeepEqual(beforeSummary, afterSummary) {
		t.Errorf("out-of-scope DRAFT owner changed scalar roots/keys: before=%#v after=%#v err=%v",
			beforeSummary, afterSummary, err)
	}
	afterCapture, captureErr := f.repo(tx).CaptureCoverageForBAS(ctx, postingOrg)
	assertPurchaseOpeningEmptyCapture(t, afterCapture, captureErr)
	if afterCapture != nil && beforeCapture != nil &&
		(afterCapture.RootCount != beforeCapture.RootCount || afterCapture.PayloadBytes != beforeCapture.PayloadBytes ||
			!bytes.Equal(afterCapture.Fingerprint, beforeCapture.Fingerprint) || !reflect.DeepEqual(afterCapture.Rows, beforeCapture.Rows)) {
		t.Errorf("out-of-scope DRAFT changed payable-rooted capture: roots=%d/%d payload=%d/%d rows=%d/%d",
			afterCapture.RootCount, beforeCapture.RootCount, afterCapture.PayloadBytes, beforeCapture.PayloadBytes,
			len(afterCapture.Rows), len(beforeCapture.Rows))
	}
	if !tx.Authenticated() {
		t.Fatal("unlinked DRAFT scope control changed caller transaction authentication")
	}
}
