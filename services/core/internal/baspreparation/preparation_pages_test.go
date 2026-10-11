//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"errors"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestPreparationRepositoryPagesRetainLatestAttemptVersionAtSignedSnapshot(t *testing.T) {
	workspace, org, otherOrg := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	repository := newPreparationRepositoryForValidationTest(t, tx)
	root := typedPreparationForAppend(t, tx, org)
	rootRef, err := repository.Append(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err = repository.GetExact(ctx, org, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	families, _ := NewFamilyStateRepository(tx, newBASRepositoryCursorCodec(t))
	if _, err := families.Append(ctx, org, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
		t.Fatal(err)
	}
	first := appendPreparationListAttempt(t, tx, repository, root, rootRef, 9611)
	second := appendPreparationListAttempt(t, tx, repository, root, rootRef, 9621)
	updated := clonePreparationRetainedSnapshot(root)
	updated.Preparation.Version++
	updated.Preparation.ContentHash = nil
	updated.Preparation.UpdatedAt.Seconds++
	updated.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED
	rootVersionTwo, err := repository.Append(ctx, updated, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	request := PreparationListRequest{OrganisationID: org, Family: root.Preparation.Family, PageSize: 1}
	page, err := repository.List(ctx, request)
	if err != nil || page == nil || len(page.Preparations) != 1 || page.Page.ReturnedCount != 1 || page.Page.NextCursor == nil ||
		!sameBASSourceRef(page.Preparations[0].Preparation, first) {
		t.Fatalf("first snapshot page: page=%v err=%v", page, err)
	}
	cursor := *page.Page.NextCursor
	// New versions of returned and unseen attempts, and newly created attempts,
	// must neither duplicate nor redirect rows in the existing page snapshot.
	firstBody, err := repository.GetExact(ctx, org, first)
	if err != nil {
		t.Fatal(err)
	}
	firstNext := clonePreparationRetainedSnapshot(firstBody)
	firstNext.Preparation.Version++
	firstNext.Preparation.ContentHash = nil
	firstNext.Preparation.UpdatedAt.Seconds++
	firstNext.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED
	if _, err := repository.Append(ctx, firstNext, first); err != nil {
		t.Fatal(err)
	}
	updated, err = repository.GetExact(ctx, org, rootVersionTwo)
	if err != nil {
		t.Fatal(err)
	}
	rootNext := clonePreparationRetainedSnapshot(updated)
	rootNext.Preparation.Version++
	rootNext.Preparation.ContentHash = nil
	rootNext.Preparation.UpdatedAt.Seconds++
	rootNext.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	currentRoot, err := repository.Append(ctx, rootNext, rootVersionTwo)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = repository.GetExact(ctx, org, currentRoot)
	if err != nil {
		t.Fatal(err)
	}
	appendPreparationListAttempt(t, tx, repository, updated, currentRoot, 9631)
	commitBASRepositoryTx(t, tx)
	readTx := beginBASRepositoryTx(t, workspace)
	defer readTx.Rollback()
	repository = newPreparationRepositoryForValidationTest(t, readTx)
	request.Cursor = &cursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Preparations) != 1 || page.Page.ReturnedCount != 1 || page.Page.NextCursor == nil ||
		!sameBASSourceRef(page.Preparations[0].Preparation, second) {
		t.Fatalf("second snapshot page after restart: page=%v err=%v", page, err)
	}
	request.Cursor = page.Page.NextCursor
	page, err = repository.List(ctx, request)
	if err != nil || page == nil || len(page.Preparations) != 1 || page.Page.ReturnedCount != 1 || page.Page.NextCursor != nil ||
		!sameBASSourceRef(page.Preparations[0].Preparation, rootVersionTwo) || page.Preparations[0].State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED {
		t.Fatalf("last page redirected historical workflow state: page=%v err=%v", page, err)
	}
	for _, kind := range []string{"other_organisation", "changed_family_filter", "tampered_cursor", "wrong_family_hash", "overlarge_page"} {
		t.Run(kind, func(t *testing.T) {
			bad := PreparationListRequest{OrganisationID: org, Family: cloneRef(root.Preparation.Family), PageSize: 1, Cursor: &cursor}
			switch kind {
			case "other_organisation":
				bad.OrganisationID = otherOrg
			case "changed_family_filter":
				bad.Family = nil
			case "tampered_cursor":
				changed := cursor[:len(cursor)-1] + "!"
				bad.Cursor = &changed
			case "wrong_family_hash":
				bad.Family.ContentHash[0] ^= 0xff
			case "overlarge_page":
				bad.PageSize = 201
			}
			if page, err := repository.List(ctx, bad); err == nil || page != nil {
				t.Fatalf("invalid %s page accepted: %v %v", kind, page, err)
			}
		})
	}
	fresh, err := repository.List(ctx, PreparationListRequest{OrganisationID: org, Family: root.Preparation.Family, PageSize: 200})
	if err != nil || fresh == nil || len(fresh.Preparations) != 4 || fresh.Page.ReturnedCount != 4 || fresh.Page.NextCursor != nil {
		t.Fatalf("fresh page must include four attempts, once each: %v %v", fresh, err)
	}
	empty, err := repository.List(ctx, PreparationListRequest{OrganisationID: otherOrg})
	if err != nil || empty == nil || len(empty.Preparations) != 0 || empty.Page.ReturnedCount != 0 || empty.Page.NextCursor != nil {
		t.Fatalf("empty scoped page: %v %v", empty, err)
	}
}

func TestPreparationRepositoryPagesRejectCorruptLookaheadBody(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	repository := newPreparationRepositoryForValidationTest(t, tx)
	root := typedPreparationForAppend(t, tx, org)
	rootRef, err := repository.Append(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err = repository.GetExact(ctx, org, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	families, _ := NewFamilyStateRepository(tx, newBASRepositoryCursorCodec(t))
	if _, err := families.Append(ctx, org, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
		t.Fatal(err)
	}
	lookahead := appendPreparationListAttempt(t, tx, repository, root, rootRef, 9641)
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=?`, lookahead.Id); err != nil {
		t.Fatal(err)
	}
	if page, err := repository.List(ctx, PreparationListRequest{OrganisationID: org, PageSize: 1}); page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
		t.Fatalf("corrupt lookahead was skipped or returned: %v %v", page, err)
	}
}

func appendPreparationListAttempt(t *testing.T, executor app.CommandSQLExecutor, repository *PreparationRepository,
	root *PreparationRetainedSnapshot, rootRef *tammyv1.SourceRef, number uint64) *tammyv1.SourceRef {
	t.Helper()
	identities, err := NewBranchIdentityRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	families, err := NewFamilyStateRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	family, err := families.GetCurrent(context.Background(), root.Preparation.OrganisationId, root.Preparation.Family)
	if err != nil {
		t.Fatal(err)
	}
	branch, err := identities.Append(context.Background(), typedReplacementIdentity(root.Preparation, family.RootPreparation, number))
	if err != nil {
		t.Fatal(err)
	}
	attempt := clonePreparationRetainedSnapshot(root)
	attempt.Preparation.PreparationId, attempt.AttemptID, attempt.BranchID = testUUID(number+1), testUUID(number+2), &branch.Id
	attempt.Preparation.Version, attempt.Preparation.ContentHash = 1, nil
	attempt.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
	attempt.Preparation.Predecessor = cloneRef(rootRef)
	ref, err := repository.Append(context.Background(), attempt, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
