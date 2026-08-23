//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/sbr"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrSQLCipherWorkspace = errors.New("restore: SQLCipher workspace boundary failed")

const maximumStagedDatabaseBytes = 256 * 1024 * 1024

type SQLCipherWorkspaceAdapterConfig struct {
	ActivePath             string
	StagingDirectory       string
	Key                    []byte
	NewID                  func() (string, error)
	NewReceiptID           func() (string, error)
	NewEventID             func() (string, error)
	Now                    func() time.Time
	Random                 io.Reader
	AuditSchemaFingerprint []byte
	hooks                  *sqlcipherWorkspaceHooks
}

type sqlcipherWorkspaceHooks struct {
	afterStagedMutation func() error
}

type SQLCipherWorkspaceAdapter struct {
	mu                     sync.Mutex
	root                   *os.Root
	baseInfo               os.FileInfo
	directory              string
	activePath             string
	key                    []byte
	newID                  func() (string, error)
	newReceiptID           func() (string, error)
	newEventID             func() (string, error)
	now                    func() time.Time
	random                 io.Reader
	auditSchemaFingerprint []byte
	hooks                  *sqlcipherWorkspaceHooks
	staged                 map[string]*StagedWorkspace
	closed                 bool
}

func NewSQLCipherWorkspaceAdapter(config SQLCipherWorkspaceAdapterConfig) (*SQLCipherWorkspaceAdapter, error) {
	if config.ActivePath == "" || config.StagingDirectory == "" || !filepath.IsAbs(config.ActivePath) ||
		!filepath.IsAbs(config.StagingDirectory) || filepath.Clean(config.ActivePath) != config.ActivePath ||
		filepath.Clean(config.StagingDirectory) != config.StagingDirectory || filepath.Dir(config.ActivePath) != config.StagingDirectory ||
		len(config.Key) != sqlcipher.KeySize || nilInterface(config.NewID) || nilInterface(config.NewReceiptID) ||
		nilInterface(config.NewEventID) || config.Now == nil || config.Random == nil || len(config.AuditSchemaFingerprint) != sha256.Size {
		return nil, ErrSQLCipherWorkspace
	}
	baseInfo, err := os.Lstat(config.StagingDirectory)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 || baseInfo.Mode().Perm()&0o077 != 0 {
		return nil, ErrSQLCipherWorkspace
	}
	root, err := os.OpenRoot(config.StagingDirectory)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(baseInfo, rootInfo) {
		_ = root.Close()
		return nil, ErrSQLCipherWorkspace
	}
	activeInfo, err := root.Lstat(filepath.Base(config.ActivePath))
	if err != nil || !activeInfo.Mode().IsRegular() || activeInfo.Mode().Perm() != 0o600 || activeInfo.Mode()&os.ModeSymlink != 0 {
		_ = root.Close()
		return nil, ErrSQLCipherWorkspace
	}
	return &SQLCipherWorkspaceAdapter{root: root, baseInfo: baseInfo, directory: config.StagingDirectory,
		activePath: config.ActivePath, key: append([]byte(nil), config.Key...), newID: config.NewID,
		newReceiptID: config.NewReceiptID, newEventID: config.NewEventID, now: config.Now, random: config.Random,
		auditSchemaFingerprint: append([]byte(nil), config.AuditSchemaFingerprint...), hooks: config.hooks,
		staged: make(map[string]*StagedWorkspace)}, nil
}

