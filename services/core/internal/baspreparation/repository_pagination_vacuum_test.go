//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"database/sql"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/testkit"
)

func TestProfileRepositoryCursorSurvivesVacuumAndReopen(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)

	writeTx := beginBASRepositoryTx(t, workspace)
	writeRepository := newProfileRepositoryForTest(t, writeTx, codec)
	first := validBASProfile(organisationID, testUUID(9851), 1)
	firstRef := appendBASProfile(t, writeRepository, first, nil)
	appendBASProfile(t, writeRepository, nextBASProfile(first, firstRef, 2), firstRef)
	appendBASProfile(t, writeRepository, validBASProfile(organisationID, testUUID(9852), 1), nil)
	appendBASProfile(t, writeRepository, validBASProfile(organisationID, testUUID(9853), 1), nil)
	commitBASRepositoryTx(t, writeTx)

	makeVersionRowidsSparse(t, workspace.Database, "bas_profile_versions_v1", "bas_profile_versions_v1_no_update", organisationID)
	pageOneTx := beginBASRepositoryTx(t, workspace)
	pageOneRepository := newProfileRepositoryForTest(t, pageOneTx, codec)
	pageOne, err := pageOneRepository.List(ctx, ProfileListRequest{OrganisationID: organisationID, PageSize: 2})
	if err != nil || pageOne == nil || pageOne.Page == nil || len(pageOne.Profiles) != 2 || pageOne.Page.NextCursor == nil {
		t.Fatalf("first profile page before VACUUM = %#v, %v", pageOne, err)
	}
	if pageOne.Profiles[0].Profile.Id != first.ProfileId || pageOne.Profiles[0].Profile.Revision != 1 ||
		pageOne.Profiles[1].Profile.Id != first.ProfileId || pageOne.Profiles[1].Profile.Revision != 2 {
		t.Fatalf("first profile page order = %#v", pageOne.Profiles)
	}
	cursor := *pageOne.Page.NextCursor
	commitBASRepositoryTx(t, pageOneTx)

	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	pageTwo, err := newProfileRepositoryForTest(t, reopened, newBASRepositoryCursorCodec(t)).List(ctx,
		ProfileListRequest{OrganisationID: organisationID, PageSize: 2, Cursor: &cursor})
	if err != nil || pageTwo == nil || pageTwo.Page == nil || len(pageTwo.Profiles) != 2 || pageTwo.Page.NextCursor != nil {
		t.Fatalf("second profile page after VACUUM/reopen = %#v, %v", pageTwo, err)
	}
	if pageTwo.Profiles[0].Profile.Id != testUUID(9852) || pageTwo.Profiles[1].Profile.Id != testUUID(9853) {
		t.Fatalf("profile cursor skipped or repeated retained revisions after VACUUM: %#v", pageTwo.Profiles)
	}
}

