//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

// Package testkit provides encrypted, public-contract-oriented test fixtures.
package testkit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

// EncryptedWorkspace is a fully migrated ephemeral SQLCipher workspace.
type EncryptedWorkspace struct {
	Database *sqlcipher.Database
	Key      []byte
	Path     string
}

func NewEncryptedWorkspace(t testing.TB) *EncryptedWorkspace {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), path, key, 2); err != nil {
		t.Fatalf("migrate encrypted workspace: %v", err)
	}
	database, err := sqlcipher.Open(context.Background(), path, key)
	if err != nil {
		t.Fatalf("open encrypted workspace: %v", err)
	}
	workspace := &EncryptedWorkspace{Database: database, Key: key, Path: path}
	t.Cleanup(func() {
		_ = database.Close()
		for index := range workspace.Key {
			workspace.Key[index] = 0
		}
	})
	return workspace
}