func (adapter *SQLCipherWorkspaceAdapter) Stage(ctx context.Context, request StageRequest) (*StagedWorkspace, error) {
	if adapter == nil || ctx == nil || !ids.IsCanonicalV7(request.OperationID) || !ids.IsCanonicalV7(request.WorkspaceID) ||
		request.Manifest == nil || request.Manifest.WorkspaceId != request.WorkspaceID || request.Manifest.SchemaVersion == 0 ||
		len(request.Manifest.MigrationManifestHash) != sha256.Size || len(request.ManifestHash) != sha256.Size ||
		request.Artifacts == nil {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || adapter.root == nil || len(adapter.key) != sqlcipher.KeySize || ctx.Err() != nil ||
		!sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return nil, errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	var databaseBytes []byte
	for _, object := range request.Objects {
		if object.Path == "database/workspace.db" {
			if databaseBytes != nil || object.Provider != "workspace" || object.ProviderVersion != 1 ||
				len(object.Bytes) == 0 || len(object.Bytes) > maximumStagedDatabaseBytes {
				return nil, ErrSQLCipherWorkspace
			}
			databaseBytes = object.Bytes
		}
	}
	if databaseBytes == nil {
		return nil, ErrSQLCipherWorkspace
	}
	handleID, err := adapter.newID()
	if err != nil || !ids.IsCanonicalV7(handleID) {
		return nil, ErrSQLCipherWorkspace
	}
	if request.Artifacts.operationID != request.OperationID || request.Artifacts.workspaceID != request.WorkspaceID ||
		!adapter.validArtifactCapability(request.Artifacts) {
		return nil, ErrSQLCipherWorkspace
	}
	name := request.Artifacts.stageBasename
	owned := true
	defer func() {
		if owned {
			_ = adapter.removeReservedArtifacts(request.Artifacts,
				request.Artifacts.storageReservation.(*sqlcipherArtifactReservation), true)
		}
	}()
	if err := adapter.populateReservedStage(ctx, request.Artifacts, databaseBytes); err != nil ||
		!sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return nil, ErrSQLCipherWorkspace
	}
	path := filepath.Join(adapter.directory, name)
	database, err := sqlcipher.Open(ctx, path, adapter.key)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	verifyErr := verifyStagedSQLCipherDatabase(ctx, database, request.Manifest.SchemaVersion,
		request.Manifest.MigrationManifestHash)
	closeErr := database.Close()
	if verifyErr != nil || closeErr != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return nil, ErrSQLCipherWorkspace
	}
	if err := adapter.removeOwnedLock(name + ".lock"); err != nil {
		return nil, err
	}
	identity, err := adapter.root.Lstat(name)
	if err != nil || !identity.Mode().IsRegular() || identity.Mode().Perm() != 0o600 || identity.Size() != int64(len(databaseBytes)) {
		return nil, ErrSQLCipherWorkspace
	}
	staged := &StagedWorkspace{Handle: handleID, stageAuthority: adapter, stagedPath: path, artifacts: request.Artifacts}
	adapter.staged[handleID] = staged
	owned = false
	return staged, nil
}

func (adapter *SQLCipherWorkspaceAdapter) DiscardStaged(ctx context.Context, staged *StagedWorkspace) error {
	if adapter == nil || ctx == nil || staged == nil {
		return ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || staged.stageAuthority != adapter || adapter.staged[staged.Handle] != staged ||
		ctx.Err() != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return ErrSQLCipherWorkspace
	}
	if staged.artifacts == nil || !adapter.validArtifactCapability(staged.artifacts) {
		return ErrSQLCipherWorkspace
	}
	err := adapter.removeReservedArtifacts(staged.artifacts,
		staged.artifacts.storageReservation.(*sqlcipherArtifactReservation), true)
	if err == nil {
		delete(adapter.staged, staged.Handle)
		staged.stageAuthority = nil
		staged.artifacts.artifactAuthority = nil
		staged.artifacts.storageReservation = nil
		staged.artifacts = nil
	}
	return err
}

