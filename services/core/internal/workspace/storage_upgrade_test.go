//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestStorageOpenUpgradesAnExistingVersion10Workspace(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := bytes.Repeat([]byte{0x73}, sqlcipher.KeySize)
	old, err := NewSQLCipherStorageFactory(10).Create(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Database().ExecContext(ctx, `INSERT INTO workspace_metadata(key,value,revision,updated_at) VALUES('retained-probe',x'010203',7,'2026-10-04T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := NewSQLCipherStorageFactory(14).Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	var version int
	if err := opened.Database().QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != 14 {
		t.Fatalf("opened schema version = %d, %v; want 14", version, err)
	}
	var value []byte
	var revision int
	if err := opened.Database().QueryRowContext(ctx, `SELECT value,revision FROM workspace_metadata WHERE key='retained-probe'`).Scan(&value, &revision); err != nil || !bytes.Equal(value, []byte{1, 2, 3}) || revision != 7 {
		t.Fatalf("retained workspace data = %v, revision %d, %v", value, revision, err)
	}
}

func TestStorageOpenKeepsCurrentWorkspaceSharedAndRefusesActiveOldWorkspaceMigration(t *testing.T) {
	ctx := context.Background()
	for _, version := range []uint32{10, 11, 12, 13, 14} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.db")
			key := bytes.Repeat([]byte{0x74}, sqlcipher.KeySize)
			owner, err := NewSQLCipherStorageFactory(version).Create(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			if version == 14 {
				writer, err := owner.Database().BeginEncryptedTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Rollback()
				if _, err := writer.ExecContext(ctx, `INSERT INTO workspace_metadata(key,value,revision,updated_at) VALUES('uncommitted',x'01',1,'2026-10-04T00:00:00Z')`); err != nil {
					t.Fatal(err)
				}
			}
			opened, err := NewSQLCipherStorageFactory(14).Open(ctx, path, key)
			if version < 14 {
				if opened != nil {
					_ = opened.Close()
				}
				if !errors.Is(err, sqlcipher.ErrWorkspaceLocked) {
					t.Fatalf("migration of an actively held old workspace error = %v; want lock rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("opening an already current workspace acquired an unnecessary exclusive lock: %v", err)
			}
			var uncommitted int
			if err := opened.Database().QueryRowContext(ctx, `SELECT count(*) FROM workspace_metadata WHERE key='uncommitted'`).Scan(&uncommitted); err != nil || uncommitted != 0 {
				t.Fatalf("current-schema shared reader did not retain the committed snapshot: %d, %v", uncommitted, err)
			}
			if err := opened.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStorageOpenRejectsANewerSchemaWithoutChangingIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := bytes.Repeat([]byte{0x78}, sqlcipher.KeySize)
	newer, err := NewSQLCipherStorageFactory(14).Create(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := NewSQLCipherStorageFactory(12).Open(ctx, path, key)
	if opened != nil {
		_ = opened.Close()
	}
	if !errors.Is(err, sqlcipher.ErrMigrationPlan) {
		t.Fatalf("newer-schema admission error = %v; want migration-plan rejection", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("newer-schema admission changed the workspace: %v", err)
	}
}

func TestStorageOpenRejectsMissingFileWithoutCreatingAWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	opened, err := NewSQLCipherStorageFactory(14).Open(context.Background(), path, bytes.Repeat([]byte{0x75}, sqlcipher.KeySize))
	if opened != nil {
		_ = opened.Close()
	}
	if err == nil {
		t.Fatal("Open created a blank workspace from a missing file")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("missing workspace was created: %v", err)
	}
}

func TestStorageOpenWrongKeyOrChecksumFailurePreservesVersion10Workspace(t *testing.T) {
	ctx := context.Background()
	for _, test := range []string{"wrong key", "checksum drift"} {
		t.Run(test, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace.db")
			key := bytes.Repeat([]byte{0x76}, sqlcipher.KeySize)
			old, err := NewSQLCipherStorageFactory(10).Create(ctx, path, key)
			if err != nil {
				t.Fatal(err)
			}
			if test == "checksum drift" {
				if _, err := old.Database().ExecContext(ctx, `UPDATE schema_migrations SET sha256=? WHERE version=10`, strings.Repeat("0", 64)); err != nil {
					t.Fatal(err)
				}
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			openKey := key
			if test == "wrong key" {
				openKey = bytes.Repeat([]byte{0x77}, sqlcipher.KeySize)
			}
			opened, err := NewSQLCipherStorageFactory(14).Open(ctx, path, openKey)
			if opened != nil {
				_ = opened.Close()
			}
			if err == nil {
				t.Fatal("Open accepted an unauthenticated or unverified schema")
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed admission changed the original encrypted workspace: %v", readErr)
			}
		})
	}
}
