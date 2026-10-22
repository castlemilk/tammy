//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package workspace

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

const authoritativeWorkspaceRecordKey = "workspace.record.v1"

type WorkspaceMutation struct {
	OperationID     string
	Kind            string
	WorkspaceID     string
	Version         uint64
	SemanticHash    string
	HeaderOperation bool
}

var (
	ErrWorkspaceNotFound = errors.New("workspace: workspace not found")
	ErrWorkspaceExists   = errors.New("workspace: workspace already exists")
)

type workspaceRecord struct {
	ID                       string
	Version                  uint64
	State                    tammyv1.WorkspaceState
	TrustState               tammyv1.WorkspaceTrustState
	DisplayName              string
	DatabasePath             string
	HeaderPath               string
	SetupID                  string
	SetupSemanticHash        string
	SetupConfirmationHash    string
	SetupExpires             int64
	SetupExpiredAt           int64
	SetupPhase               string
	SetupCleanupDatabasePath string
	SetupCleanupHeaderPath   string
	SetupMaterialEncrypted   []byte
	RecoveryDisplayEncrypted []byte
	RecoveryGroupHashes      [][]byte
	OwnerUserID              string
	RememberedUntil          int64
	OperationHashes          map[string]string
	OperationActors          map[string]string
	OperationSessions        map[string]string
}

type Repository interface {
	Save(context.Context, workspaceRecord) error
	ByID(context.Context, string) (workspaceRecord, error)
	ByPath(context.Context, string) (workspaceRecord, error)
	BySetup(context.Context, string) (workspaceRecord, error)
	Delete(context.Context, string) error
	NormalizeOpen(context.Context) error
}

type MemoryRepository struct {
	mu      sync.Mutex
	records map[string]workspaceRecord
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{records: make(map[string]workspaceRecord)}
}

// FileRepository is the restart-safe installation catalogue and pending-setup
// journal. Its complete contents are authenticated and encrypted with the
// installation key; SQLCipher remains authoritative for committed workspace
// mutations once a workspace is open.
type FileRepository struct {
	mu   sync.Mutex
	path string
	key  []byte
}

func NewFileRepository(path string, installationKey []byte) (*FileRepository, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(installationKey) != 32 {
		return nil, ErrWorkspaceNotFound
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return nil, ErrWorkspaceNotFound
	}
	return &FileRepository{path: path, key: append([]byte(nil), installationKey...)}, nil
}

func (repository *FileRepository) Save(_ context.Context, record workspaceRecord) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.loadLocked()
	if err != nil {
		return err
	}
	for id, existing := range records {
		if id != record.ID && ((record.DatabasePath != "" && existing.DatabasePath == record.DatabasePath) ||
			(record.SetupID != "" && existing.SetupID == record.SetupID)) {
			return ErrWorkspaceExists
		}
	}
	records[record.ID] = cloneWorkspaceRecord(record)
	return repository.writeLocked(records)
}

func (repository *FileRepository) ByID(_ context.Context, id string) (workspaceRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.loadLocked()
	if err != nil {
		return workspaceRecord{}, err
	}
	record, ok := records[id]
	if !ok {
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	return cloneWorkspaceRecord(record), nil
}

func (repository *FileRepository) ByPath(_ context.Context, path string) (workspaceRecord, error) {
	if path == "" {
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.loadLocked()
	if err != nil {
		return workspaceRecord{}, err
	}
	for _, record := range records {
		if record.DatabasePath == path {
			return cloneWorkspaceRecord(record), nil
		}
	}
	return workspaceRecord{}, ErrWorkspaceNotFound
}

func (repository *FileRepository) BySetup(_ context.Context, setupID string) (workspaceRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.loadLocked()
	if err != nil {
		return workspaceRecord{}, err
	}
	for _, record := range records {
		if record.SetupID == setupID {
			return cloneWorkspaceRecord(record), nil
		}
	}
	return workspaceRecord{}, ErrWorkspaceNotFound
}

func (repository *FileRepository) Delete(_ context.Context, id string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.loadLocked()
	if err != nil {
		return err
	}
	delete(records, id)
	return repository.writeLocked(records)
}

func (repository *FileRepository) NormalizeOpen(_ context.Context) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.loadLocked()
	if err != nil {
		return err
	}
	changed := false
	for id, record := range records {
		if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED &&
			record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
			continue
		}
		record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
		records[id] = record
		changed = true
	}
	if !changed {
		return nil
	}
	return repository.writeLocked(records)
}