func (adapter *SQLCipherWorkspaceAdapter) FinalizeStagedWorkspace(ctx context.Context, request FinalizeStagedRestoreRequest) (*FinalizedRestore, error) {
	if adapter == nil || ctx == nil || request.Manifest == nil || request.Authorization == nil ||
		request.PreRestoreArchive == nil || request.Staged == nil || !ids.IsCanonicalV7(request.OperationID) ||
		request.WorkspaceID != request.Manifest.WorkspaceId || request.Authorization.WorkspaceID != request.WorkspaceID ||
		request.NewGeneration != request.Authorization.CurrentGeneration+1 || request.NewGeneration <= request.Manifest.AuditGeneration ||
		len(request.Authorization.CurrentAuditHead) != sha256.Size || len(request.Manifest.AuditHead) != sha256.Size ||
		len(request.ManifestHash) != sha256.Size || !validPreRestoreArchive(request.PreRestoreArchive) {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	staged := request.Staged
	if adapter.closed || staged.stageAuthority != adapter || adapter.staged[staged.Handle] != staged || ctx.Err() != nil {
		return nil, ErrSQLCipherWorkspace
	}
	database, err := sqlcipher.Open(ctx, staged.stagedPath, adapter.key)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	defer func() {
		_ = database.Close()
		_ = adapter.removeOwnedLock(filepath.Base(staged.stagedPath) + ".lock")
	}()
	if err := verifyArchivedAuditState(ctx, database, request.Manifest); err != nil {
		return nil, err
	}
	history, err := audit.LoadSigningKeyHistory(ctx, database, request.WorkspaceID)
	if err != nil || !validRestoredSigningHistory(history, request.WorkspaceID, request.Manifest.AuditGeneration,
		request.Manifest.SigningKeyId, request.Manifest.SigningKeyEpoch, request.Manifest.AuditRoot) {
		zeroAuditSigningHistory(history)
		return nil, ErrSQLCipherWorkspace
	}
	zeroAuditSigningHistory(history)
	transaction, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	if err := backup.SanitizeSnapshot(ctx, transaction); err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	restoredAt := adapter.now().UTC()
	if restoredAt.IsZero() || sbr.MarkRestoredState(ctx, transaction,
		restoredAt.Format("2006-01-02T15:04:05.000000000Z")) != nil {
		return nil, ErrSQLCipherWorkspace
	}
	if err := PersistPreRestoreArchive(ctx, transaction, PreRestoreArchiveRecord{
		WorkspaceID: request.WorkspaceID, OperationID: request.OperationID,
		ArchiveID: request.PreRestoreArchive.ArchiveID, Version: request.PreRestoreArchive.Version,
		State:     tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE,
		CreatedAt: request.PreRestoreArchive.CreatedAt, DeletionEligibleAt: request.PreRestoreArchive.DeletionEligibleAt,
		ContentHash:         append([]byte(nil), request.PreRestoreArchive.SHA256...),
		SourceGeneration:    request.PreRestoreArchive.SourceGeneration,
		EncryptedByteLength: request.PreRestoreArchive.EncryptedByteLength,
	}); err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	salt := make([]byte, sha256.Size)
	if _, err := io.ReadFull(adapter.random, salt); err != nil {
		zeroBytes(salt)
		return nil, ErrSQLCipherWorkspace
	}
	defer zeroBytes(salt)
	genesis, err := audit.Genesis(request.WorkspaceID, salt)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	occurredAt := restoredAt
	if occurredAt.IsZero() {
		return nil, ErrSQLCipherWorkspace
	}
	if err := audit.InitializeChain(ctx, transaction, audit.ChainHeader{WorkspaceID: request.WorkspaceID,
		Generation: request.NewGeneration, ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis,
		CreatedAt: occurredAt}); err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	if adapter.hooks != nil && adapter.hooks.afterStagedMutation != nil {
		if err := adapter.hooks.afterStagedMutation(); err != nil {
			return nil, errors.Join(ErrSQLCipherWorkspace, err)
		}
	}
	eventID, err := adapter.newEventID()
	if err != nil || !ids.IsCanonicalV7(eventID) {
		return nil, ErrSQLCipherWorkspace
	}
	payload := &tammyv1.WorkspaceRestoredEvent{WorkspaceId: request.WorkspaceID, OperationId: request.OperationID,
		PredecessorGeneration: request.Authorization.CurrentGeneration, BackupGeneration: request.Manifest.AuditGeneration,
		RestoredGeneration: request.NewGeneration, BackupManifestHash: append([]byte(nil), request.ManifestHash...),
		PreRestoreArchiveId:   request.PreRestoreArchive.ArchiveID,
		PreRestoreArchiveHash: append([]byte(nil), request.PreRestoreArchive.SHA256...),
		PredecessorHead:       append([]byte(nil), request.Authorization.CurrentAuditHead...),
		ArchivedHead:          append([]byte(nil), request.Manifest.AuditHead...)}
	payloadProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	payloadHash := sha256.Sum256(payloadProto)
	commandID := request.OperationID
	source := &tammyv1.SourceRef{Type: "workspace", Id: request.WorkspaceID, Revision: request.NewGeneration,
		ContentHash: append([]byte(nil), request.ManifestHash...)}
	event := &tammyv1.AuditEvent{Id: eventID, WorkspaceId: request.WorkspaceID,
		Type: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED, OccurredAt: timestamppb.New(occurredAt),
		CommandId: &commandID, CommandType: "tammy.v1.RestoreService.RestoreWorkspace", Source: source,
		AffectedResources:  []*tammyv1.SourceRef{proto.Clone(source).(*tammyv1.SourceRef)},
		BeforeSemanticHash: append([]byte(nil), request.Authorization.CurrentAuditHead...),
		AfterSemanticHash:  append([]byte(nil), request.Manifest.AuditHead...),
		Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.WorkspaceRestoredEvent",
			DeterministicSha256: payloadHash[:], OutcomeCode: "WORKSPACE_RESTORED"},
		PayloadSchemaFingerprint: append([]byte(nil), adapter.auditSchemaFingerprint...),
		Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceRestored{WorkspaceRestored: payload}}}
	stored, err := audit.AppendStagedWorkspaceRestored(ctx, transaction, event, payloadProto)
	if err != nil || stored.Event == nil || stored.Event.Generation != request.NewGeneration || stored.Event.Sequence != 1 ||
		len(stored.Event.EventHash) != sha256.Size {
		return nil, ErrSQLCipherWorkspace
	}
	if err := transaction.Commit(); err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	committed = true
	return &FinalizedRestore{OperationID: request.OperationID, WorkspaceID: request.WorkspaceID,
		Generation: request.NewGeneration, AuditHead: append([]byte(nil), stored.Event.EventHash...),
		EventType:             tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED,
		BackupManifestHash:    append([]byte(nil), request.ManifestHash...),
		PreRestoreArchiveID:   request.PreRestoreArchive.ArchiveID,
		PreRestoreArchiveHash: append([]byte(nil), request.PreRestoreArchive.SHA256...),
		PredecessorGeneration: request.Authorization.CurrentGeneration,
		PredecessorHead:       append([]byte(nil), request.Authorization.CurrentAuditHead...),
		BackupGeneration:      request.Manifest.AuditGeneration, BackupHead: append([]byte(nil), request.Manifest.AuditHead...),
		SigningKeyID: request.Manifest.SigningKeyId, SigningKeyEpoch: request.Manifest.SigningKeyEpoch,
		AuditRoot: append([]byte(nil), request.Manifest.AuditRoot...), SchemaVersion: request.Manifest.SchemaVersion,
		MigrationManifestHash: append([]byte(nil), request.Manifest.MigrationManifestHash...),
		Staged:                staged}, nil
}

