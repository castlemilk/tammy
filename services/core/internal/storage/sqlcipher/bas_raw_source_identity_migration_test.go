//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
)

const basRawSourceIdentityMigration13SHA256 = "1fd46bdded18cdb399eeca9f2dbacebdf3cd904f1488cefabdc3694a94d309ed"

func TestBasRawSourceIdentityKeepsSchema13PrefixAndHasNo13Table(t *testing.T) {
	steps, err := migrations.Prefix(13)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 13 || steps[12].Name != "0013_bas_preparation.sql" || steps[12].SHA256 != basRawSourceIdentityMigration13SHA256 {
		t.Fatalf("accepted schema-13 migration changed: %#v", steps)
	}
	prior, err := migrations.Prefix(12)
	if err != nil {
		t.Fatal(err)
	}
	for i := range prior {
		if steps[i].Version != prior[i].Version || steps[i].Name != prior[i].Name || steps[i].SHA256 != prior[i].SHA256 || !bytes.Equal(steps[i].SQL, prior[i].SQL) {
			t.Fatalf("historical migration prefix changed at version %d", i+1)
		}
	}

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bas-raw-prefix-13.db")
	key := testKey(0xe1)
	if _, err := MigrateWorkspace(ctx, path, key, 13); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertTablesAbsent(t, db, []string{"bas_calculation_raw_coverage_v1"})
	assertAppliedVersions(t, db, 13)
}

func TestBasRawSourceIdentityMigration14PreservesCanonicalCoverageAndLedger(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bas-raw-upgrade.db")
	key := testKey(0xe2)
	if _, err := MigrateWorkspace(ctx, path, key, 13); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	purchaseSeedHistorical(t, db)
	addBasTestSecondOrganisation(t, db)
	seedBasLifecycleRows(t, db)
	seedBasRawNullableCanonicalCoverage(t, db)
	before := snapshotBasRawCoverageTables(t, db)
	beforeLedger := purchaseQueryHash(t, db, `SELECT version,name,sha256 FROM schema_migrations WHERE version<=13 ORDER BY version`)
	var canonicalRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM bas_calculation_coverage_v1`).Scan(&canonicalRows); err != nil {
		t.Fatal(err)
	}
	if canonicalRows != 3 {
		t.Fatalf("canonical coverage fixture rows = %d, want 3", canonicalRows)
	}
	if err := db.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateWorkspace(ctx, path, key, 14); err != nil {
		t.Fatalf("schema-13 to schema-14 migration failed: %v", err)
	}
	db, err = Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	after := snapshotBasRawCoverageTables(t, db)
	for table, want := range before {
		if got, ok := after[table]; !ok || got != want {
			t.Fatalf("migration changed historical %s rows (present=%t)", table, ok)
		}
	}
	if len(after) != len(before)+1 {
		t.Fatalf("schema-14 BAS table set grew from %d to %d; want only the raw companion", len(before), len(before)+1)
	}
	if _, ok := after["bas_calculation_raw_coverage_v1"]; !ok {
		t.Fatal("schema-14 raw coverage table missing from BAS table inventory")
	}
	if got := purchaseQueryHash(t, db, `SELECT version,name,sha256 FROM schema_migrations WHERE version<=13 ORDER BY version`); got != beforeLedger {
		t.Fatal("migration changed accepted migration 1-13 ledger rows")
	}
	assertAppliedVersions(t, db, 14)
	var rawRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM bas_calculation_raw_coverage_v1`).Scan(&rawRows); err != nil {
		t.Fatal(err)
	}
	if rawRows != 0 {
		t.Fatalf("migration backfilled raw rows: %d", rawRows)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrationRelationalIntegrityCheck(ctx, db); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "bas-raw-upgrade-backup.db")
	backupFile, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.OnlineBackupTo(ctx, backupPath, backupFile, key); err != nil {
		_ = backupFile.Close()
		t.Fatal(err)
	}
	if err := backupFile.Sync(); err != nil {
		_ = backupFile.Close()
		t.Fatal(err)
	}
	if err := backupFile.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := Open(ctx, backupPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	backupSnapshot := snapshotBasRawCoverageTables(t, backup)
	for table, want := range after {
		if got, ok := backupSnapshot[table]; !ok || got != want {
			t.Fatalf("encrypted backup changed %s rows (present=%t)", table, ok)
		}
	}
	assertAppliedVersions(t, backup, 14)
}

