//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

type purchaseReportingRootOwnerAtomicProbe struct {
	app.CommandSQLExecutor
	t                     *testing.T
	fixture               *postingFixture
	tx                    *sqlcipher.Transaction
	mode                  string
	stage                 string
	ownerTable            string
	ownerID               string
	bodyTable             string
	bodyColumn            string
	bodyID                string
	hookCount             int
	afterHook             bool
	laterBodies           int
	documentReplayReads   int
	unpostedFallbackReads int
	stateAfterMutation    map[string]any
	triggersBefore        map[string]string
	triggersAfterMutation map[string]string
	beforeRead            runtime.MemStats
}

func (probe *purchaseReportingRootOwnerAtomicProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if probe.afterHook && purchaseReportingRootOwnerIsBodyStatement(query) {
		probe.laterBodies++
	}
	compact := purchaseReportingRootOwnerCompactSQL(query)
	if strings.HasPrefix(compact, "selectdocument_idfrompurchase_postings_v1whereorganisation_id=?orderbyid") {
		probe.unpostedFallbackReads++
	}
	isDocumentReplay := purchaseReportingRootOwnerIsDocumentReplayQuery(query)
	if probe.hookCount == 0 && probe.matchesHook(query) {
		if probe.stage == "posting-replay" && probe.documentReplayReads == 0 {
			probe.t.Fatalf("posting replay hook ran before its document replay query")
		}
		probe.hookCount++
		probe.triggersBefore = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
		if probe.ownerTable == probe.bodyTable && probe.ownerID == probe.bodyID {
			mutatePurchaseBASScalar(probe.t, probe.fixture, probe.ownerTable,
				"UPDATE "+probe.ownerTable+" SET organisation_id=CAST(? AS BLOB),"+probe.bodyColumn+"=zeroblob(?) WHERE id=?",
				postingOrg, purchaseRootOwnerLateCellBytes, probe.ownerID)
		} else {
			mutatePurchaseBASScalar(probe.t, probe.fixture, probe.ownerTable,
				"UPDATE "+probe.ownerTable+" SET organisation_id=CAST(? AS BLOB) WHERE id=?",
				postingOrg, probe.ownerID)
			mutatePurchaseBASScalar(probe.t, probe.fixture, probe.bodyTable,
				"UPDATE "+probe.bodyTable+" SET "+probe.bodyColumn+"=zeroblob(?) WHERE id=?",
				purchaseRootOwnerLateCellBytes, probe.bodyID)
		}
		purchaseRootOwnerAssertStoredBLOB(probe.t, probe.tx, probe.ownerTable, probe.ownerID, []byte(postingOrg))
		purchaseRootOwnerAssertLateCellMetadata(probe.t, probe.tx, probe.bodyTable, probe.bodyColumn, probe.bodyID)
		probe.triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
		if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) {
			probe.t.Fatal("owner fixture mutation did not restore exact trigger definitions")
		}
		if got := postScalar(probe.t, probe.tx, "PRAGMA ignore_check_constraints"); got != 0 {
			probe.t.Fatalf("fixture mutation left ignore_check_constraints=%d, want 0", got)
		}
		probe.stateAfterMutation = purchaseReportingRootOwnerState(probe.t, probe.tx, probe.mode)
		probe.afterHook = true
		runtime.GC()
		runtime.ReadMemStats(&probe.beforeRead)
	}

	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if isDocumentReplay && err == nil && rows != nil {
		probe.documentReplayReads++
	}
	return rows, err
}

func (probe *purchaseReportingRootOwnerAtomicProbe) matchesHook(query string) bool {
	if strings.Contains(query, probe.marker()) {
		return true
	}
	compact := purchaseReportingRootOwnerCompactSQL(query)
	switch probe.stage {
	case "posting-root":
		return strings.HasPrefix(compact, "selectp.id,p.organisation_id,p.document_id,")
	case "document-root":
		return strings.HasPrefix(compact, "selectd.id,d.operation_key,d.organisation_id,d.version,d.status,") &&
			strings.Contains(compact, "fromdocumentsasdwhere")
	case "document-replay":
		if probe.bodyColumn == "original_bytes" {
			return strings.HasPrefix(compact, "selectoriginal_bytesfromdocumentswhereid=?andorganisation_id=?")
		}
		return purchaseReportingRootOwnerIsLegacyDocumentSelect(compact)
	case "posting-replay":
		return strings.HasPrefix(compact, "selectposting_proto,source_hash,reviewed_source_proto,reviewed_snapshot_hashfrompurchase_postings_v1whereorganisation_id=?anddocument_id=?")
	default:
		return false
	}
}

