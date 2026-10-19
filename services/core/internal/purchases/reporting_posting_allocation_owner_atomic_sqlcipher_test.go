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

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/purchases"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

const (
	purchasePostingAllocationOwnerAtomicRawMarker    = "/* BAS purchase owner body purchase_posting_allocations_v1 */"
	purchasePostingAllocationOwnerAtomicReplayMarker = "/* BAS purchase owner body posting allocation replay */"
)

func TestPurchaseReportingPostingAllocationOwnerBLOBDriftAfterFreshEmptyScalarReservation(t *testing.T) {
	ctx := context.Background()
	tx, f := purchaseRootOwnerFixture(t)
	defer tx.Rollback()
	initialDocumentID := f.document.Id
	posted := postReportingPurchase(t, f, "ALLOCATION-OWNER-SCALAR", purchaseReportingDate(2026, 8, 10), 0, 0)
	if posted == nil || posted.Posting == nil {
		t.Fatal("zero-total writer returned no posting")
	}
	movePurchaseScalarRootPostingGraphOutOfTextScope(t, f, posted.Posting.Id, posted.Posting.DocumentId, initialDocumentID)
	allocationPostingID, allocationSequence := purchasePostingAllocationOwnerPrimaryKey(t, tx, 1)
	sequence := uint32(allocationSequence)

	triggersBefore := purchaseRootOwnerTriggerDefinitions(t, tx)
	probe := &purchaseBASScalarProbe{Transaction: tx}
	repo := f.repo(probe)
	stateBefore := purchaseBASScalarState(t, tx)
	empty, err := repo.SummarizeBASKeys(ctx, postingOrg)
	assertPurchaseScalarRootOwnerEmpty(t, empty, err)
	keys, err := repo.ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	if err != nil || len(keys) != 0 {
		t.Fatalf("foreign allocation scalar read returned %#v, %v; want empty keys", keys, err)
	}
	if !reflect.DeepEqual(stateBefore, purchaseBASScalarState(t, tx)) ||
		!reflect.DeepEqual(triggersBefore, purchaseRootOwnerTriggerDefinitions(t, tx)) || !tx.Authenticated() ||
		probe.summaryReads != 2 || probe.keyReads != 1 || probe.bodyReads != 0 {
		t.Fatal("empty scalar reservation/read changed retained state, read bodies, missed a scalar query, or ended the transaction")
	}

	summaryReadsBeforeLate, keyReadsBeforeLate := probe.summaryReads, probe.keyReads
	var stateAfterMutation map[string]any
	var triggersAfterMutation map[string]string
	var beforeRead runtime.MemStats
	probe.beforeKeyRead = func() error {
		if probe.summaryReads != summaryReadsBeforeLate+1 {
			return errors.New("allocation mutation did not follow the fresh scalar summary")
		}
		mutatePurchaseBASScalar(t, f, "purchase_posting_allocations_v1",
			`UPDATE purchase_posting_allocations_v1
			 SET organisation_id=CAST(? AS BLOB),account_version=zeroblob(?)
			 WHERE posting_id=? AND sequence=?`,
			postingOrg, purchaseRootOwnerLateCellBytes, allocationPostingID, sequence)
		purchasePostingAllocationOwnerAtomicAssertOwner(t, tx, allocationPostingID, sequence, "blob", []byte(postingOrg))
		purchasePostingAllocationOwnerAtomicAssertCellMetadata(t, tx, allocationPostingID, sequence,
			"account_version", "blob", purchaseRootOwnerLateCellBytes)
		if got := postScalar(t, tx, `PRAGMA ignore_check_constraints`); got != 0 {
			return errors.New("fixture mutation left ignore_check_constraints enabled")
		}
		stateAfterMutation = purchaseBASScalarState(t, tx)
		triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(t, tx)
		runtime.GC()
		runtime.ReadMemStats(&beforeRead)
		return nil
	}

	keys, err = repo.ReadBASKeys(ctx, postingOrg, *empty, 0, 0)
	var afterRead runtime.MemStats
	runtime.ReadMemStats(&afterRead)
	allocated := afterRead.TotalAlloc - beforeRead.TotalAlloc
	errorBytes := 0
	if err != nil {
		errorBytes = len(err.Error())
	}
	assertPurchaseScalarRootOwnerZeroKeyBound(t, keys, err)
	if errorBytes >= 512 || allocated >= 2*1024*1024 {
		t.Fatalf("9 MiB allocation account_version reached Go: error_bytes=%d allocated=%d err=%.256v",
			errorBytes, allocated, err)
	}
	if stateAfterMutation == nil || !reflect.DeepEqual(stateAfterMutation, purchaseBASScalarState(t, tx)) ||
		!reflect.DeepEqual(triggersBefore, triggersAfterMutation) ||
		!reflect.DeepEqual(triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, tx)) ||
		!tx.Authenticated() || postScalar(t, tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("late scalar rejection changed retained state, trigger definitions, constraint mode, or caller transaction")
	}
	if probe.summaryReads != summaryReadsBeforeLate+1 || probe.keyReads != keyReadsBeforeLate+1 || probe.bodyReads != 0 {
		t.Fatalf("late scalar guard issued summary/key/body queries %d/%d/%d; want 1/1/0",
			probe.summaryReads-summaryReadsBeforeLate, probe.keyReads-keyReadsBeforeLate, probe.bodyReads)
	}
}

