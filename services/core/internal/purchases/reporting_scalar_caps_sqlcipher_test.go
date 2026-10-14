//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const purchaseBASScalarTestLimit = 1000
const purchaseBASScalarTestKeyLimit = 16384
const purchaseBASScalarTestByteLimit = 8 * 1024 * 1024

// These minimal document rows satisfy the SQL schema and have unique opaque
// IDs/hashes. They are count-only fixture records: no API treats their tiny
// placeholder bytes as real purchase evidence or verifies their hash meaning.
func addPurchaseBASScalarCountOnlyDocuments(t *testing.T, f *postingFixture, count, idBase int64) {
	t.Helper()
	_, err := f.executor.ExecContext(context.Background(), `
		WITH RECURSIVE digit(n) AS (
			SELECT 0 UNION ALL SELECT n+1 FROM digit WHERE n<127
		), numbers(n) AS (
			SELECT high.n*128+low.n+1 FROM digit high CROSS JOIN digit low
			WHERE high.n*128+low.n < ?
		)
		INSERT INTO documents(
			id,operation_key,organisation_id,version,status,source_display_name,mime_type,
			byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,
			document_date,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at
		)
		SELECT printf('018f0000-0000-7000-8000-%012d',?+numbers.n),
			printf('018f0000-0000-7000-8000-%012d',?+numbers.n),?,
			1,'NEEDS_REVIEW','scalar-count-only.pdf','application/pdf',1,
			CAST(printf('%016x%016x',?+numbers.n,numbers.n) AS BLOB),X'61','',
			'Scalar count fixture',printf('COUNT-%06d',numbers.n),NULL,0,0,0,?,?,NULL
		FROM numbers`,
		count, idBase, idBase+50000, postingOrg, idBase, testkit.ActorUserID, f.now.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("insert %d schema-valid count-only document rows: %v", count, err)
	}
}

func appendPurchaseBASScalarAllocationCountRows(t *testing.T, executor app.CommandSQLExecutor, postingID string, count, sequenceOffset int64) {
	t.Helper()
	// The duplicated allocation bodies remain valid bounded BLOBs, while each
	// row receives its own legal primary-key sequence. They are count-only SQL
	// rows; the test never treats these copies as additional financial proof.
	_, err := executor.ExecContext(context.Background(), `
		WITH RECURSIVE digit(n) AS (
			SELECT 0 UNION ALL SELECT n+1 FROM digit WHERE n<127
		), numbers(n) AS (
			SELECT high.n*128+low.n+1 FROM digit high CROSS JOIN digit low
			WHERE high.n*128+low.n < ?
		)
		INSERT INTO purchase_posting_allocations_v1(
			posting_id,organisation_id,sequence,account_id,account_version,gross_minor,
			supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,
			cash_flow_classification,noncreditable_reason,allocation_proto
		)
		SELECT source.posting_id,source.organisation_id,numbers.n+?,source.account_id,
			source.account_version,source.gross_minor,source.supplier_gst_minor,
			source.eligible_gst_minor,source.use_kind,source.bas_classification,
			source.cash_flow_classification,source.noncreditable_reason,source.allocation_proto
		FROM purchase_posting_allocations_v1 AS source CROSS JOIN numbers
		WHERE source.posting_id=? AND source.sequence=1`, count, sequenceOffset, postingID)
	if err != nil {
		t.Fatalf("insert %d schema-valid count-only posting allocations: %v", count, err)
	}
}

