//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package purchases_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/documents"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

const (
	purchaseBASDocumentStateReadMarker    = "/* BAS document states */"
	purchaseBASDocumentStateSummaryMarker = "/* BAS document state summary */"
	purchaseBASDocumentStateMaxRows       = int64(16384)
	purchaseBASDocumentStateMaxBytes      = int64(8 * 1024 * 1024)
)

type purchaseBASDocumentStateProbe struct {
	*sqlcipher.Transaction
	queries, summaryReads, stateReads, bodyReads int
	beforeStateRead                              func() error
	afterStateQuery                              func()
}

func (probe *purchaseBASDocumentStateProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	probe.queries++
	summary := strings.Contains(query, purchaseBASDocumentStateSummaryMarker)
	stateRead := strings.Contains(query, purchaseBASDocumentStateReadMarker)
	if summary {
		probe.summaryReads++
	}
	if stateRead {
		probe.stateReads++
		if probe.beforeStateRead != nil {
			before := probe.beforeStateRead
			probe.beforeStateRead = nil
			if err := before(); err != nil {
				return nil, err
			}
		}
	}
	rows, err := probe.Transaction.QueryContext(ctx, query, args...)
	if stateRead && err == nil && probe.afterStateQuery != nil {
		after := probe.afterStateQuery
		probe.afterStateQuery = nil
		after()
	}
	if err != nil || rows == nil {
		return rows, err
	}
	if columns, err := rows.Columns(); err == nil {
		for _, column := range columns {
			if purchaseCoverageBodyResultColumn(column) {
				probe.bodyReads++
				break
			}
		}
	}
	return rows, err
}

func purchaseBASDocumentStateFixture(t *testing.T) (*sqlcipher.Transaction, *postingFixture) {
	t.Helper()
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx, newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
}

func assertPurchaseBASDocumentStateCharge(t *testing.T, tx *sqlcipher.Transaction, got *purchases.BASDocumentStateRead) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), `SELECT d.id,d.organisation_id,d.version,d.status,d.sha256,
		(SELECT p.id FROM purchase_postings_v1 AS p WHERE p.organisation_id=d.organisation_id AND p.document_id=d.id)
		FROM documents AS d WHERE d.organisation_id=? ORDER BY d.id`, postingOrg)
	if err != nil {
		t.Fatal(err)
	}
	var count, goBytes int64
	for rows.Next() {
		var id, org, status string
		var version int64
		var hash []byte
		var postingID sql.NullString
		if err := rows.Scan(&id, &org, &version, &status, &hash, &postingID); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if count >= int64(len(got.Documents)) {
			_ = rows.Close()
			t.Fatal("state projection omitted a physical document row")
		}
		state := got.Documents[count]
		wantStatus := tammyv1.DocumentStatus_DOCUMENT_STATUS_UNSPECIFIED
		switch status {
		case "NEEDS_REVIEW":
			wantStatus = tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW
		case "REVIEWED":
			wantStatus = tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED
		default:
			_ = rows.Close()
			t.Fatalf("unexpected status in valid charge fixture: %q", status)
		}
		if state.ID != id || state.OrganisationID != org || state.Version != uint64(version) ||
			state.Status != wantStatus || !bytes.Equal(state.ContentHash, hash) {
			_ = rows.Close()
			t.Fatalf("state row %d differs from selected SQL cells: got=%#v", count, state)
		}
		var wantPosting *string
		if postingID.Valid {
			value := postingID.String
			wantPosting = &value
		}
		if !reflect.DeepEqual(state.PostingID, wantPosting) {
			_ = rows.Close()
			t.Fatalf("document %s posting id=%v, want %v", id, state.PostingID, wantPosting)
		}
		goBytes += 512 + int64(len([]byte(id))+len([]byte(org))+len([]byte(status))+len(hash))
		if postingID.Valid {
			goBytes += int64(len([]byte(postingID.String)))
		}
		count++
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	wantCount := postScalar(t, tx, `SELECT count(*) FROM documents WHERE organisation_id=?`, postingOrg)
	wantSQLBytes := postScalar(t, tx, `SELECT COALESCE(SUM(512+
		length(CAST(d.id AS BLOB))+length(CAST(d.organisation_id AS BLOB))+
		length(CAST(d.status AS BLOB))+length(d.sha256)+COALESCE((
			SELECT length(CAST(p.id AS BLOB)) FROM purchase_postings_v1 AS p
			WHERE p.organisation_id=d.organisation_id AND p.document_id=d.id),0)),0)
		FROM documents AS d WHERE d.organisation_id=?`, postingOrg)
	if count != wantCount || count != int64(len(got.Documents)) || goBytes != wantSQLBytes ||
		got.Summary != (purchases.BASDocumentStateSummary{OrganisationID: postingOrg, DocumentCount: count, StateBytes: goBytes}) {
		t.Fatalf("state count/charge Go=%d/%d SQL=%d/%d summary=%#v", count, goBytes, wantCount, wantSQLBytes, got.Summary)
	}
}