func (probe *purchaseReportingRootOwnerAtomicProbe) marker() string {
	switch probe.stage {
	case "posting-root":
		return "/* BAS purchase owner body purchase_postings_v1 */"
	case "document-root":
		return "/* BAS purchase owner body documents */"
	case "document-replay":
		return "/* BAS purchase owner body document replay */"
	case "posting-replay":
		return "/* BAS purchase owner body posting replay */"
	default:
		return ""
	}
}

func purchaseReportingRootOwnerCompactSQL(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), ""))
}

func purchaseReportingRootOwnerIsLegacyDocumentSelect(compact string) bool {
	return strings.HasPrefix(compact,
		"selectid,organisation_id,version,status,source_display_name,mime_type,byte_length,sha256,extracted_text,supplier_name,invoice_number,document_date,subtotal_minor,gst_minor,total_minor,created_at,reviewed_atfromdocumentswhereid=?")
}

func purchaseReportingRootOwnerIsDocumentReplayQuery(query string) bool {
	return strings.Contains(query, "/* BAS purchase owner body document replay */") ||
		purchaseReportingRootOwnerIsLegacyDocumentSelect(purchaseReportingRootOwnerCompactSQL(query))
}

func purchaseReportingRootOwnerIsBodyStatement(query string) bool {
	lower := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(lower, "/* bas purchase owner body ") {
		return true
	}
	if strings.HasPrefix(lower, "select count(") || strings.Contains(lower, "total(payload_bytes)") ||
		strings.Contains(lower, "length(cast(") {
		return false
	}
	for _, marker := range []string{"_proto", "original_bytes", "extracted_text", "retained_input_proto"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func purchaseReportingRootOwnerState(t *testing.T, tx *sqlcipher.Transaction, mode string) map[string]any {
	t.Helper()
	var state map[string]any
	if mode == "full-coverage" {
		state = coverageStateCounts(t, tx)
	} else {
		state = reportingStateCounts(t, tx)
	}
	state["SELECT total_changes()"] = postScalar(t, tx, "SELECT total_changes()")
	return state
}

func purchaseReportingRootOwnerCapture(t *testing.T, repo *purchases.Repository, mode string) (bool, error) {
	t.Helper()
	ctx := context.Background()
	if mode == "full-coverage" {
		capture, err := repo.CaptureCoverageForBAS(ctx, postingOrg)
		return capture == nil, err
	}
	capture, err := repo.CapturePostingsForReporting(ctx, postingOrg)
	return capture == nil, err
}

func purchaseReportingRootOwnerAssertAtomicRejection(t *testing.T, probe *purchaseReportingRootOwnerAtomicProbe, nilCapture bool, err error, afterRead runtime.MemStats) {
	t.Helper()
	allocation := afterRead.TotalAlloc - probe.beforeRead.TotalAlloc
	errorBytes := 0
	errorPreview := ""
	if err != nil {
		text := err.Error()
		errorBytes = len(text)
		if len(text) > 240 {
			text = text[:240]
		}
		errorPreview = text
	}
	if probe.hookCount != 1 {
		t.Fatalf("raw %s hook count=%d, want exactly one; capture_nil=%t error_bytes=%d error=%q",
			probe.stage, probe.hookCount, nilCapture, errorBytes, errorPreview)
	}
	if !nilCapture || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("late %s owner drift returned capture_nil=%t error_bytes=%d error=%q; want nil capture and ErrRepository",
			probe.stage, nilCapture, errorBytes, errorPreview)
	}
	if errorBytes >= 512 || allocation >= 2*1024*1024 {
		t.Fatalf("late %s owner guard copied a rejected body: error_bytes=%d allocation=%d error=%q",
			probe.stage, errorBytes, allocation, errorPreview)
	}
	if probe.laterBodies != 0 {
		t.Fatalf("late %s rejection attempted %d subsequent body queries", probe.stage, probe.laterBodies)
	}
	if probe.stateAfterMutation == nil ||
		!reflect.DeepEqual(probe.stateAfterMutation, purchaseReportingRootOwnerState(t, probe.tx, probe.mode)) {
		t.Fatal("owner rejection changed retained state or total_changes after the fixture mutation")
	}
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) ||
		!reflect.DeepEqual(probe.triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("owner rejection left trigger definitions different from the post-mutation baseline")
	}
	if !probe.tx.Authenticated() {
		t.Fatal("owner rejection ended the caller-owned encrypted transaction")
	}
	if got := postScalar(t, probe.tx, "PRAGMA ignore_check_constraints"); got != 0 {
		t.Fatalf("owner rejection left ignore_check_constraints=%d, want 0", got)
	}
}