func TestPurchaseBASScalarRootCapAccepts1000AndRejects1001BeforeKeys(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)

	// One initial document root plus 997 count-only documents, one posted
	// purchase root, and one real POSTED opening payable make exactly 1,000.
	addPurchaseBASScalarCountOnlyDocuments(t, f, 997, 200000)
	postReportingPurchase(t, f, "SCALAR-ROOT-MODERN", postDate(10), 0, 0)
	legacyOpeningFixture(t, f)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || summary == nil || summary.RootCount != purchaseBASScalarTestLimit {
		t.Fatalf("exact root capacity summary=%#v err=%v", summary, err)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("exact root capacity key read=%d err=%v summary=%#v", len(keys), err, summary)
	}
	if probe.keyReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("exact root read touched a body, wrote retained state, or ended its caller transaction")
	}

	addPurchaseBASScalarCountOnlyDocuments(t, f, 1, 400000)
	beforeOverflow := purchaseBASScalarState(t, tx)
	keyReadsBeforeOverflow := probe.keyReads
	overflow, err := repository.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseCoverageBound(t, err, "purchase roots", purchaseBASScalarTestLimit)
	if overflow != nil || probe.keyReads != keyReadsBeforeOverflow || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(beforeOverflow, purchaseBASScalarState(t, tx)) {
		t.Fatal("1,001st root returned a summary/key/body, wrote retained state, or ended its caller transaction")
	}
}

func TestPurchaseBASScalarReceiptCapCountsModernAndOneRealManualReceipt(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posting := postReportingPurchase(t, f, "SCALAR-RECEIPTS", postDate(10), 10000, 1000)
	first, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, posting.Accounting.AccountingVersion, 11000, postDate(20)))
	if err != nil {
		t.Fatalf("record first modern receipt: %v", err)
	}
	// The existing fixture copies valid SQL payment/journal shapes and takes
	// each journal through DRAFT -> POSTED. These copies are count-only rows.
	for index := 1; index < purchaseBASScalarTestLimit-1; index++ {
		appendCoveragePaymentCountFixture(t, f, tx, first, index)
	}
	f.addDocumentReview("Scalar manual receipt", "SCALAR-MANUAL-RECEIPT", 10000, 1000, true)
	seedLegacyCashPayment(t, f, f.document.Id)

	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository := f.repo(probe)
	before := purchaseBASScalarState(t, tx)
	summary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	modernReceipts := postScalar(t, tx, `SELECT count(*) FROM purchase_payments_v1 WHERE organisation_id=?`, postingOrg)
	if err != nil || summary == nil || summary.ReceiptCount != purchaseBASScalarTestLimit ||
		modernReceipts != purchaseBASScalarTestLimit-1 ||
		summary.FamilyCounts[purchases.BASScalarPayment-1] != modernReceipts {
		t.Fatalf("exact combined receipt summary=%#v err=%v SQL modern=%d; want 999 modern plus one actual manual receipt",
			summary, err, modernReceipts)
	}
	keys, err := repository.ReadBASKeys(ctx, postingOrg, *summary, summary.ScalarKeyCount, summary.ScalarBytes)
	if err != nil || int64(len(keys)) != summary.ScalarKeyCount {
		t.Fatalf("exact receipt capacity key read=%d err=%v summary=%#v", len(keys), err, summary)
	}
	if probe.keyReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("exact receipt read touched a body, wrote retained state, or ended its caller transaction")
	}

	appendCoveragePaymentCountFixture(t, f, tx, first, purchaseBASScalarTestLimit-1)
	beforeOverflow := purchaseBASScalarState(t, tx)
	keyReadsBeforeOverflow := probe.keyReads
	overflow, err := repository.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseCoverageBound(t, err, "receipt sources", purchaseBASScalarTestLimit)
	if overflow != nil || probe.keyReads != keyReadsBeforeOverflow || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(beforeOverflow, purchaseBASScalarState(t, tx)) {
		t.Fatal("1,001st receipt returned a summary/key/body, wrote retained state, or ended its caller transaction")
	}
}