func TestBasRawSourceIdentitySchemaAndSharedSequenceGuards(t *testing.T) {
	db := newBasRawSourceIdentityV14Fixture(t, 0xe3)
	ctx := context.Background()

	assertBasRawCoverageColumns(t, db)
	assertBasRawCoverageParentForeignKey(t, db)

	raw := basRawCoverageRow(3)
	raw["raw_source_id"] = strings.Repeat("legacy-row-", 32) // 352 bytes; raw identifiers have no canonical 256-byte cap.
	purchaseInsert(t, db, "bas_calculation_raw_coverage_v1", raw, true)

	var gotID string
	var revision sql.NullInt64
	var originalHash []byte
	var computedHash []byte
	var proofScope string
	if err := db.QueryRowContext(ctx, `SELECT raw_source_id,original_revision,original_content_hash,computed_proof_hash,proof_scope
		FROM bas_calculation_raw_coverage_v1 WHERE calculation_id=? AND calculation_version=1 AND sequence=3`, basRawCoverageCalculationID).
		Scan(&gotID, &revision, &originalHash, &computedHash, &proofScope); err != nil {
		t.Fatal(err)
	}
	if gotID != raw["raw_source_id"].(string) || revision.Valid || originalHash != nil {
		t.Fatalf("nullable original provenance changed: id length=%d revision=%#v original hash=%x", len(gotID), revision, originalHash)
	}
	if !bytes.Equal(computedHash, raw["computed_proof_hash"].([]byte)) || proofScope != "SOURCE_ROW_PROJECTION" {
		t.Fatalf("computed proof was not retained independently: hash=%x scope=%q", computedHash, proofScope)
	}
	// A same-table conflict must not replace immutable raw history. SQLite's
	// REPLACE conflict algorithm can delete the old row without firing its
	// delete trigger, so the migration needs an explicit insert guard too.
	sameRawSequence := purchaseChanged(raw, map[string]any{
		"raw_source_id": "replacement-at-same-raw-sequence",
		"reason":        "attempted replacement",
	})
	replaceSQL, replaceArgs := purchaseInsertSQL("bas_calculation_raw_coverage_v1", sameRawSequence, "INSERT OR REPLACE")
	if _, err := db.ExecContext(ctx, replaceSQL, replaceArgs...); err == nil {
		t.Fatal("INSERT OR REPLACE changed an existing raw row at the same calculation sequence")
	}
	var afterReplaceID string
	if err := db.QueryRowContext(ctx, `SELECT raw_source_id FROM bas_calculation_raw_coverage_v1
		WHERE calculation_id=? AND calculation_version=1 AND sequence=3`, basRawCoverageCalculationID).Scan(&afterReplaceID); err != nil {
		t.Fatal(err)
	}
	if afterReplaceID != gotID {
		t.Fatalf("failed replacement changed raw identity from %q to %q", gotID, afterReplaceID)
	}

	// Zero is a valid original revision when the source row actually retained it.
	zeroRevision := basRawCoverageRow(5)
	zeroRevision["raw_source_id"] = "source-with-revision-zero"
	zeroRevision["original_revision"] = 0
	zeroRevision["original_content_hash"] = bytes.Repeat([]byte{0x41}, 32)
	purchaseInsert(t, db, "bas_calculation_raw_coverage_v1", zeroRevision, true)
	var gotRevision sql.NullInt64
	var gotOriginal []byte
	if err := db.QueryRowContext(ctx, `SELECT original_revision,original_content_hash FROM bas_calculation_raw_coverage_v1
		WHERE calculation_id=? AND calculation_version=1 AND sequence=5`, basRawCoverageCalculationID).Scan(&gotRevision, &gotOriginal); err != nil {
		t.Fatal(err)
	}
	if !gotRevision.Valid || gotRevision.Int64 != 0 || !bytes.Equal(gotOriginal, zeroRevision["original_content_hash"].([]byte)) {
		t.Fatalf("present zero revision/hash changed: revision=%#v hash=%x", gotRevision, gotOriginal)
	}
	for sequence, scope := range map[int]string{17: "COMPLETE_JOURNAL", 18: "REFERENCE_ONLY_PROJECTION"} {
		row := purchaseChanged(raw, map[string]any{
			"sequence": sequence, "raw_source_id": "scope-" + scope, "proof_scope": scope,
		})
		purchaseInsert(t, db, "bas_calculation_raw_coverage_v1", row, true)
	}
	for sequence, sourceType := range map[int]string{20: "tax__facts", 21: "tax_facts_"} {
		row := purchaseChanged(raw, map[string]any{
			"sequence": sequence, "raw_source_id": "type-" + sourceType, "raw_source_type": sourceType,
		})
		purchaseInsert(t, db, "bas_calculation_raw_coverage_v1", row, true)
	}

	for name, changes := range map[string]map[string]any{
		"oversized raw source id":     {"sequence": 22, "raw_source_id": strings.Repeat("x", (1<<20)+1)},
		"nonpositive sequence":        {"sequence": 0},
		"negative original revision":  {"sequence": 6, "original_revision": -1},
		"short original hash":         {"sequence": 7, "original_content_hash": []byte{1}},
		"short computed hash":         {"sequence": 8, "computed_proof_hash": []byte{1}},
		"unsupported scope":           {"sequence": 9, "proof_scope": "INVENTED_SCOPE"},
		"blank raw type":              {"sequence": 10, "raw_source_type": ""},
		"blank raw id":                {"sequence": 11, "raw_source_id": "  "},
		"wrong composite parent hash": {"sequence": 12, "calculation_hash": bytes.Repeat([]byte{0x22}, 32)},
		"non-snake raw type":          {"sequence": 14, "raw_source_type": "TaxFacts"},
		"raw type too long":           {"sequence": 19, "raw_source_type": strings.Repeat("a", 129)},
	} {
		t.Run(name, func(t *testing.T) {
			purchaseInsert(t, db, "bas_calculation_raw_coverage_v1", purchaseChanged(raw, changes), false)
		})
	}
	for name, changes := range map[string]map[string]any{
		"oversized lineage frame":  {"sequence": 13, "lineage_proto": bytes.Repeat([]byte{0x01}, (2<<20)+1)},
		"oversized evidence frame": {"sequence": 15, "evidence_proto": bytes.Repeat([]byte{0x01}, (2<<20)+1)},
		"oversized coverage frame": {"sequence": 16, "coverage_proto": bytes.Repeat([]byte{0x01}, (1<<20)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			purchaseInsert(t, db, "bas_calculation_raw_coverage_v1", purchaseChanged(raw, changes), false)
		})
	}

	// Both table directions reject a sequence collision even when SQLite is asked to replace.
	canonicalAtRawSequence := basCanonicalCoverageRow(3)
	for _, verb := range []string{"INSERT", "INSERT OR REPLACE"} {
		q, args := purchaseInsertSQL("bas_calculation_coverage_v1", canonicalAtRawSequence, verb)
		if _, err := db.ExecContext(ctx, q, args...); err == nil {
			t.Fatalf("canonical %s replaced a raw row at the same calculation sequence", verb)
		}
	}
	canonical := basCanonicalCoverageRow(4)
	purchaseInsert(t, db, "bas_calculation_coverage_v1", canonical, true)
	rawAtCanonicalSequence := purchaseChanged(raw, map[string]any{"sequence": 4, "raw_source_id": "different-raw-row"})
	for _, verb := range []string{"INSERT", "INSERT OR REPLACE"} {
		q, args := purchaseInsertSQL("bas_calculation_raw_coverage_v1", rawAtCanonicalSequence, verb)
		if _, err := db.ExecContext(ctx, q, args...); err == nil {
			t.Fatalf("raw %s replaced a canonical row at the same calculation sequence", verb)
		}
	}
	for _, query := range []string{
		`UPDATE bas_calculation_raw_coverage_v1 SET reason=reason WHERE calculation_id='` + basRawCoverageCalculationID + `' AND sequence=3`,
		`DELETE FROM bas_calculation_raw_coverage_v1 WHERE calculation_id='` + basRawCoverageCalculationID + `' AND sequence=3`,
	} {
		assertExecFails(t, db, query)
	}
}

