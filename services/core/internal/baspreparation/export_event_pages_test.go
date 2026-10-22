//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestExportEventRepositoryPagesPinTargetAndSurviveLateAppendVacuumReopen(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	tx := beginBASRepositoryTx(t, workspace)
	calculation, inputs := seedCalculationRepositoryFixture(t, tx, org)
	calculationRef, err := newCalculationRepositoryForTest(t, tx).Append(ctx, calculation, inputs)
	if err != nil {
		t.Fatal(err)
	}
	target := calculationExportTargetForTest(calculationRef)
	first := appendExportPageEvent(t, tx, org, target, 0, 43001, 300)
	second := appendExportPageEvent(t, tx, org, target, 1, 43002, 200)
	third := appendExportPageEvent(t, tx, org, target, 2, 43003, 100)
	// A different exact revision has an independent empty event history.
	newer := appendNewerCalculationForExportBoundaryTest(t, tx, org, calculation, inputs)
	empty, err := newExportEventRepositoryForTest(t, tx).List(ctx, ExportEventListRequest{OrganisationID: org, Target: calculationExportTargetForTest(newer), PageSize: 200})
	if err != nil || empty == nil || len(empty.Events) != 0 || empty.Page.NextCursor != nil || empty.Page.ReturnedCount != 0 {
		t.Fatalf("empty exact target: %v %v", empty, err)
	}
	commitBASRepositoryTx(t, tx)
	makeVersionRowidsSparse(t, workspace.Database, "bas_export_events_v1", "bas_export_events_v1_no_update", org)
	repository := newExportEventRepositoryForTest(t, workspace.Database)
	request := ExportEventListRequest{OrganisationID: org, Target: target, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || len(page.Events) != 1 || page.Page.NextCursor == nil || !sameBASSourceRef(page.Events[0].ExportEvent, first) {
		t.Fatalf("first append-ordered page: %v %v", page, err)
	}
	cursor := *page.Page.NextCursor
	for _, kind := range []string{"organisation", "target_hash", "target_revision", "invalid_target_kind", "tamper", "size", "nil_target"} {
		bad := request
		bad.Target = proto.Clone(request.Target).(*tammyv1.BasExportTarget)
		bad.Cursor = &cursor
		switch kind {
		case "organisation":
			bad.OrganisationID = otherOrg
		case "target_hash":
			bad.Target.GetCalculation().ContentHash[0] ^= 0xff
		case "target_revision":
			bad.Target = calculationExportTargetForTest(newer)
		case "invalid_target_kind":
			bad.Target = frozenExportTargetForBoundaryTest(calculationRef)
		case "tamper":
			token := cursor + "!"
			bad.Cursor = &token
		case "size":
			bad.PageSize = 201
		case "nil_target":
			bad.Target = nil
		}
		if got, err := repository.List(ctx, bad); got != nil || err == nil {
			t.Fatalf("accepted %s cursor: %v %v", kind, got, err)
		}
	}
	lateTx := beginBASRepositoryTx(t, workspace)
	fourth := appendExportPageEvent(t, lateTx, org, target, 3, 43004, 50)
	commitBASRepositoryTx(t, lateTx)
	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	repository = newExportEventRepositoryForTest(t, reopened)
	request.Cursor = &cursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Events) != 1 || page.Page.NextCursor == nil || !sameBASSourceRef(page.Events[0].ExportEvent, second) {
		t.Fatalf("second page after vacuum/reopen: %v %v", page, err)
	}
	request.Cursor = page.Page.NextCursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Events) != 1 || page.Page.NextCursor != nil || !sameBASSourceRef(page.Events[0].ExportEvent, third) {
		t.Fatalf("last pinned page: %v %v", page, err)
	}
	fresh, err := repository.List(ctx, ExportEventListRequest{OrganisationID: org, Target: target, PageSize: 200})
	if err != nil || fresh == nil || len(fresh.Events) != 4 || fresh.Page.ReturnedCount != 4 || !sameBASSourceRef(fresh.Events[3].ExportEvent, fourth) {
		t.Fatalf("fresh complete page: %v %v", fresh, err)
	}
}