func TestPurchaseBASDocumentStatesEmptyAndInvalidReservations(t *testing.T) {
	ctx := context.Background()
	tx, f := emptyPurchaseBASScalarFixture(t)
	defer tx.Rollback()
	probe := &purchaseBASDocumentStateProbe{Transaction: tx}
	repo := f.repo(probe)
	before := purchaseBASScalarState(t, tx)

	summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || summary == nil || *summary != (purchases.BASDocumentStateSummary{OrganisationID: postingOrg}) {
		t.Fatalf("empty state summary=%#v %v", summary, err)
	}
	read, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, 0, 0)
	if err != nil || read == nil || read.Summary != *summary || len(read.Documents) != 0 {
		t.Fatalf("empty state read=%#v %v", read, err)
	}
	queries := probe.queries
	for _, budgets := range [][2]int64{{-1, 0}, {0, -1}} {
		if got, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, budgets[0], budgets[1]); got != nil || !errors.Is(err, purchases.ErrRepository) {
			t.Fatalf("negative allowance returned %#v, %v", got, err)
		}
		if probe.queries != queries {
			t.Fatal("negative document-state allowance reached SQL")
		}
	}
	negativeExpected := *summary
	negativeExpected.StateBytes = -1
	if got, err := repo.ReadBASDocumentStates(ctx, postingOrg, negativeExpected, 0, 0); got != nil || !errors.Is(err, purchases.ErrRepository) || probe.queries != queries {
		t.Fatalf("negative expected reservation=%#v %v queries=%d want=%d", got, err, probe.queries, queries)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := repo.SummarizeBASDocumentStates(cancelled, postingOrg); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled state summary=%#v %v", got, err)
	}
	if got, err := repo.ReadBASDocumentStates(cancelled, postingOrg, *summary, 0, 0); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled state read=%#v %v", got, err)
	}
	if probe.stateReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("empty/canceled state calls read bodies, wrote retained state, or ended caller transaction")
	}
}