func TestPurchaseReportingRootOwnerLatePostingBodiesAreAtomic(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		for _, column := range []string{"posting_proto", "reviewed_source_proto"} {
			t.Run(mode+"/"+column, func(t *testing.T) {
				ctx := context.Background()
				workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
				tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
				posted := postReportingPurchase(t, f, "OWNER-ROOT-POSTING", purchaseReportingDate(2026, 8, 10), 10000, 1000)
				if posted == nil || posted.Posting == nil {
					t.Fatal("PostReviewed fixture returned no posting")
				}
				probe := &purchaseReportingRootOwnerAtomicProbe{
					CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode,
					stage: "posting-root", ownerTable: "purchase_postings_v1", ownerID: posted.Posting.Id,
					bodyTable: "purchase_postings_v1", bodyColumn: column, bodyID: posted.Posting.Id,
				}
				nilCapture, err := purchaseReportingRootOwnerCapture(t, f.repo(probe), mode)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				purchaseReportingRootOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
			})
		}
	}
}

func TestPurchaseReportingRootOwnerLateDocumentBodiesAreAtomic(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		for _, column := range []string{"original_bytes", "extracted_text"} {
			t.Run(mode+"/"+column, func(t *testing.T) {
				ctx := context.Background()
				workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
				tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
				targetID := f.document.Id
				if mode == "modern-capture" {
					posted := postReportingPurchase(t, f, "OWNER-DOCUMENT-ROOT", purchaseReportingDate(2026, 8, 10), 10000, 1000)
					if posted == nil || posted.Posting == nil {
						t.Fatal("PostReviewed fixture returned no posting")
					}
					targetID = posted.Posting.DocumentId
				}
				probe := &purchaseReportingRootOwnerAtomicProbe{
					CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode,
					stage: "document-root", ownerTable: "documents", ownerID: targetID,
					bodyTable: "documents", bodyColumn: column, bodyID: targetID,
				}
				nilCapture, err := purchaseReportingRootOwnerCapture(t, f.repo(probe), mode)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				purchaseReportingRootOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
			})
		}
	}
}

func TestPurchaseReportingRootOwnerEmptyReservationBodySentinelIsAtomic(t *testing.T) {
	cases := []struct {
		name, mode, ownerTable, column string
	}{
		{"full/posting_owner", "full-coverage", "purchase_postings_v1", "posting_proto"},
		{"full/document_owner", "full-coverage", "documents", "extracted_text"},
		{"modern/posting_owner", "modern-capture", "purchase_postings_v1", "posting_proto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			initialDocumentID := f.document.Id
			posted := postReportingPurchase(t, f, "OWNER-EMPTY-ROOT", purchaseReportingDate(2026, 8, 10), 0, 0)
			if posted == nil || posted.Posting == nil {
				t.Fatal("zero-total PostReviewed fixture returned no posting")
			}
			purchaseReportingRootOwnerIsolateForeignGraph(t, f, initialDocumentID, posted.Posting.DocumentId, posted.Posting.Id)
			baseline, baselineErr := purchaseReportingRootOwnerCaptureValue(t, f.repo(tx), tc.mode)
			if baselineErr != nil || !purchaseReportingRootOwnerIsEmpty(baseline, tc.mode) {
				t.Fatalf("foreign graph baseline empty capture=%t err=%v", purchaseReportingRootOwnerIsEmpty(baseline, tc.mode), baselineErr)
			}
			targetID := posted.Posting.Id
			if tc.ownerTable == "documents" {
				targetID = posted.Posting.DocumentId
			}
			probe := &purchaseReportingRootOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: tc.mode,
				stage: "posting-root", ownerTable: tc.ownerTable, ownerID: targetID,
				bodyTable: tc.ownerTable, bodyColumn: tc.column, bodyID: targetID,
			}
			nilCapture, err := purchaseReportingRootOwnerCapture(t, f.repo(probe), tc.mode)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseReportingRootOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
		})
	}
}

