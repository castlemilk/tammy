//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"sync"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/sbr"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

// FixedCopyProjectionSet is constructed by the composition root over one
// sanitized staged database. It exposes only typed projection operations to
// Backup and must close every module-owned staged reader on Close.
type FixedCopyProjectionSet interface {
	SnapshotHeader(context.Context, SnapshotRequest) ([]byte, error)
	ProjectionSources(context.Context) ([]ProjectionSourceRegistration, error)
	Close() error
}

// SQLCipherProjectionFactory is the Task 8 composition boundary. Its
// implementation constructs module-owned typed readers over this exact fixed
// SQLCipher copy; providers and the registry never receive the database.
type SQLCipherProjectionFactory interface {
	OpenFixedCopy(context.Context, *sqlcipher.Database) (FixedCopyProjectionSet, error)
}

type SQLCipherSnapshotSourceConfig struct {
	Live        *sqlcipher.Database
	Key         []byte
	Staging     SQLCipherStagedCaptureConfig
	Projections SQLCipherProjectionFactory
}

// SQLCipherSnapshotSource owns the only transition from a live encrypted
// workspace to one sanitized, fixed, fully projected snapshot.
type SQLCipherSnapshotSource struct {
	mu          sync.Mutex
	live        *sqlcipher.Database
	key         []byte
	staging     SQLCipherStagedCaptureConfig
	projections SQLCipherProjectionFactory
}

func NewSQLCipherSnapshotSource(config SQLCipherSnapshotSourceConfig) (*SQLCipherSnapshotSource, error) {
	if config.Live == nil || config.Live.DB == nil || len(config.Key) != sqlcipher.KeySize ||
		nilInterface(config.Projections) || config.Staging.Directory == "" ||
		len(config.Staging.AuthenticationKey) != sha256.Size || nilInterface(config.Staging.NewID) {
		return nil, ErrService
	}
	if _, err := RecoverSQLCipherStagedCaptures(context.Background(), config.Staging); err != nil {
		return nil, ErrService
	}
	config.Staging.AuthenticationKey = append([]byte(nil), config.Staging.AuthenticationKey...)
	return &SQLCipherSnapshotSource{live: config.Live, key: append([]byte(nil), config.Key...),
		staging: config.Staging, projections: config.Projections}, nil
}

func (source *SQLCipherSnapshotSource) Close() {
	if source == nil {
		return
	}
	source.mu.Lock()
	zero(source.key)
	source.key = nil
	zero(source.staging.AuthenticationKey)
	source.staging.AuthenticationKey = nil
	source.mu.Unlock()
}

func (source *SQLCipherSnapshotSource) ConsistentSnapshot(
	ctx context.Context,
	workspaceID string,
	registry *ProviderRegistry,
) (CapturedSnapshot, error) {
	if source == nil || ctx == nil || !ids.IsCanonicalV7(workspaceID) || registry == nil {
		return CapturedSnapshot{}, ErrService
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if len(source.key) != sqlcipher.KeySize || source.live == nil || source.live.DB == nil || nilInterface(source.projections) {
		return CapturedSnapshot{}, ErrService
	}
	var captured CapturedSnapshot
	contents, err := captureSanitizedSQLCipherSnapshot(ctx, source.live, source.key, source.staging,
		func(ctx context.Context, staged *sqlcipher.Database, reader snapshotSQLReader,
			schemaVersion uint64, migrationHash []byte,
		) error {
			if err := sbr.VerifyBackupState(ctx, staged); err != nil {
				return ErrService
			}
			lineage, err := loadStagedAuditLineage(ctx, reader, workspaceID)
			if err != nil {
				return err
			}
			request := SnapshotRequest{WorkspaceID: workspaceID, AuditGeneration: lineage.Generation,
				AuditSequence: lineage.Sequence, AuditHead: append([]byte(nil), lineage.Head...)}
			projections, err := source.projections.OpenFixedCopy(ctx, staged)
			if err != nil || nilInterface(projections) {
				return ErrService
			}
			closed := false
			defer func() {
				if !closed {
					_ = projections.Close()
				}
			}()
			header, err := projections.SnapshotHeader(ctx, request)
			if err != nil || len(header) == 0 || len(header) > 1024*1024 {
				zero(header)
				return ErrService
			}
			sources, err := projections.ProjectionSources(ctx)
			if err != nil {
				zero(header)
				return ErrService
			}
			objects, err := registry.Collect(ctx, sources, request)
			if err != nil {
				zero(header)
				return err
			}
			if err := projections.Close(); err != nil {
				zero(header)
				for index := range objects {
					zero(objects[index].Bytes)
				}
				return ErrService
			}
			closed = true
			captured = CapturedSnapshot{Workspace: WorkspaceSnapshot{Header: append([]byte(nil), header...),
				SchemaVersion: schemaVersion, MigrationManifestHash: append([]byte(nil), migrationHash...)},
				Lineage: lineage, ProviderObjects: cloneObjects(objects)}
			zero(header)
			return nil
		})
	if err != nil {
		zeroCapturedSnapshot(&captured)
		return CapturedSnapshot{}, ErrService
	}
	captured.Workspace.Database = contents
	if !validWorkspaceSnapshot(captured.Workspace) || !validAuditLineage(captured.Lineage) ||
		registry.ValidateCollected(captured.ProviderObjects) != nil {
		zeroCapturedSnapshot(&captured)
		return CapturedSnapshot{}, ErrService
	}
	return captured, nil
}

type stagedAuditExecutor struct {
	reader snapshotSQLReader
}

func (executor stagedAuditExecutor) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	return executor.reader.QueryContext(ctx, query, arguments...)
}

