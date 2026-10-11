//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestExternalObservationRepositoryPagesRetainCorrectionsThroughVacuumAndReopen(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	tx := beginBASRepositoryTx(t, workspace)
	fixture := seedFrozenRepositoryFixture(t, tx, org, 81, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	_, first := appendExternalObservationForRepositoryTest(t, repository, org, frozen, 67001, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 900)
	correction := externalCorrectionForRepositoryTest(org, frozen, 67002, first, tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN.Enum(), nil, 800)
	second, err := repository.Append(ctx, org, testUUID(67102), testUUID(67202), correction)
	if err != nil {
		t.Fatal(err)
	}
	_, third := appendExternalObservationForRepositoryTest(t, repository, org, frozen, 67003, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT, 700)
	otherFrozen := appendFrozenPageApproval(t, tx, org, fixture, 82)
	commitBASRepositoryTx(t, tx)
	var triggerSQL string
	if err := workspace.Database.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='trigger' AND name='bas_family_events_v1_no_update'").Scan(&triggerSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, "DROP TRIGGER bas_family_events_v1_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, "UPDATE bas_family_events_v1 SET event_ordinal=event_ordinal*10 WHERE organisation_id=?", org); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Database.ExecContext(ctx, triggerSQL); err != nil {
		t.Fatal(err)
	}
	repository = newExternalObservationRepositoryForTest(t, workspace.Database)
	request := ExternalObservationListRequest{OrganisationID: org, Frozen: frozen, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || len(page.Observations) != 1 || page.Page.NextCursor == nil || !sameBASSourceRef(page.Observations[0].Observation, first) {
		t.Fatalf("first Core append-ordered page: %v %v", page, err)
	}
	cursor := *page.Page.NextCursor
	empty, err := repository.List(ctx, ExternalObservationListRequest{OrganisationID: org, Frozen: otherFrozen, PageSize: 1})
	if err != nil || empty == nil || len(empty.Observations) != 0 || empty.Page.NextCursor != nil {
		t.Fatalf("empty other frozen history: %v %v", empty, err)
	}
	for _, kind := range []string{"organisation", "frozen_hash", "other_valid_frozen", "tamper", "size"} {
		bad := request
		bad.Frozen = cloneRef(request.Frozen)
		bad.Cursor = &cursor
		switch kind {
		case "organisation":
			bad.OrganisationID = otherOrg
		case "frozen_hash":
			bad.Frozen.ContentHash[0] ^= 0xff
		case "other_valid_frozen":
			bad.Frozen = otherFrozen
		case "tamper":
			token := cursor + "!"
			bad.Cursor = &token
		case "size":
			bad.PageSize = 201
		}
		if got, err := repository.List(ctx, bad); got != nil || err == nil {
			t.Fatalf("accepted %s cursor: %v %v", kind, got, err)
		}
	}
	lateTx := beginBASRepositoryTx(t, workspace)
	late := externalCorrectionForRepositoryTest(org, frozen, 67004, second, tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil, 600)
	fourth, err := newExternalObservationRepositoryForTest(t, lateTx).Append(ctx, org, testUUID(67104), testUUID(67204), late)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, lateTx)
	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	repository = newExternalObservationRepositoryForTest(t, reopened)
	request.Cursor = &cursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Observations) != 1 || page.Page.NextCursor == nil || !sameBASSourceRef(page.Observations[0].Observation, second) || page.Observations[0].CorrectionKind == nil || !sameBASSourceRef(page.Observations[0].CorrectsObservation, first) {
		t.Fatalf("correction page after vacuum/reopen: %v %v", page, err)
	}
	request.Cursor = page.Page.NextCursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Observations) != 1 || page.Page.NextCursor != nil || !sameBASSourceRef(page.Observations[0].Observation, third) {
		t.Fatalf("last pinned observation page: %v %v", page, err)
	}
	fresh, err := repository.List(ctx, ExternalObservationListRequest{OrganisationID: org, Frozen: frozen, PageSize: 200})
	if err != nil || fresh == nil || len(fresh.Observations) != 4 || !sameBASSourceRef(fresh.Observations[3].Observation, fourth) || fresh.Observations[3].CorrectionKind == nil {
		t.Fatalf("fresh correction history: %v %v", fresh, err)
	}
}