func TestExportEventRepositoryPagesRejectCorruptEndpointAnchorLookaheadAndVersion(t *testing.T) {
	for _, kind := range []string{"snapshot_endpoint", "anchor", "returned", "lookahead", "version"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			calculation, inputs := seedCalculationRepositoryFixture(t, tx, org)
			ref, err := newCalculationRepositoryForTest(t, tx).Append(ctx, calculation, inputs)
			if err != nil {
				t.Fatal(err)
			}
			target := calculationExportTargetForTest(ref)
			first := appendExportPageEvent(t, tx, org, target, 0, 43101, 900)
			second := appendExportPageEvent(t, tx, org, target, 1, 43102, 800)
			third := appendExportPageEvent(t, tx, org, target, 2, 43103, 700)
			repository := newExportEventRepositoryForTest(t, tx)
			request := ExportEventListRequest{OrganisationID: org, Target: target, PageSize: 1}
			page, err := repository.List(ctx, request)
			if err != nil || page.Page.NextCursor == nil {
				t.Fatalf("initial page: %v %v", page, err)
			}
			if _, err := tx.ExecContext(ctx, "DROP TRIGGER bas_export_events_v1_no_update"); err != nil {
				t.Fatal(err)
			}
			id, mutation := second.Id, "export_proto=X'01'"
			switch kind {
			case "snapshot_endpoint":
				id = third.Id
				request.Cursor = page.Page.NextCursor
			case "anchor":
				id = first.Id
				request.Cursor = page.Page.NextCursor
			case "returned":
				request.Cursor = page.Page.NextCursor
			case "version":
				mutation = "event_version=99"
			}
			if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET "+mutation+" WHERE export_event_id=?", id); err != nil {
				t.Fatal(err)
			}
			if kind == "version" {
				if got, err := repository.GetExact(ctx, org, second); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("exact read accepted corrupt target version: %v %v", got, err)
				}
			}
			if got, err := repository.List(ctx, request); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("accepted corrupt %s: %v %v", kind, got, err)
			}
		})
	}
}

func TestExportEventRepositoryCursorRejectsDifferentValidTargetKind(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 41, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	calculationTarget := calculationExportTargetForTest(fixture.frozen.Calculation)
	frozenTarget := frozenExportTargetForBoundaryTest(frozen)
	for index, target := range []*tammyv1.BasExportTarget{calculationTarget, frozenTarget} {
		_ = appendExportPageEvent(t, tx, org, target, 0, 43301+uint64(index*2), 500)
		_ = appendExportPageEvent(t, tx, org, target, 1, 43302+uint64(index*2), 400)
	}
	repository := newExportEventRepositoryForTest(t, tx)
	page, err := repository.List(ctx, ExportEventListRequest{OrganisationID: org, Target: calculationTarget, PageSize: 1})
	if err != nil || page == nil || page.Page.NextCursor == nil {
		t.Fatalf("calculation cursor: %v %v", page, err)
	}
	validFrozen, err := repository.List(ctx, ExportEventListRequest{OrganisationID: org, Target: frozenTarget, PageSize: 200})
	if err != nil || validFrozen == nil || len(validFrozen.Events) != 2 {
		t.Fatalf("valid frozen history: %v %v", validFrozen, err)
	}
	if got, err := repository.List(ctx, ExportEventListRequest{OrganisationID: org, Target: frozenTarget, PageSize: 1, Cursor: page.Page.NextCursor}); got != nil || err == nil {
		t.Fatalf("calculation cursor redirected to valid frozen history: %v %v", got, err)
	}
}

