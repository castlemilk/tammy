//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

type SQLCipherStagedCaptureConfig struct {
	Directory         string
	AuthenticationKey []byte
	NewID             func() (string, error)
	hooks             *captureHooks
}

type captureHooks struct {
	beforeBackup             func() error
	afterPreflight           func() error
	afterCreate              func() error
	afterMarkerTempCreate    func() error
	afterMarkerTempWrite     func() error
	afterMarkerTempSync      func() error
	afterMarkerPublish       func() error
	afterMarkerDirectorySync func() error
	afterBackupStep          func(remaining, total int)
	afterBackup              func() error
	afterSanitize            func() error
	afterRead                func() error
}

type snapshotSQLReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type scopedSnapshotReader struct {
	mu       sync.RWMutex
	executor snapshotSQLReader
}

func newScopedSnapshotReader(executor snapshotSQLReader) *scopedSnapshotReader {
	return &scopedSnapshotReader{executor: executor}
}

func (reader *scopedSnapshotReader) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	if reader == nil {
		return nil, ErrProviderRegistry
	}
	reader.mu.RLock()
	defer reader.mu.RUnlock()
	if reader.executor == nil {
		return nil, ErrProviderRegistry
	}
	return reader.executor.QueryContext(ctx, query, arguments...)
}

func (reader *scopedSnapshotReader) close() {
	if reader == nil {
		return
	}
	reader.mu.Lock()
	reader.executor = nil
	reader.mu.Unlock()
}

type stagedSnapshotInspector func(context.Context, *sqlcipher.Database, snapshotSQLReader, uint64, []byte) error

// CaptureSanitizedSQLCipherSnapshot uses SQLCipher's online backup API
// into an owned staging file, sanitizes only that copy in a caller-owned
// encrypted transaction, verifies schema/invariants, returns encrypted bytes,
// and removes the staging file before returning.
func CaptureSanitizedSQLCipherSnapshot(
	ctx context.Context,
	live *sqlcipher.Database,
	key []byte,
	config SQLCipherStagedCaptureConfig,
) (_ []byte, resultErr error) {
	return captureSanitizedSQLCipherSnapshot(ctx, live, key, config, nil)
}