func TestExternalObservationRepositoryPagesRejectCorruptEndpointAnchorLookaheadAndFrozenScalar(t *testing.T) {
	for _, kind := range []string{"snapshot_endpoint", "anchor", "returned", "lookahead", "correction_frozen_scalar"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedFrozenRepositoryFixture(t, tx, org, 83, true, 0)
			frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
			if err != nil {
				t.Fatal(err)
			}
			repository := newExternalObservationRepositoryForTest(t, tx)
			_, first := appendExternalObservationForRepositoryTest(t, repository, org, frozen, 67301, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 900)
			correction := externalCorrectionForRepositoryTest(org, frozen, 67302, first, tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN.Enum(), nil, 800)
			second, err := repository.Append(ctx, org, testUUID(67402), testUUID(67502), correction)
			if err != nil {
				t.Fatal(err)
			}
			_, third := appendExternalObservationForRepositoryTest(t, repository, org, frozen, 67303, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT, 700)
			request := ExternalObservationListRequest{OrganisationID: org, Frozen: frozen, PageSize: 1}
			page, err := repository.List(ctx, request)
			if err != nil || page == nil || page.Page.NextCursor == nil {
				t.Fatalf("initial page: %v %v", page, err)
			}
			if kind == "correction_frozen_scalar" {
				otherFrozen := appendFrozenPageApproval(t, tx, org, fixture, 84)
				if _, err := tx.ExecContext(ctx, "DROP TRIGGER bas_external_observation_corrections_v1_no_update"); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_external_observation_corrections_v1 SET frozen_id=?,frozen_hash=? WHERE correction_id=?", otherFrozen.Id, otherFrozen.ContentHash, second.Id); err != nil {
					t.Fatal(err)
				}
				if got, err := repository.GetExact(ctx, org, second); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("accepted altered correction frozen target: %v %v", got, err)
				}
				if got, err := repository.List(ctx, request); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
					t.Fatalf("silently omitted corrupt original-target history: %v %v", got, err)
				}
				request.Frozen = otherFrozen
			} else {
				id, table, column := second.Id, "bas_external_observation_corrections_v1", "correction_proto"
				switch kind {
				case "snapshot_endpoint":
					id = third.Id
					table = "bas_external_observations_v1"
					column = "observation_proto"
					request.Cursor = page.Page.NextCursor
				case "anchor":
					id = first.Id
					table = "bas_external_observations_v1"
					column = "observation_proto"
					request.Cursor = page.Page.NextCursor
				case "returned":
					request.Cursor = page.Page.NextCursor
				}
				if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+table+"_no_update"); err != nil {
					t.Fatal(err)
				}
				idColumn := "observation_id"
				if table == "bas_external_observation_corrections_v1" {
					idColumn = "correction_id"
				}
				if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET "+column+"=X'01' WHERE "+idColumn+"=?", id); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := repository.List(ctx, request); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("accepted corrupt %s page: %v %v", kind, got, err)
			}
		})
	}
}

func TestExternalObservationRepositoryDuplicateChildRollsBackInsertedCoreEvent(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedFrozenRepositoryFixture(t, tx, org, 85, true, 0)
	frozen, err := newFrozenRepositoryForTest(t, tx).Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	repository := newExternalObservationRepositoryForTest(t, tx)
	value, first := appendExternalObservationForRepositoryTest(t, repository, org, frozen, 67601, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, 900)
	changed := proto.Clone(value).(*tammyv1.BasExternalObservation)
	changed.RecordedAt.Seconds++
	if ref, err := repository.Append(ctx, org, testUUID(67702), testUUID(67802), changed); ref != nil || err == nil {
		t.Fatalf("duplicate child appended: %v %v", ref, err)
	}
	assertBASRepositoryCount(t, tx, "bas_family_events_v1", 1)
	assertBASRepositoryCount(t, tx, "bas_external_observations_v1", 1)
	if got, err := repository.GetExact(ctx, org, first); err != nil || got == nil {
		t.Fatalf("failed write damaged retained observation: %v %v", got, err)
	}
}