func TestPurchaseReportingPostingAllocationOwnerRawBLOBDriftIsAtomic(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		for _, scenario := range []string{"same-row", "cross-row"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				tx, f := purchaseRootOwnerFixture(t)
				defer tx.Rollback()
				first := postReportingPurchase(t, f, "ALLOCATION-OWNER-RAW-1", purchaseReportingDate(2026, 8, 10), 10000, 1000)
				if first == nil || first.Posting == nil {
					t.Fatal("first real writer returned no posting")
				}
				ownerPostingID, ownerSequence := first.Posting.Id, uint32(1)
				bodyPostingID, bodySequence := first.Posting.Id, uint32(1)
				if scenario == "cross-row" {
					second := postReportingPurchase(t, f, "ALLOCATION-OWNER-RAW-2", purchaseReportingDate(2026, 8, 11), 12000, 1200)
					if second == nil || second.Posting == nil {
						t.Fatal("second real writer returned no posting")
					}
					ownerPostingID = second.Posting.Id
				}
				purchasePostingAllocationOwnerAtomicAssertWriterRow(t, tx, first.Posting.Id, 1)
				if scenario == "cross-row" {
					purchasePostingAllocationOwnerAtomicAssertWriterRow(t, tx, ownerPostingID, 1)
				}
				probe := &purchasePostingAllocationOwnerAtomicProbe{
					CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode, stage: "raw",
					ownerPostingID: ownerPostingID, ownerSequence: ownerSequence,
					bodyPostingID: bodyPostingID, bodySequence: bodySequence,
				}
				nilCapture, err := purchasePostingAllocationOwnerAtomicCapture(t, f.repo(probe), mode)
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				purchasePostingAllocationOwnerAtomicAssertRejection(t, probe, nilCapture, err, after)
			})
		}
	}
}

func TestPurchaseReportingPostingAllocationOwnerEmptyRawQueriesRejectBLOB(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		t.Run(mode, func(t *testing.T) {
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			initialDocumentID := f.document.Id
			posted := postReportingPurchase(t, f, "ALLOCATION-OWNER-EMPTY", purchaseReportingDate(2026, 8, 10), 0, 0)
			if posted == nil || posted.Posting == nil {
				t.Fatal("zero-total writer returned no posting")
			}
			movePurchaseScalarRootPostingGraphOutOfTextScope(t, f, posted.Posting.Id, posted.Posting.DocumentId, initialDocumentID)
			assertPurchaseRootOwnerEmptyCoverage(t, tx, f)
			allocationPostingID, allocationSequence := purchasePostingAllocationOwnerPrimaryKey(t, tx, 1)
			sequence := uint32(allocationSequence)
			probe := &purchasePostingAllocationOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode, stage: "raw",
				ownerPostingID: allocationPostingID, ownerSequence: sequence,
				bodyPostingID: allocationPostingID, bodySequence: sequence,
			}
			nilCapture, err := purchasePostingAllocationOwnerAtomicCapture(t, f.repo(probe), mode)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchasePostingAllocationOwnerAtomicAssertRejection(t, probe, nilCapture, err, after)
		})
	}
}