func TestPeriodRepositoryCursorSurvivesVacuumAndReopen(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	fullYear := &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 1, 1), EndDate: testCivilDate(2026, 12, 31)}

	writeTx := beginBASRepositoryTx(t, workspace)
	profileRef := appendBASProfile(t, newProfileRepositoryForTest(t, writeTx, codec), validBASProfile(organisationID, testUUID(9861), 1), nil)
	periodRepository := newPeriodRepositoryForTest(t, writeTx, codec)
	quarters := []struct {
		id     string
		start  *tammyv1.CivilDate
		end    *tammyv1.CivilDate
		window tammyv1.BasPeriodWindow
	}{
		{testUUID(9862), testCivilDate(2026, 1, 1), testCivilDate(2026, 3, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR},
		{testUUID(9863), testCivilDate(2026, 4, 1), testCivilDate(2026, 6, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN},
		{testUUID(9864), testCivilDate(2026, 7, 1), testCivilDate(2026, 9, 30), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP},
		{testUUID(9865), testCivilDate(2026, 10, 1), testCivilDate(2026, 12, 31), tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC},
	}
	for _, quarter := range quarters {
		appendBASPeriod(t, periodRepository, validBASPeriod(organisationID, quarter.id, profileRef, 1,
			quarter.start, quarter.end, quarter.window), nil)
	}
	commitBASRepositoryTx(t, writeTx)

	makeVersionRowidsSparse(t, workspace.Database, "bas_period_versions_v1", "bas_period_versions_v1_no_update", organisationID)
	pageOneTx := beginBASRepositoryTx(t, workspace)
	pageOneRepository := newPeriodRepositoryForTest(t, pageOneTx, codec)
	pageOne, err := pageOneRepository.List(ctx, PeriodListRequest{OrganisationID: organisationID, Range: fullYear, PageSize: 2})
	if err != nil || pageOne == nil || pageOne.Page == nil || len(pageOne.Periods) != 2 || pageOne.Page.NextCursor == nil {
		t.Fatalf("first period page before VACUUM = %#v, %v", pageOne, err)
	}
	cursor := *pageOne.Page.NextCursor
	commitBASRepositoryTx(t, pageOneTx)

	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	pageTwo, err := newPeriodRepositoryForTest(t, reopened, newBASRepositoryCursorCodec(t)).List(ctx,
		PeriodListRequest{OrganisationID: organisationID, Range: fullYear, PageSize: 2, Cursor: &cursor})
	if err != nil || pageTwo == nil || pageTwo.Page == nil || len(pageTwo.Periods) != 2 || pageTwo.Page.NextCursor != nil {
		t.Fatalf("second period page after VACUUM/reopen = %#v, %v", pageTwo, err)
	}
	if pageTwo.Periods[0].Period.Id != testUUID(9864) || pageTwo.Periods[1].Period.Id != testUUID(9865) {
		t.Fatalf("period cursor skipped or repeated retained versions after VACUUM: %#v", pageTwo.Periods)
	}
}

func TestPreparationRepositoryCursorSurvivesVacuumAndReopen(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	ctx := context.Background()
	codec := newBASRepositoryCursorCodec(t)
	repository := newPreparationRepositoryForValidationTest(t, tx)
	root := typedPreparationForAppend(t, tx, organisationID)
	rootRef, err := repository.Append(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err = repository.GetExact(ctx, organisationID, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	families, err := NewFamilyStateRepository(tx, codec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := families.Append(ctx, organisationID, initialTypedFamilyState(root.Preparation, rootRef), nil); err != nil {
		t.Fatal(err)
	}
	first := appendPreparationListAttempt(t, tx, repository, root, rootRef, 9871)
	second := appendPreparationListAttempt(t, tx, repository, root, rootRef, 9881)
	updated := clonePreparationRetainedSnapshot(root)
	updated.Preparation.Version++
	updated.Preparation.ContentHash = nil
	updated.Preparation.UpdatedAt.Seconds++
	updated.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED
	rootVersionTwo, err := repository.Append(ctx, updated, rootRef)
	if err != nil {
		t.Fatal(err)
	}
	commitBASRepositoryTx(t, tx)

	makeVersionRowidsSparse(t, workspace.Database, "bas_preparation_versions_v1", "bas_preparation_versions_v1_no_update", organisationID)
	pageOneTx := beginBASRepositoryTx(t, workspace)
	pageOneRepository := newPreparationRepositoryForValidationTest(t, pageOneTx)
	pageOne, err := pageOneRepository.List(ctx, PreparationListRequest{OrganisationID: organisationID, Family: root.Preparation.Family, PageSize: 1})
	if err != nil || pageOne == nil || pageOne.Page == nil || len(pageOne.Preparations) != 1 || pageOne.Page.NextCursor == nil ||
		!sameBASSourceRef(pageOne.Preparations[0].Preparation, first) {
		t.Fatalf("first preparation page before VACUUM = %#v, %v", pageOne, err)
	}
	cursor := *pageOne.Page.NextCursor
	commitBASRepositoryTx(t, pageOneTx)

	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	pageTwo, err := newPreparationRepositoryForValidationTest(t, reopened).List(ctx,
		PreparationListRequest{OrganisationID: organisationID, Family: root.Preparation.Family, PageSize: 1, Cursor: &cursor})
	if err != nil || pageTwo == nil || pageTwo.Page == nil || len(pageTwo.Preparations) != 1 || pageTwo.Page.NextCursor == nil ||
		!sameBASSourceRef(pageTwo.Preparations[0].Preparation, second) {
		t.Fatalf("second preparation page after VACUUM/reopen = %#v, %v", pageTwo, err)
	}
	pageThree, err := newPreparationRepositoryForValidationTest(t, reopened).List(ctx,
		PreparationListRequest{OrganisationID: organisationID, Family: root.Preparation.Family, PageSize: 1, Cursor: pageTwo.Page.NextCursor})
	if err != nil || pageThree == nil || pageThree.Page == nil || len(pageThree.Preparations) != 1 || pageThree.Page.NextCursor != nil ||
		!sameBASSourceRef(pageThree.Preparations[0].Preparation, rootVersionTwo) {
		t.Fatalf("last preparation page after VACUUM/reopen = %#v, %v", pageThree, err)
	}
}

// makeVersionRowidsSparse creates gaps in the stable append key before the
// cursor is issued. With an explicit INTEGER PRIMARY KEY this updates its
// rowid alias; on older schemas it updates SQLite's hidden rowid.
func makeVersionRowidsSparse(t *testing.T, database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, table, immutableTrigger, organisationID string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), "DROP TRIGGER "+immutableTrigger); err != nil {
		t.Fatalf("drop immutable trigger to create sparse append ordinals: %v", err)
	}
	result, err := database.ExecContext(context.Background(), "UPDATE "+table+" SET rowid=rowid+1000 WHERE organisation_id=?", organisationID)
	if err != nil {
		t.Fatalf("create sparse append ordinals in %s: %v", table, err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected < 2 {
		t.Fatalf("sparse append ordinal fixture changed %d rows in %s: %v", affected, table, err)
	}
}

func vacuumAndReopenVersionPageWorkspace(t *testing.T, workspace *testkit.EncryptedWorkspace) *sqlcipher.Database {
	t.Helper()
	database := workspace.Database
	if _, err := database.ExecContext(context.Background(), "VACUUM"); err != nil {
		t.Fatalf("VACUUM version page workspace: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reopened, err := sqlcipher.Open(ctx, workspace.Path, workspace.Key)
	if err != nil {
		t.Fatalf("reopen workspace after VACUUM: %v", err)
	}
	return reopened
}
