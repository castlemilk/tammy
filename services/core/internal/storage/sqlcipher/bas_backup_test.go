//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func snapshotBasRetentionTables(t *testing.T, db *Database) map[string][32]byte {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'bas_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(tables) < 28 {
		t.Fatalf("missing BAS retention tables: %d", len(tables))
	}
	result := make(map[string][32]byte, len(tables))
	for _, table := range tables {
		result[table] = purchaseQueryHash(t, db, `SELECT * FROM `+quoteBasSQLIdentifier(table)+` ORDER BY rowid`)
	}
	return result
}

func TestBasRetentionSurvivesEncryptedOnlineBackupRestoreAndReopen(t *testing.T) {
	source := newBasLifecycleConstraintFixture(t)
	want := snapshotBasRetentionTables(t, source)
	key := testKey(0x93)
	copyTo := func(database *Database, path string) {
		t.Helper()
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := database.OnlineBackupTo(context.Background(), path, file, key); err != nil {
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(path)
		if err != nil || len(wire) < 16 || bytes.HasPrefix(wire, []byte("SQLite format 3\x00")) {
			t.Fatalf("backup is not an encrypted database: %v", err)
		}
	}
	backupPath := filepath.Join(t.TempDir(), "bas-backup.db")
	copyTo(source, backupPath)
	// A later live export event must not enter the already captured backup.
	export := basFixtureRow(t, source, "bas_export_events_v1", "phase='STARTED'")
	purchaseInsert(t, source, "bas_export_events_v1", purchaseChanged(export, map[string]any{
		"export_event_id": "0199c109-0000-7000-8000-000000000001", "operation_key": "later-export",
		"event_version": 2,
		"content_hash":  bytes.Repeat([]byte{0xf1}, 32), "export_proto": []byte("later-live-export")}), true)
	backup, err := Open(context.Background(), backupPath, key)
	if err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(t.TempDir(), "bas-restored.db")
	copyTo(backup, restorePath)
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	for reopen := 0; reopen < 2; reopen++ {
		restored, err := Open(context.Background(), restorePath, key)
		if err != nil {
			t.Fatal(err)
		}
		got := snapshotBasRetentionTables(t, restored)
		if len(got) != len(want) {
			t.Fatal("restore lost retention tables")
		}
		for table, hash := range want {
			if got[table] != hash {
				t.Fatalf("restore changed %s complete rows on reopen %d", table, reopen)
			}
		}
		assertAppliedVersions(t, restored, 13)
		assertBasTypedRetentionRows(t, restored)
		if err := restored.IntegrityCheck(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := migrationRelationalIntegrityCheck(context.Background(), restored); err != nil {
			t.Fatal(err)
		}
		if err := restored.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