func TestExportEventRepositoryMixedTargetScanPreservesEmptyContinuation(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 42, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	calculationTarget := calculationExportTargetForTest(fixture.frozen.Calculation)
	frozenTarget := frozenExportTargetForBoundaryTest(frozen)
	for index := uint64(0); index < 3; index++ {
		_ = appendExportPageEvent(t, tx, org, calculationTarget, index, 43401+index, 500-int64(index))
	}
	want := appendExportPageEvent(t, tx, org, frozenTarget, 0, 43404, 100)
	repository := newExportEventRepositoryForTest(t, tx)
	request := ExportEventListRequest{OrganisationID: org, Target: frozenTarget, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || len(page.Events) != 0 || page.Page.ReturnedCount != 0 || page.Page.NextCursor == nil {
		t.Fatalf("bounded unmatched family batch must retain continuation: %v %v", page, err)
	}
	// A later matching export remains outside the original family watermark.
	_ = appendExportPageEvent(t, tx, org, frozenTarget, 1, 43405, 50)
	var found []*tammyv1.BasExportEventSummary
	pages := 1
	for page.Page.NextCursor != nil {
		if pages >= 5 {
			t.Fatal("mixed target cursor failed to terminate")
		}
		request.Cursor = page.Page.NextCursor
		page, err = repository.List(ctx, request)
		if err != nil || page == nil || page.Page == nil || page.Page.ReturnedCount != uint32(len(page.Events)) {
			t.Fatalf("mixed target continuation: %v %v", page, err)
		}
		found = append(found, page.Events...)
		pages++
	}
	if pages != 4 || len(found) != 1 || !sameBASSourceRef(found[0].ExportEvent, want) {
		t.Fatalf("mixed target watermark history: pages=%d values=%v", pages, found)
	}
}

func TestExportEventRepositoryListBatchVerifiesMixedTargetVersionPositions(t *testing.T) {
	for index, corruption := range []string{"valid_mixed_targets", "older_prefix_cursor", "gap", "swap", "duplicate"} {
		t.Run(corruption, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedFrozenRepositoryFixture(t, tx, org, uint64(44000+index), true, 0)
			frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
			if err != nil {
				t.Fatal(err)
			}
			calculationTarget := calculationExportTargetForTest(fixture.frozen.Calculation)
			frozenTarget := frozenExportTargetForBoundaryTest(frozen)
			calculationFirst := appendExportPageEvent(t, tx, org, calculationTarget, 0, 44001, 300)
			frozenFirst := appendExportPageEvent(t, tx, org, frozenTarget, 0, 44002, 250)
			calculationSecond := appendExportPageEvent(t, tx, org, calculationTarget, 1, 44003, 200)
			frozenSecond := appendExportPageEvent(t, tx, org, frozenTarget, 1, 44004, 150)
			repository := newExportEventRepositoryForTest(t, tx)
			request := ExportEventListRequest{OrganisationID: org, Target: calculationTarget, PageSize: 200}
			if corruption == "older_prefix_cursor" {
				request.PageSize = 1
				firstPage, err := repository.List(ctx, request)
				if err != nil || firstPage == nil || len(firstPage.Events) != 1 ||
					!sameBASSourceRef(firstPage.Events[0].ExportEvent, calculationFirst) || firstPage.Page == nil || firstPage.Page.NextCursor == nil {
					t.Fatalf("first cursor page did not return the first calculation event: page=%#v err=%v", firstPage, err)
				}
				request.Cursor = firstPage.Page.NextCursor
				secondPage, err := repository.List(ctx, request)
				if err != nil || secondPage == nil || len(secondPage.Events) != 0 || secondPage.Page == nil || secondPage.Page.NextCursor == nil {
					t.Fatalf("second interleaved cursor page should be empty for this target and retain a cursor: page=%#v err=%v", secondPage, err)
				}
				if _, err := tx.ExecContext(ctx, "DROP TRIGGER bas_export_events_v1_no_update"); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET event_version=99 WHERE export_event_id=?", calculationFirst.Id); err != nil {
					t.Fatal(err)
				}
				request.Cursor = secondPage.Page.NextCursor
				if page, err := repository.List(ctx, request); page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("accepted earlier malformed calculation version outside the selected cursor batch: page=%#v err=%v", page, err)
				}
				return
			}
			if corruption == "valid_mixed_targets" {
				probe := &exportPagePrefixProbe{CommandSQLExecutor: repository.executor}
				repository.executor = probe
				page, err := repository.List(ctx, request)
				if err != nil || page == nil || len(page.Events) != 2 ||
					!sameBASSourceRef(page.Events[0].ExportEvent, calculationFirst) ||
					!sameBASSourceRef(page.Events[1].ExportEvent, calculationSecond) {
					t.Fatalf("valid interleaved target-local versions were rejected or omitted: page=%#v err=%v", page, err)
				}
				frozenPage, err := repository.List(ctx, ExportEventListRequest{OrganisationID: org, Target: frozenTarget, PageSize: 200})
				if err != nil || frozenPage == nil || len(frozenPage.Events) != 2 ||
					!sameBASSourceRef(frozenPage.Events[0].ExportEvent, frozenFirst) ||
					!sameBASSourceRef(frozenPage.Events[1].ExportEvent, frozenSecond) {
					t.Fatalf("valid interleaved frozen target-local versions were rejected or omitted: page=%#v err=%v", frozenPage, err)
				}
				if probe.prefixAggregates != 0 || probe.windowProofs != 2 {
					t.Fatalf("two list pages did %d prefix aggregates and %d window proofs", probe.prefixAggregates, probe.windowProofs)
				}
				if probe.windowPlanFailures != 0 || probe.calculationIndexPlans != 2 || probe.frozenIndexPlans != 2 {
					t.Fatalf("candidate target history did not use both bounded indexes: planFailures=%d calculationIndex=%d frozenIndex=%d",
						probe.windowPlanFailures, probe.calculationIndexPlans, probe.frozenIndexPlans)
				}
				if _, err := repository.GetExact(ctx, org, calculationSecond); err != nil {
					t.Fatal(err)
				}
				if probe.prefixAggregates != 1 {
					t.Fatal("public exact read bypassed its independent prefix proof")
				}
				return
			}
			if _, err := tx.ExecContext(ctx, "DROP TRIGGER bas_export_events_v1_no_update"); err != nil {
				t.Fatal(err)
			}
			switch corruption {
			case "gap":
				if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET event_version=3 WHERE export_event_id=?", calculationSecond.Id); err != nil {
					t.Fatal(err)
				}
			case "swap":
				if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET event_version=99 WHERE export_event_id=?", calculationFirst.Id); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET event_version=1 WHERE export_event_id=?", calculationSecond.Id); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET event_version=2 WHERE export_event_id=?", calculationFirst.Id); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				if _, err := tx.ExecContext(ctx, "DROP INDEX bas_export_events_calculation_version_unique"); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_export_events_v1 SET event_version=1 WHERE export_event_id=?", calculationSecond.Id); err != nil {
					t.Fatal(err)
				}
			}
			if page, err := repository.List(ctx, request); page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("accepted mixed-target %s corruption: page=%#v err=%v", corruption, page, err)
			}
		})
	}
}