func TestPurchaseBASDocumentStatesMatchRealWritersAndScalarKeys(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseBASDocumentStateFixture(t)
	firstDocument := f.document
	firstRequest := f.request
	posted, err := f.repo(tx).PostReviewed(ctx, firstRequest)
	if err != nil {
		t.Fatalf("promote reviewed source: %v", err)
	}

	f.addDocument("Retained legacy source", "STATE-LEGACY", 0, 0)
	legacySource := f.document
	legacy := legacyRequest(t, f, tammyv1.LegacyPurchaseDisposition_LEGACY_PURCHASE_DISPOSITION_NONCREDITABLE)
	retainedEvidence := f.document
	if _, err := legacyRepo(t, f, tx).ResolveLegacy(ctx, legacy); err != nil {
		t.Fatalf("retain real legacy evidence: %v", err)
	}
	f.addDocumentReview("Review transition", "STATE-TRANSITION", 0, 0, false)
	transition := f.document
	transitionSummary, err := f.repo(tx).SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || transitionSummary == nil {
		t.Fatalf("pre-review transition summary=%#v %v", transitionSummary, err)
	}
	documentRepo, err := documents.NewRepository(tx)
	if err != nil {
		t.Fatal(err)
	}
	transitioned, err := documentRepo.SaveReview(ctx, transition.Id, transition.Version, transition.Candidate, f.now)
	if err != nil || transitioned.Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		transitioned.Version != transition.Version+1 || !bytes.Equal(transitioned.Sha256, transition.Sha256) {
		t.Fatalf("real review transition=%#v %v", transitioned, err)
	}
	if stale, err := f.repo(tx).ReadBASDocumentStates(ctx, postingOrg, *transitionSummary, transitionSummary.DocumentCount, transitionSummary.StateBytes); stale != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("stale pre-review state reservation=%#v %v", stale, err)
	}
	transitionSummary, err = f.repo(tx).SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || transitionSummary == nil {
		t.Fatalf("post-review transition summary=%#v %v", transitionSummary, err)
	}
	transitionRead, err := f.repo(tx).ReadBASDocumentStates(ctx, postingOrg, *transitionSummary, transitionSummary.DocumentCount, transitionSummary.StateBytes)
	if err != nil || transitionRead == nil {
		t.Fatalf("post-review transition read=%#v %v", transitionRead, err)
	}
	var transitionedState *purchases.BASDocumentState
	for index := range transitionRead.Documents {
		if transitionRead.Documents[index].ID == transition.Id {
			transitionedState = &transitionRead.Documents[index]
			break
		}
	}
	if transitionedState == nil || transitionedState.Status != transitioned.Status || transitionedState.Version != transitioned.Version ||
		!bytes.Equal(transitionedState.ContentHash, transition.Sha256) {
		t.Fatalf("transitioned state=%#v, full writer result=%#v", transitionedState, transitioned)
	}
	transitionPosting, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatalf("promote the just-reviewed document: %v", err)
	}
	f.addDocumentReview("Unreviewed state", "STATE-NEEDS-REVIEW", 0, 0, false)
	needsReview := f.document

	before := purchaseBASScalarState(t, tx)
	probe := &purchaseBASDocumentStateProbe{Transaction: tx}
	repo := f.repo(probe)
	summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || summary == nil || summary.DocumentCount != 5 || summary.StateBytes <= 0 {
		t.Fatalf("complete real-writer summary=%#v %v", summary, err)
	}
	read, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, summary.DocumentCount, summary.StateBytes)
	if err != nil || read == nil || read.Summary != *summary || len(read.Documents) != 5 {
		t.Fatalf("complete real-writer read=%#v %v", read, err)
	}
	for _, budget := range []struct {
		maximumDocs, maximumBytes int64
		resource                  string
		limit                     int64
	}{
		{summary.DocumentCount - 1, summary.StateBytes, "document state records", summary.DocumentCount - 1},
		{summary.DocumentCount, summary.StateBytes - 1, "document state bytes", summary.StateBytes - 1},
	} {
		partial, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, budget.maximumDocs, budget.maximumBytes)
		if partial != nil {
			t.Fatal("short state allowance returned a partial census")
		}
		assertPurchaseCoverageBound(t, err, budget.resource, budget.limit)
	}
	assertPurchaseBASDocumentStateCharge(t, tx, read)
	if probe.stateReads != 1 || probe.bodyReads != 0 {
		t.Fatalf("document-state read queries=%d body reads=%d; want one metadata read and no bodies", probe.stateReads, probe.bodyReads)
	}

	byID := make(map[string]purchases.BASDocumentState, len(read.Documents))
	for index, state := range read.Documents {
		if index > 0 && read.Documents[index-1].ID >= state.ID {
			t.Fatalf("document states are not in exact ID order: %s then %s", read.Documents[index-1].ID, state.ID)
		}
		byID[state.ID] = state
	}
	for _, want := range []*tammyv1.Document{firstDocument, legacySource, retainedEvidence, transitioned, needsReview} {
		state, ok := byID[want.Id]
		if !ok {
			t.Fatalf("complete state census omitted %s", want.Id)
		}
		documentsRepo, err := documents.NewRepository(tx)
		if err != nil {
			t.Fatal(err)
		}
		full, err := documentsRepo.Get(ctx, want.Id)
		if err != nil || full.Status != state.Status || full.Version != state.Version || !bytes.Equal(full.Sha256, state.ContentHash) {
			t.Fatalf("document state differs from full owner %s: state=%#v full=%#v err=%v", want.Id, state, full, err)
		}
	}
	if byID[firstDocument.Id].Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		byID[legacySource.Id].Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		byID[retainedEvidence.Id].Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		byID[transition.Id].Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_REVIEWED ||
		byID[needsReview.Id].Status != tammyv1.DocumentStatus_DOCUMENT_STATUS_NEEDS_REVIEW ||
		byID[firstDocument.Id].PostingID == nil || *byID[firstDocument.Id].PostingID != posted.Posting.Id ||
		byID[transition.Id].PostingID == nil || *byID[transition.Id].PostingID != transitionPosting.Posting.Id ||
		byID[legacySource.Id].PostingID != nil || byID[retainedEvidence.Id].PostingID != nil || byID[needsReview.Id].PostingID != nil {
		t.Fatalf("review-state/promoted/evidence coverage was filtered or relinked: %#v", byID)
	}

	scalarSummary, err := repo.SummarizeBASKeys(ctx, postingOrg)
	if err != nil || scalarSummary == nil {
		t.Fatalf("scalar summary=%#v %v", scalarSummary, err)
	}
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *scalarSummary, scalarSummary.ScalarKeyCount, scalarSummary.ScalarBytes)
	if err != nil {
		t.Fatalf("physical scalar read: %v", err)
	}
	for id, state := range byID {
		key := purchaseBASScalarFindKey(t, keys, purchases.BASScalarDocument, id)
		if key.OrganisationID != state.OrganisationID || key.ObjectVersion == nil || *key.ObjectVersion != state.Version ||
			!bytes.Equal(key.SourceHash, state.ContentHash) || key.IsRoot != (state.PostingID == nil) {
			t.Fatalf("document state differs from exact physical scalar key %s: state=%#v key=%#v", id, state, key)
		}
	}
	if !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
		t.Fatal("metadata/scalar reads wrote retained state or ended caller transaction")
	}
}