func (adapter *SQLCipherWorkspaceAdapter) VerifyStaged(ctx context.Context, request StagedVerificationRequest) (*VerifiedStagedWorkspace, error) {
	if adapter == nil || ctx == nil || request.Manifest == nil || request.Authorization == nil || request.Finalized == nil ||
		request.Finalized.Staged == nil || !ids.IsCanonicalV7(request.OperationID) || request.WorkspaceID != request.Manifest.WorkspaceId ||
		request.Finalized.WorkspaceID != request.WorkspaceID || request.Finalized.Generation != request.Authorization.CurrentGeneration+1 ||
		len(request.ManifestHash) != sha256.Size || len(request.Finalized.AuditHead) != sha256.Size {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	staged := request.Finalized.Staged
	if adapter.closed || staged.stageAuthority != adapter || adapter.staged[staged.Handle] != staged || ctx.Err() != nil {
		return nil, ErrSQLCipherWorkspace
	}
	stageName := filepath.Base(staged.stagedPath)
	verifiedIdentity, err := adapter.root.Lstat(stageName)
	if err != nil || !validStagedDatabaseIdentity(verifiedIdentity) {
		return nil, ErrSQLCipherWorkspace
	}
	database, err := sqlcipher.Open(ctx, staged.stagedPath, adapter.key)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	verifyErr := verifyStagedSQLCipherDatabase(ctx, database, request.Manifest.SchemaVersion,
		request.Manifest.MigrationManifestHash)
	if verifyErr == nil {
		verifyErr = backup.VerifySnapshotExclusions(ctx, database)
	}
	if verifyErr == nil {
		verifyErr = verifyFinalizedAuditState(ctx, database, request)
	}
	closeErr := database.Close()
	lockErr := adapter.removeOwnedLock(stageName + ".lock")
	if verifyErr != nil || closeErr != nil || lockErr != nil {
		return nil, ErrSQLCipherWorkspace
	}
	verifiedHash, err := hashVerifiedStagedDatabase(ctx, adapter.root, stageName, verifiedIdentity)
	if err != nil {
		return nil, err
	}
	return &VerifiedStagedWorkspace{Finalized: request.Finalized, verificationAuthority: adapter,
		stagedIdentity: verifiedIdentity, stagedSHA256: verifiedHash}, nil
}

// VerifyActivated reopens the swapped database without a transaction and
// repeats every staged invariant before external capabilities are published.
func (adapter *SQLCipherWorkspaceAdapter) VerifyActivated(ctx context.Context, request ActivatedVerificationRequest) error {
	if adapter == nil || ctx == nil || !ids.IsCanonicalV7(request.OperationID) || request.Verified == nil ||
		request.Verified.Finalized == nil || request.Receipt == nil || request.Verified.Finalized.OperationID != request.OperationID ||
		request.Verified.Finalized.SchemaVersion == 0 || len(request.Verified.Finalized.MigrationManifestHash) != sha256.Size {
		return ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	finalized := request.Verified.Finalized
	if adapter.closed || request.Receipt.swapAuthority != adapter || ctx.Err() != nil ||
		!sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return ErrSQLCipherWorkspace
	}
	database, err := sqlcipher.Open(ctx, adapter.activePath, adapter.key)
	if err != nil {
		return ErrSQLCipherWorkspace
	}
	verifyErr := verifyStagedSQLCipherDatabase(ctx, database, finalized.SchemaVersion, finalized.MigrationManifestHash)
	if verifyErr == nil {
		verifyErr = backup.VerifySnapshotExclusions(ctx, database)
	}
	if verifyErr == nil {
		verifyErr = verifyFinalizedAuditState(ctx, database, StagedVerificationRequest{
			OperationID: request.OperationID, WorkspaceID: finalized.WorkspaceID, Finalized: finalized})
	}
	closeErr := database.Close()
	if verifyErr != nil || closeErr != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return ErrSQLCipherWorkspace
	}
	return nil
}