func TestPurchaseReportingPostingAllocationOwnerReplayCrossRowBLOBIsAtomic(t *testing.T) {
	for _, mode := range []string{"full-coverage", "modern-capture"} {
		t.Run(mode, func(t *testing.T) {
			tx, f := purchaseRootOwnerFixture(t)
			defer tx.Rollback()
			first := postReportingPurchase(t, f, "ALLOCATION-OWNER-REPLAY-1", purchaseReportingDate(2026, 8, 10), 10000, 1000)
			second := postReportingPurchase(t, f, "ALLOCATION-OWNER-REPLAY-2", purchaseReportingDate(2026, 8, 11), 12000, 1200)
			if first == nil || first.Posting == nil || second == nil || second.Posting == nil {
				t.Fatal("two real writers did not return postings")
			}
			purchasePostingAllocationOwnerAtomicAssertWriterRow(t, tx, first.Posting.Id, 1)
			purchasePostingAllocationOwnerAtomicAssertWriterRow(t, tx, second.Posting.Id, 1)
			firstPostingID, ownerPostingID := first.Posting.Id, second.Posting.Id
			if ownerPostingID < firstPostingID {
				firstPostingID, ownerPostingID = ownerPostingID, first.Posting.Id
			}
			probe := &purchasePostingAllocationOwnerAtomicProbe{
				CommandSQLExecutor: tx, t: t, fixture: f, tx: tx, mode: mode, stage: "replay",
				ownerPostingID: ownerPostingID, ownerSequence: 1,
				bodyPostingID: firstPostingID, bodySequence: 1,
				firstPostingID: firstPostingID,
			}
			nilCapture, err := purchasePostingAllocationOwnerAtomicCapture(t, f.repo(probe), mode)
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			purchasePostingAllocationOwnerAtomicAssertRejection(t, probe, nilCapture, err, after)
			if probe.documentReplayReads == 0 || probe.postingReplayReads == 0 || probe.replayHookBeforePostings != firstPostingID {
				t.Fatalf("allocation replay preceded document/posting reads or selected posting %q; reads=%d/%d",
					probe.replayHookBeforePostings, probe.documentReplayReads, probe.postingReplayReads)
			}
		})
	}
}

type purchasePostingAllocationOwnerAtomicProbe struct {
	app.CommandSQLExecutor
	t                        *testing.T
	fixture                  *postingFixture
	tx                       *sqlcipher.Transaction
	mode                     string
	stage                    string
	ownerPostingID           string
	ownerSequence            uint32
	bodyPostingID            string
	bodySequence             uint32
	firstPostingID           string
	hookCount                int
	afterHook                bool
	laterBodies              int
	documentReplayReads      int
	postingReplayReads       int
	replayHookBeforePostings string
	stateAfterMutation       map[string]any
	triggersBefore           map[string]string
	triggersAfterMutation    map[string]string
	beforeRead               runtime.MemStats
}

func (probe *purchasePostingAllocationOwnerAtomicProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	isRaw := probe.stage == "raw" && probe.matchesHook(query)
	isReplay := probe.stage == "replay" && probe.matchesHook(query)
	isDocumentReplay := strings.Contains(query, "/* BAS purchase owner body document replay */")
	isPostingReplay := strings.Contains(query, "/* BAS purchase owner body posting replay */")
	if probe.afterHook && purchaseReportingRootOwnerIsBodyStatement(query) {
		probe.laterBodies++
	}
	if probe.hookCount == 0 && (isRaw || isReplay) {
		probe.hookCount++
		if probe.stage == "replay" {
			if probe.documentReplayReads == 0 || probe.postingReplayReads == 0 {
				probe.t.Fatalf("allocation replay hook ran before document/posting replay reads: %d/%d",
					probe.documentReplayReads, probe.postingReplayReads)
			}
			if len(args) < 2 || args[len(args)-2] != probe.firstPostingID || args[len(args)-1] != postingOrg {
				probe.t.Fatalf("first allocation replay args=%#v; want posting %q and requested owner", args, probe.firstPostingID)
			}
			postingID, ok := args[len(args)-2].(string)
			if !ok {
				probe.t.Fatalf("allocation replay posting argument has type %T, want string", args[0])
			}
			probe.replayHookBeforePostings = postingID
		}
		probe.triggersBefore = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
		probe.mutateLateRows()
		if got := postScalar(probe.t, probe.tx, `PRAGMA ignore_check_constraints`); got != 0 {
			probe.t.Fatalf("allocation mutation left ignore_check_constraints=%d, want 0", got)
		}
		probe.triggersAfterMutation = purchaseRootOwnerTriggerDefinitions(probe.t, probe.tx)
		if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) {
			probe.t.Fatal("allocation mutation did not restore exact trigger definitions")
		}
		probe.stateAfterMutation = purchaseReportingRootOwnerState(probe.t, probe.tx, probe.mode)
		probe.afterHook = true
		runtime.GC()
		runtime.ReadMemStats(&probe.beforeRead)
	}

	rows, err := probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
	if err == nil && rows != nil {
		if isDocumentReplay {
			probe.documentReplayReads++
		}
		if isPostingReplay {
			probe.postingReplayReads++
		}
	}
	return rows, err
}