// These rows are structural metadata fixtures. They deliberately do not prove
// source bytes or purchase validity; all semantic checks use real document writers.
func addPurchaseBASDocumentStateCountRows(t *testing.T, f *postingFixture, count, idBase int64) {
	t.Helper()
	query := `WITH RECURSIVE digit(n) AS (
			SELECT 0 UNION ALL SELECT n+1 FROM digit WHERE n<127
		), numbers(n) AS (
			SELECT high.n*128+low.n FROM digit AS high CROSS JOIN digit AS low
			WHERE high.n*128+low.n < ?
		)
		INSERT INTO documents(
			id,operation_key,organisation_id,version,status,source_display_name,mime_type,
			byte_length,sha256,original_bytes,extracted_text,supplier_name,invoice_number,
			document_date,subtotal_minor,gst_minor,total_minor,created_by_user_id,created_at,reviewed_at
		)
		SELECT printf('018f0000-0000-7000-8000-%012d',?+numbers.n),
			printf('018f0000-0000-7000-8000-%012d',?+numbers.n),?,1,'NEEDS_REVIEW',
			'state-count-only.pdf','application/pdf',1,CAST(printf('%032x',?+numbers.n) AS BLOB),
			X'61','', 'State count fixture',printf('COUNT-%06d',numbers.n),NULL,0,0,0,?,?,NULL
		FROM numbers`
	mutatePurchaseBASScalar(t, f, "documents", query, count, idBase, idBase+50000, postingOrg, idBase, testkit.ActorUserID, f.now.Format(time.RFC3339Nano))
}

func TestPurchaseBASDocumentStatesAcceptMoreThanLegacyRootLimit(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseBASDocumentStateFixture(t)
	addPurchaseBASDocumentStateCountRows(t, f, 1000, 300000)
	probe := &purchaseBASDocumentStateProbe{Transaction: tx}
	repo := f.repo(probe)
	summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || summary == nil || summary.DocumentCount != 1001 {
		t.Fatalf("metadata census was capped like candidate roots: %#v %v", summary, err)
	}
	read, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, summary.DocumentCount, summary.StateBytes)
	if err != nil || read == nil || len(read.Documents) != 1001 {
		t.Fatalf("1,001-row metadata census=%#v %v", read, err)
	}
	assertPurchaseBASDocumentStateCharge(t, tx, read)
	if probe.stateReads != 1 || probe.bodyReads != 0 {
		t.Fatalf("1,001-row read queries=%d body reads=%d", probe.stateReads, probe.bodyReads)
	}
}

