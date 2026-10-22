//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

/*
#include <sqlite3.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/tammyapp/tammy/services/core/internal/storage/migrations"
)

var (
	ErrMigrationChecksum = errors.New("sqlcipher: migration checksum mismatch")
	ErrMigrationPlan     = errors.New("sqlcipher: invalid migration plan")
	ErrMigrationSQL      = errors.New("sqlcipher: invalid migration SQL")
	ErrMigrationStorage  = errors.New("sqlcipher: migration storage boundary failed")
)

const migrationTableSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY CHECK (version > 0),
  name TEXT NOT NULL UNIQUE,
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  applied_at TEXT NOT NULL
)`

// applyMigrations is intentionally package-private: only the copy-migration
// boundary may apply schema SQL to a workspace file.
func applyMigrations(
	ctx context.Context,
	database *Database,
	steps []migrations.Migration,
	target uint32,
) error {
	if database == nil || database.DB == nil {
		return ErrMigrationPlan
	}
	if err := validateMigrationPlan(steps, target); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, migrationTableSQL); err != nil {
		return fmt.Errorf("sqlcipher: create migration ledger: %w", err)
	}
	applied, err := readAppliedMigrations(ctx, database)
	if err != nil {
		return err
	}
	if err := verifyAppliedMigrations(applied, steps, target); err != nil {
		return err
	}
	for index := len(applied); index < int(target); index++ {
		if err := applyMigration(ctx, database, steps[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateMigrationPlan(steps []migrations.Migration, target uint32) error {
	if target == 0 || target > uint32(len(steps)) {
		return ErrMigrationPlan
	}
	for index := range steps {
		step := &steps[index]
		if step.Version != uint32(index+1) || step.Name == "" || len(step.SQL) == 0 {
			return ErrMigrationPlan
		}
		digest := sha256.Sum256(step.SQL)
		if step.SHA256 != hex.EncodeToString(digest[:]) {
			return ErrMigrationChecksum
		}
	}
	return nil
}

func verifyAppliedMigrations(
	applied []appliedMigration,
	steps []migrations.Migration,
	target uint32,
) error {
	if len(applied) > int(target) {
		return ErrMigrationPlan
	}
	for index, record := range applied {
		step := steps[index]
		if record.version != step.Version || record.name != step.Name || record.sha256 != step.SHA256 {
			return ErrMigrationChecksum
		}
	}
	return nil
}

type appliedMigration struct {
	version uint32
	name    string
	sha256  string
}

func readAppliedMigrations(ctx context.Context, database *Database) ([]appliedMigration, error) {
	rows, err := database.QueryContext(ctx, `SELECT version, name, sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("sqlcipher: read migration ledger: %w", err)
	}
	defer rows.Close()
	var records []appliedMigration
	for rows.Next() {
		var record appliedMigration
		if err := rows.Scan(&record.version, &record.name, &record.sha256); err != nil {
			return nil, fmt.Errorf("sqlcipher: read migration ledger: %w", err)
		}
		if record.version != uint32(len(records)+1) {
			return nil, ErrMigrationPlan
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlcipher: read migration ledger: %w", err)
	}
	return records, nil
}

func applyMigration(ctx context.Context, database *Database, step migrations.Migration) (runErr error) {
	statements, err := splitMigrationSQL(step.SQL)
	if err != nil {
		return fmt.Errorf("sqlcipher: split migration %s: %w", step.Name, err)
	}
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("sqlcipher: begin migration %s: %w", step.Name, err)
	}
	defer func() {
		if runErr != nil {
			_ = transaction.Rollback()
		}
	}()
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("sqlcipher: apply migration %s: %w", step.Name, err)
		}
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO schema_migrations(version, name, sha256, applied_at) VALUES (?,?,?,?)`,
		step.Version,
		step.Name,
		step.SHA256,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("sqlcipher: record migration %s: %w", step.Name, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("sqlcipher: commit migration %s: %w", step.Name, err)
	}
	return nil
}

func splitMigrationSQL(source []byte) ([]string, error) {
	if len(source) == 0 || !utf8.Valid(source) || strings.IndexByte(string(source), 0) >= 0 {
		return nil, ErrMigrationSQL
	}
	start := 0
	statements := make([]string, 0, 16)
	for index, value := range source {
		if value != ';' {
			continue
		}
		candidate := source[start : index+1]
		text := C.CString(string(candidate))
		complete := C.sqlite3_complete(text) == 1
		C.free(unsafe.Pointer(text))
		if !complete {
			continue
		}
		statement := strings.TrimSpace(string(candidate))
		if statement == "" {
			return nil, ErrMigrationSQL
		}
		statements = append(statements, statement)
		start = index + 1
	}
	if strings.TrimSpace(string(source[start:])) != "" || len(statements) == 0 {
		return nil, ErrMigrationSQL
	}
	return statements, nil
}

// WorkspaceMigrationResult identifies the active encrypted file and any
// recoverable predecessor or staged residue retained by the migration boundary.
type WorkspaceMigrationResult struct {
	ActivePath      string
	PredecessorPath string
	StagedPath      string
	Version         uint32
}

type migrationBoundaryHooks struct {
	afterPredecessorReady func() error
	afterStagedIntegrity  func() error
}

type workspaceMigrationOptions struct {
	hooks  migrationBoundaryHooks
	steps  []migrations.Migration
	target uint32
}

// MigrateWorkspace copy-migrates an encrypted file and atomically activates it.
func MigrateWorkspace(
	ctx context.Context,
	databasePath string,
	key []byte,
	target uint32,
) (WorkspaceMigrationResult, error) {
	steps, err := migrations.All()
	if err != nil {
		return WorkspaceMigrationResult{}, err
	}
	return migrateWorkspace(ctx, databasePath, key, workspaceMigrationOptions{
		steps:  steps,
		target: target,
	})
}

func migrateWorkspace(
	ctx context.Context,
	databasePath string,
	key []byte,
	options workspaceMigrationOptions,
) (result WorkspaceMigrationResult, runErr error) {
	result = WorkspaceMigrationResult{ActivePath: databasePath, Version: options.target}
	if len(key) != KeySize {
		return result, ErrKeyRequired
	}
	if err := validateMigrationPlan(options.steps, options.target); err != nil {
		return result, err
	}
	cleaned, err := validateDatabasePath(databasePath)
	if err != nil {
		return result, err
	}
	result.ActivePath = cleaned
	migrationLock, err := acquireWorkspaceFileLock(cleaned, true)
	if err != nil {
		return result, err
	}
	defer func() {
		runErr = errors.Join(runErr, migrationLock.close())
	}()
	exists, err := workspaceFileExists(cleaned)
	if err != nil {
		return result, err
	}
	if exists {
		currentVersion, inspectErr := inspectWorkspaceBeforeCopy(ctx, cleaned, key, options.steps, options.target)
		if inspectErr != nil {
			return result, inspectErr
		}
		if currentVersion == options.target {
			return result, nil
		}
	}
	stagedPath, err := stageEncryptedWorkspace(cleaned, exists)
	result.StagedPath = stagedPath
	if err != nil {
		return result, err
	}
	stagedDatabase, err := openDatabase(ctx, stagedPath, key, openBoundaryHooks{withoutWorkspaceLock: true})
	if err != nil {
		return result, fmt.Errorf("%w: open staged workspace: %v", ErrMigrationStorage, err)
	}
	stageErr := applyMigrations(ctx, stagedDatabase, options.steps, options.target)
	if stageErr == nil {
		stageErr = stagedDatabase.IntegrityCheck(ctx)
	}
	if stageErr == nil {
		stageErr = migrationRelationalIntegrityCheck(ctx, stagedDatabase)
	}
	if stageErr == nil {
		stageErr = stagedDatabase.Checkpoint(ctx)
	}
	closeErr := stagedDatabase.Close()
	if stageErr != nil || closeErr != nil {
		return result, errors.Join(stageErr, closeErr)
	}
	if options.hooks.afterStagedIntegrity != nil {
		if err := options.hooks.afterStagedIntegrity(); err != nil {
			return result, err
		}
	}
	stagedIdentity, err := os.Lstat(stagedPath)
	if err != nil || !stagedIdentity.Mode().IsRegular() || stagedIdentity.Mode()&os.ModeSymlink != 0 {
		return result, ErrMigrationStorage
	}
	if exists {
		predecessor, linkErr := retainMigrationPredecessor(cleaned)
		result.PredecessorPath = predecessor
		if linkErr != nil {
			return result, linkErr
		}
		if options.hooks.afterPredecessorReady != nil {
			if err := options.hooks.afterPredecessorReady(); err != nil {
				return result, err
			}
		}
	}
	if err := activateMigratedWorkspace(stagedPath, cleaned, exists); err != nil {
		return result, fmt.Errorf("%w: activate workspace: %v", ErrMigrationStorage, err)
	}
	result.StagedPath = ""
	activeIdentity, err := os.Lstat(cleaned)
	if err != nil || !os.SameFile(stagedIdentity, activeIdentity) {
		return result, ErrMigrationStorage
	}
	if err := syncMigrationParent(filepath.Dir(cleaned)); err != nil {
		return result, fmt.Errorf("%w: sync activation: %v", ErrMigrationStorage, err)
	}
	return result, nil
}

func workspaceFileExists(candidate string) (bool, error) {
	stats, err := os.Lstat(candidate)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || !stats.Mode().IsRegular() || stats.Mode()&os.ModeSymlink != 0 {
		return false, ErrDatabaseIdentity
	}
	return true, nil
}

func inspectWorkspaceBeforeCopy(
	ctx context.Context,
	path string,
	key []byte,
	steps []migrations.Migration,
	target uint32,
) (version uint32, runErr error) {
	database, err := openDatabase(ctx, path, key, openBoundaryHooks{withoutWorkspaceLock: true})
	if err != nil {
		return 0, err
	}
	defer func() {
		runErr = errors.Join(runErr, database.Close())
	}()
	if err := database.IntegrityCheck(ctx); err != nil {
		return 0, err
	}
	if err := migrationRelationalIntegrityCheck(ctx, database); err != nil {
		return 0, err
	}
	if err := database.Checkpoint(ctx); err != nil {
		return 0, err
	}
	var tableCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='schema_migrations'`,
	).Scan(&tableCount); err != nil {
		return 0, err
	}
	if tableCount == 0 {
		return 0, nil
	}
	applied, err := readAppliedMigrations(ctx, database)
	if err != nil {
		return 0, err
	}
	if err := verifyAppliedMigrations(applied, steps, target); err != nil {
		return 0, err
	}
	return uint32(len(applied)), nil
}

func migrationRelationalIntegrityCheck(ctx context.Context, database *Database) error {
	rows, err := database.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("%w: sqlite integrity check: %v", ErrMigrationStorage, err)
	}
	var findings []string
	for rows.Next() {
		var finding string
		if err := rows.Scan(&finding); err != nil {
			_ = rows.Close()
			return fmt.Errorf("%w: sqlite integrity result: %v", ErrMigrationStorage, err)
		}
		if finding != "ok" {
			findings = append(findings, finding)
		}
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil {
		return fmt.Errorf("%w: sqlite integrity read: %v", ErrMigrationStorage, errors.Join(rowsErr, closeErr))
	}
	if len(findings) != 0 {
		return fmt.Errorf("%w: sqlite integrity findings: %s", ErrMigrationStorage, strings.Join(findings, "; "))
	}

	foreignKeys, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("%w: foreign key check: %v", ErrMigrationStorage, err)
	}
	if foreignKeys.Next() {
		_ = foreignKeys.Close()
		return fmt.Errorf("%w: foreign key violation", ErrMigrationStorage)
	}
	foreignKeyErr := foreignKeys.Err()
	foreignKeyCloseErr := foreignKeys.Close()
	if foreignKeyErr != nil || foreignKeyCloseErr != nil {
		return fmt.Errorf("%w: foreign key check read: %v", ErrMigrationStorage, errors.Join(foreignKeyErr, foreignKeyCloseErr))
	}
	return nil
}

func stageEncryptedWorkspace(activePath string, copyExisting bool) (string, error) {
	parent := filepath.Dir(activePath)
	handle, err := os.CreateTemp(parent, "."+filepath.Base(activePath)+".migration-")
	if err != nil {
		return "", ErrMigrationStorage
	}
	stagedPath := handle.Name()
	failed := true
	defer func() {
		if failed {
			_ = handle.Close()
		}
	}()
	if err := handle.Chmod(0o600); err != nil {
		return stagedPath, ErrMigrationStorage
	}
	if copyExisting {
		identity, created, err := retainDatabaseFile(activePath)
		if err != nil || created {
			return stagedPath, ErrMigrationStorage
		}
		var copyErr error
		if _, copyErr = identity.handle.Seek(0, io.SeekStart); copyErr == nil {
			_, copyErr = io.Copy(handle, identity.handle)
		}
		verifyErr := verifyDatabaseIdentity(activePath, identity.file, identity.parent)
		closeErr := identity.close()
		if copyErr != nil || verifyErr != nil || closeErr != nil {
			return stagedPath, ErrMigrationStorage
		}
	}
	if err := handle.Sync(); err != nil {
		return stagedPath, ErrMigrationStorage
	}
	if err := handle.Close(); err != nil {
		return stagedPath, ErrMigrationStorage
	}
	failed = false
	return stagedPath, nil
}

func retainMigrationPredecessor(activePath string) (string, error) {
	original, err := os.Lstat(activePath)
	if err != nil || !original.Mode().IsRegular() || original.Mode()&os.ModeSymlink != 0 {
		return "", ErrMigrationStorage
	}
	for attempt := 0; attempt < 8; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return "", ErrMigrationStorage
		}
		candidate := activePath + ".pre-migration-" + hex.EncodeToString(random)
		if err := os.Link(activePath, candidate); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", ErrMigrationStorage
		}
		linked, err := os.Lstat(candidate)
		if err != nil || !os.SameFile(original, linked) {
			return candidate, ErrMigrationStorage
		}
		if err := syncMigrationParent(filepath.Dir(activePath)); err != nil {
			return candidate, ErrMigrationStorage
		}
		return candidate, nil
	}
	return "", ErrMigrationStorage
}