const (
	basRawCoverageCalculationID = "0199c104-0000-7000-8000-000000000001"
	basRawCoverageFamilyID      = "0199c101-0000-7000-8000-000000000005"
)

func newBasRawSourceIdentityV14Fixture(t *testing.T, keyByte byte) *Database {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bas-raw-schema-14.db")
	key := testKey(keyByte)
	if _, err := MigrateWorkspace(ctx, path, key, 14); err != nil {
		t.Fatalf("migrate to schema 14: %v", err)
	}
	db, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	purchaseSeedHistorical(t, db)
	addBasTestSecondOrganisation(t, db)
	seedBasLifecycleRows(t, db)
	seedBasRawNullableCanonicalCoverage(t, db)
	return db
}

func seedBasRawNullableCanonicalCoverage(t *testing.T, db *Database) {
	t.Helper()
	purchaseInsert(t, db, "bas_calculation_coverage_v1", map[string]any{
		"calculation_id": basRawCoverageCalculationID, "organisation_id": "org", "family_id": basRawCoverageFamilyID,
		"calculation_version": 1, "calculation_hash": bytes.Repeat([]byte{0x82}, 32), "sequence": 2,
		"candidate_kind": "MANUAL_TAX_FACT", "disposition": "EXCLUDED_WITH_REASON",
		"source_type": "tax_facts", "source_id": "manual-tax-fact", "source_revision": 1,
		"source_hash": bytes.Repeat([]byte{0x31}, 32), "eligible_amount_minor": nil, "currency_code": nil, "tax_date": nil,
		"classification_code": "UNCLASSIFIED", "reason": "historical row had no classified amount",
		"lineage_proto": []byte{0x21}, "evidence_proto": []byte{0x22}, "coverage_proto": []byte{0x23},
		"content_hash": bytes.Repeat([]byte{0x24}, 32),
	}, true)
}

