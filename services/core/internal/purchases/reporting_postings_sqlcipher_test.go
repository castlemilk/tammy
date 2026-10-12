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

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func purchaseReportingDate(year, month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: year, Month: month, Day: day}
}

func postReportingPurchase(t *testing.T, f *postingFixture, invoice string, date *tammyv1.CivilDate, net, gst int64) *tammyv1.PostReviewedPurchaseResponse {
	t.Helper()
	f.date = proto.Clone(date).(*tammyv1.CivilDate)
	f.addDocument("Reporting supplier", invoice, net, gst)
	response, err := f.repo(f.executor).PostReviewed(context.Background(), f.request)
	if err != nil {
		t.Fatalf("post %s: %v", invoice, err)
	}
	return response
}

func capturePostings(t *testing.T, repo *purchases.Repository, ctx context.Context, org string) *purchases.PostingReportingCapture {
	t.Helper()
	capture, err := repo.CapturePostingsForReporting(ctx, org)
	if err != nil {
		t.Fatalf("capture purchase reporting rows: %v", err)
	}
	if capture == nil {
		t.Fatal("successful capture returned nil")
	}
	return capture
}

func reportingStateCounts(t *testing.T, executor app.CommandSQLExecutor) map[string]any {
	t.Helper()
	queries := []string{
		`SELECT count(*) FROM documents`,
		`SELECT count(*) FROM purchase_postings_v1`,
		`SELECT count(*) FROM purchase_posting_allocations_v1`,
		`SELECT count(*) FROM purchase_payments_v1`,
		`SELECT count(*) FROM purchase_payment_allocations_v1`,
		`SELECT count(*) FROM purchase_tax_evidence_v1`,
		`SELECT count(*) FROM purchase_gst_attributions_v1`,
		`SELECT count(*) FROM purchase_gst_deferrals_v1`,
		`SELECT count(*) FROM purchase_gst_deferral_portions_v1`,
		`SELECT count(*) FROM purchase_accounting_events_v1`,
		`SELECT count(*) FROM financial_revision_claims`,
		`SELECT count(*) FROM journals`,
		`SELECT count(*) FROM journal_lines`,
		`SELECT count(*) FROM cash_flow_facts`,
		`SELECT financial_revision FROM financial_revisions WHERE id=1`,
		`SELECT ledger_revision FROM financial_revisions WHERE id=1`,
		`SELECT settlement_revision FROM financial_revisions WHERE id=1`,
		`SELECT banking_revision FROM financial_revisions WHERE id=1`,
		`SELECT tax_source_revision FROM financial_revisions WHERE id=1`,
		`SELECT organisation_profile_revision FROM financial_revisions WHERE id=1`,
		`SELECT rule_bundle_revision FROM financial_revisions WHERE id=1`,
	}
	result := make(map[string]any, len(queries)+1)
	for _, query := range queries {
		result[query] = postScalar(t, executor, query)
	}
	const updatedAtQuery = `SELECT updated_at FROM financial_revisions WHERE id=1`
	rows, err := executor.QueryContext(context.Background(), updatedAtQuery)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		_ = rows.Close()
		t.Fatal("financial revision singleton is missing")
	}
	var updatedAt string
	if err := rows.Scan(&updatedAt); err != nil {
		_ = rows.Close()
		t.Fatal(err)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	result[updatedAtQuery] = updatedAt
	return result
}

func retainedValue(row purchases.PurchaseReportingRetainedRow, column string) (purchases.PurchaseReportingRetainedValue, bool) {
	for index, name := range row.Columns {
		if name == column && index < len(row.Values) {
			return row.Values[index], true
		}
	}
	return purchases.PurchaseReportingRetainedValue{}, false
}

func retainedRowForText(t *testing.T, capture *purchases.PostingReportingCapture, table, column, want string) purchases.PurchaseReportingRetainedRow {
	t.Helper()
	for _, row := range capture.Rows {
		if row.Table != table {
			continue
		}
		value, ok := retainedValue(row, column)
		if ok && value.Kind == "TEXT" && value.Text == want {
			return row
		}
	}
	t.Fatalf("no %s raw row has %s=%q", table, column, want)
	return purchases.PurchaseReportingRetainedRow{}
}

