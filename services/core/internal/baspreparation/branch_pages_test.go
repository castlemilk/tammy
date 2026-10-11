//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBranchStateRepositoryPagesKeepHistoricalVersionsAtSignedSnapshot(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	tx := beginBASRepositoryTx(t, workspace)
	root, _, branchRef, branches := seedBranchHistoryPageOwner(t, tx, org, codec, 9701)
	active, activeRef := seedTypedBranchPreparation(t, tx, root, branchRef.Id, 9702, 9703)
	stateOne, stateOneRef := appendBranchHistoryPageState(t, branches, ctx, org, branchRef, nil, nil, activeRef)

	// Advance the mutable preparation head while retaining its first exact
	// version. The branch history must retain both pins rather than resolve
	// state one through the latest preparation head.
	activeTwo, activeTwoRef := advanceTypedBranchHistoryPreparation(t, tx, active, activeRef)
	stateTwo, stateTwoRef := appendBranchHistoryPageState(t, branches, ctx, org, branchRef, stateOne, stateOneRef, activeTwoRef)
	stateThree, stateThreeRef := appendBranchHistoryPageState(t, branches, ctx, org, branchRef, stateTwo, stateTwoRef, activeTwoRef)
	commitBASRepositoryTx(t, tx)

	// Give the append key gaps before issuing a cursor. VACUUM/reopen plus a
	// later append must not move that cursor's endpoint or return the late row.
	makeVersionRowidsSparse(t, workspace.Database, "bas_branch_versions_v1", "bas_branch_versions_v1_no_update", org)
	pageOneTx := beginBASRepositoryTx(t, workspace)
	pageOneRepository, err := NewBranchStateRepository(pageOneTx, codec)
	if err != nil {
		t.Fatal(err)
	}
	request := BranchHistoryRequest{OrganisationID: org, Branch: branchRef, PageSize: 1}
	pageOne, err := pageOneRepository.List(ctx, request)
	if err != nil || pageOne == nil || pageOne.Page == nil || len(pageOne.States) != 1 || pageOne.Page.ReturnedCount != 1 || pageOne.Page.NextCursor == nil ||
		!sameBASSourceRef(branchStateReference(pageOne.States[0]), stateOneRef) ||
		!sameBASSourceRef(pageOne.States[0].ActiveAttempt.GetRetained(), activeRef) {
		t.Fatalf("first branch page = %#v, %v", pageOne, err)
	}
	cursor := *pageOne.Page.NextCursor
	commitBASRepositoryTx(t, pageOneTx)

	// Advance the preparation head and branch after the cursor watermark. The
	// cursor must still return state two and state three with their old exact
	// active-preparation pin.
	lateTx := beginBASRepositoryTx(t, workspace)
	_, activeThreeRef := advanceTypedBranchHistoryPreparation(t, lateTx, activeTwo, activeTwoRef)
	lateRepository, err := NewBranchStateRepository(lateTx, codec)
	if err != nil {
		t.Fatal(err)
	}
	stateFour, _ := appendBranchHistoryPageState(t, lateRepository, ctx, org, branchRef, stateThree,
		stateThreeRef, activeThreeRef)
	if stateFour.Version != 4 || !sameBASSourceRef(stateFour.ActiveAttempt.GetRetained(), activeThreeRef) {
		t.Fatalf("late branch state setup = %#v", stateFour)
	}
	commitBASRepositoryTx(t, lateTx)

	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	branches, err = NewBranchStateRepository(reopened, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &cursor
	pageTwo, err := branches.List(ctx, request)
	if err != nil || pageTwo == nil || pageTwo.Page == nil || len(pageTwo.States) != 1 || pageTwo.Page.ReturnedCount != 1 || pageTwo.Page.NextCursor == nil ||
		!sameBASSourceRef(branchStateReference(pageTwo.States[0]), stateTwoRef) ||
		!sameBASSourceRef(pageTwo.States[0].ActiveAttempt.GetRetained(), activeTwoRef) {
		t.Fatalf("second branch page after append/VACUUM/reopen = %#v, %v", pageTwo, err)
	}
	pageThree, err := branches.List(ctx, BranchHistoryRequest{OrganisationID: org, Branch: branchRef,
		PageSize: 1, Cursor: pageTwo.Page.NextCursor})
	if err != nil || pageThree == nil || pageThree.Page == nil || len(pageThree.States) != 1 || pageThree.Page.ReturnedCount != 1 || pageThree.Page.NextCursor != nil ||
		!sameBASSourceRef(branchStateReference(pageThree.States[0]), stateThreeRef) ||
		!sameBASSourceRef(pageThree.States[0].ActiveAttempt.GetRetained(), activeTwoRef) {
		t.Fatalf("last branch page followed mutable head or included late state: %#v, %v", pageThree, err)
	}
	if pageThree.States[0].Version == stateFour.Version || sameBASSourceRef(activeTwoRef, activeThreeRef) {
		t.Fatal("page fixture did not distinguish the historical state and preparation pins")
	}
}