func (adapter *SQLCipherWorkspaceAdapter) Swap(ctx context.Context, request SwapRequest) (*SwapReceipt, error) {
	if adapter == nil || ctx == nil || !ids.IsCanonicalV7(request.OperationID) || request.Verified == nil ||
		request.Verified.Finalized == nil || request.Verified.Finalized.Staged == nil || request.PreRestoreArchive == nil ||
		request.Reservation == nil {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	staged := request.Verified.Finalized.Staged
	if adapter.closed || staged.stageAuthority != adapter || adapter.staged[staged.Handle] != staged ||
		ctx.Err() != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) || staged.artifacts == nil ||
		!adapter.validArtifactCapability(staged.artifacts) || request.Reservation.swapAuthority != adapter ||
		request.Reservation.operationID != request.OperationID ||
		request.Reservation.workspaceID != request.Verified.Finalized.WorkspaceID {
		return nil, ErrSQLCipherWorkspace
	}
	receiptID, err := adapter.newReceiptID()
	if err != nil || !ids.IsCanonicalV7(receiptID) {
		return nil, ErrSQLCipherWorkspace
	}
	storageReservation, ok := request.Reservation.storageReservation.(*sqlcipher.RestoreReservationLock)
	if !ok || storageReservation == nil {
		return nil, ErrSQLCipherWorkspace
	}
	storageReceipt, err := sqlcipher.SwapWorkspaceForRestore(ctx, adapter.activePath, staged.stagedPath,
		request.OperationID, request.Reservation.workspaceID, staged.artifacts.rollbackBasename, receiptID,
		staged.artifacts.ownershipDigest[:], staged.artifacts.stageMarkerHash[:], staged.artifacts.rollbackMarkerHash[:],
		request.Reservation.predecessorHash[:], request.Reservation.activatedHash[:], storageReservation)
	if err != nil {
		return nil, errors.Join(ErrSQLCipherWorkspace, err)
	}
	delete(adapter.staged, staged.Handle)
	staged.stageAuthority = nil
	staged.artifacts.artifactAuthority = nil
	staged.artifacts.storageReservation = nil
	staged.artifacts = nil
	request.Reservation.swapAuthority = nil
	request.Reservation.storageReservation = nil
	return &SwapReceipt{ReceiptID: receiptID, swapAuthority: adapter, storageReceipt: storageReceipt}, nil
}

