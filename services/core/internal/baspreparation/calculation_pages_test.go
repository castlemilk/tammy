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

func TestCalculationRepositoryPagesPinPreparationAndSurviveVacuumAndReopen(t *testing.T) {
	workspace, organisationID, otherOrganisationID := newBASRepositoryWorkspace(t)
	ctx := context.Background()
	writeTx := beginBASRepositoryTx(t, workspace)
	calculations := newCalculationRepositoryForTest(t, writeTx)
	firstCalculation, savedInputs := seedCalculationRepositoryFixture(t, writeTx, organisationID)
	preparationV2 := cloneRef(firstCalculation.Preparation)
	firstRef, err := calculations.Append(ctx, firstCalculation, savedInputs)
	if err != nil {
		t.Fatalf("append first calculation: %v", err)
	}

	preparationV3 := appendCalculationPageOwner(t, writeTx, organisationID, preparationV2, firstRef)
	secondCalculation := proto.Clone(firstCalculation).(*tammyv1.BasCalculation)
	secondCalculation.Version++
	secondCalculation.CreatedAt.Seconds++
	secondCalculation.ContentHash = nil
	secondCalculation.Preparation = cloneRef(preparationV3)
	secondRef, err := calculations.Append(ctx, secondCalculation, savedInputs)
	if err != nil {
		t.Fatalf("append second version against refreshed preparation capture: %v", err)
	}

	thirdCalculation := proto.Clone(secondCalculation).(*tammyv1.BasCalculation)
	thirdCalculation.CalculationId = testUUID(9901) // Deliberately not lexical append order.
	thirdCalculation.Version = 1
	thirdCalculation.CreatedAt.Seconds++
	thirdCalculation.ContentHash = nil
	thirdRef, err := calculations.Append(ctx, thirdCalculation, savedInputs)
	if err != nil {
		t.Fatalf("append another calculation for the same preparation: %v", err)
	}

	owner, err := newPreparationReaderForTest(t, writeTx).GetExact(ctx, organisationID, preparationV3)
	if err != nil {
		t.Fatal(err)
	}
	siblingPreparation, siblingInputs, err := appendSiblingCalculationOwner(t, writeTx, owner.Preparation)
	if err != nil {
		t.Fatalf("append sibling preparation owner: %v", err)
	}
	siblingCalculation := proto.Clone(thirdCalculation).(*tammyv1.BasCalculation)
	siblingCalculation.CalculationId = testUUID(9902)
	siblingCalculation.Preparation = cloneRef(siblingPreparation)
	siblingCalculation.CreatedAt.Seconds++
	siblingCalculation.ContentHash = nil
	if _, err := calculations.Append(ctx, siblingCalculation, siblingInputs); err != nil {
		t.Fatalf("append sibling calculation: %v", err)
	}
	commitBASRepositoryTx(t, writeTx)

	// Exercise durable ordering with sparse append ordinals before issuing the cursor.
	makeVersionRowidsSparse(t, workspace.Database, "bas_calculations_v1", "bas_calculations_v1_no_update", organisationID)
	pageOneTx := beginBASRepositoryTx(t, workspace)
	pageOneRepository := newCalculationRepositoryForTest(t, pageOneTx)
	request := CalculationListRequest{OrganisationID: organisationID, Preparation: cloneRef(preparationV2), PageSize: 1}
	pageOne, err := pageOneRepository.List(ctx, request)
	if err != nil || pageOne == nil || pageOne.Page == nil || len(pageOne.Calculations) != 1 ||
		pageOne.Page.ReturnedCount != 1 || pageOne.Page.NextCursor == nil ||
		!sameBASSourceRef(pageOne.Calculations[0].Calculation, firstRef) ||
		!sameBASSourceRef(pageOne.Calculations[0].Preparation, preparationV2) {
		t.Fatalf("first calculation page should start at the historical owner capture: page=%v err=%v", pageOne, err)
	}
	cursor := *pageOne.Page.NextCursor

	for _, kind := range []string{"other_organisation", "newer_preparation_revision", "sibling_preparation", "missing_preparation_filter", "tampered_cursor", "overlarge_page"} {
		t.Run(kind, func(t *testing.T) {
			bad := CalculationListRequest{OrganisationID: organisationID, Preparation: cloneRef(preparationV2), PageSize: 1, Cursor: &cursor}
			switch kind {
			case "other_organisation":
				bad.OrganisationID = otherOrganisationID
			case "newer_preparation_revision":
				bad.Preparation = cloneRef(preparationV3)
			case "sibling_preparation":
				bad.Preparation = cloneRef(siblingPreparation)
			case "missing_preparation_filter":
				bad.Preparation = nil
			case "tampered_cursor":
				changed := cursor[:len(cursor)-1] + "!"
				bad.Cursor = &changed
			case "overlarge_page":
				bad.PageSize = 201
			}
			if page, err := pageOneRepository.List(ctx, bad); err == nil || page != nil {
				t.Fatalf("invalid %s calculation cursor accepted: page=%v err=%v", kind, page, err)
			}
		})
	}
	wrongHash := cloneRef(preparationV2)
	wrongHash.ContentHash[0] ^= 0xff
	if page, err := pageOneRepository.List(ctx, CalculationListRequest{OrganisationID: organisationID, Preparation: wrongHash, PageSize: 1}); err == nil || page != nil {
		t.Fatalf("calculation pages accepted an invalid exact preparation hash: page=%v err=%v", page, err)
	}
	commitBASRepositoryTx(t, pageOneTx)

	reopened := vacuumAndReopenVersionPageWorkspace(t, workspace)
	defer reopened.Close()
	lateWriteTx, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	lateCalculation := proto.Clone(secondCalculation).(*tammyv1.BasCalculation)
	lateCalculation.CalculationId = testUUID(9801)
	lateCalculation.Version = 1
	lateCalculation.CreatedAt.Seconds++
	lateCalculation.ContentHash = nil
	if _, err := newCalculationRepositoryForTest(t, lateWriteTx).Append(ctx, lateCalculation, savedInputs); err != nil {
		_ = lateWriteTx.Rollback()
		t.Fatalf("append calculation after the page watermark: %v", err)
	}
	commitBASRepositoryTx(t, lateWriteTx)

	readTx, err := reopened.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readTx.Rollback()
	pageRepository := newCalculationRepositoryForTest(t, readTx)
	request.Cursor = &cursor
	pageTwo, err := pageRepository.List(ctx, request)
	if err != nil || pageTwo == nil || pageTwo.Page == nil || len(pageTwo.Calculations) != 1 ||
		pageTwo.Page.ReturnedCount != 1 || pageTwo.Page.NextCursor == nil ||
		!sameBASSourceRef(pageTwo.Calculations[0].Calculation, secondRef) ||
		!sameBASSourceRef(pageTwo.Calculations[0].Preparation, preparationV3) {
		t.Fatalf("second calculation page after VACUUM/reopen should retain its owner capture: page=%v err=%v", pageTwo, err)
	}
	pageThree, err := pageRepository.List(ctx, CalculationListRequest{OrganisationID: organisationID, Preparation: cloneRef(preparationV2),
		PageSize: 1, Cursor: pageTwo.Page.NextCursor})
	if err != nil || pageThree == nil || pageThree.Page == nil || len(pageThree.Calculations) != 1 ||
		pageThree.Page.ReturnedCount != 1 || pageThree.Page.NextCursor != nil ||
		!sameBASSourceRef(pageThree.Calculations[0].Calculation, thirdRef) ||
		!sameBASSourceRef(pageThree.Calculations[0].Preparation, preparationV3) {
		t.Fatalf("last calculation page should include only the pre-watermark sibling attempt: page=%v err=%v", pageThree, err)
	}
}