func (repository *FileRepository) loadLocked() (map[string]workspaceRecord, error) {
	payload, _, err := readSecureRegularFile(repository.path, maxWorkspaceCatalogueFileSize)
	if os.IsNotExist(err) {
		return make(map[string]workspaceRecord), nil
	}
	if err != nil {
		return nil, ErrWorkspaceNotFound
	}
	block, err := aes.NewCipher(repository.key)
	if err != nil {
		return nil, ErrWorkspaceNotFound
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(payload) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrWorkspaceNotFound
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte("tammy.workspace-catalogue.v1"))
	if err != nil {
		return nil, ErrWorkspaceNotFound
	}
	defer Zero(plaintext)
	var records map[string]workspaceRecord
	if err := json.Unmarshal(plaintext, &records); err != nil || records == nil {
		return nil, ErrWorkspaceNotFound
	}
	for id, record := range records {
		records[id] = cloneWorkspaceRecord(record)
	}
	return records, nil
}

func (repository *FileRepository) writeLocked(records map[string]workspaceRecord) error {
	plaintext, err := json.Marshal(records)
	if err != nil {
		return ErrWorkspaceNotFound
	}
	defer Zero(plaintext)
	block, err := aes.NewCipher(repository.key)
	if err != nil {
		return ErrWorkspaceNotFound
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return ErrWorkspaceNotFound
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	payload := append(nonce, aead.Seal(nil, nonce, plaintext, []byte("tammy.workspace-catalogue.v1"))...)
	temporary, err := os.CreateTemp(filepath.Dir(repository.path), ".tammy-catalogue-*")
	if err != nil {
		return fmt.Errorf("workspace: create catalogue: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, repository.path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(repository.path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (repository *MemoryRepository) Save(_ context.Context, record workspaceRecord) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for id, existing := range repository.records {
		if id != record.ID && ((record.DatabasePath != "" && existing.DatabasePath == record.DatabasePath) ||
			(record.SetupID != "" && existing.SetupID == record.SetupID)) {
			return ErrWorkspaceExists
		}
	}
	repository.records[record.ID] = cloneWorkspaceRecord(record)
	return nil
}

func (repository *MemoryRepository) ByID(_ context.Context, id string) (workspaceRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, ok := repository.records[id]
	if !ok {
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	return cloneWorkspaceRecord(record), nil
}

func (repository *MemoryRepository) ByPath(_ context.Context, path string) (workspaceRecord, error) {
	if path == "" {
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, record := range repository.records {
		if record.DatabasePath == path {
			return cloneWorkspaceRecord(record), nil
		}
	}
	return workspaceRecord{}, ErrWorkspaceNotFound
}

func (repository *MemoryRepository) BySetup(_ context.Context, setupID string) (workspaceRecord, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, record := range repository.records {
		if record.SetupID == setupID {
			return cloneWorkspaceRecord(record), nil
		}
	}
	return workspaceRecord{}, ErrWorkspaceNotFound
}

func (repository *MemoryRepository) Delete(_ context.Context, id string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	delete(repository.records, id)
	return nil
}

func (repository *MemoryRepository) NormalizeOpen(_ context.Context) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for id, record := range repository.records {
		if record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED &&
			record.State != tammyv1.WorkspaceState_WORKSPACE_STATE_AUTHENTICATED {
			continue
		}
		record.State = tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED
		repository.records[id] = record
	}
	return nil
}

func cloneWorkspaceRecord(record workspaceRecord) workspaceRecord {
	record.RecoveryDisplayEncrypted = append([]byte(nil), record.RecoveryDisplayEncrypted...)
	record.SetupMaterialEncrypted = append([]byte(nil), record.SetupMaterialEncrypted...)
	operations := record.OperationHashes
	record.OperationHashes = make(map[string]string, len(operations))
	for key, value := range operations {
		record.OperationHashes[key] = value
	}
	actors := record.OperationActors
	record.OperationActors = make(map[string]string, len(actors))
	for key, value := range actors {
		record.OperationActors[key] = value
	}
	sessions := record.OperationSessions
	record.OperationSessions = make(map[string]string, len(sessions))
	for key, value := range sessions {
		record.OperationSessions[key] = value
	}
	hashes := record.RecoveryGroupHashes
	record.RecoveryGroupHashes = make([][]byte, len(hashes))
	for index := range hashes {
		record.RecoveryGroupHashes[index] = append([]byte(nil), hashes[index]...)
	}
	return record
}

type StorageHandle interface {
	CommitWorkspaceMutation(context.Context, WorkspaceMutation, workspaceRecord, func(MutationExecutor, *workspaceRecord) error) error
	LoadWorkspaceRecord(context.Context) (workspaceRecord, error)
	HeaderOperationCommitted(context.Context, string, uint64) bool
	Database() *sqlcipher.Database
	Close() error
}

type StorageFactory interface {
	Create(context.Context, string, []byte) (StorageHandle, error)
	Open(context.Context, string, []byte) (StorageHandle, error)
}

type sqlCipherStorageFactory struct{ target uint32 }

func NewSQLCipherStorageFactory(target uint32) StorageFactory {
	return sqlCipherStorageFactory{target: target}
}

func (factory sqlCipherStorageFactory) Create(ctx context.Context, path string, key []byte) (StorageHandle, error) {
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, factory.target); err != nil {
		return nil, err
	}
	return factory.Open(ctx, path, key)
}

func (factory sqlCipherStorageFactory) Open(ctx context.Context, path string, key []byte) (StorageHandle, error) {
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		return nil, err
	}
	return &sqlCipherStorageHandle{database: database}, nil
}

type sqlCipherStorageHandle struct{ database *sqlcipher.Database }

func (handle *sqlCipherStorageHandle) CommitWorkspaceMutation(
	ctx context.Context,
	mutation WorkspaceMutation,
	record workspaceRecord,
	dependent func(MutationExecutor, *workspaceRecord) error,
) error {
	if mutation.OperationID == "" || mutation.WorkspaceID != record.ID || mutation.Version != record.Version ||
		(mutation.HeaderOperation && mutation.Kind != "CREATE" && mutation.Kind != "PASSPHRASE_CHANGE" && mutation.Kind != "RECOVERY" && mutation.Kind != "ADMIN_RECOVERY") {
		return ErrHeaderOperation
	}
	transaction, err := handle.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	lifecycle := &mutationTransaction{MutationExecutor: transaction}
	if mutation.HeaderOperation {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO header_operation_ids(operation_id, operation_kind, header_version, committed_at)
			VALUES (?, ?, ?, ?)`, mutation.OperationID, mutation.Kind, mutation.Version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if dependent != nil {
		if err := dependent(lifecycle, &record); err != nil {
			return err
		}
	}
	if mutation.WorkspaceID != record.ID || mutation.Version != record.Version {
		return ErrHeaderOperation
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return ErrHeaderOperation
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO workspace_metadata(key, value, revision, updated_at) VALUES (?, ?, 1, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, revision = workspace_metadata.revision + 1,
		updated_at = excluded.updated_at`, authoritativeWorkspaceRecordKey, payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	committed = true
	return lifecycle.publish(ctx)
}

func (handle *sqlCipherStorageHandle) LoadWorkspaceRecord(ctx context.Context) (workspaceRecord, error) {
	return loadWorkspaceRecordFrom(ctx, handle.database)
}

func loadWorkspaceRecordFrom(ctx context.Context, executor MutationExecutor) (workspaceRecord, error) {
	rows, err := executor.QueryContext(ctx, `SELECT value FROM workspace_metadata WHERE key = ?`, authoritativeWorkspaceRecordKey)
	if err != nil {
		return workspaceRecord{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return workspaceRecord{}, ErrWorkspaceNotFound
		}
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	var payload []byte
	if err := rows.Scan(&payload); err != nil || rows.Next() || rows.Err() != nil {
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	var record workspaceRecord
	if err := json.Unmarshal(payload, &record); err != nil || record.ID == "" {
		return workspaceRecord{}, ErrWorkspaceNotFound
	}
	return cloneWorkspaceRecord(record), nil
}

func (handle *sqlCipherStorageHandle) HeaderOperationCommitted(ctx context.Context, operationID string, version uint64) bool {
	var count int
	return handle.database.QueryRowContext(ctx, `SELECT count(*) FROM header_operation_ids WHERE operation_id = ? AND header_version = ?`, operationID, version).Scan(&count) == nil && count == 1
}
func (handle *sqlCipherStorageHandle) Database() *sqlcipher.Database { return handle.database }
func (handle *sqlCipherStorageHandle) Close() error                  { return handle.database.Close() }