func (probe *purchasePostingAllocationOwnerAtomicProbe) mutateLateRows() {
	probe.t.Helper()
	if probe.ownerPostingID == probe.bodyPostingID && probe.ownerSequence == probe.bodySequence {
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_posting_allocations_v1",
			`UPDATE purchase_posting_allocations_v1 SET organisation_id=CAST(? AS BLOB),allocation_proto=zeroblob(?)
			 WHERE posting_id=? AND sequence=?`,
			postingOrg, purchaseRootOwnerLateCellBytes, probe.ownerPostingID, probe.ownerSequence)
	} else {
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_posting_allocations_v1",
			`UPDATE purchase_posting_allocations_v1 SET organisation_id=CAST(? AS BLOB)
			 WHERE posting_id=? AND sequence=?`, postingOrg, probe.ownerPostingID, probe.ownerSequence)
		mutatePurchaseBASScalar(probe.t, probe.fixture, "purchase_posting_allocations_v1",
			`UPDATE purchase_posting_allocations_v1 SET allocation_proto=zeroblob(?)
			 WHERE posting_id=? AND sequence=?`, purchaseRootOwnerLateCellBytes, probe.bodyPostingID, probe.bodySequence)
		purchasePostingAllocationOwnerAtomicAssertOwner(probe.t, probe.tx, probe.bodyPostingID, probe.bodySequence, "text", []byte(postingOrg))
	}
	purchasePostingAllocationOwnerAtomicAssertOwner(probe.t, probe.tx, probe.ownerPostingID, probe.ownerSequence, "blob", []byte(postingOrg))
	purchasePostingAllocationOwnerAtomicAssertCellMetadata(probe.t, probe.tx, probe.bodyPostingID, probe.bodySequence,
		"allocation_proto", "blob", purchaseRootOwnerLateCellBytes)
}

func (probe *purchasePostingAllocationOwnerAtomicProbe) matchesHook(query string) bool {
	if probe.stage == "raw" && strings.Contains(query, purchasePostingAllocationOwnerAtomicRawMarker) {
		return true
	}
	if probe.stage == "replay" && strings.Contains(query, purchasePostingAllocationOwnerAtomicReplayMarker) {
		return true
	}
	compact := purchaseReportingRootOwnerCompactSQL(query)
	switch probe.stage {
	case "raw":
		return strings.HasPrefix(compact,
			"selecta.posting_id,a.organisation_id,a.sequence,a.account_id,a.account_version,a.gross_minor,a.supplier_gst_minor,a.eligible_gst_minor,a.use_kind,a.bas_classification,a.cash_flow_classification,a.noncreditable_reason,a.allocation_protofrompurchase_posting_allocations_v1asa")
	case "replay":
		return strings.HasPrefix(compact,
			"selectsequence,account_id,account_version,gross_minor,supplier_gst_minor,eligible_gst_minor,use_kind,bas_classification,cash_flow_classification,noncreditable_reason,allocation_protofrompurchase_posting_allocations_v1whereposting_id=?andorganisation_id=?orderbysequence")
	default:
		return false
	}
}

func purchasePostingAllocationOwnerAtomicCapture(t *testing.T, repo *purchases.Repository, mode string) (bool, error) {
	t.Helper()
	if mode == "full-coverage" {
		capture, err := repo.CaptureCoverageForBAS(context.Background(), postingOrg)
		return capture == nil, err
	}
	capture, err := repo.CapturePostingsForReporting(context.Background(), postingOrg)
	return capture == nil, err
}

