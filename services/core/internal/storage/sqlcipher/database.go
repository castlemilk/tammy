//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	BusyTimeoutMilliseconds = 5_000
	KeySize                 = 32
	ReleaseVersion          = "4.15.0"
	PinnedVersion           = ReleaseVersion + " community"
)

var (
	ErrDatabasePath = errors.New("sqlcipher: invalid database path")
	ErrKeyRequired  = errors.New("sqlcipher: a 32-byte key is required")
)

// Database is an authenticated SQLCipher database whose connector keys and
// validates every physical SQLite connection before it is returned to callers.
type Database struct {
	*sql.DB
	connector *connector
	closeOnce sync.Once
	closeErr  error
}

// Open creates or opens a database using an exact 32-byte SQLCipher raw key.
// The caller retains ownership of key; Open copies it and Close clears that copy.
func Open(ctx context.Context, databasePath string, key []byte) (*Database, error) {
	if len(key) != KeySize {
		return nil, ErrKeyRequired
	}
	cleaned, err := validateDatabasePath(databasePath)
	if err != nil {
		return nil, err
	}
	ownedKey := append([]byte(nil), key...)
	created, createdIdentity, err := createDatabaseFile(cleaned)
	if err != nil {
		zeroBytes(ownedKey)
		return nil, err
	}
	cleanupCreated := func() {
		if !created || createdIdentity == nil {
			return
		}
		current, statErr := os.Lstat(cleaned)
		if statErr == nil && current.Mode().IsRegular() && os.SameFile(createdIdentity, current) {
			_ = os.Remove(cleaned)
		}
	}
	connectorInstance := &connector{key: ownedKey, path: cleaned}
	sqlDatabase := sql.OpenDB(connectorInstance)
	sqlDatabase.SetMaxIdleConns(4)
	sqlDatabase.SetMaxOpenConns(4)
	sqlDatabase.SetConnMaxIdleTime(0)
	sqlDatabase.SetConnMaxLifetime(0)
	database := &Database{DB: sqlDatabase, connector: connectorInstance}
	if err := sqlDatabase.PingContext(ctx); err != nil {
		_ = sqlDatabase.Close()
		connectorInstance.destroy()
		cleanupCreated()
		return nil, fmt.Errorf("sqlcipher: open failed: %w", err)
	}
	if err := os.Chmod(cleaned, 0o600); err != nil {
		_ = sqlDatabase.Close()
		connectorInstance.destroy()
		cleanupCreated()
		return nil, errors.New("sqlcipher: database permissions failed")
	}
	return database, nil
}

func createDatabaseFile(candidate string) (bool, os.FileInfo, error) {
	handle, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, errors.New("sqlcipher: database creation failed")
	}
	identity, statErr := handle.Stat()
	closeErr := handle.Close()
	if statErr != nil || closeErr != nil {
		if identity != nil {
			current, currentErr := os.Lstat(candidate)
			if currentErr == nil && os.SameFile(identity, current) {
				_ = os.Remove(candidate)
			}
		}
		return false, nil, errors.New("sqlcipher: database creation failed")
	}
	return true, identity, nil
}

func validateDatabasePath(candidate string) (string, error) {
	if candidate == "" ||
		strings.IndexByte(candidate, 0) >= 0 ||
		!filepath.IsAbs(candidate) ||
		filepath.Clean(candidate) != candidate ||
		candidate == filepath.VolumeName(candidate)+string(filepath.Separator) {
		return "", ErrDatabasePath
	}
	parent := filepath.Dir(candidate)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil || !filepath.IsAbs(resolvedParent) {
		return "", ErrDatabasePath
	}
	resolvedParent = filepath.Clean(resolvedParent)
	parentStats, err := os.Lstat(resolvedParent)
	if err != nil || !parentStats.IsDir() || parentStats.Mode()&os.ModeSymlink != 0 {
		return "", ErrDatabasePath
	}
	resolvedCandidate := filepath.Join(resolvedParent, filepath.Base(candidate))
	if existing, statErr := os.Lstat(resolvedCandidate); statErr == nil {
		if !existing.Mode().IsRegular() || existing.Mode()&os.ModeSymlink != 0 {
			return "", ErrDatabasePath
		}
	} else if !os.IsNotExist(statErr) {
		return "", ErrDatabasePath
	}
	return resolvedCandidate, nil
}

// Close drains database/sql, closes every physical connection, and clears the
// connector's owned key bytes. It is safe to call more than once.
func (database *Database) Close() error {
	database.closeOnce.Do(func() {
		database.closeErr = database.DB.Close()
		database.connector.destroy()
	})
	return database.closeErr
}

// CipherVersion returns the linked SQLCipher version.
func (database *Database) CipherVersion(ctx context.Context) (string, error) {
	var version string
	if err := database.QueryRowContext(ctx, "PRAGMA cipher_version").Scan(&version); err != nil {
		return "", fmt.Errorf("sqlcipher: cipher version: %w", err)
	}
	if version != PinnedVersion {
		return "", fmt.Errorf("sqlcipher: cipher version %q is not pinned %q", version, PinnedVersion)
	}
	return version, nil
}

// IntegrityCheck runs SQLCipher's authenticated page-level integrity check.
func (database *Database) IntegrityCheck(ctx context.Context) error {
	rows, err := database.QueryContext(ctx, "PRAGMA cipher_integrity_check")
	if err != nil {
		return fmt.Errorf("sqlcipher: cipher integrity check: %w", err)
	}
	defer rows.Close()
	var findings []string
	for rows.Next() {
		var finding string
		if err := rows.Scan(&finding); err != nil {
			return fmt.Errorf("sqlcipher: cipher integrity check: %w", err)
		}
		if finding != "ok" {
			findings = append(findings, finding)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlcipher: cipher integrity check: %w", err)
	}
	if len(findings) > 0 {
		return fmt.Errorf("sqlcipher: cipher integrity findings: %s", strings.Join(findings, "; "))
	}
	return nil
}

// Checkpoint checkpoints and truncates the WAL, failing if SQLite reports busy
// readers or pages that could not be checkpointed.
func (database *Database) Checkpoint(ctx context.Context) error {
	var busy, logPages, checkpointedPages int64
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(
		&busy,
		&logPages,
		&checkpointedPages,
	); err != nil {
		return fmt.Errorf("sqlcipher: WAL checkpoint: %w", err)
	}
	if busy != 0 || logPages != checkpointedPages {
		return fmt.Errorf(
			"sqlcipher: WAL checkpoint incomplete (busy=%d log=%d checkpointed=%d)",
			busy,
			logPages,
			checkpointedPages,
		)
	}
	return nil
}
