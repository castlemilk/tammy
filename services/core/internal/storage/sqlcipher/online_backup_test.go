//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOnlineBackupIsPageSteppedAndCancellable(t *testing.T) {
	key := make([]byte, KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zeroBytes(key)
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	if _, err := MigrateWorkspace(context.Background(), sourcePath, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := Open(context.Background(), sourcePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(context.Background(), `CREATE TABLE backup_cancellation_pages(id INTEGER PRIMARY KEY, value BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), `INSERT INTO backup_cancellation_pages(value) VALUES(?)`, make([]byte, 2*1024*1024)); err != nil {
		t.Fatal(err)
	}
	destinationPath := filepath.Join(t.TempDir(), "destination.db")
	destination, err := os.OpenFile(destinationPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	ctx, cancel := context.WithCancel(context.Background())
	steps := 0
	err = database.onlineBackupTo(ctx, destinationPath, destination, key, onlineBackupHooks{afterStep: func(_, _ int) error {
		steps++
		cancel()
		return ctx.Err()
	}})
	if !errors.Is(err, context.Canceled) || steps != 1 {
		t.Fatalf("onlineBackupTo() error = %v, steps = %d, want context cancellation after one bounded step", err, steps)
	}
}
