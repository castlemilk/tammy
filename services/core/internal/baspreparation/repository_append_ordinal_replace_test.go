//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package baspreparation

import (
	"bytes"
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestCalculationAppendOrdinalCannotReplaceAnotherRetainedIdentity(t *testing.T) {
	workspace, organisationID, _ := newBASRepositoryWorkspace(t)
	tx := beginBASRepositoryTx(t, workspace)
	defer tx.Rollback()

	firstValue, inputsRef := seedCalculationRepositoryFixture(t, tx, organisationID)
	firstValue.Coverage = nil
	repository := newCalculationRepositoryForTest(t, tx)
	firstRef, err := repository.Append(context.Background(), firstValue, inputsRef)
	if err != nil {
		t.Fatalf("append first unreferenced calculation: %v", err)
	}
	var firstOrdinal int64
	if err := tx.QueryRowContext(context.Background(),
		`SELECT append_ordinal FROM bas_calculations_v1 WHERE calculation_id=? AND version=?`, firstRef.Id, firstRef.Revision).
		Scan(&firstOrdinal); err != nil {
		t.Fatalf("read first calculation append ordinal: %v", err)
	}

	secondValue := proto.Clone(firstValue).(*tammyv1.BasCalculation)
	secondValue.CalculationId = testUUID(9961)
	secondValue.ContentHash = nil
	secondValue.Coverage = nil
	secondRef, err := repository.Append(context.Background(), secondValue, inputsRef)
	if err != nil {
		t.Fatalf("append fresh unreferenced calculation identity: %v", err)
	}
	if secondRef.Id == firstRef.Id || secondRef.Revision != 1 {
		t.Fatalf("fresh calculation reference = %#v; want a distinct revision-one identity", secondRef)
	}

	// Preserve a valid row for the fresh identity, then remove its original
	// ordinal so the attempted insert conflicts only with the first identity's
	// append_ordinal. The retained candidate has no coverage or other children.
	if _, err := tx.ExecContext(context.Background(), `CREATE TEMP TABLE bas_calculation_ordinal_candidate AS
		SELECT * FROM bas_calculations_v1 WHERE calculation_id=? AND version=?`, secondRef.Id, secondRef.Revision); err != nil {
		t.Fatalf("copy valid replacement candidate: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `UPDATE bas_calculation_ordinal_candidate SET append_ordinal=?`, firstOrdinal); err != nil {
		t.Fatalf("set candidate's conflicting append ordinal: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `DROP TRIGGER bas_calculations_v1_no_delete`); err != nil {
		t.Fatalf("drop delete guard for isolated candidate cleanup: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `DELETE FROM bas_calculations_v1 WHERE calculation_id=? AND version=?`, secondRef.Id, secondRef.Revision); err != nil {
		t.Fatalf("remove candidate before replacement attempt: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `CREATE TRIGGER bas_calculations_v1_no_delete
		BEFORE DELETE ON bas_calculations_v1 BEGIN SELECT RAISE(ABORT,'BAS calculation is retained'); END`); err != nil {
		t.Fatalf("restore calculation delete guard: %v", err)
	}

	if _, err := tx.ExecContext(context.Background(), `INSERT OR REPLACE INTO bas_calculations_v1
		SELECT * FROM bas_calculation_ordinal_candidate`); err == nil {
		t.Fatal("INSERT OR REPLACE must not replace one retained calculation through an explicit append ordinal")
	}
	var retainedVersion int64
	var retainedHash []byte
	if err := tx.QueryRowContext(context.Background(), `SELECT version,content_hash FROM bas_calculations_v1 WHERE append_ordinal=?`, firstOrdinal).
		Scan(&retainedVersion, &retainedHash); err != nil {
		t.Fatalf("read retained first calculation after rejected replacement: %v", err)
	}
	if retainedVersion != int64(firstRef.Revision) || !bytes.Equal(retainedHash, firstRef.ContentHash) {
		t.Fatalf("append ordinal %d now holds version %d hash %x; want original version %d hash %x",
			firstOrdinal, retainedVersion, retainedHash, firstRef.Revision, firstRef.ContentHash)
	}
}