func TestCalculationRepositoryPagesRejectCorruptReturnedAndLookaheadDependencies(t *testing.T) {
	for _, kind := range []string{"returned_coverage", "lookahead_coverage", "returned_preparation", "lookahead_preparation"} {
		t.Run(kind, func(t *testing.T) {
			workspace, organisationID, _ := newBASRepositoryWorkspace(t)
			ctx := context.Background()
			tx := beginBASRepositoryTx(t, workspace)
			calculations := newCalculationRepositoryForTest(t, tx)
			firstCalculation, savedInputs := seedCalculationRepositoryFixture(t, tx, organisationID)
			preparationV2 := cloneRef(firstCalculation.Preparation)
			firstRef, err := calculations.Append(ctx, firstCalculation, savedInputs)
			if err != nil {
				t.Fatal(err)
			}
			preparationV3 := appendCalculationPageOwner(t, tx, organisationID, preparationV2, firstRef)
			lookaheadCalculation := proto.Clone(firstCalculation).(*tammyv1.BasCalculation)
			lookaheadCalculation.CalculationId = testUUID(9903)
			lookaheadCalculation.CreatedAt.Seconds++
			lookaheadCalculation.Preparation = cloneRef(preparationV3)
			lookaheadCalculation.ContentHash = nil
			if _, err := calculations.Append(ctx, lookaheadCalculation, savedInputs); err != nil {
				t.Fatalf("append lookahead calculation: %v", err)
			}

			switch kind {
			case "returned_coverage", "lookahead_coverage":
				target := firstCalculation.CalculationId
				if kind == "lookahead_coverage" {
					target = lookaheadCalculation.CalculationId
				}
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_calculation_coverage_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_calculation_coverage_v1 SET coverage_proto=X'01' WHERE calculation_id=? AND calculation_version=1 AND sequence=1`, target); err != nil {
					t.Fatal(err)
				}
			case "returned_preparation", "lookahead_preparation":
				target := preparationV2
				if kind == "lookahead_preparation" {
					target = preparationV3
				}
				if _, err := tx.ExecContext(ctx, `DROP TRIGGER bas_preparation_versions_v1_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE bas_preparation_versions_v1 SET preparation_proto=X'01' WHERE preparation_id=? AND version=?`,
					target.Id, target.Revision); err != nil {
					t.Fatal(err)
				}
			}

			page, err := calculations.List(ctx, CalculationListRequest{OrganisationID: organisationID, Preparation: preparationV2, PageSize: 1})
			if page != nil || !errors.Is(err, ErrSnapshotCorrupt) {
				t.Fatalf("corrupt %s dependency was returned or skipped: page=%v err=%v", kind, page, err)
			}
		})
	}
}

func appendCalculationPageOwner(t *testing.T, executor app.CommandSQLExecutor, organisationID string,
	currentRef, currentCalculation *tammyv1.SourceRef) *tammyv1.SourceRef {
	t.Helper()
	ctx := context.Background()
	reader := newPreparationReaderForTest(t, executor)
	current, err := reader.GetExact(ctx, organisationID, currentRef)
	if err != nil {
		t.Fatalf("read current preparation for calculation page fixture: %v", err)
	}
	next := clonePreparationRetainedSnapshot(current)
	next.Preparation.Version++
	next.Preparation.UpdatedAt.Seconds++
	next.Preparation.ContentHash = nil
	next.Preparation.CurrentCalculation = cloneRef(currentCalculation)
	repository, err := NewPreparationRepository(executor, newBASRepositoryCursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := repository.Append(ctx, next, currentRef)
	if err != nil {
		t.Fatalf("append refreshed preparation capture for calculation page fixture: %v", err)
	}
	return ref
}