func captureSanitizedSQLCipherSnapshot(
	ctx context.Context,
	live *sqlcipher.Database,
	key []byte,
	config SQLCipherStagedCaptureConfig,
	inspect stagedSnapshotInspector,
) (_ []byte, resultErr error) {
	if ctx == nil || live == nil || live.DB == nil || len(key) != sqlcipher.KeySize || config.Directory == "" ||
		!filepath.IsAbs(config.Directory) || filepath.Clean(config.Directory) != config.Directory ||
		len(config.AuthenticationKey) != sha256.Size || nilInterface(config.NewID) {
		return nil, ErrSnapshotExclusion
	}
	baseInfo, err := os.Lstat(config.Directory)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSnapshotExclusion
	}
	root, err := os.OpenRoot(config.Directory)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(baseInfo, rootInfo) {
		return nil, ErrSnapshotExclusion
	}
	reference, err := config.NewID()
	if err != nil || !ids.IsCanonicalV7(reference) {
		return nil, ErrSnapshotExclusion
	}
	name := authenticatedSnapshotCaptureName(reference, config.AuthenticationKey)
	path := filepath.Join(config.Directory, name)
	for _, artifact := range []string{name, name + ".owner", name + ".lock", name + "-journal", name + "-wal", name + "-shm"} {
		if _, err := root.Lstat(artifact); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrSnapshotExclusion
		}
	}
	if config.hooks != nil && config.hooks.afterPreflight != nil {
		if err := config.hooks.afterPreflight(); err != nil {
			return nil, errors.Join(ErrSnapshotExclusion, err)
		}
	}
	handle, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	initialStageInfo, err := handle.Stat()
	if err != nil || initialStageInfo == nil || !initialStageInfo.Mode().IsRegular() || initialStageInfo.Size() != 0 {
		_ = handle.Close()
		return nil, ErrSnapshotExclusion
	}
	marker := authenticatedSnapshotCaptureMarker(name, config.AuthenticationKey)
	var markerContents [sha256.Size]byte
	copy(markerContents[:], marker)
	zero(marker)
	var markerIdentity os.FileInfo
	owned := true
	defer func() {
		_ = handle.Close()
		if owned {
			_ = removeOwnedCaptureFiles(root, name, initialStageInfo, markerIdentity, markerContents[:])
		}
		zero(markerContents[:])
	}()
	if err := handle.Chmod(0o600); err != nil {
		return nil, ErrSnapshotExclusion
	}
	securedStageInfo, err := handle.Stat()
	if err != nil || !os.SameFile(initialStageInfo, securedStageInfo) || securedStageInfo.Mode().Perm() != 0o600 {
		return nil, ErrSnapshotExclusion
	}
	initialStageInfo = securedStageInfo
	if handle.Sync() != nil || syncCaptureDirectory(root) != nil {
		return nil, ErrSnapshotExclusion
	}
	markerIdentity, err = writeSnapshotCaptureMarker(root, name, initialStageInfo, markerContents[:],
		config.AuthenticationKey, config.hooks)
	if err != nil {
		return nil, err
	}
	if config.hooks != nil && config.hooks.afterCreate != nil {
		if err := config.hooks.afterCreate(); err != nil {
			return nil, errors.Join(ErrSnapshotExclusion, err)
		}
	}
	liveSchemaVersion, liveSchema, err := snapshotSchemaMetadata(ctx, live)
	if err != nil {
		return nil, err
	}
	if config.hooks != nil && config.hooks.beforeBackup != nil {
		if err := config.hooks.beforeBackup(); err != nil {
			return nil, ErrSnapshotExclusion
		}
	}
	var backupErr error
	if config.hooks != nil && config.hooks.afterBackupStep != nil {
		backupErr = live.OnlineBackupToWithProgress(ctx, path, handle, key, func(remaining, total int) error {
			config.hooks.afterBackupStep(remaining, total)
			return ctx.Err()
		})
	} else {
		backupErr = live.OnlineBackupTo(ctx, path, handle, key)
	}
	if backupErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, errors.Join(ErrSnapshotExclusion, contextErr)
		}
		return nil, ErrSnapshotExclusion
	}
	if config.hooks != nil && config.hooks.afterBackup != nil {
		if err := config.hooks.afterBackup(); err != nil {
			return nil, ErrSnapshotExclusion
		}
	}
	currentBase, err := os.Lstat(config.Directory)
	if err != nil || !os.SameFile(baseInfo, currentBase) {
		return nil, ErrSnapshotExclusion
	}
	stageInfo, err := handle.Stat()
	if err != nil || !stageInfo.Mode().IsRegular() || stageInfo.Size() <= 0 || stageInfo.Size() > maximumArchivePlaintext {
		return nil, ErrSnapshotExclusion
	}
	if err := handle.Close(); err != nil {
		return nil, ErrSnapshotExclusion
	}
	staged, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	stagedClosed := false
	defer func() {
		if !stagedClosed {
			_ = staged.Close()
		}
	}()
	tx, err := staged.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	if err := SanitizeSnapshot(ctx, tx); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, ErrSnapshotExclusion
	}
	if config.hooks != nil && config.hooks.afterSanitize != nil {
		if err := config.hooks.afterSanitize(); err != nil {
			return nil, errors.Join(ErrSnapshotExclusion, err)
		}
	}
	if err := VerifySnapshotExclusions(ctx, staged); err != nil {
		return nil, err
	}
	stagedSchemaVersion, stagedSchema, err := snapshotSchemaMetadata(ctx, staged)
	if err != nil || liveSchemaVersion != stagedSchemaVersion || liveSchema != stagedSchema || verifyDatabaseIntegrity(ctx, staged) != nil {
		return nil, ErrSnapshotExclusion
	}
	if inspect != nil {
		reader := newScopedSnapshotReader(staged)
		inspectErr := inspect(ctx, staged, reader, stagedSchemaVersion, append([]byte(nil), stagedSchema[:]...))
		reader.close()
		if inspectErr != nil {
			return nil, inspectErr
		}
	}
	if err := staged.Close(); err != nil {
		return nil, ErrSnapshotExclusion
	}
	stagedClosed = true
	contents, err := readCapturedFile(ctx, root, name, stageInfo)
	if err != nil {
		return nil, err
	}
	if config.hooks != nil && config.hooks.afterRead != nil {
		if err := config.hooks.afterRead(); err != nil {
			zero(contents)
			return nil, errors.Join(ErrSnapshotExclusion, err)
		}
	}
	if err := removeOwnedCaptureFiles(root, name, initialStageInfo, markerIdentity, markerContents[:]); err != nil {
		zero(contents)
		return nil, ErrSnapshotExclusion
	}
	owned = false
	return contents, nil
}