func TestPurchaseBASDocumentStatesEnforceAggregatePhysicalCaps(t *testing.T) {
	for _, test := range []struct {
		name, resource string
		rows           int64
		limit          int64
	}{
		{"bytes_at_record_capacity", "document state bytes", 16383, purchaseBASDocumentStateMaxBytes},
		{"records_over_capacity", "document state records", 16384, purchaseBASDocumentStateMaxRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, f := purchaseBASDocumentStateFixture(t)
			addPurchaseBASDocumentStateCountRows(t, f, test.rows, 600000)
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			summary, err := f.repo(probe).SummarizeBASDocumentStates(context.Background(), postingOrg)
			if summary != nil {
				t.Fatalf("over-cap metadata summary=%#v", summary)
			}
			assertPurchaseCoverageBound(t, err, test.resource, test.limit)
			if probe.stateReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("aggregate cap rejection read state/body cells or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesEnforceSeparateLocalCapsBeforeSQL(t *testing.T) {
	tx, f := emptyPurchaseBASScalarFixture(t)
	defer tx.Rollback()
	probe := &purchaseBASDocumentStateProbe{Transaction: tx}
	repo := f.repo(probe)
	for _, test := range []struct {
		name, resource string
		expected       purchases.BASDocumentStateSummary
		maximumDocs    int64
		maximumBytes   int64
		limit          int64
	}{
		{"records", "document state records", purchases.BASDocumentStateSummary{OrganisationID: postingOrg, DocumentCount: purchaseBASDocumentStateMaxRows + 1, StateBytes: purchaseBASDocumentStateMaxBytes}, purchaseBASDocumentStateMaxRows + 100, purchaseBASDocumentStateMaxBytes, purchaseBASDocumentStateMaxRows},
		{"bytes", "document state bytes", purchases.BASDocumentStateSummary{OrganisationID: postingOrg, DocumentCount: 13400, StateBytes: purchaseBASDocumentStateMaxBytes + 1}, purchaseBASDocumentStateMaxRows, purchaseBASDocumentStateMaxBytes + 100, purchaseBASDocumentStateMaxBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := repo.ReadBASDocumentStates(context.Background(), postingOrg, test.expected, test.maximumDocs, test.maximumBytes)
			if got != nil {
				t.Fatal("over-cap expected reservation returned a projection")
			}
			assertPurchaseCoverageBound(t, err, test.resource, test.limit)
			if probe.queries != 0 {
				t.Fatal("local cap rejection queried SQL")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesRejectMalformedOwnerCells(t *testing.T) {
	for _, test := range []struct {
		name, query string
		args        []any
	}{
		{"status", `UPDATE documents SET status='PENDING' WHERE id=?`, nil},
		{"version_storage", `UPDATE documents SET version=1.5 WHERE id=?`, nil},
		{"raw_hash", `UPDATE documents SET sha256=zeroblob(31) WHERE id=?`, nil},
		{"identity_storage", `UPDATE documents SET id=zeroblob(36) WHERE id=?`, nil},
		{"identity_length", `UPDATE documents SET id=? WHERE id=?`, []any{strings.Repeat("x", 35)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, f := purchaseBASDocumentStateFixture(t)
			args := append([]any(nil), test.args...)
			args = append(args, f.document.Id)
			mutatePurchaseBASScalar(t, f, "documents", test.query, args...)
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			got, err := f.repo(probe).SummarizeBASDocumentStates(context.Background(), postingOrg)
			if got != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Fatalf("malformed state summary=%#v %v", got, err)
			}
			if probe.stateReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("malformed summary copied state/body cells or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesRejectPostingOwnerEdges(t *testing.T) {
	for _, edge := range []string{"missing_document", "foreign_posting", "foreign_document"} {
		t.Run(edge, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseBASDocumentStateFixture(t)
			posted, err := f.repo(tx).PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			switch edge {
			case "missing_document":
				mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `UPDATE purchase_postings_v1 SET document_id='missing-document' WHERE id=?`, posted.Posting.Id)
			case "foreign_posting":
				mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `UPDATE purchase_postings_v1 SET organisation_id='foreign-owner' WHERE id=?`, posted.Posting.Id)
			case "foreign_document":
				mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET organisation_id='foreign-owner' WHERE id=?`, posted.Posting.DocumentId)
			}
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			got, err := f.repo(probe).SummarizeBASDocumentStates(ctx, postingOrg)
			if got != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Fatalf("invalid posting/document owner summary=%#v %v", got, err)
			}
			if probe.stateReads != 0 || probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("invalid owner summary read state/body cells or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesRejectNoncanonicalDomainIDsAfterStorageGuard(t *testing.T) {
	for _, owner := range []string{"document", "posting"} {
		t.Run(owner, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseBASDocumentStateFixture(t)
			table, id := "documents", f.document.Id
			if owner == "posting" {
				posted, err := f.repo(tx).PostReviewed(ctx, f.request)
				if err != nil {
					t.Fatal(err)
				}
				table, id = "purchase_postings_v1", posted.Posting.Id
			}
			// Preserve exact TEXT/byte shape and reservation so rejection must
			// reach domain-ID validation after the guarded owner projection.
			mutatePurchaseBASScalar(t, f, table, "UPDATE "+table+" SET id=? WHERE id=?", strings.Repeat("z", 36), id)
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			repo := f.repo(probe)
			summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
			if err != nil || summary == nil {
				t.Fatalf("bounded noncanonical ID summary=%#v %v", summary, err)
			}
			read, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, summary.DocumentCount, summary.StateBytes)
			if read != nil || !errors.Is(err, purchases.ErrRepository) || probe.stateReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatalf("noncanonical %s ID read=%#v err=%v queries=%d bodies=%d", owner, read, err, probe.stateReads, probe.bodyReads)
			}
		})
	}
}

func TestPurchaseBASDocumentStatesRejectSameByteBlobOrganisationOwnership(t *testing.T) {
	for _, owner := range []string{"document", "orphan_posting"} {
		t.Run(owner, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseBASDocumentStateFixture(t)
			if owner == "document" {
				mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET organisation_id=CAST(organisation_id AS BLOB) WHERE id=?`, f.document.Id)
			} else {
				posted, err := f.repo(tx).PostReviewed(ctx, f.request)
				if err != nil {
					t.Fatal(err)
				}
				mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `UPDATE purchase_postings_v1 SET organisation_id=CAST(organisation_id AS BLOB),document_id='018f0000-0000-7000-8000-999999999999' WHERE id=?`, posted.Posting.Id)
			}
			before := purchaseBASScalarState(t, tx)
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			repo := f.repo(probe)
			summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
			if summary != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("same-byte BLOB %s owner was silently omitted: summary=%#v err=%v", owner, summary, err)
			}
			expected := purchases.BASDocumentStateSummary{OrganisationID: postingOrg}
			if summary != nil {
				expected = *summary
			}
			read, err := repo.ReadBASDocumentStates(ctx, postingOrg, expected, purchaseBASDocumentStateMaxRows, purchaseBASDocumentStateMaxBytes)
			if read != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("same-byte BLOB %s owner returned a partial census: read=%#v err=%v", owner, read, err)
			}
			if probe.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
				t.Fatal("malformed owner read selected a body, wrote, or ended the caller transaction")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesRejectSameByteBlobDocumentReferences(t *testing.T) {
	for _, stage := range []string{"owned_posting", "foreign_posting", "foreign_after_fresh_summary"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			tx, f := purchaseBASDocumentStateFixture(t)
			posted, err := f.repo(tx).PostReviewed(ctx, f.request)
			if err != nil {
				t.Fatal(err)
			}
			owner := postingOrg
			if stage != "owned_posting" {
				owner = "foreign-owner"
			}
			mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `UPDATE purchase_postings_v1 SET organisation_id=?,document_id=CAST(document_id AS BLOB) WHERE id=?`, owner, posted.Posting.Id)
			if stage == "foreign_after_fresh_summary" {
				// Retain a corrupt reference outside the census until the guarded
				// query. Its return does not change the visible document charge.
				if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE document_state_reference_backup AS SELECT * FROM purchase_postings_v1 WHERE id=?`, posted.Posting.Id); err != nil {
					t.Fatal(err)
				}
				mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `DELETE FROM purchase_postings_v1 WHERE id=?`, posted.Posting.Id)
			}
			before := purchaseBASScalarState(t, tx)
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			repo := f.repo(probe)
			summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
			if stage == "foreign_after_fresh_summary" {
				if err != nil || summary == nil || summary.DocumentCount != 1 {
					t.Fatalf("clean reference baseline=%#v %v", summary, err)
				}
				probe.beforeStateRead = func() error {
					mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `INSERT INTO purchase_postings_v1 SELECT * FROM document_state_reference_backup`)
					before = purchaseBASScalarState(t, tx)
					return nil
				}
			} else if summary != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("BLOB document reference was hidden: summary=%#v err=%v", summary, err)
			}
			expected := purchases.BASDocumentStateSummary{OrganisationID: postingOrg}
			if summary != nil {
				expected = *summary
			}
			read, err := repo.ReadBASDocumentStates(ctx, postingOrg, expected, purchaseBASDocumentStateMaxRows, purchaseBASDocumentStateMaxBytes)
			if read != nil || !errors.Is(err, purchases.ErrRepository) {
				t.Errorf("BLOB document reference returned a partial census: read=%#v err=%v", read, err)
			}
			if stage == "foreign_after_fresh_summary" && probe.stateReads != 1 {
				t.Fatal("reference drift did not reach the private guarded header")
			}
			if probe.bodyReads != 0 || !tx.Authenticated() || !reflect.DeepEqual(before, purchaseBASScalarState(t, tx)) {
				t.Fatal("malformed reference read selected bodies, wrote, or ended the caller transaction")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesIgnoreFinancialAndSourceBodies(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseBASDocumentStateFixture(t)
	if _, err := f.repo(tx).PostReviewed(ctx, f.request); err != nil {
		t.Fatal(err)
	}
	summary, err := f.repo(tx).SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || summary == nil {
		t.Fatal(err)
	}
	mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET original_bytes=zeroblob(9437184),extracted_text=zeroblob(9437184),supplier_name=zeroblob(9437184),total_minor=zeroblob(9437184) WHERE id=?`, f.document.Id)
	mutatePurchaseBASScalar(t, f, "purchase_accounting_events_v1", `UPDATE purchase_accounting_events_v1 SET result_proto=zeroblob(9437184)`)
	probe := &purchaseBASDocumentStateProbe{Transaction: tx}
	read, err := f.repo(probe).ReadBASDocumentStates(ctx, postingOrg, *summary, summary.DocumentCount, summary.StateBytes)
	if err != nil || read == nil || read.Summary != *summary || len(read.Documents) != 1 || probe.bodyReads != 0 {
		t.Fatalf("metadata read depended on unrelated source/financial/event bodies: %#v %v body reads=%d", read, err, probe.bodyReads)
	}
}

func TestPurchaseBASDocumentStatesRejectDriftCancellationAndOversizedCells(t *testing.T) {
	for _, stage := range []string{"count_drift", "count_sentinel", "valid_byte_drift", "oversized_sentinel_cell", "early_large_identity", "large_status", "large_hash", "large_version", "large_posting_id", "before_query_cancel", "after_query_cancel"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tx, f := purchaseBASDocumentStateFixture(t)
			var postedID string
			if stage == "large_posting_id" {
				posted, err := f.repo(f.executor).PostReviewed(ctx, f.request)
				if err != nil {
					t.Fatal(err)
				}
				postedID = posted.Posting.Id
			}
			probe := &purchaseBASDocumentStateProbe{Transaction: tx}
			repo := f.repo(probe)
			summary, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
			if err != nil || summary == nil {
				t.Fatal(err)
			}
			var before runtime.MemStats
			switch stage {
			case "count_drift":
				f.addDocumentReview("Count drift", "STATE-COUNT-DRIFT", 0, 0, false)
			case "count_sentinel":
				probe.beforeStateRead = func() error {
					f.addDocumentReview("Count drift", "STATE-COUNT-DRIFT", 0, 0, false)
					return nil
				}
			case "valid_byte_drift":
				probe.beforeStateRead = func() error {
					if _, err := f.repo(f.executor).PostReviewed(ctx, f.request); err != nil {
						return err
					}
					return nil
				}
			case "oversized_sentinel_cell":
				probe.beforeStateRead = func() error {
					f.addDocumentReview("Sentinel", "STATE-SENTINEL", 0, 0, false)
					mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET sha256=zeroblob(9437184) WHERE id=?`, f.document.Id)
					runtime.GC()
					runtime.ReadMemStats(&before)
					return nil
				}
			case "early_large_identity":
				probe.beforeStateRead = func() error {
					// Its leading zeroes sort before the canonical fixture ID. SQL must
					// mask this malformed large TEXT cell before driver copying.
					mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET id=CAST(zeroblob(9437184) AS TEXT) WHERE id=?`, f.document.Id)
					runtime.GC()
					runtime.ReadMemStats(&before)
					return nil
				}
			case "large_status", "large_hash", "large_version", "large_posting_id":
				probe.beforeStateRead = func() error {
					switch stage {
					case "large_status":
						mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET status=zeroblob(9437184) WHERE id=?`, f.document.Id)
					case "large_hash":
						mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET sha256=zeroblob(9437184) WHERE id=?`, f.document.Id)
					case "large_version":
						mutatePurchaseBASScalar(t, f, "documents", `UPDATE documents SET version=zeroblob(9437184) WHERE id=?`, f.document.Id)
					case "large_posting_id":
						mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `UPDATE purchase_postings_v1 SET id=zeroblob(9437184) WHERE id=?`, postedID)
					}
					runtime.GC()
					runtime.ReadMemStats(&before)
					return nil
				}
			case "before_query_cancel":
				probe.beforeStateRead = func() error { cancel(); return nil }
			case "after_query_cancel":
				probe.afterStateQuery = cancel
			}
			read, err := repo.ReadBASDocumentStates(ctx, postingOrg, *summary, summary.DocumentCount, summary.StateBytes)
			if read != nil {
				t.Fatal("drift/cancellation returned partial document states")
			}
			switch stage {
			case "count_sentinel", "oversized_sentinel_cell":
				assertPurchaseCoverageBound(t, err, "document state records", summary.DocumentCount)
			case "count_drift":
				if !errors.Is(err, purchases.ErrRepository) {
					t.Fatalf("fresh count drift error=%v", err)
				}
			case "early_large_identity":
				if err == nil || len(err.Error()) > 1024 {
					t.Fatalf("large malformed identity error=%v", err)
				}
			case "valid_byte_drift", "large_status", "large_hash", "large_version", "large_posting_id":
				if err == nil || len(err.Error()) > 1024 {
					t.Fatalf("%s drift error=%v", stage, err)
				}
			case "before_query_cancel", "after_query_cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation error=%v", err)
				}
			}
			if stage == "oversized_sentinel_cell" || stage == "early_large_identity" || stage == "large_status" ||
				stage == "large_hash" || stage == "large_version" || stage == "large_posting_id" {
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				if after.TotalAlloc-before.TotalAlloc >= 2*1024*1024 {
					t.Fatalf("guard copied rejected %s cell: allocated %d bytes", stage, after.TotalAlloc-before.TotalAlloc)
				}
			}
			wantStateReads := 1
			if stage == "count_drift" {
				wantStateReads = 0
			}
			if probe.stateReads != wantStateReads || probe.bodyReads != 0 || !tx.Authenticated() {
				t.Fatal("failed state read escaped its metadata guard, selected a body, or ended caller transaction")
			}
		})
	}
}

