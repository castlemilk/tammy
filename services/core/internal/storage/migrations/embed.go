// Package migrations provides the immutable ordered SQL schema programme.
package migrations

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrTarget = errors.New("migrations: target version is unavailable")

// Migration is one content-authenticated, monotonically ordered schema step.
type Migration struct {
	Version uint32
	Name    string
	SHA256  string
	SQL     []byte
}

//go:embed 0001_platform.sql 0002_ledger.sql 0003_audit_idempotency.sql 0004_pre_restore_archives.sql 0005_documents.sql 0006_banking_reporting.sql
var migrationFiles embed.FS

var orderedFiles = [...]string{"0001_platform.sql", "0002_ledger.sql", "0003_audit_idempotency.sql", "0004_pre_restore_archives.sql", "0005_documents.sql", "0006_banking_reporting.sql"}

// All returns an independent copy of every embedded migration.
func All() ([]Migration, error) {
	steps := make([]Migration, 0, len(orderedFiles))
	for index, name := range orderedFiles {
		contents, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("migrations: read %s: %w", name, err)
		}
		digest := sha256.Sum256(contents)
		steps = append(steps, Migration{
			Version: uint32(index + 1),
			Name:    name,
			SHA256:  hex.EncodeToString(digest[:]),
			SQL:     append([]byte(nil), contents...),
		})
	}
	return steps, nil
}

// Prefix returns an independent ordered prefix ending at target.
func Prefix(target uint32) ([]Migration, error) {
	steps, err := All()
	if err != nil {
		return nil, err
	}
	if target == 0 || target > uint32(len(steps)) {
		return nil, ErrTarget
	}
	return steps[:target], nil
}
