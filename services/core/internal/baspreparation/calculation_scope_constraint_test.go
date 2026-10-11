//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestCalculationStorageRejectsReusingIdentityForAnotherPreparation(t *testing.T) {
	workspace, org, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()
	ctx := context.Background()
	value, input := seedCalculationRepositoryFixture(t, tx, org)
	calculations := newCalculationRepositoryForTest(t, tx)
	first, err := calculations.Append(ctx, value, input)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newPreparationReaderForTest(t, tx).GetExact(ctx, org, value.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	other, otherInput, err := appendSiblingCalculationOwner(t, tx, owner.Preparation)
	if err != nil {
		t.Fatal(err)
	}
	candidate := proto.Clone(value).(*tammyv1.BasCalculation)
	candidate.Preparation, candidate.Version, candidate.ContentHash = other, 2, nil
	wire, hash, err := sealCalculation(candidate)
	if err != nil {
		t.Fatal(err)
	}
	// All normalized foreign-key anchors remain valid. This deliberately uses
	// SQL to test the storage guard independently of repository preflight.
	_, err = tx.ExecContext(ctx, `INSERT INTO bas_calculations_v1 SELECT
		calculation_id,organisation_id,family_id,?,?,?,2,?,?,input_revision,input_hash,?,
		profile_id,profile_version,profile_hash,period_id,period_version,period_hash,outcome,source_snapshot_hash,
		coverage_manifest_hash,revision_vector_hash,rule_version,rule_hash,calculator_version,calculator_hash,
		form_version,form_hash,schema_version,schema_hash,descriptor_version,descriptor_hash,organisation_snapshot_hash,
		result_proto,workpaper_snapshot_proto,revision_vector_proto,organisation_snapshot_proto,?,created_at
		FROM bas_calculations_v1 WHERE calculation_id=? AND version=1`, other.Id, other.Revision, other.ContentHash, hash, otherInput.Id, otherInput.ContentHash, wire, first.Id)
	if err == nil {
		t.Fatal("calculation UUID was rebound to another preparation through storage")
	}
	assertBASRepositoryCount(t, tx, "bas_calculations_v1", 1)
}