func snapshotSchemaMetadata(ctx context.Context, executor SQLExecutor) (uint64, [sha256.Size]byte, error) {
	rows, err := executor.QueryContext(ctx, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		return 0, [sha256.Size]byte{}, ErrSnapshotExclusion
	}
	defer rows.Close()
	digest := sha256.New()
	count := 0
	for rows.Next() {
		var version uint64
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil || version != uint64(count+1) || name == "" || len(checksum) != 64 {
			return 0, [sha256.Size]byte{}, ErrSnapshotExclusion
		}
		_, _ = digest.Write([]byte(strconv.FormatUint(version, 10)))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(name))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(checksum))
		_, _ = digest.Write([]byte{0})
		count++
	}
	if rows.Err() != nil || count == 0 {
		return 0, [sha256.Size]byte{}, ErrSnapshotExclusion
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return uint64(count), result, nil
}

func verifyDatabaseIntegrity(ctx context.Context, executor SQLExecutor) error {
	rows, err := executor.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return ErrSnapshotExclusion
	}
	defer rows.Close()
	if !rows.Next() {
		return ErrSnapshotExclusion
	}
	var result string
	if err := rows.Scan(&result); err != nil || result != "ok" || rows.Next() || rows.Err() != nil {
		return ErrSnapshotExclusion
	}
	return nil
}

func readCapturedFile(ctx context.Context, root *os.Root, name string, expected os.FileInfo) ([]byte, error) {
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(expected, current) || !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 ||
		current.Size() <= 0 || current.Size() > maximumArchivePlaintext {
		return nil, ErrSnapshotExclusion
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(current, opened) {
		return nil, ErrSnapshotExclusion
	}
	contents := make([]byte, int(current.Size()))
	for offset := 0; offset < len(contents); {
		if err := ctx.Err(); err != nil {
			zero(contents)
			return nil, ErrSnapshotExclusion
		}
		read, readErr := io.ReadFull(file, contents[offset:min(offset+32*1024, len(contents))])
		offset += read
		if readErr != nil {
			zero(contents)
			return nil, ErrSnapshotExclusion
		}
	}
	return contents, nil
}

func syncCaptureDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func writeSnapshotCaptureMarker(
	root *os.Root,
	databaseName string,
	databaseIdentity os.FileInfo,
	contents []byte,
	authenticationKey []byte,
	hooks *captureHooks,
) (os.FileInfo, error) {
	base, validName := authenticatedSnapshotCaptureBase(databaseName, authenticationKey)
	if root == nil || databaseIdentity == nil || len(contents) != sha256.Size || len(authenticationKey) != sha256.Size ||
		!validName || base != databaseName {
		return nil, ErrSnapshotExclusion
	}
	markerName := databaseName + ".owner"
	tempName := authenticatedSnapshotCaptureMarkerTempName(databaseName, authenticationKey)
	file, err := root.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	identity, statErr := file.Stat()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if sameSnapshotCaptureIdentity(root, tempName, identity) {
			_ = root.Remove(tempName)
			_ = syncCaptureDirectory(root)
		}
	}()
	if statErr != nil || identity == nil || !identity.Mode().IsRegular() || identity.Mode().Perm() != 0o600 ||
		identity.Size() != 0 {
		return nil, ErrSnapshotExclusion
	}
	if err := runCaptureMarkerHook(hooks, func(h *captureHooks) func() error { return h.afterMarkerTempCreate }); err != nil {
		return nil, err
	}
	half := len(contents) / 2
	written, writeErr := file.Write(contents[:half])
	if written != half || writeErr != nil {
		return nil, ErrSnapshotExclusion
	}
	if err := runCaptureMarkerHook(hooks, func(h *captureHooks) func() error { return h.afterMarkerTempWrite }); err != nil {
		return nil, err
	}
	written, writeErr = file.Write(contents[half:])
	if written != len(contents)-half || writeErr != nil || file.Sync() != nil {
		return nil, ErrSnapshotExclusion
	}
	if err := runCaptureMarkerHook(hooks, func(h *captureHooks) func() error { return h.afterMarkerTempSync }); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, ErrSnapshotExclusion
	}
	closed = true
	if !sameSnapshotCaptureIdentity(root, databaseName, databaseIdentity) ||
		!sameSnapshotCaptureIdentity(root, tempName, identity) {
		return nil, ErrSnapshotExclusion
	}
	if _, err := root.Lstat(markerName); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrSnapshotExclusion
	}
	if err := root.Link(tempName, markerName); err != nil {
		return nil, ErrSnapshotExclusion
	}
	markerIdentity, err := root.Lstat(markerName)
	if err != nil || !os.SameFile(identity, markerIdentity) || !sameSnapshotCaptureIdentity(root, markerName, markerIdentity) {
		return markerIdentity, ErrSnapshotExclusion
	}
	if err := runCaptureMarkerHook(hooks, func(h *captureHooks) func() error { return h.afterMarkerPublish }); err != nil {
		return markerIdentity, err
	}
	if err := syncCaptureDirectory(root); err != nil {
		return markerIdentity, ErrSnapshotExclusion
	}
	if err := runCaptureMarkerHook(hooks, func(h *captureHooks) func() error { return h.afterMarkerDirectorySync }); err != nil {
		return markerIdentity, err
	}
	if !sameSnapshotCaptureIdentity(root, tempName, identity) || root.Remove(tempName) != nil || syncCaptureDirectory(root) != nil {
		return markerIdentity, ErrSnapshotExclusion
	}
	return markerIdentity, nil
}