func (stagedAuditExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, ErrService
}

func loadStagedAuditLineage(ctx context.Context, reader snapshotSQLReader, workspaceID string) (AuditLineage, error) {
	if ctx == nil || nilInterface(reader) || !ids.IsCanonicalV7(workspaceID) {
		return AuditLineage{}, ErrService
	}
	executor := stagedAuditExecutor{reader: reader}
	header, err := audit.LoadChainHeader(ctx, executor, workspaceID, 0)
	if err != nil {
		return AuditLineage{}, ErrService
	}
	history, err := audit.LoadSigningKeyHistory(ctx, executor, workspaceID)
	if err != nil || len(history) == 0 {
		zeroSigningHistory(history)
		return AuditLineage{}, ErrService
	}
	defer zeroSigningHistory(history)
	root := history[0]
	active := history[len(history)-1]
	if active.Generation > header.Generation || root.Epoch != 1 || active.Epoch != uint64(len(history)) ||
		len(root.PublicKey) != ed25519.PublicKeySize || len(active.PublicKey) != ed25519.PublicKeySize {
		return AuditLineage{}, ErrService
	}
	verifier, err := audit.NewStreamingStoredChainVerifier(ctx, header)
	if err != nil {
		return AuditLineage{}, ErrService
	}
	closed := false
	defer func() {
		if !closed {
			_ = verifier.Close()
		}
	}()
	snapshot := audit.StoredEventSnapshot{WorkspaceID: workspaceID, Generation: header.Generation,
		EndSequence: header.CurrentSequence, EndHead: header.CurrentHead}
	checkpoint := audit.StoredEventCheckpoint{Head: header.GenesisHash}
	for checkpoint.AfterSequence < snapshot.EndSequence {
		page, err := audit.LoadStoredEventPage(ctx, executor, snapshot, checkpoint,
			audit.StoredEventPageSizeLimit, audit.StoredEventPageByteBudget)
		if err != nil || len(page.Events) == 0 || page.Checkpoint.AfterSequence <= checkpoint.AfterSequence ||
			verifier.AcceptPage(page.Events) != nil {
			return AuditLineage{}, ErrService
		}
		checkpoint = page.Checkpoint
		if !page.HasMore && checkpoint.AfterSequence != snapshot.EndSequence {
			return AuditLineage{}, ErrService
		}
	}
	verification := verifier.Finish()
	if verifier.TerminalError() != nil || verification == nil ||
		verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		verification.VerifiedThroughSequence != header.CurrentSequence ||
		!bytes.Equal(verification.VerifiedHead, header.CurrentHead[:]) {
		return AuditLineage{}, ErrService
	}
	if err := verifier.Close(); err != nil {
		return AuditLineage{}, ErrService
	}
	closed = true
	rootFingerprint, err := audit.SigningLineageRootFingerprint(workspaceID, root.KeyID, root.PublicKey)
	if err != nil {
		return AuditLineage{}, ErrService
	}
	return AuditLineage{Generation: header.Generation, Sequence: header.CurrentSequence,
		Head: append([]byte(nil), header.CurrentHead[:]...), Root: append([]byte(nil), rootFingerprint[:]...),
		SigningKeyID: active.KeyID, SigningKeyEpoch: active.Epoch,
		PublicKey: append(ed25519.PublicKey(nil), active.PublicKey...)}, nil
}

func zeroSigningHistory(history []audit.SigningKeyRecord) {
	for index := range history {
		audit.Zero(history[index].EncryptedPrivateKey)
		audit.Zero(history[index].Nonce)
		audit.Zero(history[index].PreviousSignature)
		audit.Zero(history[index].PossessionSignature)
		audit.Zero(history[index].RotationPriorHead)
	}
}

func zeroCapturedSnapshot(snapshot *CapturedSnapshot) {
	if snapshot == nil {
		return
	}
	zero(snapshot.Workspace.Database)
	zero(snapshot.Workspace.Header)
	zero(snapshot.Workspace.MigrationManifestHash)
	zero(snapshot.Lineage.Head)
	zero(snapshot.Lineage.Root)
	zero(snapshot.Lineage.PublicKey)
	for index := range snapshot.ProviderObjects {
		zero(snapshot.ProviderObjects[index].Bytes)
	}
	*snapshot = CapturedSnapshot{}
}