func (adapter *SQLCipherWorkspaceAdapter) ReserveSwap(
	ctx context.Context,
	operationID string,
	workspaceID string,
	verified *VerifiedStagedWorkspace,
) (*RestoreSwapReservation, error) {
	if adapter == nil || ctx == nil || verified == nil || verified.Finalized == nil || verified.Finalized.Staged == nil ||
		!ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(workspaceID) {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	staged := verified.Finalized.Staged
	verifiedIdentity, identityOK := verified.stagedIdentity.(os.FileInfo)
	if adapter.closed || ctx.Err() != nil || verified.Finalized.WorkspaceID != workspaceID ||
		staged.stageAuthority != adapter || adapter.staged[staged.Handle] != staged || staged.artifacts == nil ||
		staged.artifacts.operationID != operationID || staged.artifacts.workspaceID != workspaceID ||
		!adapter.validArtifactCapability(staged.artifacts) || verified.verificationAuthority != adapter ||
		!identityOK || verifiedIdentity == nil {
		return nil, ErrSQLCipherWorkspace
	}
	storageReservation, err := sqlcipher.ReserveWorkspaceRestore(ctx, adapter.activePath, staged.stagedPath,
		verifiedIdentity, verified.stagedSHA256[:])
	if err != nil {
		return nil, errors.Join(ErrSQLCipherWorkspace, err)
	}
	predecessorHash := storageReservation.PredecessorHash()
	activatedHash := storageReservation.ActivatedHash()
	if len(predecessorHash) != sha256.Size || len(activatedHash) != sha256.Size {
		_ = sqlcipher.ReleaseWorkspaceRestoreReservation(storageReservation)
		zeroBytes(predecessorHash)
		zeroBytes(activatedHash)
		return nil, ErrSQLCipherWorkspace
	}
	var boundHash [sha256.Size]byte
	copy(boundHash[:], predecessorHash)
	var boundActivatedHash [sha256.Size]byte
	copy(boundActivatedHash[:], activatedHash)
	zeroBytes(predecessorHash)
	zeroBytes(activatedHash)
	return &RestoreSwapReservation{operationID: operationID, workspaceID: workspaceID,
		predecessorHash: boundHash, activatedHash: boundActivatedHash,
		swapAuthority: adapter, storageReservation: storageReservation}, nil
}

func (adapter *SQLCipherWorkspaceAdapter) ReleaseSwap(ctx context.Context, reservation *RestoreSwapReservation) error {
	if adapter == nil || ctx == nil || reservation == nil {
		return ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	storageReservation, ok := reservation.storageReservation.(*sqlcipher.RestoreReservationLock)
	if adapter.closed || ctx.Err() != nil || reservation.swapAuthority != adapter || !ok || storageReservation == nil {
		return errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	if err := sqlcipher.ReleaseWorkspaceRestoreReservation(storageReservation); err != nil {
		return errors.Join(ErrSQLCipherWorkspace, err)
	}
	reservation.swapAuthority = nil
	reservation.storageReservation = nil
	zeroBytes(reservation.predecessorHash[:])
	zeroBytes(reservation.activatedHash[:])
	return nil
}

func (adapter *SQLCipherWorkspaceAdapter) Rollback(ctx context.Context, receipt *SwapReceipt) error {
	return adapter.finishSwap(ctx, receipt, true)
}

func (adapter *SQLCipherWorkspaceAdapter) CommitSwap(ctx context.Context, receipt *SwapReceipt) error {
	return adapter.finishSwap(ctx, receipt, false)
}

func (adapter *SQLCipherWorkspaceAdapter) finishSwap(ctx context.Context, receipt *SwapReceipt, rollback bool) error {
	if adapter == nil || ctx == nil || receipt == nil {
		return ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	storageReceipt, ok := receipt.storageReceipt.(*sqlcipher.RestoreSwapReceipt)
	if adapter.closed || receipt.swapAuthority != adapter || !ok || storageReceipt == nil {
		return ErrSQLCipherWorkspace
	}
	var err error
	if rollback {
		err = sqlcipher.RollbackWorkspaceRestore(ctx, storageReceipt)
	} else {
		err = sqlcipher.CommitWorkspaceRestore(ctx, storageReceipt)
	}
	if err != nil {
		return errors.Join(ErrSQLCipherWorkspace, err)
	}
	receipt.swapAuthority = nil
	receipt.storageReceipt = nil
	return nil
}

func (adapter *SQLCipherWorkspaceAdapter) Close() error {
	if adapter == nil {
		return nil
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed {
		return nil
	}
	for _, staged := range adapter.staged {
		if staged.artifacts != nil && adapter.validArtifactCapability(staged.artifacts) {
			_ = adapter.removeReservedArtifacts(staged.artifacts,
				staged.artifacts.storageReservation.(*sqlcipherArtifactReservation), true)
			staged.artifacts.artifactAuthority = nil
			staged.artifacts.storageReservation = nil
			staged.artifacts = nil
		}
		staged.stageAuthority = nil
	}
	adapter.staged = nil
	zeroBytes(adapter.key)
	adapter.key = nil
	adapter.closed = true
	err := adapter.root.Close()
	adapter.root = nil
	if err != nil {
		return ErrSQLCipherWorkspace
	}
	return nil
}

func verifyStagedSQLCipherDatabase(ctx context.Context, database *sqlcipher.Database, expectedVersion uint64, expectedHash []byte) error {
	if database == nil || database.DB == nil || database.IntegrityCheck(ctx) != nil {
		return ErrSQLCipherWorkspace
	}
	foreignKeys, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return ErrSQLCipherWorkspace
	}
	violation := foreignKeys.Next()
	rowsErr := foreignKeys.Err()
	closeErr := foreignKeys.Close()
	if violation || rowsErr != nil || closeErr != nil {
		return ErrSQLCipherWorkspace
	}
	version, hash, err := stagedSchemaMetadata(ctx, database)
	if err != nil || version != expectedVersion || subtle.ConstantTimeCompare(hash, expectedHash) != 1 {
		return ErrSQLCipherWorkspace
	}
	return nil
}

func stagedSchemaMetadata(ctx context.Context, database *sqlcipher.Database) (uint64, []byte, error) {
	rows, err := database.QueryContext(ctx, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		return 0, nil, ErrSQLCipherWorkspace
	}
	defer rows.Close()
	digest := sha256.New()
	var count uint64
	for rows.Next() {
		var version uint64
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil || version != count+1 || name == "" || len(checksum) != 64 {
			return 0, nil, ErrSQLCipherWorkspace
		}
		for _, value := range []string{strconv.FormatUint(version, 10), name, checksum} {
			_, _ = io.WriteString(digest, value)
			_, _ = digest.Write([]byte{0})
		}
		count++
	}
	if rows.Err() != nil || count == 0 {
		return 0, nil, ErrSQLCipherWorkspace
	}
	return count, digest.Sum(nil), nil
}

func verifyArchivedAuditState(ctx context.Context, database *sqlcipher.Database, manifest *tammyv1.BackupArchiveManifest) error {
	if manifest == nil || manifest.AuditGeneration == 0 || len(manifest.AuditHead) != sha256.Size {
		return ErrSQLCipherWorkspace
	}
	header, err := audit.LoadChainHeader(ctx, database, manifest.WorkspaceId, manifest.AuditGeneration)
	if err != nil || header.Generation != manifest.AuditGeneration || header.CurrentSequence != manifest.AuditSequence ||
		subtle.ConstantTimeCompare(header.CurrentHead[:], manifest.AuditHead) != 1 {
		return ErrSQLCipherWorkspace
	}
	if header.CurrentSequence == 0 {
		if header.CurrentHead != header.GenesisHash {
			return ErrSQLCipherWorkspace
		}
		return nil
	}
	verifier, err := audit.NewStreamingStoredChainVerifier(ctx, header)
	if err != nil {
		return ErrSQLCipherWorkspace
	}
	closed := false
	defer func() {
		if !closed {
			_ = verifier.Close()
		}
	}()
	snapshot := audit.StoredEventSnapshot{WorkspaceID: manifest.WorkspaceId, Generation: manifest.AuditGeneration,
		EndSequence: header.CurrentSequence, EndHead: header.CurrentHead}
	checkpoint := audit.StoredEventCheckpoint{Head: header.GenesisHash}
	for checkpoint.AfterSequence < snapshot.EndSequence {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrSQLCipherWorkspace, err)
		}
		page, err := audit.LoadStoredEventPage(ctx, database, snapshot, checkpoint,
			audit.StoredEventPageSizeLimit, audit.StoredEventPageByteBudget)
		if err != nil || len(page.Events) == 0 || page.Checkpoint.AfterSequence <= checkpoint.AfterSequence ||
			verifier.AcceptPage(page.Events) != nil {
			return ErrSQLCipherWorkspace
		}
		checkpoint = page.Checkpoint
		if !page.HasMore && checkpoint.AfterSequence != snapshot.EndSequence {
			return ErrSQLCipherWorkspace
		}
	}
	verification := verifier.Finish()
	if verifier.TerminalError() != nil || verification == nil ||
		verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		verification.VerifiedThroughSequence != header.CurrentSequence ||
		subtle.ConstantTimeCompare(verification.VerifiedHead, manifest.AuditHead) != 1 || verifier.Close() != nil {
		return ErrSQLCipherWorkspace
	}
	closed = true
	return nil
}

func verifyFinalizedAuditState(ctx context.Context, database *sqlcipher.Database, request StagedVerificationRequest) error {
	finalized := request.Finalized
	header, err := audit.LoadChainHeader(ctx, database, request.WorkspaceID, finalized.Generation)
	if err != nil || header.Generation != finalized.Generation || header.CurrentSequence != 1 ||
		subtle.ConstantTimeCompare(header.CurrentHead[:], finalized.AuditHead) != 1 {
		return ErrSQLCipherWorkspace
	}
	events, err := audit.LoadStoredEvents(ctx, database, request.WorkspaceID, finalized.Generation, 1, 1)
	if err != nil || len(events) != 1 || events[0].Event == nil ||
		events[0].Event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED {
		return ErrSQLCipherWorkspace
	}
	verification := audit.VerifyStoredChain(header, events)
	if verification == nil || verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		subtle.ConstantTimeCompare(verification.VerifiedHead, finalized.AuditHead) != 1 {
		return ErrSQLCipherWorkspace
	}
	payload := events[0].Event.GetPayload().GetWorkspaceRestored()
	if payload == nil || payload.WorkspaceId != request.WorkspaceID || payload.OperationId != finalized.OperationID ||
		payload.PredecessorGeneration != finalized.PredecessorGeneration || payload.BackupGeneration != finalized.BackupGeneration ||
		payload.RestoredGeneration != finalized.Generation || payload.PreRestoreArchiveId != finalized.PreRestoreArchiveID ||
		subtle.ConstantTimeCompare(payload.BackupManifestHash, finalized.BackupManifestHash) != 1 ||
		subtle.ConstantTimeCompare(payload.PreRestoreArchiveHash, finalized.PreRestoreArchiveHash) != 1 ||
		subtle.ConstantTimeCompare(payload.PredecessorHead, finalized.PredecessorHead) != 1 ||
		subtle.ConstantTimeCompare(payload.ArchivedHead, finalized.BackupHead) != 1 {
		return ErrSQLCipherWorkspace
	}
	history, err := audit.LoadSigningKeyHistory(ctx, database, request.WorkspaceID)
	valid := err == nil && validRestoredSigningHistory(history, request.WorkspaceID, finalized.BackupGeneration,
		finalized.SigningKeyID, finalized.SigningKeyEpoch, finalized.AuditRoot)
	zeroAuditSigningHistory(history)
	if !valid {
		return ErrSQLCipherWorkspace
	}
	return nil
}

func validRestoredSigningHistory(history []audit.SigningKeyRecord, workspaceID string, backupGeneration uint64,
	signingKeyID string, signingKeyEpoch uint64, expectedRoot []byte) bool {
	if len(history) == 0 || history[0].WorkspaceID != workspaceID || history[0].Epoch != 1 ||
		history[len(history)-1].RetiredAt != nil || history[len(history)-1].KeyID != signingKeyID ||
		history[len(history)-1].Epoch != signingKeyEpoch || len(expectedRoot) != sha256.Size {
		return false
	}
	for index := range history {
		if history[index].WorkspaceID != workspaceID || history[index].Epoch != uint64(index+1) ||
			history[index].Generation == 0 || history[index].Generation > backupGeneration ||
			(index == 0 && history[index].PreviousKeyID != "") || (index > 0 && history[index].PreviousKeyID != history[index-1].KeyID) {
			return false
		}
	}
	root, err := audit.SigningLineageRootFingerprint(workspaceID, history[0].KeyID, history[0].PublicKey)
	return err == nil && subtle.ConstantTimeCompare(root[:], expectedRoot) == 1
}

func zeroAuditSigningHistory(history []audit.SigningKeyRecord) {
	for index := range history {
		audit.Zero(history[index].EncryptedPrivateKey)
		audit.Zero(history[index].Nonce)
		audit.Zero(history[index].PreviousSignature)
		audit.Zero(history[index].PossessionSignature)
		audit.Zero(history[index].RotationPriorHead)
	}
}

func (adapter *SQLCipherWorkspaceAdapter) removeOwnedLock(name string) error {
	info, err := adapter.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != 0 {
		return ErrSQLCipherWorkspace
	}
	if err := adapter.root.Remove(name); err != nil || syncRestoreRoot(adapter.root) != nil {
		return ErrSQLCipherWorkspace
	}
	return nil
}

func (adapter *SQLCipherWorkspaceAdapter) removeStageFiles(databaseName string) error {
	for _, name := range []string{databaseName + "-wal", databaseName + "-shm", databaseName + ".lock", databaseName} {
		if err := adapter.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrSQLCipherWorkspace
		}
	}
	if err := syncRestoreRoot(adapter.root); err != nil {
		return ErrSQLCipherWorkspace
	}
	return nil
}

func sameRestoreDirectory(path string, expected os.FileInfo) bool {
	current, err := os.Lstat(path)
	return err == nil && expected != nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, expected)
}