func TestPurchaseBASDocumentStatesRejectOrphanPostingAfterFreshEmptySummary(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseBASDocumentStateFixture(t)
	posted, err := f.repo(tx).PostReviewed(ctx, f.request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE document_state_posting_backup AS SELECT * FROM purchase_postings_v1 WHERE id=?`, posted.Posting.Id); err != nil {
		t.Fatal(err)
	}
	mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `DELETE FROM purchase_postings_v1 WHERE id=?`, posted.Posting.Id)
	mutatePurchaseBASScalar(t, f, "documents", `DELETE FROM documents WHERE id=?`, posted.Posting.DocumentId)
	probe := &purchaseBASDocumentStateProbe{Transaction: tx}
	repo := f.repo(probe)
	empty, err := repo.SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || empty == nil || empty.DocumentCount != 0 || empty.StateBytes != 0 {
		t.Fatalf("valid empty census before orphan drift=%#v %v", empty, err)
	}
	probe.beforeStateRead = func() error {
		// This executes after Read has taken its fresh summary. No owned document
		// row remains, so only the private leading header can report this link.
		mutatePurchaseBASScalar(t, f, "purchase_postings_v1", `INSERT INTO purchase_postings_v1 SELECT * FROM document_state_posting_backup`)
		return nil
	}
	read, err := repo.ReadBASDocumentStates(ctx, postingOrg, *empty, 0, 0)
	if read != nil || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("empty-census orphan posting read=%#v %v", read, err)
	}
	if probe.stateReads != 1 || probe.bodyReads != 0 || !tx.Authenticated() {
		t.Fatal("empty orphan header was not inspected or read a body/ended transaction")
	}
}

func TestPurchaseBASDocumentStatesSurviveEncryptedReopenAndCallerRollback(t *testing.T) {
	ctx := context.Background()
	workspace := testkit.NewEncryptedWorkspaceAtVersion(t, 13)
	tx, err := workspace.Database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	f := newPostingFixture(t, tx, tammyv1.GstBasis_GST_BASIS_NON_CASH)
	baseline, err := f.repo(tx).SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || baseline == nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Database.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	readTx, err := db.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	f.executor = readTx
	probe := &purchaseBASDocumentStateProbe{Transaction: readTx}
	read, err := f.repo(probe).ReadBASDocumentStates(ctx, postingOrg, *baseline, baseline.DocumentCount, baseline.StateBytes)
	if err != nil || read == nil || read.Summary != *baseline || len(read.Documents) != 1 {
		t.Fatalf("reopened states=%#v %v", read, err)
	}
	f.addDocumentReview("Caller draft", "STATE-CALLER-DRAFT", 0, 0, false)
	uncommitted, err := f.repo(probe).SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || uncommitted == nil || uncommitted.DocumentCount != baseline.DocumentCount+1 {
		t.Fatalf("caller-owned state summary=%#v %v", uncommitted, err)
	}
	read, err = f.repo(probe).ReadBASDocumentStates(ctx, postingOrg, *uncommitted, uncommitted.DocumentCount, uncommitted.StateBytes)
	if err != nil || read == nil || len(read.Documents) != 2 {
		t.Fatalf("caller-owned state read=%#v %v", read, err)
	}
	if err := readTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	check, err := db.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Rollback()
	final, err := f.repo(check).SummarizeBASDocumentStates(ctx, postingOrg)
	if err != nil || final == nil || *final != *baseline || probe.bodyReads != 0 {
		t.Fatalf("caller rollback changed committed document states: %#v %v", final, err)
	}
}
