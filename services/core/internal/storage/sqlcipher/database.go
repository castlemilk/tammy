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
	ErrDatabaseIdentity    = errors.New("sqlcipher: database identity changed")
	ErrDatabasePath        = errors.New("sqlcipher: invalid database path")
	ErrDatabasePermissions = errors.New("sqlcipher: insecure database permissions")
	ErrKeyRequired         = errors.New("sqlcipher: a 32-byte key is required")
)

type databaseFileIdentity struct {
	closeErr  error
	closeOnce sync.Once
	file      os.FileInfo
	handle    *os.File
	parent    os.FileInfo
}

func (identity *databaseFileIdentity) close() error {
	identity.closeOnce.Do(func() {
		identity.closeErr = identity.handle.Close()
	})
	return identity.closeErr
}

// Database is an authenticated SQLCipher database whose connector keys and
// validates every physical SQLite connection before it is returned to callers.
type Database struct {
	*sql.DB
	connector *connector
	closeOnce sync.Once
	closeErr  error
	identity  *databaseFileIdentity
}

type openBoundaryHooks struct {
	afterPing                func() error
	beforeFinalIdentityCheck func()
}

// Open creates or opens a database using an exact 32-byte SQLCipher raw key.
// The caller retains ownership of key; Open copies it and Close clears that copy.
func Open(ctx context.Context, databasePath string, key []byte) (*Database, error) {
	return openDatabase(ctx, databasePath, key, openBoundaryHooks{})
}

func openDatabase(
	ctx context.Context,
	databasePath string,
	key []byte,
	hooks openBoundaryHooks,
) (*Database, error) {
	if len(key) != KeySize {
		return nil, ErrKeyRequired
	}
	cleaned, err := validateDatabasePath(databasePath)
	if err != nil {
		return nil, err
	}
	ownedKey := append([]byte(nil), key...)
	identity, _, err := retainDatabaseFile(cleaned)
	if err != nil {
		zeroBytes(ownedKey)
		return nil, err
	}
	connectorInstance := &connector{
		fileIdentity:   identity.file,
		key:            ownedKey,
		parentIdentity: identity.parent,
		path:           cleaned,
	}
	sqlDatabase := sql.OpenDB(connectorInstance)
	sqlDatabase.SetMaxIdleConns(4)
	sqlDatabase.SetMaxOpenConns(4)
	sqlDatabase.SetConnMaxIdleTime(0)
	sqlDatabase.SetConnMaxLifetime(0)
	database := &Database{DB: sqlDatabase, connector: connectorInstance, identity: identity}
	failOpen := func(openErr error) (*Database, error) {
		_ = sqlDatabase.Close()
		connectorInstance.destroy()
		_ = identity.close()
		// Identity-conditional path deletion is not atomic on either supported
		// target. Leave a newly created, mode-validated residue for recovery.
		return nil, openErr
	}
	if err := sqlDatabase.PingContext(ctx); err != nil {
		return failOpen(fmt.Errorf("sqlcipher: open failed: %w", err))
	}
	if hooks.afterPing != nil {
		if err := hooks.afterPing(); err != nil {
			return failOpen(err)
		}
	}
	if hooks.beforeFinalIdentityCheck != nil {
		hooks.beforeFinalIdentityCheck()
	}
	if err := verifyDatabaseIdentity(cleaned, identity.file, identity.parent); err != nil {
		return failOpen(err)
	}
	return database, nil
}

func retainDatabaseFile(candidate string) (*databaseFileIdentity, bool, error) {
	parent, err := os.Lstat(filepath.Dir(candidate))
	if err != nil || validateDatabaseParentSecurity(parent) != nil {
		return nil, false, ErrDatabasePermissions
	}
	initial, statErr := os.Lstat(candidate)
	created := false
	flags := os.O_RDWR
	if os.IsNotExist(statErr) {
		created = true
		flags |= os.O_CREATE | os.O_EXCL
	} else if statErr != nil {
		return nil, false, ErrDatabaseIdentity
	} else if err := validateDatabaseFileSecurity(initial); err != nil {
		return nil, false, err
	}
	handle, err := os.OpenFile(candidate, flags, 0o600)
	if err != nil {
		return nil, false, errors.New("sqlcipher: database file open failed")
	}
	identity := &databaseFileIdentity{handle: handle, parent: parent}
	identity.file, err = handle.Stat()
	if err == nil {
		err = validateDatabaseFileSecurity(identity.file)
	}
	if err == nil && !created && !os.SameFile(initial, identity.file) {
		err = ErrDatabaseIdentity
	}
	if err == nil {
		err = verifyDatabaseIdentity(candidate, identity.file, parent)
	}
	if err != nil {
		_ = identity.close()
		return nil, false, err
	}
	return identity, created, nil
}

func verifyDatabaseIdentity(candidate string, expectedFile, expectedParent os.FileInfo) error {
	parent, err := os.Lstat(filepath.Dir(candidate))
	if err != nil || validateDatabaseParentSecurity(parent) != nil || !os.SameFile(parent, expectedParent) {
		return ErrDatabaseIdentity
	}
	current, err := os.Lstat(candidate)
	if err != nil || validateDatabaseFileSecurity(current) != nil || !os.SameFile(current, expectedFile) {
		return ErrDatabaseIdentity
	}
	return nil
}

func retainExpectedDatabaseFile(
	candidate string,
	expectedFile os.FileInfo,
	expectedParent os.FileInfo,
) (*os.File, error) {
	if err := verifyDatabaseIdentity(candidate, expectedFile, expectedParent); err != nil {
		return nil, err
	}
	handle, err := os.OpenFile(candidate, os.O_RDWR, 0)
	if err != nil {
		return nil, ErrDatabaseIdentity
	}
	identity, statErr := handle.Stat()
	if statErr != nil || validateDatabaseFileSecurity(identity) != nil || !os.SameFile(identity, expectedFile) {
		_ = handle.Close()
		return nil, ErrDatabaseIdentity
	}
	if err := verifyDatabaseIdentity(candidate, expectedFile, expectedParent); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
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
		databaseError := database.DB.Close()
		database.connector.destroy()
		database.closeErr = errors.Join(databaseError, database.identity.close())
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