func purchaseReportingRootOwnerCaptureValue(t *testing.T, repo *purchases.Repository, mode string) (any, error) {
	t.Helper()
	ctx := context.Background()
	if mode == "full-coverage" {
		return repo.CaptureCoverageForBAS(ctx, postingOrg)
	}
	return repo.CapturePostingsForReporting(ctx, postingOrg)
}

func purchaseReportingRootOwnerIsEmpty(capture any, mode string) bool {
	if mode == "full-coverage" {
		value, ok := capture.(*purchases.PurchaseCoverageReportingCapture)
		return ok && value != nil && value.RootCount == 0 && len(value.Postings) == 0 && len(value.Documents) == 0
	}
	value, ok := capture.(*purchases.PostingReportingCapture)
	return ok && value != nil && len(value.Postings) == 0 && len(value.Documents) == 0
}

func purchaseReportingRootOwnerIsolateForeignGraph(t *testing.T, f *postingFixture, initialDocumentID, postedDocumentID, postingID string) {
	t.Helper()
	const (
		foreignPostingID  = "018f0000-0000-7000-8000-000000009991"
		foreignDocumentID = "018f0000-0000-7000-8000-000000009992"
		foreignAccountID  = "018f0000-0000-7000-8000-000000009993"
	)
	mutatePurchaseBASScalar(t, f, "documents",
		"UPDATE documents SET organisation_id='foreign-owner' WHERE id IN (?,?)", initialDocumentID, postedDocumentID)
	mutatePurchaseBASScalar(t, f, "accounts",
		"UPDATE accounts SET organisation_id='foreign-owner' WHERE id=?", postingExpense)
	mutatePurchaseBASScalar(t, f, "purchase_posting_allocations_v1",
		"UPDATE purchase_posting_allocations_v1 SET organisation_id='foreign-owner',posting_id=?,account_id=? WHERE posting_id=?",
		foreignPostingID, foreignAccountID, postingID)
	mutatePurchaseBASScalar(t, f, "purchase_tax_evidence_v1",
		"UPDATE purchase_tax_evidence_v1 SET organisation_id='foreign-owner',posting_id=?,document_id=? WHERE posting_id=?",
		foreignPostingID, foreignDocumentID, postingID)
	mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1",
		"UPDATE purchase_accounting_events_v1 SET organisation_id='foreign-owner',posting_id=?,document_id=?,scope_id=?",
		foreignPostingID, foreignDocumentID, foreignPostingID)
	mutatePurchaseBASScalar(t, f, "purchase_postings_v1",
		"UPDATE purchase_postings_v1 SET organisation_id='foreign-owner',document_id=? WHERE id=?",
		foreignDocumentID, postingID)
}

func TestPurchaseReportingRootOwnerDocumentReplayMasksEachSelectedBody(t *testing.T) {
	cases := []struct {
		name, mode, ownerTable, column string
	}{
		{"full/posting_owner/extracted_text", "full-coverage", "purchase_postings_v1", "extracted_text"},
		{"modern/posting_owner/original_bytes", "modern-capture", "purchase_postings_v1", "original_bytes"},
		{"modern/document_owner/extracted_text", "modern-capture", "documents", "extracted_text"},
		{"full/document_owner/original_bytes", "full-coverage", "documents", "original_bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
			tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
			posted := postReportingPurchase(t, f, "OWNER-DOCUMENT-REPLAY", purchaseReportingDate(2026, 8, 10), 10000, 1000)
			if posted == nil || posted.Posting == nil {
				t.Fatal("PostReviewed fixture returned no posting")
			}
			ownerID := posted.Posting.DocumentId
			if tc.ownerTable == "purchase_postings_v1" {
				ownerID = posted.Posting.Id
			}
			probe := &purchaseReportingRootOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: tc.mode,
				stage: "document-replay", ownerTable: tc.ownerTable, ownerID: ownerID,
				bodyTable: "documents", bodyColumn: tc.column, bodyID: posted.Posting.DocumentId,
			}
			nilCapture, err := purchaseReportingRootOwnerCapture(t, f.repo(probe), tc.mode)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseReportingRootOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
			if tc.ownerTable == "purchase_postings_v1" && probe.unpostedFallbackReads != 0 {
				t.Fatalf("posting-owner document replay fell back to unposted discovery %d times", probe.unpostedFallbackReads)
			}
		})
	}
}