func purchasePostingAllocationOwnerAtomicAssertRejection(t *testing.T, probe *purchasePostingAllocationOwnerAtomicProbe, nilCapture bool, err error, afterRead runtime.MemStats) {
	t.Helper()
	allocated := afterRead.TotalAlloc - probe.beforeRead.TotalAlloc
	errorBytes := 0
	if err != nil {
		errorBytes = len(err.Error())
	}
	if probe.hookCount != 1 {
		t.Fatalf("%s allocation hook count=%d, want exactly one; capture_nil=%t error_bytes=%d err=%.240v",
			probe.stage, probe.hookCount, nilCapture, errorBytes, err)
	}
	if !nilCapture || !errors.Is(err, purchases.ErrRepository) {
		t.Fatalf("late %s allocation owner drift returned capture_nil=%t error_bytes=%d err=%.240v; want nil capture and ErrRepository",
			probe.stage, nilCapture, errorBytes, err)
	}
	if errorBytes >= 512 || allocated >= 2*1024*1024 {
		t.Fatalf("late %s allocation guard copied a 9 MiB body: error_bytes=%d allocated=%d err=%.240v",
			probe.stage, errorBytes, allocated, err)
	}
	if probe.laterBodies != 0 {
		t.Fatalf("late %s allocation rejection attempted %d later body queries", probe.stage, probe.laterBodies)
	}
	purchasePostingAllocationOwnerAtomicAssertOwner(t, probe.tx, probe.ownerPostingID, probe.ownerSequence, "blob", []byte(postingOrg))
	purchasePostingAllocationOwnerAtomicAssertCellMetadata(t, probe.tx, probe.bodyPostingID, probe.bodySequence,
		"allocation_proto", "blob", purchaseRootOwnerLateCellBytes)
	if probe.stateAfterMutation == nil ||
		!reflect.DeepEqual(probe.stateAfterMutation, purchaseReportingRootOwnerState(t, probe.tx, probe.mode)) {
		t.Fatal("allocation owner rejection changed retained state or total_changes after the fixture mutation")
	}
	if !reflect.DeepEqual(probe.triggersBefore, probe.triggersAfterMutation) ||
		!reflect.DeepEqual(probe.triggersAfterMutation, purchaseRootOwnerTriggerDefinitions(t, probe.tx)) {
		t.Fatal("allocation owner rejection changed trigger definitions after the fixture mutation")
	}
	if !probe.tx.Authenticated() || postScalar(t, probe.tx, `PRAGMA ignore_check_constraints`) != 0 {
		t.Fatal("allocation owner rejection ended the caller transaction or left check constraints disabled")
	}
}

func purchasePostingAllocationOwnerAtomicAssertWriterRow(t *testing.T, tx *sqlcipher.Transaction, postingID string, wantCount int) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		`SELECT sequence FROM purchase_posting_allocations_v1 WHERE posting_id=? ORDER BY sequence`, postingID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		count++
		if sequence != int64(count) {
			t.Fatalf("writer allocation sequence=%d at position %d", sequence, count)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if count != wantCount {
		t.Fatalf("real writer created %d allocations for %s; want %d", count, postingID, wantCount)
	}
}

func purchasePostingAllocationOwnerAtomicAssertOwner(t *testing.T, tx *sqlcipher.Transaction, postingID string, sequence uint32, wantType string, wantBytes []byte) {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(),
		`SELECT typeof(organisation_id),length(organisation_id),CAST(organisation_id AS BLOB)
		 FROM purchase_posting_allocations_v1 WHERE posting_id=? AND sequence=?`, postingID, sequence)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var storageType string
	var byteLength int64
	var owner []byte
	if !rows.Next() || rows.Scan(&storageType, &byteLength, &owner) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique allocation owner at composite key (%s,%d)", postingID, sequence)
	}
	if storageType != wantType || byteLength != int64(len(wantBytes)) || !bytes.Equal(owner, wantBytes) {
		t.Fatalf("allocation (%s,%d) owner type/bytes=%q/%d/%x; want %q/%d/%x",
			postingID, sequence, storageType, byteLength, owner, wantType, len(wantBytes), wantBytes)
	}
}

func purchasePostingAllocationOwnerAtomicAssertCellMetadata(t *testing.T, tx *sqlcipher.Transaction, postingID string, sequence uint32, column, wantType string, wantLength int) {
	t.Helper()
	if column != "account_version" && column != "allocation_proto" {
		t.Fatalf("unexpected allocation metadata column %q", column)
	}
	rows, err := tx.QueryContext(context.Background(),
		`SELECT typeof(`+column+`),length(`+column+`) FROM purchase_posting_allocations_v1 WHERE posting_id=? AND sequence=?`, postingID, sequence)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var storageType string
	var byteLength int64
	if !rows.Next() || rows.Scan(&storageType, &byteLength) != nil || rows.Next() || rows.Err() != nil {
		t.Fatalf("read unique allocation %s metadata at composite key (%s,%d)", column, postingID, sequence)
	}
	if storageType != wantType || byteLength != int64(wantLength) {
		t.Fatalf("allocation (%s,%d) %s metadata=%s/%d, want %s/%d",
			postingID, sequence, column, storageType, byteLength, wantType, wantLength)
	}
}
