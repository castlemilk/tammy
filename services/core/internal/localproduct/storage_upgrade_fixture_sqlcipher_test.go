//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type historicalSchemaObject struct{ kind, name, table, statement string }

// Reconstruct a test-created workspace on a genuine migration prefix. Only the
// destination's retained columns are copied; source schema/migration rows never
// enter the historical fixture. All key handling stays in this test file.
func replaceWorkspaceWithHistoricalPrefix(t *testing.T, ctx context.Context, root string, passphrase []byte, target uint32) {
	t.Helper()
	key := historicalFixtureDEK(t, root, passphrase)
	defer workspace.Zero(key)
	path := filepath.Join(root, "core", "workspace", "tammy-workspace.db")
	fresh := filepath.Join(filepath.Dir(path), "historical-prefix-fixture.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, fresh, key, target); err != nil {
		t.Fatal(err)
	}
	source, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := sqlcipher.Open(ctx, fresh, key)
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	pristine := historicalSchemaSnapshot(t, ctx, destination)
	migrationRows := historicalRows(t, ctx, destination, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`)
	expected, err := migrations.Prefix(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrationRows) != len(expected) {
		t.Fatal("fresh prefix catalogue incomplete")
	}
	for i, row := range migrationRows {
		if row[0] != int64(expected[i].Version) || row[1] != expected[i].Name || row[2] != expected[i].SHA256 {
			t.Fatalf("fresh prefix migration %d differs", i+1)
		}
	}
	transplantHistoricalRows(t, ctx, source, destination, pristine)
	if !reflect.DeepEqual(pristine, historicalSchemaSnapshot(t, ctx, destination)) {
		t.Fatal("fixture schema differs from pristine migration prefix")
	}
	if !reflect.DeepEqual(migrationRows, historicalRows(t, ctx, destination, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`)) {
		t.Fatal("fixture migration catalogue was rewritten")
	}
	assertHistoricalFixtureIntegrity(t, ctx, destination)
	var future int
	if err := destination.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name IN ('purchase_postings_v1','purchase_payments_v1','purchase_accounting_events_v1')`).Scan(&future); err != nil || future != 0 {
		t.Fatalf("historical fixture contains future tables: %d %v", future, err)
	}
	if _, err := destination.QueryContext(ctx, `SELECT source_kind FROM journals LIMIT 0`); err == nil {
		t.Fatal("historical fixture contains future journal column")
	}
	if err := destination.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	// All handles are closed and WALs checkpointed before replacing this t.TempDir fixture.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fresh, path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(fresh + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func historicalFixtureDEK(t *testing.T, root string, passphrase []byte) []byte {
	t.Helper()
	master, err := os.ReadFile(filepath.Join(root, "core", "installation.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Zero(master)
	digest := sha256.New()
	digest.Write([]byte("tammy.local-composition.v1\x00workspace-header\x00"))
	digest.Write(master)
	auth := digest.Sum(nil)
	defer workspace.Zero(auth)
	store, err := workspace.NewHeaderStore(filepath.Join(root, "core", "workspace", "tammy-workspace.db.header"), auth)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	slots, err := store.Slots()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := workspace.UnwrapWithPassphrase(policy, passphrase, slots[0].PassphraseWrap, slots[0].WorkspaceID, slots[0].Version)
	if err != nil {
		t.Fatal("cannot unwrap authenticated test fixture")
	}
	if len(key) != sqlcipher.KeySize {
		workspace.Zero(key)
		t.Fatal("test fixture key length invalid")
	}
	return key
}

func historicalSchemaSnapshot(t *testing.T, ctx context.Context, db *sqlcipher.Database) []historicalSchemaObject {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []historicalSchemaObject
	for rows.Next() {
		var object historicalSchemaObject
		if err := rows.Scan(&object.kind, &object.name, &object.table, &object.statement); err != nil {
			t.Fatal(err)
		}
		out = append(out, object)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func transplantHistoricalRows(t *testing.T, ctx context.Context, source, destination *sqlcipher.Database, schema []historicalSchemaObject) {
	t.Helper()
	type tableRows struct {
		name    string
		columns []string
		rows    [][]any
		query   string
	}
	var tables []tableRows
	for _, object := range schema {
		if object.kind != "table" || object.name == "schema_migrations" {
			continue
		}
		quoted := historicalIdentifier(object.name)
		info, err := destination.QueryContext(ctx, `PRAGMA table_info(`+quoted+`)`)
		if err != nil {
			t.Fatal(err)
		}
		var columns, keys []string
		for info.Next() {
			var ordinal, notnull, pk int
			var name, kind string
			var defaultValue any
			if err := info.Scan(&ordinal, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
				t.Fatal(err)
			}
			columns = append(columns, historicalIdentifier(name))
			if pk > 0 {
				keys = append(keys, historicalIdentifier(name))
			}
		}
		if err := info.Err(); err != nil {
			t.Fatal(err)
		}
		info.Close()
		order := "rowid"
		if strings.Contains(strings.ToUpper(object.statement), "WITHOUT ROWID") {
			order = strings.Join(keys, ",")
		}
		query := `SELECT ` + strings.Join(columns, ",") + ` FROM ` + quoted + ` ORDER BY ` + order
		tables = append(tables, tableRows{object.name, columns, historicalRows(t, ctx, source, query), query})
	}
	conn, err := destination.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, object := range schema {
		if object.kind == "trigger" {
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER `+historicalIdentifier(object.name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+historicalIdentifier(table.name)); err != nil {
			t.Fatal(err)
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(table.columns)), ",")
		query := `INSERT INTO ` + historicalIdentifier(table.name) + `(` + strings.Join(table.columns, ",") + `) VALUES(` + marks + `)`
		for _, row := range table.rows {
			if _, err := tx.ExecContext(ctx, query, row...); err != nil {
				t.Fatalf("seed retained prefix table %s: %v", table.name, err)
			}
		}
	}
	// AUTOINCREMENT state belongs only to destination prefix tables.
	for _, object := range schema {
		if object.kind == "table" && strings.Contains(strings.ToUpper(object.statement), "AUTOINCREMENT") {
			var seq int64
			if err := source.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name=?`, object.name).Scan(&seq); err != nil && err != sql.ErrNoRows {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM sqlite_sequence WHERE name=?`, object.name); err != nil {
				t.Fatal(err)
			}
			if seq > 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence(name,seq) VALUES(?,?)`, object.name, seq); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, object := range schema {
		if object.kind == "trigger" {
			if _, err := tx.ExecContext(ctx, object.statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if got := historicalRows(t, ctx, destination, table.query); !reflect.DeepEqual(got, table.rows) {
			t.Fatalf("retained prefix rows differ: %s", table.name)
		}
	}
}

func historicalIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
func historicalRows(t *testing.T, ctx context.Context, db *sqlcipher.Database, query string) [][]any {
	t.Helper()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(columns))
		targets := make([]any, len(row))
		for i := range row {
			targets[i] = &row[i]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				row[i] = append([]byte(nil), b...)
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
func assertHistoricalFixtureIntegrity(t *testing.T, ctx context.Context, db *sqlcipher.Database) {
	t.Helper()
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid any
		var key int
		rows.Scan(&table, &rowid, &parent, &key)
		t.Fatal(fmt.Sprintf("fixture foreign key mismatch in %s", table))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