func runCaptureMarkerHook(hooks *captureHooks, selectHook func(*captureHooks) func() error) error {
	if hooks == nil {
		return nil
	}
	hook := selectHook(hooks)
	if hook == nil {
		return nil
	}
	if err := hook(); err != nil {
		return errors.Join(ErrSnapshotExclusion, err)
	}
	return nil
}

func removeOwnedCaptureFiles(
	root *os.Root,
	databaseName string,
	databaseIdentity os.FileInfo,
	markerIdentity os.FileInfo,
	markerContents []byte,
) error {
	if !sameSnapshotCaptureIdentity(root, databaseName, databaseIdentity) {
		return ErrSnapshotExclusion
	}
	if markerIdentity != nil {
		if !sameSnapshotCaptureIdentity(root, databaseName+".owner", markerIdentity) {
			return ErrSnapshotExclusion
		}
		current, err := readSnapshotCaptureMarker(root, databaseName+".owner")
		if err != nil {
			return err
		}
		matches := len(markerContents) == sha256.Size && subtle.ConstantTimeCompare(current, markerContents) == 1
		zero(current)
		if !matches {
			return ErrSnapshotExclusion
		}
	}
	var cleanupErr error
	for _, name := range []string{databaseName, databaseName + ".lock", databaseName + "-journal", databaseName + "-wal", databaseName + "-shm", databaseName + ".owner"} {
		if name == databaseName+".owner" && markerIdentity == nil {
			continue
		}
		if name == databaseName && !sameSnapshotCaptureIdentity(root, name, databaseIdentity) {
			cleanupErr = errors.Join(cleanupErr, ErrSnapshotExclusion)
			continue
		}
		if name == databaseName+".owner" && !sameSnapshotCaptureIdentity(root, name, markerIdentity) {
			cleanupErr = errors.Join(cleanupErr, ErrSnapshotExclusion)
			continue
		}
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if err := syncCaptureDirectory(root); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	return cleanupErr
}

func sameSnapshotCaptureIdentity(root *os.Root, name string, expected os.FileInfo) bool {
	current, err := root.Lstat(name)
	return err == nil && expected != nil && current.Mode().IsRegular() && current.Mode().Perm() == 0o600 &&
		current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, expected)
}