func TestExportEventRepositorySchemaRejectsTargetVersionGapsAndReplaceCollisions(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	calculation, inputs := seedCalculationRepositoryFixture(t, tx, org)
	ref, err := newCalculationRepositoryForTest(t, tx).Append(ctx, calculation, inputs)
	if err != nil {
		t.Fatal(err)
	}
	target := calculationExportTargetForTest(ref)
	first := appendExportPageEvent(t, tx, org, target, 0, 43201, 1000)
	// Copy otherwise valid normalized facts. Different IDs/operation keys cannot
	// bypass the exact-target version or durable ordinal guards with OR REPLACE.
	for _, kind := range []string{"duplicate_version", "version_gap", "ordinal_collision", "operation_phase"} {
		t.Run(kind, func(t *testing.T) {
			ordinal, version, operation := any(nil), 2, testUUID(43220)
			switch kind {
			case "duplicate_version":
				version = 1
			case "version_gap":
				version = 3
			case "ordinal_collision":
				var currentOrdinal int64
				rows, err := tx.QueryContext(ctx, "SELECT append_ordinal FROM bas_export_events_v1 WHERE export_event_id=?", first.Id)
				if err != nil {
					t.Fatal(err)
				}
				if !rows.Next() {
					t.Fatal("missing ordinal")
				}
				if err := rows.Scan(&currentOrdinal); err != nil {
					t.Fatal(err)
				}
				if err := rows.Close(); err != nil {
					t.Fatal(err)
				}
				ordinal = currentOrdinal
			case "operation_phase":
				operation = testUUID(143201)
			}
			_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO bas_export_events_v1(
				append_ordinal,export_event_id,operation_key,organisation_id,family_id,target_kind,event_version,
				calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash,format,phase,outcome,artifact_hash,
				actor_user_id,reason,content_hash,export_proto,recorded_at)
				SELECT ?,?,?,organisation_id,family_id,target_kind,?,calculation_id,calculation_version,calculation_hash,
				frozen_id,frozen_hash,format,phase,outcome,artifact_hash,actor_user_id,reason,content_hash,export_proto,recorded_at
				FROM bas_export_events_v1 WHERE export_event_id=?`, ordinal, testUUID(43221), operation, version, first.Id)
			if err == nil {
				t.Fatalf("schema accepted %s", kind)
			}
			assertBASRepositoryCount(t, tx, "bas_export_events_v1", 1)
			if _, err := newExportEventRepositoryForTest(t, tx).GetExact(ctx, org, first); err != nil {
				t.Fatalf("rejected replacement damaged retained event: %v", err)
			}
		})
	}
}

func appendExportPageEvent(t *testing.T, executor app.CommandSQLExecutor, org string, target *tammyv1.BasExportTarget, expected, id uint64, seconds int64) *tammyv1.SourceRef {
	t.Helper()
	value := exportEventForTest(org, target, id, seconds, tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_UNSPECIFIED, nil)
	ref, err := newExportEventRepositoryForTest(t, executor).Append(context.Background(), org, testUUID(100000+id), expected, value)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// Observe database work, independently of the number of bodies returned by a
// page. Listing must not repeat a full prefix aggregate for every body.
type exportPagePrefixProbe struct {
	app.CommandSQLExecutor
	prefixAggregates      int
	windowProofs          int
	windowPlanFailures    int
	calculationIndexPlans int
	frozenIndexPlans      int
}

func (probe *exportPagePrefixProbe) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "COUNT(DISTINCT event_version)") {
		probe.prefixAggregates++
	}
	if strings.Contains(query, "ROW_NUMBER() OVER (") {
		probe.windowProofs++
		planRows, err := probe.CommandSQLExecutor.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			probe.windowPlanFailures++
		} else {
			for planRows.Next() {
				var selectID, parentID, unused int64
				var detail string
				if err := planRows.Scan(&selectID, &parentID, &unused, &detail); err != nil {
					probe.windowPlanFailures++
					break
				}
				if strings.Contains(detail, "bas_export_events_calculation_page_idx") {
					probe.calculationIndexPlans++
				}
				if strings.Contains(detail, "bas_export_events_frozen_page_idx") {
					probe.frozenIndexPlans++
				}
			}
			if err := errors.Join(planRows.Err(), planRows.Close()); err != nil {
				probe.windowPlanFailures++
			}
		}
	}
	return probe.CommandSQLExecutor.QueryContext(ctx, query, args...)
}
