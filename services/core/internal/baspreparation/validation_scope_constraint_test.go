//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidationStorageRejectsReusingIdentityForAnotherPreparation(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	fixture := seedValidationRepositoryFixture(t, tx, org)
	first, err := newValidationRepositoryForTest(t, tx).Append(ctx, org, fixture.validation, fixture.ownerRef)
	if err != nil {
		t.Fatal(err)
	}
	sibling, inputs, err := appendSiblingCalculationOwner(t, tx, fixture.owner.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	calculation := proto.Clone(fixture.calculation).(*tammyv1.BasCalculation)
	calculation.CalculationId, calculation.Preparation, calculation.ContentHash = testUUID(9959), sibling, nil
	calculationRef, err := newCalculationRepositoryForTest(t, tx).Append(ctx, calculation, inputs)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newPreparationReaderForTest(t, tx).GetExact(ctx, org, sibling)
	if err != nil {
		t.Fatal(err)
	}
	next := clonePreparationRetainedSnapshot(owner)
	next.Preparation.Version++
	next.Preparation.UpdatedAt.Seconds++
	next.Preparation.ContentHash = nil
	next.Preparation.CurrentCalculation = calculationRef
	ownerRef, err := newPreparationRepositoryForValidationTest(t, tx).Append(ctx, next, sibling)
	if err != nil {
		t.Fatal(err)
	}
	owner, err = newPreparationReaderForTest(t, tx).GetExact(ctx, org, ownerRef)
	if err != nil {
		t.Fatal(err)
	}
	candidate := nextValidationRevision(fixture.validation)
	candidate.PreparationId, candidate.PreparationHash = ownerRef.Id, ownerRef.ContentHash
	candidate.CalculationId, candidate.CalculationHash = calculationRef.Id, calculationRef.ContentHash
	wire, hash, err := sealValidation(candidate, owner.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	// The alternate owner, calculation and saved envelope are all valid scoped
	// parents. Only the validation identity's immutable owner differs.
	_, err = tx.ExecContext(ctx, `INSERT INTO bas_validations_v1 SELECT
		validation_id,organisation_id,family_id,?,?,2,?,?,?, ?,?,source_snapshot_hash,input_revision,input_hash,?,?,?,created_at
		FROM bas_validations_v1 WHERE validation_id=? AND revision=1`, ownerRef.Id, int64(ownerRef.Revision), hash, ownerRef.ContentHash,
		calculationRef.Id, int64(calculationRef.Revision), calculationRef.ContentHash, inputs.Id, inputs.ContentHash, wire, first.Id)
	if err == nil {
		t.Fatal("validation UUID was rebound to another preparation through storage")
	}
	assertBASRepositoryCount(t, tx, "bas_validations_v1", 1)
}
