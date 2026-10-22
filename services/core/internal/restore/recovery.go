package restore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrStartupRecovery = errors.New("restore: startup recovery failed")

const maximumStartupRecoveryRecords = maximumJournalDirectoryEntries

// RecoveryAction is the only safe restart direction for a durable journal
// state. PREPARED/STAGED cannot have an accepted active swap; SWAPPED must
// finish forward because the staged database was internally complete before
// activation; COMPLETE needs cleanup only.
type RecoveryAction uint8

const (
	RecoveryNone RecoveryAction = iota
	RecoveryRollback
	RecoveryFinishForward
	RecoveryCleanup
)

func RequiredRecoveryAction(state tammyv1.RestoreState) RecoveryAction {
	switch state {
	case tammyv1.RestoreState_RESTORE_STATE_PREPARED, tammyv1.RestoreState_RESTORE_STATE_STAGED:
		return RecoveryRollback
	case tammyv1.RestoreState_RESTORE_STATE_SWAPPED:
		return RecoveryFinishForward
	case tammyv1.RestoreState_RESTORE_STATE_COMPLETE:
		return RecoveryCleanup
	default:
		return RecoveryNone
	}
}

type StartupRecoveryJournal interface {
	ListRecoveryRecords(context.Context, string, uint32) (*RestoreJournalPage, error)
	CheckpointRecovery(context.Context, string, tammyv1.RestoreState, *tammyv1.RestoreRecoveryRecord) (*tammyv1.RestoreStatus, error)
	Advance(context.Context, string, tammyv1.RestoreState, tammyv1.RestoreState, []byte) (*tammyv1.RestoreStatus, error)
}

type StartupRecoveryWorkspace interface {
	// RollbackInterruptedRestore removes only the authenticated operation's
	// stage residue or reverses its exact active/rollback rename pair.
	RollbackInterruptedRestore(context.Context, *tammyv1.RestoreStatus) error
	// ReconstructAndVerifyActivatedRestore reopens the active encrypted
	// database and returns only metadata proven against the journal record.
	ReconstructAndVerifyActivatedRestore(context.Context, *tammyv1.RestoreStatus) (*FinalizedRestore, error)
	// CleanupCompletedRestore removes only the authenticated operation's
	// rollback/stage residue after COMPLETE is durable.
	CleanupCompletedRestore(context.Context, *tammyv1.RestoreStatus) error
}

type StartupRecoveryArchives interface {
	CleanupInterruptedPreRestoreArchive(context.Context, *tammyv1.RestoreStatus) error
}

type StartupRecoveryCredentialRevoker interface {
	RevokeRecoveredMachineCredentials(context.Context, *tammyv1.RestoreStatus) error
}

type StartupRecoveryConfig struct {
	Journal            StartupRecoveryJournal
	Workspace          StartupRecoveryWorkspace
	Archives           StartupRecoveryArchives
	MachineCredentials StartupRecoveryCredentialRevoker
	Mirror             RestoreMirrorPublisher
	BatchSize          uint32
}

type StartupRecoveryCoordinator struct{ config StartupRecoveryConfig }

type StartupRecoveryReport struct {
	Processed  uint32
	RolledBack uint32
	Completed  uint32
	Cleaned    uint32
}

func NewStartupRecoveryCoordinator(config StartupRecoveryConfig) (*StartupRecoveryCoordinator, error) {
	if nilInterface(config.Journal) || nilInterface(config.Workspace) || nilInterface(config.Archives) ||
		nilInterface(config.MachineCredentials) || nilInterface(config.Mirror) ||
		config.BatchSize == 0 || config.BatchSize > maximumJournalPageSize {
		return nil, ErrStartupRecovery
	}
	return &StartupRecoveryCoordinator{config: config}, nil
}

