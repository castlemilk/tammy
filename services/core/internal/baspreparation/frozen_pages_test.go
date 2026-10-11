//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestFrozenRepositoryPagesRemainStableThroughLateApprovalVacuumAndReopen(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	tx := beginBASRepositoryTx(t, workspace)
	fixture := seedFrozenRepositoryFixture(t, tx, org, 11, true, 0)
	repository := newFrozenRepositoryForTest(t, tx)
	first, err := repository.Append(ctx, org, fixture.frozen, fixture.preparationRef)
	if err != nil {
		t.Fatal(err)
	}
	second := appendFrozenPageApproval(t, tx, org, fixture, 12)
	third := appendFrozenPageApproval(t, tx, org, fixture, 13)
	commitBASRepositoryTx(t, tx)
	makeVersionRowidsSparse(t, workspace.Database, "bas_frozen_bas_v1", "bas_frozen_bas_v1_no_update", org)
	repository = newFrozenRepositoryForTest(t, workspace.Database)
	request := FrozenListRequest{OrganisationID: org, Family: cloneRef(fixture.frozen.Family), PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || page.Page == nil || len(page.Frozen) != 1 || page.Page.NextCursor == nil ||
		!sameBASSourceRef(page.Frozen[0].FrozenBas, first) || !sameBASSourceRef(page.Frozen[0].Preparation, fixture.preparationRef) {
		t.Fatalf("first frozen page: %v %v", page, err)
	}
	cursor := *page.Page.NextCursor
	for _, kind := range []string{"organisation", "family_hash", "omit_filter", "tamper", "size"} {
		bad := request
		bad.Family = cloneRef(request.Family)
		bad.Cursor = &cursor
		switch kind {
		case "organisation":
			bad.OrganisationID = otherOrg
		case "family_hash":
			bad.Family.ContentHash[0] ^= 0xff
		case "omit_filter":
			bad.Family = nil
		case "tamper":
			changed := cursor + "!"
			bad.Cursor = &changed
		case "size":
			bad.PageSize = 201
		}
		if got, err := repository.List(ctx, bad); got != nil || err == nil {
			t.Fatalf("accepted %s: %v %v", kind, got, err)
		}
	}
	lateTx := beginBASRepositoryTx(t, workspace)
	fourth := appendFrozenPageApproval(t, lateTx, org, fixture, 14)
	commitBASRepositoryTx(t, lateTx)
	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	repository = newFrozenRepositoryForTest(t, reopened)
	request.Cursor = &cursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Frozen) != 1 || page.Page.NextCursor == nil || !sameBASSourceRef(page.Frozen[0].FrozenBas, second) {
		t.Fatalf("second frozen page after reopen: %v %v", page, err)
	}
	request.Cursor = page.Page.NextCursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Frozen) != 1 || page.Page.NextCursor != nil || !sameBASSourceRef(page.Frozen[0].FrozenBas, third) {
		t.Fatalf("last frozen page includes late approval: %v %v", page, err)
	}
	fresh, err := repository.List(ctx, FrozenListRequest{OrganisationID: org, PageSize: 200})
	if err != nil || fresh == nil || len(fresh.Frozen) != 4 || fresh.Page.ReturnedCount != 4 || !sameBASSourceRef(fresh.Frozen[3].FrozenBas, fourth) {
		t.Fatalf("fresh frozen page: %v %v", fresh, err)
	}
}

func TestFrozenRepositoryPagesRejectCorruptLookaheadAndEmptyScope(t *testing.T) {
	for _, kind := range []string{"body", "normalized", "parent"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, otherOrg := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx := context.Background()
			fixture := seedFrozenRepositoryFixture(t, tx, org, 21, true, 0)
			repository := newFrozenRepositoryForTest(t, tx)
			if _, err := repository.Append(ctx, org, fixture.frozen, fixture.preparationRef); err != nil {
				t.Fatal(err)
			}
			second := appendFrozenPageApproval(t, tx, org, fixture, 22)
			_ = appendFrozenPageApproval(t, tx, org, fixture, 23)
			empty, err := repository.List(ctx, FrozenListRequest{OrganisationID: otherOrg, PageSize: 1})
			if err != nil || empty == nil || len(empty.Frozen) != 0 || empty.Page.ReturnedCount != 0 || empty.Page.NextCursor != nil {
				t.Fatalf("empty scope: %v %v", empty, err)
			}
			switch kind {
			case "body", "normalized":
				if _, err := tx.ExecContext(ctx, "DROP TRIGGER bas_frozen_bas_v1_no_update"); err != nil {
					t.Fatal(err)
				}
				mutation := "frozen_proto=X'01'"
				if kind == "normalized" {
					mutation = "approval_reason='Changed normalized reason'"
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_frozen_bas_v1 SET "+mutation+" WHERE frozen_id=?", second.Id); err != nil {
					t.Fatal(err)
				}
			case "parent":
				value, err := repository.GetExact(ctx, org, second)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "DROP TRIGGER bas_preparation_versions_v1_no_update"); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=?", value.Preparation.Id, value.Preparation.Revision); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := repository.List(ctx, FrozenListRequest{OrganisationID: org, Family: fixture.frozen.Family, PageSize: 1}); got != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt %s lookahead: %v %v", kind, got, err)
			}
		})
	}
}

// Storage-only approvals leave the current owner review-ready. The real freeze
// command must advance its preparation/family atomically in the same transaction.
func appendFrozenPageApproval(t *testing.T, executor app.CommandSQLExecutor, org string,
	fixture frozenRepositoryTestFixture, number uint64) *tammyv1.SourceRef {
	t.Helper()
	ctx := context.Background()
	reader := newPreparationReaderForTest(t, executor)
	current, err := reader.GetCurrent(ctx, org, fixture.preparationRef.Id)
	if err != nil {
		t.Fatal(err)
	}
	owner := appendWarningAcknowledgementCurrentOwner(t, executor, org, preparationReference(current.Preparation), fixture.frozen.Validation)
	value := proto.Clone(fixture.frozen).(*tammyv1.BasFrozenBas)
	value.FrozenId, value.ContentHash, value.Preparation = testUUID(36000+number), nil, owner
	value.ApprovedAt.Seconds += int64(number)
	value.FreshFactorAssertionId = testUUID(37000 + number)
	sessionID := testUUID(38000 + number)
	// Rotate the active session while retaining each earlier approval's assertion
	// and session. These are storage fixtures, not fresh-factor authorization.
	if _, err := executor.ExecContext(ctx, `UPDATE application_sessions
		SET state=4,ended_at='2026-10-11T00:00:00Z',repository_version=repository_version+1 WHERE state=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO application_sessions(id,user_id,state,created_at,last_active_at,expires_at)
		SELECT ?,s.user_id,1,s.created_at,s.last_active_at,s.expires_at FROM application_sessions s
		JOIN factor_assertions f ON f.session_id=s.id WHERE f.id=?`, sessionID, fixture.frozen.FreshFactorAssertionId); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO factor_assertions(id,user_id,session_id,purpose,asserted_at,consumed)
		SELECT ?,user_id,?,'bas_freeze_manual_handoff','2026-10-11T00:00:00Z',0
		FROM factor_assertions WHERE id=?`, value.FreshFactorAssertionId, sessionID, fixture.frozen.FreshFactorAssertionId); err != nil {
		t.Fatal(err)
	}
	ref, err := newFrozenRepositoryForTest(t, executor).Append(ctx, org, value, owner)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