func TestPurchaseReportingRootOwnerPostingReplayRejectsBeforeUnpostedFallback(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		for _, column := range []string{"posting_proto", "reviewed_source_proto"} {
			t.Run(mode+"/"+column, func(t *testing.T) {
				ctx := context.Background()
				workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
				tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_CASH)
				posted := postReportingPurchase(t, f, "OWNER-POSTING-REPLAY", purchaseReportingDate(2026, 8, 10), 10000, 1000)
				if posted == nil || posted.Posting == nil {
					t.Fatal("PostReviewed fixture returned no posting")
				}
				probe := &purchaseReportingRootOwnerAtomicProbe{
					CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode,
					stage: "posting-replay", ownerTable: "purchase_postings_v1", ownerID: posted.Posting.Id,
					bodyTable: "purchase_postings_v1", bodyColumn: column, bodyID: posted.Posting.Id,
				}
				nilCapture, err := purchaseReportingRootOwnerCapture(t, f.repo(probe), mode)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				purchaseReportingRootOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
				if probe.documentReplayReads == 0 {
					t.Fatal("posting replay was not preceded by a successful document replay")
				}
				if probe.unpostedFallbackReads != 0 {
					t.Fatalf("invalid posting replay owner fell back to unposted discovery %d times", probe.unpostedFallbackReads)
				}
			})
		}
	}
}

// The selected body's own TEXT owner remains valid. A malformed owner in the
// other root table must still withhold these cells in this actual statement.
func TestPurchaseReportingRootOwnerCrossTableBodyGuardsAreAtomic(t *testing.T) {
	cases := []struct{ mode, stage, ownerTable, bodyTable, bodyColumn string }{
		{"full-coverage", "posting-root", "documents", "purchase_postings_v1", "posting_proto"},
		{"full-coverage", "posting-root", "documents", "purchase_postings_v1", "reviewed_source_proto"},
		{"full-coverage", "document-root", "purchase_postings_v1", "documents", "original_bytes"},
		{"full-coverage", "document-root", "purchase_postings_v1", "documents", "extracted_text"},
		{"modern-capture", "document-root", "purchase_postings_v1", "documents", "original_bytes"},
		{"modern-capture", "document-root", "purchase_postings_v1", "documents", "extracted_text"},
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/"+tc.stage+"/"+tc.bodyColumn, func(t *testing.T) {
			tx, fixture := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			posted := postReportingPurchase(t, fixture, "OWNER-CROSS-BODY", purchaseReportingDate(2026, 8, 10), 10000, 1000)
			if posted == nil || posted.Posting == nil {
				t.Fatal("real writer returned no posting")
			}
			ownerID, bodyID := posted.Posting.Id, posted.Posting.DocumentId
			if tc.ownerTable == "documents" {
				ownerID = posted.Posting.DocumentId
			}
			if tc.bodyTable == "purchase_postings_v1" {
				bodyID = posted.Posting.Id
			}
			probe := &purchaseReportingRootOwnerAtomicProbe{CommandSQLExecutor: tx,
				t: t, fixture: fixture, tx: tx, mode: tc.mode, stage: tc.stage,
				ownerTable: tc.ownerTable, ownerID: ownerID,
				bodyTable: tc.bodyTable, bodyColumn: tc.bodyColumn, bodyID: bodyID}
			nilCapture, err := purchaseReportingRootOwnerCapture(t, fixture.repo(probe), tc.mode)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchaseReportingRootOwnerAssertAtomicRejection(t, probe, nilCapture, err, after)
		})
	}
}