func (coordinator *StartupRecoveryCoordinator) Recover(ctx context.Context) (*StartupRecoveryReport, error) {
	if coordinator == nil || ctx == nil {
		return nil, ErrStartupRecovery
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrStartupRecovery, err)
	}
	report := new(StartupRecoveryReport)
	after := ""
	for {
		page, err := coordinator.config.Journal.ListRecoveryRecords(ctx, after, coordinator.config.BatchSize)
		if err != nil || !validRecoveryPage(page, after, coordinator.config.BatchSize) {
			return nil, errors.Join(ErrStartupRecovery, err)
		}
		for _, status := range page.Records {
			if report.Processed >= maximumStartupRecoveryRecords {
				return nil, ErrStartupRecovery
			}
			if err := coordinator.recoverRecord(ctx, status, report); err != nil {
				return nil, err
			}
			report.Processed++
		}
		if page.NextAfterOperationID == "" {
			return report, nil
		}
		after = page.NextAfterOperationID
	}
}

func (coordinator *StartupRecoveryCoordinator) recoverRecord(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
	report *StartupRecoveryReport,
) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrStartupRecovery, err)
	}
	switch RequiredRecoveryAction(status.State) {
	case RecoveryRollback:
		if status.State == tammyv1.RestoreState_RESTORE_STATE_STAGED && status.Recovery == nil {
			return ErrStartupRecovery
		}
		if err := coordinator.config.Workspace.RollbackInterruptedRestore(ctx, cloneStatus(status)); err != nil {
			return errors.Join(ErrStartupRecovery, err)
		}
		if err := coordinator.config.Archives.CleanupInterruptedPreRestoreArchive(ctx, cloneStatus(status)); err != nil {
			return errors.Join(ErrStartupRecovery, err)
		}
		terminal, err := coordinator.config.Journal.Advance(ctx, status.OperationId, status.State,
			tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK, status.NewAuditHead)
		if err != nil || terminal == nil || terminal.State != tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK {
			return errors.Join(ErrStartupRecovery, err)
		}
		report.RolledBack++
		return nil
	case RecoveryFinishForward:
		return coordinator.finishForward(ctx, status, report)
	case RecoveryCleanup:
		if status.Recovery == nil || !status.Recovery.PostSwapVerified ||
			!status.Recovery.MachineCredentialsRevoked || !status.Recovery.MirrorPublished {
			return ErrStartupRecovery
		}
		if err := coordinator.config.Workspace.CleanupCompletedRestore(ctx, cloneStatus(status)); err != nil {
			return errors.Join(ErrStartupRecovery, err)
		}
		report.Cleaned++
		return nil
	default:
		return ErrStartupRecovery
	}
}

func (coordinator *StartupRecoveryCoordinator) finishForward(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
	report *StartupRecoveryReport,
) error {
	if status.Recovery == nil {
		return ErrStartupRecovery
	}
	finalized, err := coordinator.config.Workspace.ReconstructAndVerifyActivatedRestore(ctx, cloneStatus(status))
	if err != nil || !validRecoveredFinalized(status, finalized) {
		return errors.Join(ErrStartupRecovery, err)
	}
	recovery := proto.Clone(status.Recovery).(*tammyv1.RestoreRecoveryRecord)
	if !recovery.PostSwapVerified {
		recovery.PostSwapVerified = true
		checkpoint, err := coordinator.config.Journal.CheckpointRecovery(ctx, status.OperationId,
			tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery)
		if err != nil || !statusBindsRecovery(checkpoint, tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery) {
			return errors.Join(ErrStartupRecovery, err)
		}
		status = checkpoint
	}
	if !recovery.MachineCredentialsRevoked {
		if err := coordinator.config.MachineCredentials.RevokeRecoveredMachineCredentials(ctx, cloneStatus(status)); err != nil {
			return errors.Join(ErrStartupRecovery, err)
		}
		recovery.MachineCredentialsRevoked = true
		checkpoint, err := coordinator.config.Journal.CheckpointRecovery(ctx, status.OperationId,
			tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery)
		if err != nil || !statusBindsRecovery(checkpoint, tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery) {
			return errors.Join(ErrStartupRecovery, err)
		}
		status = checkpoint
	}
	if !recovery.MirrorPublished {
		if err := coordinator.config.Mirror.PublishRestoredMirror(ctx, cloneFinalizedRestore(finalized)); err != nil {
			return errors.Join(ErrStartupRecovery, err)
		}
		recovery.MirrorPublished = true
		checkpoint, err := coordinator.config.Journal.CheckpointRecovery(ctx, status.OperationId,
			tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery)
		if err != nil || !statusBindsRecovery(checkpoint, tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery) {
			return errors.Join(ErrStartupRecovery, err)
		}
		status = checkpoint
	}
	completed, err := coordinator.config.Journal.Advance(ctx, status.OperationId,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, tammyv1.RestoreState_RESTORE_STATE_COMPLETE, finalized.AuditHead)
	if err != nil || !statusBindsRecovery(completed, tammyv1.RestoreState_RESTORE_STATE_COMPLETE, recovery) {
		return errors.Join(ErrStartupRecovery, err)
	}
	if err := coordinator.config.Workspace.CleanupCompletedRestore(ctx, completed); err != nil {
		return errors.Join(ErrStartupRecovery, err)
	}
	report.Completed++
	report.Cleaned++
	return nil
}