func TestPurchaseBASScalarKeyAndByteCapsCountPostingAllocations(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	posting := postReportingPurchase(t, f, "SCALAR-KEY-CAP", postDate(10), 10000, 1000)
	repository := f.repo(tx)
	baseline, err := repository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || baseline == nil || baseline.ScalarKeyCount >= purchaseBASScalarTestKeyLimit {
		t.Fatalf("small physical key baseline=%#v err=%v", baseline, err)
	}

	// Physical child keys do not consume the 1,000 source-root ceiling. Accept
	// 1,001 allocations within the scalar budget before testing its hard caps.
	appendPurchaseBASScalarAllocationCountRows(t, tx, posting.Posting.Id, 1000, 1)
	childProbe := &purchaseBASScalarProbe{Transaction: tx}
	childRepository := f.repo(childProbe)
	beforeChildren := purchaseBASScalarState(t, tx)
	childSummary, err := childRepository.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || childSummary == nil ||
		childSummary.FamilyCounts[purchases.BASScalarPostingAllocation-1] != 1001 ||
		childSummary.ScalarKeyCount != baseline.ScalarKeyCount+1000 ||
		childSummary.RootCount != baseline.RootCount || childSummary.ReceiptCount != baseline.ReceiptCount {
		t.Fatalf("1,001 physical allocations summary=%#v err=%v", childSummary, err)
	}
	childKeys, err := childRepository.ReadBASKeys(ctx, postingOrg, *childSummary, childSummary.ScalarKeyCount, childSummary.ScalarBytes)
	if err != nil || int64(len(childKeys)) != childSummary.ScalarKeyCount ||
		childProbe.keyReads != 1 || childProbe.bodyReads != 0 || !tx.Authenticated() ||
		!reflect.DeepEqual(beforeChildren, purchaseBASScalarState(t, tx)) {
		t.Fatalf("1,001 physical allocation read=%d err=%v; changed state, read a body, or ended caller transaction", len(childKeys), err)
	}

	// Fill the legal key reservation exactly. Each copied physical allocation
	// contributes one scalar key and at least its 512-byte row charge plus
	// organization and primary-key text, so bytes overflow at this boundary.
	extraForByteBoundary := purchaseBASScalarTestKeyLimit - baseline.ScalarKeyCount
	appendPurchaseBASScalarAllocationCountRows(t, tx, posting.Posting.Id, extraForByteBoundary-1000, 1001)
	if got := postScalar(t, tx, `SELECT count(*) FROM purchase_posting_allocations_v1 WHERE posting_id=?`, posting.Posting.Id); got <= 1000 {
		t.Fatalf("count-only physical posting allocations=%d, want well over 1,000", got)
	}
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repository = f.repo(probe)
	beforeByteBound := purchaseBASScalarState(t, tx)
	byteSummary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseCoverageBound(t, err, "BAS scalar bytes", purchaseBASScalarTestByteLimit)
	if byteSummary != nil || probe.keyReads != 0 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(beforeByteBound, purchaseBASScalarState(t, tx)) {
		t.Fatal("exact maximum key count escaped the byte cap or read keys/bodies/wrote state")
	}

	// One extra allocation crosses the key cap; key-count rejection takes
	// precedence over the already-over-budget byte total.
	_, err = tx.ExecContext(ctx, `INSERT INTO purchase_posting_allocations_v1(posting_id,organisation_id,sequence,account_id,account_version,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,noncreditable_reason,allocation_proto) SELECT posting_id,organisation_id,?+1,account_id,account_version,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,noncreditable_reason,allocation_proto FROM purchase_posting_allocations_v1 WHERE posting_id=? AND sequence=1`, extraForByteBoundary+1, posting.Posting.Id)
	if err != nil {
		t.Fatal(err)
	}
	beforeKeyBound := purchaseBASScalarState(t, tx)
	keySummary, err := repository.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseCoverageBound(t, err, "BAS scalar keys", purchaseBASScalarTestKeyLimit)
	if keySummary != nil || probe.keyReads != 0 || probe.bodyReads != 0 ||
		!tx.Authenticated() || !reflect.DeepEqual(beforeKeyBound, purchaseBASScalarState(t, tx)) {
		t.Fatal("key-count overflow escaped its cap or read keys/bodies/wrote state")
	}
}
