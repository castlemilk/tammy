//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func newBasLifecycleConstraintFixture(t *testing.T, beforeCorrectiveAdoption ...func(*Database, map[string]any)) *Database {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "lifecycle.db"), testKey(0x93))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := applyMigrations(context.Background(), db, allMigrations(t), 13); err != nil {
		t.Fatal(err)
	}
	purchaseSeedHistorical(t, db)
	addBasTestSecondOrganisation(t, db)
	seedBasLifecycleRows(t, db, beforeCorrectiveAdoption...)
	return db
}

// SQL-shape tests deliberately use opaque bodies; codec tests independently
// verify canonical protobuf bodies and their complete hashes.
func basFixtureRow(t *testing.T, db *Database, table, predicate string, args ...any) map[string]any {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT * FROM `+table+` WHERE `+predicate, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || !rows.Next() {
		t.Fatalf("fixture row missing: %s err=%v", table, err)
	}
	values := make([]any, len(columns))
	destinations := make([]any, len(columns))
	for i := range values {
		destinations[i] = &values[i]
	}
	if err := rows.Scan(destinations...); err != nil {
		t.Fatal(err)
	}
	result := make(map[string]any, len(columns))
	for i, column := range columns {
		// These fields seed a new retained fact. Its storage append ordinal is
		// allocated by SQLite rather than copied from the source snapshot.
		if column == "append_ordinal" {
			continue
		}
		result[column] = values[i]
	}
	if rows.Next() || rows.Err() != nil {
		t.Fatal("fixture predicate did not resolve exactly one row")
	}
	return result
}

func clearBasFixtureBarrier(row map[string]any) map[string]any {
	return purchaseChanged(row, map[string]any{"barrier_preparation_id": nil, "barrier_preparation_version": nil,
		"barrier_preparation_hash": nil, "barrier_attempt_id": nil, "barrier_frozen_id": nil, "barrier_frozen_hash": nil})
}

func TestBasCorrectiveAdoptionRequiresBarrierForAlreadyFrozenAttempt(t *testing.T) {
	newBasLifecycleConstraintFixture(t, func(db *Database, adopted map[string]any) {
		purchaseInsert(t, db, "bas_branch_versions_v1", clearBasFixtureBarrier(adopted), false)
	})
}

func TestBasCorrectiveRetryClearsFrozenBarrierOnlyAfterExactTerminalPredecessor(t *testing.T) {
	db := newBasLifecycleConstraintFixture(t)
	branch := basFixtureRow(t, db, "bas_branch_versions_v1", "version=2")
	nextBranch := clearBasFixtureBarrier(purchaseChanged(branch, map[string]any{"version": 3, "content_hash": bytes.Repeat([]byte{0x44}, 32), "branch_state_proto": []byte("corrective-retry-v3")}))
	// The already-frozen attempt cannot clear its own pre-resolution barrier.
	purchaseInsert(t, db, "bas_branch_versions_v1", nextBranch, false)
	terminal := basFixtureRow(t, db, "bas_preparation_versions_v1", "preparation_id=? AND version=5", branch["active_preparation_id"])
	terminal = purchaseChanged(terminal, map[string]any{"version": 6, "state": "SUPERSEDED_BEFORE_CONFIRMATION", "content_hash": bytes.Repeat([]byte{0x35}, 32), "preparation_proto": []byte("unused-attempt-v6")})
	appendBasFixturePreparation(t, db, terminal)
	newID := "0199c108-0000-7000-8000-000000000001"
	identity := basFixtureRow(t, db, "bas_preparation_identities_v1", "preparation_id=?", branch["active_preparation_id"])
	identity = purchaseChanged(identity, map[string]any{"preparation_id": newID, "attempt_id": "retry-attempt", "predecessor_id": branch["active_preparation_id"]})
	purchaseInsert(t, db, "bas_preparation_identities_v1", identity, true)
	retry := purchaseChanged(terminal, map[string]any{"preparation_id": newID, "version": 1, "state": "COLLECTING", "content_hash": bytes.Repeat([]byte{0x36}, 32), "preparation_proto": []byte("retry-collecting-v1"),
		"input_snapshot_id": nil, "input_content_hash": nil, "calculation_id": nil, "calculation_version": nil, "calculation_hash": nil,
		"validation_id": nil, "validation_revision": nil, "validation_hash": nil, "freeze_id": nil, "freeze_hash": nil,
		"predecessor_id": terminal["preparation_id"], "predecessor_version": 6, "predecessor_hash": terminal["content_hash"]})
	appendBasFixturePreparation(t, db, retry)
	nextBranch = purchaseChanged(nextBranch, map[string]any{"active_preparation_id": newID, "active_attempt_id": "retry-attempt", "active_attempt_version": 1, "active_attempt_hash": retry["content_hash"]})
	purchaseInsert(t, db, "bas_branch_versions_v1", nextBranch, true)
	if err := migrationRelationalIntegrityCheck(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestBasCorrectiveAdoptionCanReserveMutableAttemptWithoutFrozenBarrier(t *testing.T) {
	db := newBasLifecycleConstraintFixture(t)
	old := basFixtureRow(t, db, "bas_branch_versions_v1", "version=2")
	closed := purchaseChanged(old, map[string]any{"version": 3, "workflow_state": "CLOSED", "content_hash": bytes.Repeat([]byte{0x44}, 32), "branch_state_proto": []byte("closed-v3")})
	purchaseInsert(t, db, "bas_branch_versions_v1", closed, true)
	assertBasRowsAffected(t, db, 1, `UPDATE bas_branch_heads_v1 SET current_version=3,current_hash=?,cas_version=3 WHERE branch_id=? AND cas_version=2`, closed["content_hash"], old["branch_id"])
	newBranchID := "0199c108-0000-7000-8000-000000000011"
	identity := basFixtureRow(t, db, "bas_branch_identities_v1", "branch_id=?", old["branch_id"])
	identity = purchaseChanged(identity, map[string]any{"branch_id": newBranchID, "identity_hash": bytes.Repeat([]byte{0x45}, 32), "branch_proto": []byte("mutable-branch-identity")})
	purchaseInsert(t, db, "bas_branch_identities_v1", identity, true)
	newPrepID := "0199c108-0000-7000-8000-000000000012"
	prepIdentity := basFixtureRow(t, db, "bas_preparation_identities_v1", "preparation_id=?", old["active_preparation_id"])
	prepIdentity = purchaseChanged(prepIdentity, map[string]any{"preparation_id": newPrepID, "attempt_id": "mutable-attempt", "branch_id": newBranchID})
	purchaseInsert(t, db, "bas_preparation_identities_v1", prepIdentity, true)
	prep := basFixtureRow(t, db, "bas_preparation_versions_v1", "preparation_id=? AND version=1", old["active_preparation_id"])
	prep = purchaseChanged(prep, map[string]any{"preparation_id": newPrepID, "version": 1, "content_hash": bytes.Repeat([]byte{0x46}, 32), "preparation_proto": []byte("mutable-collecting")})
	appendBasFixturePreparation(t, db, prep)
	active := clearBasFixtureBarrier(purchaseChanged(old, map[string]any{"branch_id": newBranchID, "version": 1, "workflow_state": "ACTIVE", "corrective_resolution_id": nil, "corrective_resolution_hash": nil,
		"active_preparation_id": newPrepID, "active_attempt_id": "mutable-attempt", "active_attempt_version": 1, "active_attempt_hash": prep["content_hash"], "content_hash": bytes.Repeat([]byte{0x47}, 32), "branch_state_proto": []byte("active-mutable-v1")}))
	purchaseInsert(t, db, "bas_branch_versions_v1", active, true)
	purchaseInsert(t, db, "bas_branch_heads_v1", map[string]any{"branch_id": newBranchID, "organisation_id": "org", "family_id": old["family_id"], "current_version": 1, "current_hash": active["content_hash"], "cas_version": 1, "updated_at": active["created_at"]}, true)
	adopted := purchaseChanged(active, map[string]any{"version": 2, "workflow_state": "CORRECTIVE", "corrective_resolution_id": old["corrective_resolution_id"], "corrective_resolution_hash": old["corrective_resolution_hash"], "content_hash": bytes.Repeat([]byte{0x48}, 32), "branch_state_proto": []byte("adopted-mutable-v2")})
	purchaseInsert(t, db, "bas_branch_versions_v1", adopted, true)
	if err := migrationRelationalIntegrityCheck(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestBasFamilyStateRetainsEffectiveOriginalAndConfirmedHead(t *testing.T) {
	for _, mutation := range []string{"head_preparation_id=NULL,head_version=NULL,head_hash=NULL", "effective_original_version=7,effective_original_hash=zeroblob(32)"} {
		t.Run(mutation, func(t *testing.T) {
			db := newBasLifecycleConstraintFixture(t)
			if mutation == "effective_original_version=7,effective_original_hash=zeroblob(32)" {
				mutation = "effective_original_version=7,effective_original_hash=" + "X'" + "3232323232323232323232323232323232323232323232323232323232323232'"
			}
			assertExecFails(t, db, `UPDATE bas_family_state_v1 SET cas_version=5,family_state_proto=X'05',state_hash=zeroblob(32),`+mutation+` WHERE organisation_id='org' AND cas_version=4`)
		})
	}
}
