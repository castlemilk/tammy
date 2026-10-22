//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

type SQLCipherStartupRecoveryAdapterConfig struct {
	ActivePath       string
	StagingDirectory string
	Key              []byte
}

type SQLCipherStartupRecoveryAdapter struct {
	mu        sync.Mutex
	directory string
	baseInfo  os.FileInfo
	active    string
	key       []byte
	closed    bool
}

func NewSQLCipherStartupRecoveryAdapter(
	config SQLCipherStartupRecoveryAdapterConfig,
) (*SQLCipherStartupRecoveryAdapter, error) {
	if config.ActivePath == "" || config.StagingDirectory == "" || !filepath.IsAbs(config.ActivePath) ||
		!filepath.IsAbs(config.StagingDirectory) || filepath.Clean(config.ActivePath) != config.ActivePath ||
		filepath.Clean(config.StagingDirectory) != config.StagingDirectory ||
		filepath.Dir(config.ActivePath) != config.StagingDirectory || len(config.Key) != sqlcipher.KeySize {
		return nil, ErrSQLCipherWorkspace
	}
	baseInfo, err := os.Lstat(config.StagingDirectory)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 || baseInfo.Mode().Perm()&0o077 != 0 {
		return nil, ErrSQLCipherWorkspace
	}
	return &SQLCipherStartupRecoveryAdapter{directory: config.StagingDirectory, baseInfo: baseInfo,
		active: config.ActivePath, key: append([]byte(nil), config.Key...)}, nil
}

func (adapter *SQLCipherStartupRecoveryAdapter) RollbackInterruptedRestore(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) error {
	if adapter == nil || ctx == nil || status == nil || !validStatus(status) ||
		(status.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED &&
			status.State != tammyv1.RestoreState_RESTORE_STATE_STAGED) {
		return ErrSQLCipherWorkspace
	}
	if status.Recovery == nil {
		adapter.mu.Lock()
		defer adapter.mu.Unlock()
		if adapter.closed || ctx.Err() != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
			return errors.Join(ErrSQLCipherWorkspace, ctx.Err())
		}
		return cleanupUnboundRestoreArtifacts(ctx, adapter.directory, status.OperationId, adapter.key)
	}
	stageBasename := status.Recovery.StageBasename
	rollbackBasename := status.Recovery.RollbackBasename
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || ctx.Err() != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	if err := sqlcipher.RecoverInterruptedWorkspaceRestore(ctx, adapter.active, status.OperationId,
		status.Recovery.WorkspaceId, stageBasename, rollbackBasename, status.Recovery.ArtifactOwnershipDigest,
		status.Recovery.StageOwnerMarkerSha256, status.Recovery.RollbackOwnerMarkerSha256,
		status.Recovery.RollbackPredecessorHash); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return errors.Join(ErrSQLCipherWorkspace, contextErr)
		}
		return errors.Join(ErrSQLCipherWorkspace, err)
	}
	return nil
}

func (adapter *SQLCipherStartupRecoveryAdapter) ReconstructAndVerifyActivatedRestore(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) (*FinalizedRestore, error) {
	if adapter == nil || ctx == nil || status == nil || !validStatus(status) || status.Recovery == nil ||
		status.State != tammyv1.RestoreState_RESTORE_STATE_SWAPPED {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || ctx.Err() != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return nil, errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	database, err := sqlcipher.Open(ctx, adapter.active, adapter.key)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	recovery := status.Recovery
	verifyErr := verifyStagedSQLCipherDatabase(ctx, database, recovery.GetSchemaVersion(), recovery.MigrationManifestHash)
	if verifyErr == nil {
		verifyErr = backup.VerifySnapshotExclusions(ctx, database)
	}
	var finalized *FinalizedRestore
	if verifyErr == nil {
		finalized, verifyErr = reconstructFinalizedRestore(ctx, database, status)
	}
	closeErr := database.Close()
	if verifyErr != nil || closeErr != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return nil, ErrSQLCipherWorkspace
	}
	return finalized, nil
}

func (adapter *SQLCipherStartupRecoveryAdapter) CleanupCompletedRestore(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) error {
	if adapter == nil || ctx == nil || status == nil || !validStatus(status) || status.Recovery == nil ||
		status.State != tammyv1.RestoreState_RESTORE_STATE_COMPLETE || !status.Recovery.PostSwapVerified ||
		!status.Recovery.MachineCredentialsRevoked || !status.Recovery.MirrorPublished {
		return ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || ctx.Err() != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	if err := sqlcipher.CleanupCompletedWorkspaceRestore(ctx, adapter.active, status.OperationId,
		status.Recovery.WorkspaceId, status.Recovery.StageBasename, status.Recovery.RollbackBasename,
		status.Recovery.ArtifactOwnershipDigest, status.Recovery.StageOwnerMarkerSha256,
		status.Recovery.RollbackOwnerMarkerSha256, status.Recovery.RollbackPredecessorHash,
		status.Recovery.ActivatedDatabaseSha256); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return errors.Join(ErrSQLCipherWorkspace, contextErr)
		}
		return errors.Join(ErrSQLCipherWorkspace, err)
	}
	return nil
}