func basRawCoverageRow(sequence int) map[string]any {
	return map[string]any{
		"calculation_id": basRawCoverageCalculationID, "organisation_id": "org", "family_id": basRawCoverageFamilyID,
		"calculation_version": 1, "calculation_hash": bytes.Repeat([]byte{0x82}, 32), "sequence": sequence,
		"candidate_kind": "MANUAL_TAX_FACT", "disposition": "EXCLUDED_WITH_REASON",
		"raw_source_type": "tax_facts", "raw_source_id": "legacy-tax-row", "original_revision": nil,
		"original_content_hash": nil, "computed_proof_hash": bytes.Repeat([]byte{0x51}, 32), "proof_scope": "SOURCE_ROW_PROJECTION",
		"eligible_amount_minor": nil, "currency_code": nil, "tax_date": nil,
		"classification_code": "UNCLASSIFIED", "reason": "legacy fact retained without fabricated source hash",
		"lineage_proto": []byte{0x61}, "evidence_proto": []byte{0x62}, "coverage_proto": []byte{0x63},
		"content_hash": bytes.Repeat([]byte{0x64}, 32),
	}
}

func basCanonicalCoverageRow(sequence int) map[string]any {
	return map[string]any{
		"calculation_id": basRawCoverageCalculationID, "organisation_id": "org", "family_id": basRawCoverageFamilyID,
		"calculation_version": 1, "calculation_hash": bytes.Repeat([]byte{0x82}, 32), "sequence": sequence,
		"candidate_kind": "MANUAL_TAX_FACT", "disposition": "EXCLUDED_WITH_REASON",
		"source_type": "tax_facts", "source_id": "canonical-tax-row", "source_revision": 1,
		"source_hash": bytes.Repeat([]byte{0x71}, 32), "eligible_amount_minor": nil, "currency_code": nil, "tax_date": nil,
		"classification_code": "UNCLASSIFIED", "reason": "canonical collision guard fixture",
		"lineage_proto": []byte{0x72}, "evidence_proto": []byte{0x73}, "coverage_proto": []byte{0x74},
		"content_hash": bytes.Repeat([]byte{0x75}, 32),
	}
}