func assertRawSchemaComplete(t *testing.T, executor app.CommandSQLExecutor, capture *purchases.PostingReportingCapture, table string) {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for rows.Next() {
		var position, notNull, primaryKey int
		var name, declaredType string
		var defaultValue sql.NullString
		if err := rows.Scan(&position, &name, &declaredType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	schemaSet := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		if _, duplicate := schemaSet[column]; duplicate {
			t.Fatalf("%s SQLite schema reported duplicate column %q", table, column)
		}
		schemaSet[column] = struct{}{}
	}
	matched := 0
	for _, retained := range capture.Rows {
		if retained.Table != table {
			continue
		}
		matched++
		if len(retained.Columns) != len(schemaSet) || len(retained.Values) != len(schemaSet) {
			t.Fatalf("%s retained columns = %v (%d values), schema = %v", table, retained.Columns, len(retained.Values), columns)
		}
		retainedSet := make(map[string]struct{}, len(retained.Columns))
		for _, column := range retained.Columns {
			if _, duplicate := retainedSet[column]; duplicate {
				t.Fatalf("%s retained duplicate column %q: %v", table, column, retained.Columns)
			}
			retainedSet[column] = struct{}{}
		}
		for column := range schemaSet {
			if _, ok := retainedSet[column]; !ok {
				t.Fatalf("%s retained columns %v omit schema column %q", table, retained.Columns, column)
			}
		}
		for column := range retainedSet {
			if _, ok := schemaSet[column]; !ok {
				t.Fatalf("%s retained unexpected column %q: %v", table, column, retained.Columns)
			}
		}
		for _, value := range retained.Values {
			switch value.Kind {
			case "NULL", "INTEGER", "TEXT", "BLOB":
			default:
				t.Fatalf("%s has unsupported raw storage kind %q", table, value.Kind)
			}
		}
	}
	if matched == 0 {
		t.Fatalf("capture omitted raw rows from %s", table)
	}
}

func assertEveryCapturedTableHasCompleteSchema(t *testing.T, executor app.CommandSQLExecutor, capture *purchases.PostingReportingCapture) {
	t.Helper()
	tables := make(map[string]struct{})
	for _, row := range capture.Rows {
		tables[row.Table] = struct{}{}
	}
	if len(tables) == 0 {
		t.Fatal("capture omitted raw retained SQL rows")
	}
	for table := range tables {
		assertRawSchemaComplete(t, executor, capture, table)
	}
}

func assertCapturedTableSet(t *testing.T, capture *purchases.PostingReportingCapture, expected ...string) {
	t.Helper()
	want := make(map[string]struct{}, len(expected))
	for _, table := range expected {
		if _, duplicate := want[table]; duplicate {
			t.Fatalf("test expected retained table %q more than once", table)
		}
		want[table] = struct{}{}
	}
	got := make(map[string]struct{})
	for _, row := range capture.Rows {
		got[row.Table] = struct{}{}
	}
	for table := range want {
		if _, ok := got[table]; !ok {
			t.Errorf("capture omitted expected retained table %q; got tables %v", table, got)
		}
	}
	for table := range got {
		if _, ok := want[table]; !ok {
			t.Errorf("capture retained unexpected table %q; expected %v", table, want)
		}
	}
}

func assertPurchaseSourceBound(t *testing.T, err error, wantLimit int64) {
	t.Helper()
	if err == nil || !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("error = %v, want typed purchase source bound", err)
	}
	var bound *purchases.SourceBoundError
	if !errors.As(err, &bound) || bound.Resource == "" || bound.Limit != wantLimit {
		t.Fatalf("bound error = %#v, want resource and limit %d", bound, wantLimit)
	}
}