func hashVerifiedStagedDatabase(
	ctx context.Context,
	root *os.Root,
	name string,
	expected os.FileInfo,
) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if ctx == nil || ctx.Err() != nil || root == nil || filepath.Base(name) != name ||
		!validStagedDatabaseIdentity(expected) {
		return result, ErrSQLCipherWorkspace
	}
	current, err := root.Lstat(name)
	if err != nil || !validStagedDatabaseIdentity(current) || !os.SameFile(current, expected) {
		return result, ErrSQLCipherWorkspace
	}
	file, err := root.Open(name)
	if err != nil {
		return result, ErrSQLCipherWorkspace
	}
	digest := sha256.New()
	buffer := make([]byte, 32*1024)
	defer zeroBytes(buffer)
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return result, errors.Join(ErrSQLCipherWorkspace, err)
		}
		read, readErr := file.Read(buffer)
		if read > 0 {
			_, _ = digest.Write(buffer[:read])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = file.Close()
			return result, ErrSQLCipherWorkspace
		}
	}
	opened, statErr := file.Stat()
	closeErr := file.Close()
	after, pathErr := root.Lstat(name)
	if statErr != nil || closeErr != nil || pathErr != nil || !validStagedDatabaseIdentity(opened) ||
		!validStagedDatabaseIdentity(after) || !os.SameFile(expected, opened) || !os.SameFile(expected, after) ||
		opened.Size() != expected.Size() || after.Size() != expected.Size() {
		return result, ErrSQLCipherWorkspace
	}
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func validStagedDatabaseIdentity(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 && info.Mode()&os.ModeSymlink == 0 &&
		info.Size() > 0 && info.Size() <= maximumStagedDatabaseBytes
}

func syncRestoreRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