func TestBranchStateRepositoryPagesBindOrganisationBranchAndCursor(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
	root, rootRef, branchRef, branches := seedBranchHistoryPageOwner(t, tx, org, codec, 9711)
	stateOne, stateOneRef := appendBranchHistoryPageState(t, branches, ctx, org, branchRef, nil, nil, nil)
	_, _ = appendBranchHistoryPageState(t, branches, ctx, org, branchRef, stateOne, stateOneRef, nil)
	identities, err := NewBranchIdentityRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	emptyBranch, err := identities.Append(ctx, typedReplacementIdentity(root, rootRef, 9712))
	if err != nil {
		t.Fatal(err)
	}
	page, err := branches.List(ctx, BranchHistoryRequest{OrganisationID: org, Branch: branchRef, PageSize: 1})
	if err != nil || page == nil || page.Page == nil || len(page.States) != 1 || page.Page.NextCursor == nil {
		t.Fatalf("first branch page: %#v, %v", page, err)
	}
	cursor := *page.Page.NextCursor

	for _, kind := range []string{"other_organisation", "changed_branch_filter", "wrong_branch_hash", "wrong_branch_revision", "tampered_cursor", "overlarge_page"} {
		t.Run(kind, func(t *testing.T) {
			bad := BranchHistoryRequest{OrganisationID: org, Branch: cloneRef(branchRef), PageSize: 1, Cursor: &cursor}
			switch kind {
			case "other_organisation":
				bad.OrganisationID = otherOrg
			case "changed_branch_filter":
				bad.Branch = emptyBranch
			case "wrong_branch_hash":
				bad.Branch.ContentHash[0] ^= 0xff
			case "wrong_branch_revision":
				bad.Branch.Revision++
			case "tampered_cursor":
				changed := cursor[:len(cursor)-1] + "!"
				bad.Cursor = &changed
			case "overlarge_page":
				bad.PageSize = 201
			}
			if got, err := branches.List(ctx, bad); err == nil || got != nil {
				t.Fatalf("invalid %s cursor accepted: %#v, %v", kind, got, err)
			}
		})
	}
	wrongHash := cloneRef(branchRef)
	wrongHash.ContentHash[0] ^= 0xff
	if got, err := branches.List(ctx, BranchHistoryRequest{OrganisationID: org, Branch: wrongHash, PageSize: 1}); err == nil || got != nil {
		t.Fatalf("wrong exact branch identity accepted: %#v, %v", got, err)
	}
	empty, err := branches.List(ctx, BranchHistoryRequest{OrganisationID: org, Branch: emptyBranch, PageSize: 1})
	if err != nil || empty == nil || empty.Page == nil || len(empty.States) != 0 || empty.Page.ReturnedCount != 0 || empty.Page.NextCursor != nil {
		t.Fatalf("valid branch with no retained states = %#v, %v", empty, err)
	}
}