func validRecoveryPage(page *RestoreJournalPage, after string, limit uint32) bool {
	if page == nil || len(page.Records) > int(limit) ||
		(page.NextAfterOperationID != "" && (!ids.IsCanonicalV7(page.NextAfterOperationID) || len(page.Records) == 0 ||
			page.NextAfterOperationID != page.Records[len(page.Records)-1].OperationId)) {
		return false
	}
	previous := after
	for _, status := range page.Records {
		if status == nil || !validStatus(status) || status.OperationId <= previous {
			return false
		}
		previous = status.OperationId
	}
	return true
}

func statusBindsRecovery(status *tammyv1.RestoreStatus, state tammyv1.RestoreState,
	recovery *tammyv1.RestoreRecoveryRecord,
) bool {
	return status != nil && validStatus(status) && status.State == state && status.Recovery != nil &&
		proto.Equal(status.Recovery, recovery)
}

func validRecoveredFinalized(status *tammyv1.RestoreStatus, finalized *FinalizedRestore) bool {
	recovery := status.Recovery
	return finalized != nil && recovery != nil && finalized.OperationID == status.OperationId &&
		finalized.WorkspaceID == recovery.WorkspaceId && finalized.Generation == recovery.GetFinalizedGeneration() &&
		subtle.ConstantTimeCompare(finalized.AuditHead, recovery.FinalizedAuditHead) == 1 &&
		subtle.ConstantTimeCompare(finalized.BackupManifestHash, status.BackupManifestHash) == 1 &&
		finalized.PreRestoreArchiveID == recovery.PreRestoreArchiveId &&
		subtle.ConstantTimeCompare(finalized.PreRestoreArchiveHash, recovery.PreRestoreArchiveHash) == 1 &&
		finalized.SchemaVersion == recovery.GetSchemaVersion() && finalized.SchemaVersion > 0 &&
		len(finalized.MigrationManifestHash) == sha256.Size &&
		subtle.ConstantTimeCompare(finalized.MigrationManifestHash, recovery.MigrationManifestHash) == 1
}

func cloneFinalizedRestore(finalized *FinalizedRestore) *FinalizedRestore {
	if finalized == nil {
		return nil
	}
	clone := *finalized
	clone.AuditHead = append([]byte(nil), finalized.AuditHead...)
	clone.BackupManifestHash = append([]byte(nil), finalized.BackupManifestHash...)
	clone.PreRestoreArchiveHash = append([]byte(nil), finalized.PreRestoreArchiveHash...)
	clone.MigrationManifestHash = append([]byte(nil), finalized.MigrationManifestHash...)
	return &clone
}
