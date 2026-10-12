//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
)

func TestBasRawSourceIdentityMigration14FailurePreservesSchema13History(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bas-raw-upgrade-rollback.db")
	key := testKey(0xe4)
	if _, err := MigrateWorkspace(ctx, path, key, 13); err != nil {
		t.Fatal(err)
	}

	var database *Database
	var err error
	database, err = Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if database != nil {
			_ = database.Close()
		}
	})
	purchaseSeedHistorical(t, database)
	addBasTestSecondOrganisation(t, database)
	seedBasLifecycleRows(t, database)
	seedBasRawNullableCanonicalCoverage(t, database)
	beforeTables := snapshotBasRawCoverageTables(t, database)
	beforeLedger := purchaseQueryHash(t, database, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`)
	var canonicalRows int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM bas_calculation_coverage_v1`).Scan(&canonicalRows); err != nil {
		t.Fatal(err)
	}
	if canonicalRows != 3 {
		t.Fatalf("seeded schema-13 canonical coverage rows = %d, want 3", canonicalRows)
	}

	steps := allMigrations(t)
	if len(steps) != 14 {
		t.Fatalf("embedded migration count = %d, want 14", len(steps))
	}
	if steps[13].Name != "0014_bas_raw_source_identities.sql" {
		t.Fatalf("embedded migration tail = %q, want 0014_bas_raw_source_identities.sql", steps[13].Name)
	}
	// Let the real 0014 table and triggers execute, then fail within the same
	// migration transaction. Recalculate only this modified step's checksum so
	// the failure exercises SQL rollback rather than migration-plan validation.
	failingSQL := append(append([]byte(nil), steps[13].SQL...), []byte("\nCREATE TABLE migration_0014_rollback_probe(id INTEGER PRIMARY KEY); SELECT * FROM migration_0014_missing_table;")...)
	digest := sha256.Sum256(failingSQL)
	steps[13] = migrations.Migration{
		Version: 14,
		Name:    steps[13].Name,
		SHA256:  hex.EncodeToString(digest[:]),
		SQL:     failingSQL,
	}
	if err := applyMigrations(ctx, database, steps, 14); err == nil {
		t.Fatal("schema-14 migration with injected SQL failure succeeded")
	}
	assertAppliedVersions(t, database, 13)
	assertTablesAbsent(t, database, []string{"bas_calculation_raw_coverage_v1", "migration_0014_rollback_probe"})
	if got := purchaseQueryHash(t, database, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`); got != beforeLedger {
		t.Fatal("failed schema-14 migration changed the schema-13 migration ledger")
	}
	afterTables := snapshotBasRawCoverageTables(t, database)
	if len(afterTables) != len(beforeTables) {
		t.Fatalf("failed migration changed BAS table count from %d to %d", len(beforeTables), len(afterTables))
	}
	for table, want := range beforeTables {
		if got, ok := afterTables[table]; !ok || got != want {
			t.Fatalf("failed migration changed schema-13 %s history (present=%t)", table, ok)
		}
	}
	if err := database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database = nil

	database, err = Open(ctx, path, key)
	if err != nil {
		t.Fatalf("reopen schema-13 workspace after failed migration: %v", err)
	}
	assertAppliedVersions(t, database, 13)
	assertTablesAbsent(t, database, []string{"bas_calculation_raw_coverage_v1", "migration_0014_rollback_probe"})
	if got := purchaseQueryHash(t, database, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`); got != beforeLedger {
		t.Fatal("reopened workspace did not retain the original schema-13 migration ledger")
	}
	afterReopen := snapshotBasRawCoverageTables(t, database)
	if len(afterReopen) != len(beforeTables) {
		t.Fatalf("reopened BAS table count = %d, want %d", len(afterReopen), len(beforeTables))
	}
	for table, want := range beforeTables {
		if got, ok := afterReopen[table]; !ok || got != want {
			t.Fatalf("reopen after failed migration changed schema-13 %s history (present=%t)", table, ok)
		}
	}
}