func TestBranchStateRepositoryPagesRejectCorruptLookahead(t *testing.T) {
	for _, kind := range []string{"lookahead_body", "lookahead_normalized", "lookahead_parent"} {
		t.Run(kind, func(t *testing.T) {
			workspace, org, _ := newBASRepositoryWorkspace(t)
			tx := beginBASRepositoryTx(t, workspace)
			defer tx.Rollback()
			ctx, codec := context.Background(), newBASRepositoryCursorCodec(t)
			root, _, branchRef, branches := seedBranchHistoryPageOwner(t, tx, org, codec, 9721)
			firstAttempt, firstAttemptRef := seedTypedBranchPreparation(t, tx, root, branchRef.Id, 9722, 9723)
			stateOne, stateOneRef := appendBranchHistoryPageState(t, branches, ctx, org, branchRef, nil, nil, nil)
			stateTwo, stateTwoRef := appendBranchHistoryPageState(t, branches, ctx, org, branchRef, stateOne, stateOneRef, firstAttemptRef)
			_, secondAttemptRef := seedTypedBranchPreparation(t, tx, root, branchRef.Id, 9724, 9725)
			_, _ = appendBranchHistoryPageState(t, branches, ctx, org, branchRef, stateTwo,
				stateTwoRef, secondAttemptRef)

			switch kind {
			case "lookahead_body":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_branch_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_branch_versions_v1 SET branch_state_proto=X'01' WHERE branch_id=? AND version=2`, branchRef.Id); err != nil {
					t.Fatal(err)
				}
			case "lookahead_normalized":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_branch_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_branch_versions_v1 SET workflow_state='CLOSED' WHERE branch_id=? AND version=2`, branchRef.Id); err != nil {
					t.Fatal(err)
				}
			case "lookahead_parent":
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=1`, firstAttempt.PreparationId); err != nil {
					t.Fatal(err)
				}
			}
			if page, err := branches.List(ctx, BranchHistoryRequest{OrganisationID: org, Branch: branchRef, PageSize: 1}); page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt %s lookahead was skipped or returned: %#v, %v", kind, page, err)
			}
		})
	}
}

func seedBranchHistoryPageOwner(t *testing.T, executor app.CommandSQLExecutor, org string, codec *paging.Codec,
	branchNumber uint64) (*tammyv1.BasPreparation, *tammyv1.SourceRef, *tammyv1.SourceRef, *BranchStateRepository) {
	t.Helper()
	ctx := context.Background()
	root, rootRef := seedTypedCollectingPreparation(t, executor, org)
	families, err := NewFamilyStateRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := families.Append(ctx, org, initialTypedFamilyState(root, rootRef), nil); err != nil {
		t.Fatal(err)
	}
	identities, err := NewBranchIdentityRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	branchRef, err := identities.Append(ctx, typedReplacementIdentity(root, rootRef, branchNumber))
	if err != nil {
		t.Fatal(err)
	}
	branches, err := NewBranchStateRepository(executor, codec)
	if err != nil {
		t.Fatal(err)
	}
	return root, rootRef, branchRef, branches
}

func appendBranchHistoryPageState(t *testing.T, repository *BranchStateRepository, ctx context.Context, org string,
	branchRef *tammyv1.SourceRef, previous *tammyv1.BasBranchState, expectedCurrent, activePreparation *tammyv1.SourceRef) (*tammyv1.BasBranchState, *tammyv1.SourceRef) {
	t.Helper()
	var value *tammyv1.BasBranchState
	if previous == nil {
		value = &tammyv1.BasBranchState{BranchIdentity: cloneRef(branchRef), Version: 1,
			WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
			UpdatedAt:     &timestamppb.Timestamp{Seconds: 1791748800}}
	} else {
		value = proto.Clone(previous).(*tammyv1.BasBranchState)
		value.Version++
		value.ContentHash = nil
		value.UpdatedAt.Seconds++
	}
	if activePreparation == nil {
		value.ActiveAttempt = absentIdentity()
	} else {
		value.ActiveAttempt = retainedExpectedIdentity(activePreparation, activePreparation.Revision)
	}
	ref, err := repository.Append(ctx, org, value, expectedCurrent)
	if err != nil {
		t.Fatalf("append branch history state %d: %v", value.Version, err)
	}
	return value, ref
}

func advanceTypedBranchHistoryPreparation(t *testing.T, executor app.CommandSQLExecutor, current *tammyv1.BasPreparation,
	currentRef *tammyv1.SourceRef) (*tammyv1.BasPreparation, *tammyv1.SourceRef) {
	t.Helper()
	next := proto.Clone(current).(*tammyv1.BasPreparation)
	next.Version++
	next.ContentHash = nil
	next.UpdatedAt = proto.Clone(current.UpdatedAt).(*timestamppb.Timestamp)
	next.UpdatedAt.Seconds++
	nextRef := appendTypedCollectingPreparation(t, executor, next)
	updatedAt, ok := basTimestampSQLText(next.UpdatedAt)
	if !ok {
		t.Fatal("invalid advanced preparation timestamp")
	}
	result, err := executor.ExecContext(context.Background(), `UPDATE bas_preparation_heads_v1
		SET current_version=?,current_hash=?,cas_version=cas_version+1,updated_at=?
		WHERE preparation_id=? AND organisation_id=? AND current_version=? AND current_hash=? AND cas_version=?`,
		int64(next.Version), nextRef.ContentHash, updatedAt, next.PreparationId, next.OrganisationId,
		int64(currentRef.Revision), currentRef.ContentHash, int64(currentRef.Revision))
	if err != nil {
		t.Fatalf("advance retained preparation head: %v", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("advanced preparation head changed %d rows: %v", affected, err)
	}
	return next, nextRef
}