func assertBasRawCoverageColumns(t *testing.T, db *Database) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `PRAGMA table_info('bas_calculation_raw_coverage_v1')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type column struct {
		typ     string
		notNull int
		pk      int
	}
	want := map[string]column{
		"calculation_id": {"TEXT", 1, 1}, "organisation_id": {"TEXT", 1, 0}, "family_id": {"TEXT", 1, 0},
		"calculation_version": {"INTEGER", 1, 2}, "calculation_hash": {"BLOB", 1, 0}, "sequence": {"INTEGER", 1, 3},
		"candidate_kind": {"TEXT", 1, 0}, "disposition": {"TEXT", 1, 0}, "raw_source_type": {"TEXT", 1, 0},
		"raw_source_id": {"TEXT", 1, 0}, "original_revision": {"INTEGER", 0, 0}, "original_content_hash": {"BLOB", 0, 0},
		"computed_proof_hash": {"BLOB", 1, 0}, "proof_scope": {"TEXT", 1, 0}, "eligible_amount_minor": {"INTEGER", 0, 0},
		"currency_code": {"TEXT", 0, 0}, "tax_date": {"TEXT", 0, 0}, "classification_code": {"TEXT", 1, 0},
		"reason": {"TEXT", 1, 0}, "lineage_proto": {"BLOB", 1, 0}, "evidence_proto": {"BLOB", 1, 0},
		"coverage_proto": {"BLOB", 1, 0}, "content_hash": {"BLOB", 1, 0},
	}
	got := make(map[string]column, len(want))
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		got[name] = column{typ: typ, notNull: notNull, pk: pk}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("raw coverage schema has %d columns, want %d: %#v", len(got), len(want), got)
	}
	for name, expected := range want {
		if actual, ok := got[name]; !ok || actual != expected {
			t.Errorf("raw coverage column %q = %#v (present=%t), want %#v", name, actual, ok, expected)
		}
	}
}

func assertBasRawCoverageParentForeignKey(t *testing.T, db *Database) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `PRAGMA foreign_key_list('bas_calculation_raw_coverage_v1')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type pair struct {
		seq      int
		from, to string
	}
	want := []pair{
		{seq: 0, from: "calculation_id", to: "calculation_id"},
		{seq: 1, from: "organisation_id", to: "organisation_id"},
		{seq: 2, from: "family_id", to: "family_id"},
		{seq: 3, from: "calculation_version", to: "version"},
		{seq: 4, from: "calculation_hash", to: "content_hash"},
	}
	got := make([]pair, 0, len(want))
	fkID := -1
	for rows.Next() {
		var id, sequence int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &sequence, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatal(err)
		}
		if table != "bas_calculations_v1" || onDelete != "RESTRICT" {
			t.Fatalf("raw coverage FK target/action = %s/%s", table, onDelete)
		}
		if fkID == -1 {
			fkID = id
		} else if id != fkID {
			t.Fatalf("raw coverage parent tuple split across foreign keys: IDs %d and %d", fkID, id)
		}
		got = append(got, pair{seq: sequence, from: from, to: to})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].seq < got[j].seq })
	if len(got) != len(want) {
		t.Fatalf("raw coverage foreign key has %d columns, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].seq != i || got[i].from != want[i].from || got[i].to != want[i].to {
			t.Fatalf("raw coverage foreign-key pair %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func snapshotBasRawCoverageTables(t *testing.T, db *Database) map[string][32]byte {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT name FROM sqlite_schema WHERE type='table' AND name LIKE 'bas_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	out := make(map[string][32]byte, len(tables))
	for _, table := range tables {
		out[table] = purchaseQueryHash(t, db, `SELECT * FROM `+quoteBasSQLIdentifier(table)+` ORDER BY rowid`)
	}
	return out
}