func TestPurchaseReportingCaptureRetainsAllModernPostingRowsAndRestarts(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	unpostedReviewedID := f.document.Id

	old := postReportingPurchase(t, f, "OLD-POSTING", purchaseReportingDate(2026, 7, 10), 10000, 1000)
	unpaid := postReportingPurchase(t, f, "UNPAID-POSTING", purchaseReportingDate(2026, 8, 11), 5000, 500)
	future := postReportingPurchase(t, f, "FUTURE-POSTING", purchaseReportingDate(2027, 1, 10), 5000, 500)
	zero := postReportingPurchase(t, f, "ZERO-POSTING", purchaseReportingDate(2026, 9, 10), 0, 0)
	paid := postReportingPurchase(t, f, "MANY-PAYMENTS", purchaseReportingDate(2026, 7, 10), 2010, 201)
	if old.Posting == nil || unpaid.Posting == nil || future.Posting == nil || zero.Posting == nil || paid.Posting == nil {
		t.Fatal("fixture did not retain all posting roots")
	}
	f.now = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 201; index++ {
		date := paymentDate(9, 30)
		if index >= 67 && index < 134 {
			date = paymentDate(10, 12)
		} else if index >= 134 {
			date = paymentDate(11, 1)
		}
		_, err := f.repo(tx).RecordPayment(ctx, paymentRequest(t, f, uint64(index+1), 11, date))
		if err != nil {
			t.Fatalf("record payment %d: %v", index+1, err)
		}
	}

	before := reportingStateCounts(t, tx)
	repo := f.repo(tx)
	capture := capturePostings(t, repo, ctx, postingOrg)
	if len(capture.Postings) != 5 || len(capture.Documents) != 5 || len(capture.Journals) != 205 {
		t.Fatalf("modern capture roots/documents/journals = %d/%d/%d, want 5/5/205", len(capture.Postings), len(capture.Documents), len(capture.Journals))
	}
	if len(capture.Fingerprint) != sha256.Size || capture.PayloadBytes <= 0 {
		t.Fatalf("capture fingerprint/bytes = %x/%d", capture.Fingerprint, capture.PayloadBytes)
	}
	byInvoice := make(map[string]purchases.PostingReportingRecord, len(capture.Postings))
	for _, record := range capture.Postings {
		if record.Posting == nil || record.Accounting == nil {
			t.Fatal("capture omitted posting or reconstructed accounting state")
		}
		byInvoice[record.Posting.InvoiceNumber] = record
	}
	for _, invoice := range []string{"OLD-POSTING", "UNPAID-POSTING", "FUTURE-POSTING", "ZERO-POSTING", "MANY-PAYMENTS"} {
		if _, ok := byInvoice[invoice]; !ok {
			t.Fatalf("complete modern capture omitted %q", invoice)
		}
	}
	if byInvoice["OLD-POSTING"].Posting.InvoiceDate.Month >= 8 || byInvoice["FUTURE-POSTING"].Posting.InvoiceDate.Year <= 2026 {
		t.Fatal("old or future invoice date was filtered or rewritten")
	}
	if byInvoice["UNPAID-POSTING"].Accounting.Outstanding.MinorUnits != 5500 || len(byInvoice["UNPAID-POSTING"].Payments) != 0 {
		t.Fatal("unpaid posted purchase was omitted or treated as settled")
	}
	if byInvoice["ZERO-POSTING"].Posting.JournalId != nil || len(byInvoice["ZERO-POSTING"].Payments) != 0 {
		t.Fatal("zero posting acquired a synthesized journal/payment")
	}
	paidPostingRow := retainedRowForText(t, capture, "purchase_postings_v1", "id", paid.Posting.Id)
	rawOriginalHash, ok := retainedValue(paidPostingRow, "original_content_hash")
	if !ok || rawOriginalHash.Kind != "BLOB" || !bytes.Equal(rawOriginalHash.Bytes, paid.Posting.OriginalContentHash) {
		t.Fatalf("original source hash was not retained exactly: %#v", rawOriginalHash)
	}
	rawPostingHash, ok := retainedValue(paidPostingRow, "source_hash")
	if !ok || rawPostingHash.Kind != "BLOB" || !bytes.Equal(rawPostingHash.Bytes, paid.Posting.SourceHash) || bytes.Equal(rawPostingHash.Bytes, rawOriginalHash.Bytes) {
		t.Fatalf("computed posting proof was synthesized from or confused with original hash: %#v", rawPostingHash)
	}
	zeroRow := retainedRowForText(t, capture, "purchase_postings_v1", "id", byInvoice["ZERO-POSTING"].Posting.Id)
	journalCell, ok := retainedValue(zeroRow, "journal_id")
	if !ok || journalCell.Kind != "NULL" {
		t.Fatalf("zero posting raw journal_id = %#v; want explicit NULL", journalCell)
	}
	paidRecord := byInvoice["MANY-PAYMENTS"]
	if len(paidRecord.Payments) != 201 || paidRecord.Accounting.Paid.MinorUnits != 2211 || paidRecord.Accounting.Outstanding.MinorUnits != 0 {
		t.Fatalf("201 receipt history or payment totals were truncated: payments=%d paid=%v outstanding=%v", len(paidRecord.Payments), paidRecord.Accounting.Paid, paidRecord.Accounting.Outstanding)
	}
	paymentDays := map[int32]bool{}
	for _, payment := range paidRecord.Payments {
		if payment == nil || payment.ActualPaymentDate == nil {
			t.Fatal("payment is missing its actual date")
		}
		paymentDays[payment.ActualPaymentDate.Day] = true
	}
	if len(paymentDays) != 3 {
		t.Fatalf("payment history collapsed distinct source dates: %v", paymentDays)
	}
	for _, parent := range capture.Journals {
		if parent == nil || parent.Journal == nil || len(parent.ContentHash) != sha256.Size || parent.PayloadBytes <= 0 || parent.CreatedAt == "" {
			t.Fatal("purchase journal capture omitted its complete parent proof")
		}
		for index, line := range parent.Journal.Lines {
			if line == nil || line.Sequence != uint32(index+1) {
				t.Fatalf("journal %s lines are incomplete or not in retained sequence order", parent.Journal.Id)
			}
		}
	}
	for invoice, record := range byInvoice {
		if record.Posting.JournalId == nil {
			continue
		}
		foundPostingJournal := false
		for _, parent := range capture.Journals {
			if parent.Journal.Id == *record.Posting.JournalId && parent.SourceType == "PAYABLE" && parent.SourceKind == 6 && parent.SourceID == record.Posting.Id && parent.SourceRevision == 1 {
				foundPostingJournal = true
				break
			}
		}
		if !foundPostingJournal {
			t.Fatalf("posting %q lacks its exact PAYABLE/6 source tuple", invoice)
		}
		for _, payment := range record.Payments {
			foundPaymentJournal := false
			for _, parent := range capture.Journals {
				if parent.Journal.Id == payment.JournalId && parent.SourceType == "BANKING" && parent.SourceKind == 7 && parent.SourceID == payment.Id && parent.SourceRevision == 1 {
					foundPaymentJournal = true
					break
				}
			}
			if !foundPaymentJournal {
				t.Fatalf("payment %s lacks its exact BANKING/7 source tuple", payment.Id)
			}
		}
	}
	for _, document := range capture.Documents {
		if document.Document == nil {
			t.Fatal("capture contains a nil document record")
		}
		if document.Document.Id == unpostedReviewedID {
			t.Fatal("modern posting capture incorrectly included a nearby unposted reviewed document")
		}
	}

	assertEveryCapturedTableHasCompleteSchema(t, tx, capture)
	assertCapturedTableSet(t, capture,
		"accounts", "cash_flow_facts", "command_idempotency_v1", "documents", "financial_revision_claims",
		"purchase_accounting_events_v1", "purchase_payment_allocations_v1", "purchase_payments_v1",
		"purchase_posting_allocations_v1", "purchase_postings_v1", "purchase_tax_evidence_v1",
	)
	if got := reportingStateCounts(t, tx); !reflect.DeepEqual(got, before) {
		t.Fatalf("reporting capture wrote purchase/accounting state: before=%v after=%v", before, got)
	}

	exactBudget, err := repo.CapturePostingsForReportingWithPayloadBudget(ctx, postingOrg, capture.PayloadBytes)
	if err != nil || exactBudget == nil || exactBudget.PayloadBytes != capture.PayloadBytes || !bytes.Equal(exactBudget.Fingerprint, capture.Fingerprint) {
		t.Fatalf("exact reported budget rejected or changed capture: %v", err)
	}
	_, err = repo.CapturePostingsForReportingWithPayloadBudget(ctx, postingOrg, capture.PayloadBytes-1)
	assertPurchaseSourceBound(t, err, capture.PayloadBytes-1)
	_, err = repo.CapturePostingsForReportingWithPayloadBudget(ctx, postingOrg, 0)
	if err == nil {
		t.Fatal("zero payload budget accepted")
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
	restartedRepo, err := purchases.NewRepository(read, func() time.Time { return f.now }, f.id)
	if err != nil {
		t.Fatal(err)
	}
	restarted := capturePostings(t, restartedRepo, ctx, postingOrg)
	if !bytes.Equal(capture.Fingerprint, restarted.Fingerprint) || !reflect.DeepEqual(capture.Rows, restarted.Rows) {
		t.Fatal("purchase reporting capture changed after encrypted workspace reopen")
	}
	if got := reportingStateCounts(t, read); !reflect.DeepEqual(got, before) {
		t.Fatalf("restart capture changed retained revisions: before=%v after=%v", before, got)
	}
}

func TestPurchaseReportingCaptureRetainsCompleteClaimAndEvidenceLineage(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, portion := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	deferral, err := deferCommand(t, repo, deferRequest(t, f, state, portion))
	if err != nil {
		t.Fatal(err)
	}
	claim := claimRequest(t, f, deferral.Accounting, portion)
	claim.DeferralId = &deferral.Deferral.Id
	attributed, err := attributeCommand(t, repo, claim)
	if err != nil {
		t.Fatal(err)
	}

	capture := capturePostings(t, repo, ctx, postingOrg)
	if len(capture.Postings) != 1 {
		t.Fatalf("claim history posting count=%d", len(capture.Postings))
	}
	record := capture.Postings[0]
	if len(record.Payments) != 1 || len(record.Evidences) != 2 || len(record.Claims.Attributions) != 1 || len(record.Claims.Deferrals) != 1 || len(record.Claims.DeferralPortions) != 1 {
		t.Fatalf("capture truncated evidence/claim history: payments=%d evidence=%d attributions=%d deferrals=%d portions=%d", len(record.Payments), len(record.Evidences), len(record.Claims.Attributions), len(record.Claims.Deferrals), len(record.Claims.DeferralPortions))
	}
	if record.Claims.Attributions[0].Id != attributed.Attributions[0].Id || record.Claims.Deferrals[0].Id != deferral.Deferral.Id || record.Claims.DeferralPortions[0].UnclaimedGST.MinorUnits != 0 {
		t.Fatal("claim lineage, linked deferral closure, or remaining amount changed")
	}
	attributeRow := retainedRowForText(t, capture, "purchase_gst_attributions_v1", "id", attributed.Attributions[0].Id)
	operationKey, ok := retainedValue(attributeRow, "operation_key")
	if !ok || operationKey.Kind != "TEXT" || operationKey.Text == "" {
		t.Fatalf("attribution raw command lineage = %#v", operationKey)
	}
	foundTaxJournal := false
	for _, parent := range capture.Journals {
		if parent.SourceType == "TAX" && parent.SourceKind == 8 && parent.SourceID == operationKey.Text && parent.SourceRevision == 1 {
			foundTaxJournal = true
			break
		}
	}
	if !foundTaxJournal {
		t.Fatalf("attribution %s lacks its exact TAX/8 event-operation source tuple", attributed.Attributions[0].Id)
	}
	events := 0
	for _, row := range capture.Rows {
		if row.Table == "purchase_accounting_events_v1" {
			events++
		}
	}
	if events != 5 {
		t.Fatalf("capture event history=%d, want post, receipt, evidence, deferral, attribution", events)
	}
	assertEveryCapturedTableHasCompleteSchema(t, tx, capture)
	assertCapturedTableSet(t, capture,
		"accounts", "cash_flow_facts", "command_idempotency_v1", "documents", "financial_revision_claims",
		"purchase_accounting_events_v1", "purchase_gst_attributions_v1", "purchase_gst_deferral_portions_v1",
		"purchase_gst_deferrals_v1", "purchase_payment_allocations_v1", "purchase_payments_v1",
		"purchase_posting_allocations_v1", "purchase_postings_v1", "purchase_tax_evidence_v1",
	)

	initialEvidenceRow := purchases.PurchaseReportingRetainedRow{}
	for _, row := range capture.Rows {
		if row.Table != "purchase_tax_evidence_v1" {
			continue
		}
		version, ok := retainedValue(row, "accounting_version")
		if ok && version.Kind == "INTEGER" && version.Integer == 1 {
			initialEvidenceRow = row
			break
		}
	}
	if initialEvidenceRow.Table == "" {
		t.Fatal("raw first evidence record missing")
	}
	originalHash, ok := retainedValue(initialEvidenceRow, "source_hash")
	if !ok || originalHash.Kind != "NULL" {
		t.Fatalf("absent evidence source hash was synthesized: %#v", originalHash)
	}
	recordHash, ok := retainedValue(initialEvidenceRow, "record_hash")
	if !ok || recordHash.Kind != "BLOB" || len(recordHash.Bytes) != sha256.Size || !bytes.Equal(recordHash.Bytes, record.Evidences[0].SourceHash) {
		t.Fatalf("computed evidence record proof was not preserved separately: %#v", recordHash)
	}
	if record.Evidences[0].Review == nil || record.Evidences[0].Review.Source != nil {
		t.Fatal("initial evidence gained a synthesized source reference")
	}
	if len(capture.Documents) < 2 {
		t.Fatalf("evidence/claim supporting documents omitted: %d", len(capture.Documents))
	}
	foundDueDateNotice := false
	for _, document := range capture.Documents {
		if document.Document != nil && document.Document.Id == portion.DueDateEvidence.SourceId {
			foundDueDateNotice = true
		}
	}
	if !foundDueDateNotice {
		t.Fatal("claim due-date evidence document was not retained in reporting capture")
	}
}

func TestPurchaseReportingFingerprintChangesForEvidenceAppendWithoutMoneyChange(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f, repo, state, _ := claimSetup(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	before := capturePostings(t, repo, ctx, postingOrg)
	priorAccounting := proto.Clone(before.Postings[0].Accounting).(*tammyv1.PurchaseAccounting)
	review := proto.Clone(f.request.Evidence).(*tammyv1.PurchaseEvidenceInput)
	review.Classification = tammyv1.PurchaseEvidenceClassification_PURCHASE_EVIDENCE_CLASSIFICATION_TAX_INVOICE
	review.Source = &tammyv1.PurchaseRetainedSourceRef{SourceId: f.document.Id, SourceVersion: f.document.Version, SourceHash: append([]byte(nil), f.document.Sha256...)}
	review.HeldDate = paymentDate(10, 1)
	review.Reason = "Appended reporting-only evidence provenance without a money change"
	if _, err := evidenceCommand(t, repo, ctx, evidenceRequest(t, f, state.AccountingVersion, review)); err != nil {
		t.Fatalf("append evidence-only reporting provenance: %v", err)
	}
	after := capturePostings(t, repo, ctx, postingOrg)
	if bytes.Equal(before.Fingerprint, after.Fingerprint) {
		t.Fatal("source-only evidence append did not change deterministic fingerprint")
	}
	if len(after.Postings) != 1 || len(after.Postings[0].Evidences) != 3 ||
		after.Postings[0].Accounting.Paid.MinorUnits != priorAccounting.Paid.MinorUnits ||
		after.Postings[0].Accounting.Outstanding.MinorUnits != priorAccounting.Outstanding.MinorUnits ||
		after.Postings[0].Accounting.AttributedGst.MinorUnits != priorAccounting.AttributedGst.MinorUnits {
		t.Fatal("evidence-only fingerprint change altered retained purchase amounts")
	}
}

func TestPurchaseReportingFingerprintExcludesMovingReadClock(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	f.date = purchaseReportingDate(2026, 8, 20)
	f.addDocument("Clock supplier", "CLOCK-1", 10000, 1000)
	repo := f.repo(tx)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 8, 19, 23, 59, 59, 0, time.UTC)
	before := capturePostings(t, repo, ctx, postingOrg)
	if before.Postings[0].Accounting.ReviewOutcomes.Timing != tammyv1.PurchaseReviewStatus_PURCHASE_REVIEW_STATUS_NEEDS_TIMING_REVIEW {
		t.Fatalf("future source outcome before held date=%v", before.Postings[0].Accounting.ReviewOutcomes)
	}
	f.now = time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	after := capturePostings(t, repo, ctx, postingOrg)
	if after.Postings[0].Accounting.ReviewOutcomes.Timing == before.Postings[0].Accounting.ReviewOutcomes.Timing {
		t.Fatalf("time-derived readiness did not update: before=%v after=%v", before.Postings[0].Accounting.ReviewOutcomes, after.Postings[0].Accounting.ReviewOutcomes)
	}
	if !bytes.Equal(before.Fingerprint, after.Fingerprint) || !reflect.DeepEqual(before.Rows, after.Rows) {
		t.Fatal("moving read clock changed raw purchase source fingerprint or rows")
	}
}

func newPurchaseReportingSupportDocument(t *testing.T, f *postingFixture, number string, original []byte) *tammyv1.Document {
	t.Helper()
	id, _ := f.id()
	operation, _ := f.id()
	digest := sha256.Sum256(original)
	candidate := &tammyv1.DocumentCandidate{
		SupplierName: "Purchase support", InvoiceNumber: number, DocumentDate: purchaseReportingDate(2026, 8, 10),
		Subtotal: postMoney(0), Gst: postMoney(0), Total: postMoney(0),
	}
	document := &tammyv1.Document{
		Id: id, OrganisationId: postingOrg, Version: 1, Status: tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW,
		SourceDisplayName: number + ".pdf", MimeType: "application/pdf", ByteLength: uint64(len(original)), Sha256: digest[:],
		Candidate: candidate, CreatedAt: timestamppb.New(f.now),
	}
	repository, err := documents.NewRepository(f.executor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(context.Background(), operation, testkit.ActorUserID, document, original); err != nil {
		t.Fatalf("create support document: %v", err)
	}
	reviewed, err := repository.SaveReview(context.Background(), id, 1, candidate, f.now)
	if err != nil {
		t.Fatalf("review support document: %v", err)
	}
	return reviewed
}

func TestPurchaseReportingCaptureRetainsEveryExceptionSupportDocument(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	supportBytes := []byte("retained manager approval and transaction support")
	support := newPurchaseReportingSupportDocument(t, f, "SUPPORT-1", supportBytes)
	f.addDocument("Low-value supplier", "LOW-VALUE-REPORTING", 7500, 750)
	review := lowValueReview(t, f)
	review.ExceptionReview.SupportingRecords = append(review.ExceptionReview.SupportingRecords, &tammyv1.PurchaseRetainedSourceRef{
		SourceId: support.Id, SourceVersion: support.Version, SourceHash: append([]byte(nil), support.Sha256...),
	})
	f.request.Evidence = review
	repo := f.repo(tx)
	retainExceptionRulesIfSupported(t, repo)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatalf("post with complete support records: %v", err)
	}
	capture := capturePostings(t, repo, ctx, postingOrg)
	if len(capture.Postings) != 1 || len(capture.Postings[0].Evidences) != 1 || len(capture.Documents) != 2 {
		t.Fatalf("support capture posting/evidence/documents=%d/%d/%d", len(capture.Postings), len(capture.Postings[0].Evidences), len(capture.Documents))
	}
	refs := capture.Postings[0].Evidences[0].Review.ExceptionReview.SupportingRecords
	if len(refs) != 2 || refs[1].SourceId != support.Id {
		t.Fatalf("exception support refs changed or were truncated: %v", refs)
	}
	found := false
	for _, row := range capture.Rows {
		if row.Table != "documents" {
			continue
		}
		id, ok := retainedValue(row, "id")
		if !ok || id.Kind != "TEXT" || id.Text != support.Id {
			continue
		}
		original, ok := retainedValue(row, "original_bytes")
		if !ok || original.Kind != "BLOB" || !bytes.Equal(original.Bytes, supportBytes) {
			t.Fatalf("support document raw bytes/hash source changed: %#v", original)
		}
		found = true
	}
	if !found {
		t.Fatal("capture omitted the exact raw row for a referenced support document")
	}
	assertEveryCapturedTableHasCompleteSchema(t, tx, capture)
}

type purchaseReportingDocumentProbe struct {
	app.CommandSQLExecutor
	preflighted              bool
	hydrated                 bool
	hydrationBeforePreflight bool
}

func (probe *purchaseReportingDocumentProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	lower := strings.ToLower(query)
	if strings.Contains(lower, "original_bytes") {
		if strings.Contains(lower, "length(") {
			probe.preflighted = true
		} else {
			probe.hydrated = true
			if !probe.preflighted {
				probe.hydrationBeforePreflight = true
			}
		}
	}
	return probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
}

func TestPurchaseReportingCapturePreflightsOversizedExceptionSupportBeforeHydration(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	oversizedBytes := bytes.Repeat([]byte{'S'}, 8*1024*1024+1)
	support := newPurchaseReportingSupportDocument(t, f, "SUPPORT-OVERSIZED", oversizedBytes)
	f.addDocument("Low-value supplier", "LOW-VALUE-OVERSIZED", 7500, 750)
	review := lowValueReview(t, f)
	review.ExceptionReview.SupportingRecords = append(review.ExceptionReview.SupportingRecords, &tammyv1.PurchaseRetainedSourceRef{
		SourceId: support.Id, SourceVersion: support.Version, SourceHash: append([]byte(nil), support.Sha256...),
	})
	f.request.Evidence = review
	repo := f.repo(tx)
	retainExceptionRulesIfSupported(t, repo)
	if _, err := repo.PostReviewed(ctx, f.request); err != nil {
		t.Fatalf("post with oversized-but-valid support source fixture: %v", err)
	}
	probe := &purchaseReportingDocumentProbe{CommandSQLExecutor: tx}
	boundedRepo := f.repo(probe)
	capture, err := boundedRepo.CapturePostingsForReporting(ctx, postingOrg)
	if capture != nil {
		t.Fatal("over-bound support document returned a partial reporting capture")
	}
	if err == nil || !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("oversized linked support document error=%v, want finite source bound", err)
	}
	var bound *purchases.SourceBoundError
	if !errors.As(err, &bound) || bound.Resource == "" || bound.Limit <= 0 {
		t.Fatalf("oversized support error lacks typed bound details: %v", err)
	}
	if !probe.preflighted || probe.hydrationBeforePreflight || probe.hydrated {
		t.Fatalf("support original_bytes preflight/hydration sequence = preflight:%t hydrated:%t early:%t", probe.preflighted, probe.hydrated, probe.hydrationBeforePreflight)
	}
}