func reconstructFinalizedRestore(
	ctx context.Context,
	database *sqlcipher.Database,
	status *tammyv1.RestoreStatus,
) (*FinalizedRestore, error) {
	recovery := status.Recovery
	header, err := audit.LoadChainHeader(ctx, database, recovery.WorkspaceId, recovery.GetFinalizedGeneration())
	if err != nil || header.Generation != recovery.GetFinalizedGeneration() || header.CurrentSequence != 1 ||
		subtle.ConstantTimeCompare(header.CurrentHead[:], recovery.FinalizedAuditHead) != 1 {
		return nil, ErrSQLCipherWorkspace
	}
	events, err := audit.LoadStoredEvents(ctx, database, recovery.WorkspaceId, header.Generation, 1, 1)
	if err != nil || len(events) != 1 || events[0].Event == nil {
		return nil, ErrSQLCipherWorkspace
	}
	event := events[0].Event
	verification := audit.VerifyStoredChain(header, events)
	if verification == nil || verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		subtle.ConstantTimeCompare(verification.VerifiedHead, recovery.FinalizedAuditHead) != 1 ||
		event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED ||
		event.WorkspaceId != recovery.WorkspaceId || event.Generation != recovery.GetFinalizedGeneration() ||
		event.Sequence != 1 || event.GetCommandId() != status.OperationId ||
		event.CommandType != "tammy.v1.RestoreService.RestoreWorkspace" || event.Result == nil ||
		event.Result.OutcomeCode != "WORKSPACE_RESTORED" {
		return nil, ErrSQLCipherWorkspace
	}
	payload := event.GetPayload().GetWorkspaceRestored()
	if payload == nil || payload.WorkspaceId != recovery.WorkspaceId || payload.OperationId != status.OperationId ||
		payload.RestoredGeneration != recovery.GetFinalizedGeneration() || payload.PredecessorGeneration == 0 ||
		payload.BackupGeneration == 0 || payload.BackupGeneration >= payload.RestoredGeneration ||
		payload.PreRestoreArchiveId != recovery.PreRestoreArchiveId ||
		subtle.ConstantTimeCompare(payload.BackupManifestHash, status.BackupManifestHash) != 1 ||
		subtle.ConstantTimeCompare(payload.PreRestoreArchiveHash, recovery.PreRestoreArchiveHash) != 1 ||
		len(payload.PredecessorHead) != sha256.Size || len(payload.ArchivedHead) != sha256.Size ||
		subtle.ConstantTimeCompare(event.BeforeSemanticHash, payload.PredecessorHead) != 1 ||
		subtle.ConstantTimeCompare(event.AfterSemanticHash, payload.ArchivedHead) != 1 {
		return nil, ErrSQLCipherWorkspace
	}
	history, err := audit.LoadSigningKeyHistory(ctx, database, recovery.WorkspaceId)
	if err != nil || len(history) == 0 {
		zeroAuditSigningHistory(history)
		return nil, ErrSQLCipherWorkspace
	}
	active := history[len(history)-1]
	root, err := audit.SigningLineageRootFingerprint(recovery.WorkspaceId, history[0].KeyID, history[0].PublicKey)
	validHistory := err == nil && validRestoredSigningHistory(history, recovery.WorkspaceId, payload.BackupGeneration,
		active.KeyID, active.Epoch, root[:])
	zeroAuditSigningHistory(history)
	if !validHistory {
		return nil, ErrSQLCipherWorkspace
	}
	return &FinalizedRestore{OperationID: status.OperationId, WorkspaceID: recovery.WorkspaceId,
		Generation: recovery.GetFinalizedGeneration(), AuditHead: append([]byte(nil), recovery.FinalizedAuditHead...),
		EventType:          tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED,
		BackupManifestHash: append([]byte(nil), status.BackupManifestHash...), PreRestoreArchiveID: recovery.PreRestoreArchiveId,
		PreRestoreArchiveHash: append([]byte(nil), recovery.PreRestoreArchiveHash...),
		PredecessorGeneration: payload.PredecessorGeneration, PredecessorHead: append([]byte(nil), payload.PredecessorHead...),
		BackupGeneration: payload.BackupGeneration, BackupHead: append([]byte(nil), payload.ArchivedHead...),
		SigningKeyID: active.KeyID, SigningKeyEpoch: active.Epoch, AuditRoot: append([]byte(nil), root[:]...),
		SchemaVersion: recovery.GetSchemaVersion(), MigrationManifestHash: append([]byte(nil), recovery.MigrationManifestHash...)}, nil
}

func (adapter *SQLCipherStartupRecoveryAdapter) Close() error {
	if adapter == nil {
		return nil
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil
	}
	adapter.closed = true
	zeroBytes(adapter.key)
	adapter.key = nil
	return nil
}