type purchaseReportingAccountProbe struct {
	app.CommandSQLExecutor
	preflighted             bool
	headerLoaded            bool
	headerLoadedBeforeCheck bool
}

func (probe *purchaseReportingAccountProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	normalized := strings.Join(strings.Fields(strings.ToLower(query)), " ")
	if strings.Contains(normalized, "from accounts") && strings.Contains(normalized, "length(") && strings.Contains(normalized, "updated_at") {
		probe.preflighted = true
	}
	if strings.Contains(normalized, "select id, organisation_id, version, code, name, account_type") && strings.Contains(normalized, "from accounts") {
		probe.headerLoaded = true
		if !probe.preflighted {
			probe.headerLoadedBeforeCheck = true
		}
	}
	return probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
}

func TestPurchaseReportingCaptureBoundsRawControlAccountHeadersBeforeGet(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	postReportingPurchase(t, f, "CONTROL-HEADER-BOUND", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	// The immutable control-field trigger allows updated_at to change. Three
	// million Unicode characters occupy more than the 8 MiB byte budget while
	// remaining below it if an unsafe character-count check is used.
	oversizedUTF8 := strings.Repeat("界", 3*1024*1024)
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET updated_at=? WHERE organisation_id=? AND code='2000'`, oversizedUTF8, postingOrg); err != nil {
		t.Fatalf("mutate only the permitted control-account timestamp: %v", err)
	}
	probe := &purchaseReportingAccountProbe{CommandSQLExecutor: tx}
	repo := f.repo(probe)
	capture, err := repo.CapturePostingsForReporting(ctx, postingOrg)
	if capture != nil {
		t.Fatal("over-bound raw account header returned a partial reporting capture")
	}
	if err == nil || !errors.Is(err, purchases.ErrSourceBound) {
		t.Fatalf("oversized raw control-account header error=%v, want source bound", err)
	}
	var bound *purchases.SourceBoundError
	if !errors.As(err, &bound) || bound.Resource == "" || bound.Limit <= 0 {
		t.Fatalf("oversized account header error lacks typed bound details: %v", err)
	}
	if !probe.preflighted || probe.headerLoaded || probe.headerLoadedBeforeCheck {
		t.Fatalf("account preflight/Get order = preflight:%t header-loaded:%t before-check:%t", probe.preflighted, probe.headerLoaded, probe.headerLoadedBeforeCheck)
	}
}

type cancelPurchaseReportingExecutor struct {
	app.CommandSQLExecutor
	cancel context.CancelFunc
	fired  bool
}

func (executor *cancelPurchaseReportingExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := executor.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if !executor.fired {
		executor.fired = true
		executor.cancel()
	}
	return rows, err
}

func TestPurchaseReportingCaptureRejectsInvalidOrgAndPropagatesCancellation(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
	postReportingPurchase(t, f, "ISOLATION-1", purchaseReportingDate(2026, 8, 10), 10000, 1000)
	repo := f.repo(tx)
	for _, org := range []string{"", "018f0000-0000-7000-8000-000000009999"} {
		capture, err := repo.CapturePostingsForReporting(ctx, org)
		if org == "" && (err == nil || capture != nil) {
			t.Fatalf("empty organisation scope returned capture=%v error=%v", capture, err)
		}
		if org != "" && ((err == nil && capture == nil) || (err != nil && capture != nil)) {
			t.Fatalf("unknown organisation returned ambiguous capture=%v error=%v", capture, err)
		}
		if capture != nil && (len(capture.Postings) != 0 || len(capture.Documents) != 0 || len(capture.Journals) != 0) {
			t.Fatalf("organisation %q observed another organisation's purchase rows", org)
		}
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	canceling := &cancelPurchaseReportingExecutor{CommandSQLExecutor: tx, cancel: cancel}
	cancelRepo := f.repo(canceling)
	capture, err := cancelRepo.CapturePostingsForReporting(cancelCtx, postingOrg)
	if capture != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reporting capture returned capture=%v error=%v", capture, err)
	}
}
