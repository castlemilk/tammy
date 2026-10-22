package restore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"regexp"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

var ErrRestore = errors.New("restore: restore failed")

var totpPattern = regexp.MustCompile(`^[0-9]{6}$`)

// RestoreProof is a closed, mutually exclusive proof family. Concrete proof
// values are copied before any verifier sees them and secret copies are wiped.
type RestoreProof interface{ restoreProof() }

type AdminTOTPProof struct {
	AdminUserID string
	Password    []byte
	TOTP        string
	IssuedAt    time.Time
	ReplayKey   string
}

func (*AdminTOTPProof) restoreProof() {}

type RecoveryProof struct {
	RecoverySecret []byte
	IssuedAt       time.Time
	ReplayKey      string
}

func (*RecoveryProof) restoreProof() {}

type RestoreAuthorization struct {
	AuthorizationID   string
	WorkspaceID       string
	CurrentGeneration uint64
	CurrentAuditHead  []byte
}

type ProofVerifier interface {
	AuthorizeRestore(context.Context, string, RestoreProof) (*RestoreAuthorization, error)
}

// TrustResolver supplies an authenticated signing lineage from the active
// workspace. Archive-contained key material is never a trust anchor.
type TrustResolver interface {
	ResolveRestoreTrust(context.Context, string) (backup.TrustAnchor, error)
}

type RestoreJournal interface {
	Prepare(context.Context, string, []byte) (*tammyv1.RestoreStatus, error)
	BindPreparedRecovery(context.Context, string, []byte, *tammyv1.RestoreRecoveryRecord) (*tammyv1.RestoreStatus, *PreparedArchiveBinding, error)
	BindStagedRecovery(context.Context, string, *tammyv1.RestoreRecoveryRecord) (*tammyv1.RestoreStatus, error)
	CheckpointRecovery(context.Context, string, tammyv1.RestoreState, *tammyv1.RestoreRecoveryRecord) (*tammyv1.RestoreStatus, error)
	Advance(context.Context, string, tammyv1.RestoreState, tammyv1.RestoreState, []byte) (*tammyv1.RestoreStatus, error)
}

type PreRestoreArchiveRequest struct {
	OperationID   string
	WorkspaceID   string
	Authorization *RestoreAuthorization
	Manifest      *tammyv1.BackupArchiveManifest
	ManifestHash  []byte
	Objects       []backup.Object
}

type PreRestoreArchive struct {
	OperationID         string
	ArchiveID           string
	Version             uint64
	SHA256              []byte
	CreatedAt           time.Time
	DeletionEligibleAt  time.Time
	SourceGeneration    uint64
	EncryptedByteLength uint64
	archiveAuthority    any
	storageName         string
}

type PreRestoreArchiver interface {
	PrepareVerifiedPreRestoreArchive(context.Context, PreRestoreArchiveRequest) (*PreRestoreArchive, error)
	PublishPreRestoreArchive(context.Context, *PreRestoreArchive, *PreparedArchiveBinding) error
	AbortPreRestoreArchive(context.Context, *PreRestoreArchive) error
}

// StageRequest contains owned, authenticated archive projections. A Stager
// may create only isolated staged state; activation is a later restore phase.
type StageRequest struct {
	OperationID  string
	WorkspaceID  string
	Manifest     *tammyv1.BackupArchiveManifest
	ManifestHash []byte
	Objects      []backup.Object
	Artifacts    *RestoreArtifactReservation
}

// RestoreArtifactReservation is an immutable adapter-issued capability for one
// exclusively reserved, authenticated stage/rollback artifact set. Accessors
// return values or copies so callers cannot alter the journal binding.
type RestoreArtifactReservation struct {
	operationID        string
	workspaceID        string
	stageBasename      string
	rollbackBasename   string
	ownershipDigest    [sha256.Size]byte
	stageMarkerHash    [sha256.Size]byte
	rollbackMarkerHash [sha256.Size]byte
	artifactAuthority  any
	storageReservation any
}

func (reservation *RestoreArtifactReservation) StageBasename() string {
	if reservation == nil {
		return ""
	}
	return reservation.stageBasename
}

func (reservation *RestoreArtifactReservation) RollbackBasename() string {
	if reservation == nil {
		return ""
	}
	return reservation.rollbackBasename
}

func (reservation *RestoreArtifactReservation) OwnershipDigest() []byte {
	if reservation == nil {
		return nil
	}
	return append([]byte(nil), reservation.ownershipDigest[:]...)
}

func (reservation *RestoreArtifactReservation) StageOwnerMarkerSHA256() []byte {
	if reservation == nil {
		return nil
	}
	return append([]byte(nil), reservation.stageMarkerHash[:]...)
}

func (reservation *RestoreArtifactReservation) RollbackOwnerMarkerSHA256() []byte {
	if reservation == nil {
		return nil
	}
	return append([]byte(nil), reservation.rollbackMarkerHash[:]...)
}

type StagedWorkspace struct {
	Handle         string
	stageAuthority any
	stagedPath     string
	artifacts      *RestoreArtifactReservation
}

type Stager interface {
	ReserveRestoreArtifacts(context.Context, string, string) (*RestoreArtifactReservation, error)
	ReleaseRestoreArtifacts(context.Context, *RestoreArtifactReservation) error
	Stage(context.Context, StageRequest) (*StagedWorkspace, error)
	DiscardStaged(context.Context, *StagedWorkspace) error
}

type FinalizeStagedRestoreRequest struct {
	OperationID       string
	WorkspaceID       string
	NewGeneration     uint64
	Manifest          *tammyv1.BackupArchiveManifest
	ManifestHash      []byte
	Authorization     *RestoreAuthorization
	PreRestoreArchive *PreRestoreArchive
	Staged            *StagedWorkspace
}

type FinalizedRestore struct {
	OperationID           string
	WorkspaceID           string
	Generation            uint64
	AuditHead             []byte
	EventType             tammyv1.AuditEventType
	BackupManifestHash    []byte
	PreRestoreArchiveID   string
	PreRestoreArchiveHash []byte
	PredecessorGeneration uint64
	PredecessorHead       []byte
	BackupGeneration      uint64
	BackupHead            []byte
	SigningKeyID          string
	SigningKeyEpoch       uint64
	AuditRoot             []byte
	SchemaVersion         uint64
	MigrationManifestHash []byte
	Staged                *StagedWorkspace
}

type StagedFinalizer interface {
	// FinalizeStagedWorkspace performs generation creation, exact restore-event
	// append, and restored database session invalidation only in staged SQL.
	FinalizeStagedWorkspace(context.Context, FinalizeStagedRestoreRequest) (*FinalizedRestore, error)
}

type StagedVerificationRequest struct {
	OperationID   string
	WorkspaceID   string
	Manifest      *tammyv1.BackupArchiveManifest
	ManifestHash  []byte
	Authorization *RestoreAuthorization
	Finalized     *FinalizedRestore
}

// VerifiedStagedWorkspace is an opaque verifier-issued proof binding the exact
// staged file identity and bytes that passed semantic verification.
type VerifiedStagedWorkspace struct {
	Finalized             *FinalizedRestore
	verificationAuthority any
	stagedIdentity        any
	stagedSHA256          [sha256.Size]byte
}

type StagedVerifier interface {
	VerifyStaged(context.Context, StagedVerificationRequest) (*VerifiedStagedWorkspace, error)
}

type SwapRequest struct {
	OperationID       string
	Verified          *VerifiedStagedWorkspace
	PreRestoreArchive *PreRestoreArchive
	Reservation       *RestoreSwapReservation
}

// RestoreSwapReservation is an immutable, single-use capability holding the
// exclusive active-workspace lock and its streamed predecessor byte hash.
type RestoreSwapReservation struct {
	operationID        string
	workspaceID        string
	predecessorHash    [sha256.Size]byte
	activatedHash      [sha256.Size]byte
	swapAuthority      any
	storageReservation any
}

func (reservation *RestoreSwapReservation) PredecessorHash() []byte {
	if reservation == nil {
		return nil
	}
	return append([]byte(nil), reservation.predecessorHash[:]...)
}

func (reservation *RestoreSwapReservation) ActivatedHash() []byte {
	if reservation == nil {
		return nil
	}
	return append([]byte(nil), reservation.activatedHash[:]...)
}

type SwapReceipt struct {
	ReceiptID      string
	swapAuthority  any
	storageReceipt any
}

type WorkspaceSwapper interface {
	ReserveSwap(context.Context, string, string, *VerifiedStagedWorkspace) (*RestoreSwapReservation, error)
	ReleaseSwap(context.Context, *RestoreSwapReservation) error
	Swap(context.Context, SwapRequest) (*SwapReceipt, error)
	Rollback(context.Context, *SwapReceipt) error
	CommitSwap(context.Context, *SwapReceipt) error
}

type ActivatedVerificationRequest struct {
	OperationID string
	Verified    *VerifiedStagedWorkspace
	Receipt     *SwapReceipt
}

type PostSwapVerifier interface {
	VerifyActivated(context.Context, ActivatedVerificationRequest) error
}

type MachineCredentialRevocationRequest struct {
	OperationID string
	WorkspaceID string
	Receipt     *SwapReceipt
}

type MachineCredentialRevoker interface {
	// RevokeMachineCredentials deletes external remembered-key and session
	// capabilities only. It must never mutate the active workspace database.
	RevokeMachineCredentials(context.Context, MachineCredentialRevocationRequest) error
}

type RestoreMirrorPublisher interface {
	PublishRestoredMirror(context.Context, *FinalizedRestore) error
}

type ServiceConfig struct {
	Proofs             ProofVerifier
	Trust              TrustResolver
	Providers          *ProviderRegistry
	Journal            RestoreJournal
	PreRestoreArchives PreRestoreArchiver
	Stager             Stager
	StagedFinalizer    StagedFinalizer
	StagedVerifier     StagedVerifier
	Swapper            WorkspaceSwapper
	PostSwapVerifier   PostSwapVerifier
	MachineCredentials MachineCredentialRevoker
	Mirror             RestoreMirrorPublisher
}

type Service struct{ config ServiceConfig }

type RestoreRequest struct {
	OperationID string
	WorkspaceID string
	Archive     []byte
	Passphrase  []byte
	Proof       RestoreProof
}

type RestoreResult struct {
	ManifestHash        []byte
	Generation          uint64
	AuditHead           []byte
	PreRestoreArchiveID string
}

func NewService(config ServiceConfig) (*Service, error) {
	if nilInterface(config.Proofs) || nilInterface(config.Trust) || config.Providers == nil ||
		nilInterface(config.Journal) || nilInterface(config.PreRestoreArchives) || nilInterface(config.Stager) ||
		nilInterface(config.StagedFinalizer) || nilInterface(config.StagedVerifier) || nilInterface(config.Swapper) ||
		nilInterface(config.PostSwapVerifier) || nilInterface(config.MachineCredentials) || nilInterface(config.Mirror) {
		return nil, ErrRestore
	}
	return &Service{config: config}, nil
}

// Restore completes every staged database mutation before atomic activation.
// Once SWAPPED is durable, only read-only verification and external capability
// publication remain; restart recovery can safely finish those boundaries.
func (service *Service) Restore(ctx context.Context, request RestoreRequest) (*RestoreResult, error) {
	if service == nil || ctx == nil || !ids.IsCanonicalV7(request.OperationID) ||
		!ids.IsCanonicalV7(request.WorkspaceID) || len(request.Archive) == 0 || len(request.Passphrase) == 0 {
		return nil, ErrRestore
	}
	proof, ok := cloneRestoreProof(request.Proof)
	if !ok {
		return nil, ErrRestore
	}
	defer zeroRestoreProof(proof)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	authorization, err := service.config.Proofs.AuthorizeRestore(ctx, request.WorkspaceID, proof)
	if err != nil || !validAuthorization(authorization, request.WorkspaceID) {
		return nil, errors.Join(ErrRestore, err)
	}
	trust, err := service.config.Trust.ResolveRestoreTrust(ctx, request.WorkspaceID)
	if err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	passphrase := append([]byte(nil), request.Passphrase...)
	defer zeroBytes(passphrase)
	opened, err := backup.Open(request.Archive, passphrase, trust)
	if err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	defer zeroObjects(opened.Objects)
	if opened.Manifest == nil || opened.Manifest.WorkspaceId != request.WorkspaceID {
		return nil, ErrRestore
	}
	if err := service.config.Providers.Validate(ctx, opened.Objects); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	prepared, err := service.config.Journal.Prepare(ctx, request.OperationID, opened.ManifestHash)
	if err != nil || prepared == nil || prepared.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED ||
		subtle.ConstantTimeCompare(prepared.BackupManifestHash, opened.ManifestHash) != 1 {
		return nil, errors.Join(ErrRestore, err)
	}

	preRequest := PreRestoreArchiveRequest{OperationID: request.OperationID, WorkspaceID: request.WorkspaceID,
		Authorization: authorization, Manifest: proto.Clone(opened.Manifest).(*tammyv1.BackupArchiveManifest),
		ManifestHash: append([]byte(nil), opened.ManifestHash...), Objects: cloneObjects(opened.Objects)}
	defer zeroObjects(preRequest.Objects)
	preArchive, err := service.config.PreRestoreArchives.PrepareVerifiedPreRestoreArchive(ctx, preRequest)
	if err != nil || !validPreRestoreArchive(preArchive) {
		return nil, errors.Join(ErrRestore, err)
	}
	retainPreArchive := false
	defer func() {
		if !retainPreArchive {
			_ = service.config.PreRestoreArchives.AbortPreRestoreArchive(context.WithoutCancel(ctx), preArchive)
		}
	}()
	artifacts, err := service.config.Stager.ReserveRestoreArtifacts(ctx, request.OperationID, request.WorkspaceID)
	if err != nil || artifacts == nil {
		return nil, errors.Join(ErrRestore, err)
	}
	reservationOwned := true
	defer func() {
		if reservationOwned {
			_ = service.config.Stager.ReleaseRestoreArtifacts(context.WithoutCancel(ctx), artifacts)
		}
	}()
	recoveryRecord := &tammyv1.RestoreRecoveryRecord{WorkspaceId: request.WorkspaceID,
		PreRestoreArchiveId: preArchive.ArchiveID, PreRestoreArchiveHash: append([]byte(nil), preArchive.SHA256...),
		StageBasename:                     artifacts.StageBasename(),
		RollbackBasename:                  artifacts.RollbackBasename(),
		PreRestoreArchivePreparedBasename: preArchive.storageName,
		PreRestoreArchiveFinalBasename:    preRestoreArchiveName(preArchive.ArchiveID),
		ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
		StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
		RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256()}
	prepared, publication, err := service.config.Journal.BindPreparedRecovery(ctx, request.OperationID, opened.ManifestHash, recoveryRecord)
	if err != nil || prepared == nil || prepared.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED ||
		prepared.Recovery == nil || !proto.Equal(prepared.Recovery, recoveryRecord) || publication == nil {
		return nil, errors.Join(ErrRestore, err)
	}
	if err := service.config.PreRestoreArchives.PublishPreRestoreArchive(ctx, preArchive, publication); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}

	stageRequest := StageRequest{OperationID: request.OperationID, WorkspaceID: request.WorkspaceID,
		Manifest:     proto.Clone(opened.Manifest).(*tammyv1.BackupArchiveManifest),
		ManifestHash: append([]byte(nil), opened.ManifestHash...), Objects: cloneObjects(opened.Objects), Artifacts: artifacts}
	defer zeroObjects(stageRequest.Objects)
	staged, err := service.config.Stager.Stage(ctx, stageRequest)
	if err != nil || staged == nil || staged.Handle == "" {
		return nil, errors.Join(ErrRestore, err)
	}
	reservationOwned = false
	stageOwned := true
	defer func() {
		if stageOwned {
			_ = service.config.Stager.DiscardStaged(context.WithoutCancel(ctx), staged)
		}
	}()
	if authorization.CurrentGeneration == ^uint64(0) {
		return nil, ErrRestore
	}
	finalized, err := service.config.StagedFinalizer.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{
		OperationID: request.OperationID, WorkspaceID: request.WorkspaceID, NewGeneration: authorization.CurrentGeneration + 1,
		Manifest: opened.Manifest, ManifestHash: opened.ManifestHash, Authorization: authorization,
		PreRestoreArchive: preArchive, Staged: staged})
	if err != nil || !validFinalizedRestore(finalized, request.WorkspaceID, authorization.CurrentGeneration+1, staged) {
		return nil, errors.Join(ErrRestore, err)
	}
	verified, err := service.config.StagedVerifier.VerifyStaged(ctx, StagedVerificationRequest{OperationID: request.OperationID,
		WorkspaceID: request.WorkspaceID, Manifest: opened.Manifest, ManifestHash: opened.ManifestHash,
		Authorization: authorization, Finalized: finalized})
	if err != nil || verified == nil || verified.Finalized != finalized {
		return nil, errors.Join(ErrRestore, err)
	}
	swapReservation, err := service.config.Swapper.ReserveSwap(ctx, request.OperationID, request.WorkspaceID, verified)
	if err != nil || swapReservation == nil || len(swapReservation.PredecessorHash()) != sha256.Size ||
		len(swapReservation.ActivatedHash()) != sha256.Size {
		return nil, errors.Join(ErrRestore, err)
	}
	swapReservationOwned := true
	defer func() {
		if swapReservationOwned {
			_ = service.config.Swapper.ReleaseSwap(context.WithoutCancel(ctx), swapReservation)
		}
	}()
	finalizedGeneration := finalized.Generation
	schemaVersion := finalized.SchemaVersion
	recoveryRecord.FinalizedGeneration = &finalizedGeneration
	recoveryRecord.FinalizedAuditHead = append([]byte(nil), finalized.AuditHead...)
	recoveryRecord.SchemaVersion = &schemaVersion
	recoveryRecord.MigrationManifestHash = append([]byte(nil), finalized.MigrationManifestHash...)
	recoveryRecord.RollbackPredecessorHash = swapReservation.PredecessorHash()
	recoveryRecord.ActivatedDatabaseSha256 = swapReservation.ActivatedHash()
	stagedStatus, err := service.config.Journal.BindStagedRecovery(ctx, request.OperationID, recoveryRecord)
	if err != nil || !statusBinds(stagedStatus, tammyv1.RestoreState_RESTORE_STATE_STAGED, finalized.AuditHead) {
		return nil, errors.Join(ErrRestore, err)
	}
	receipt, err := service.config.Swapper.Swap(ctx, SwapRequest{OperationID: request.OperationID,
		Verified: verified, PreRestoreArchive: preArchive, Reservation: swapReservation})
	if err != nil || receipt == nil || !ids.IsCanonicalV7(receipt.ReceiptID) {
		return nil, errors.Join(ErrRestore, err)
	}
	stageOwned = false
	swapReservationOwned = false
	swappedStatus, err := service.config.Journal.Advance(ctx, request.OperationID,
		tammyv1.RestoreState_RESTORE_STATE_STAGED, tammyv1.RestoreState_RESTORE_STATE_SWAPPED, finalized.AuditHead)
	if err != nil || !statusBinds(swappedStatus, tammyv1.RestoreState_RESTORE_STATE_SWAPPED, finalized.AuditHead) {
		_ = service.config.Swapper.Rollback(context.WithoutCancel(ctx), receipt)
		return nil, errors.Join(ErrRestore, err)
	}
	retainPreArchive = true
	if err := service.config.PostSwapVerifier.VerifyActivated(ctx, ActivatedVerificationRequest{
		OperationID: request.OperationID, Verified: verified, Receipt: receipt}); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	recoveryRecord.PostSwapVerified = true
	if _, err := service.config.Journal.CheckpointRecovery(ctx, request.OperationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recoveryRecord); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	if err := service.config.MachineCredentials.RevokeMachineCredentials(ctx, MachineCredentialRevocationRequest{
		OperationID: request.OperationID, WorkspaceID: request.WorkspaceID, Receipt: receipt}); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	recoveryRecord.MachineCredentialsRevoked = true
	if _, err := service.config.Journal.CheckpointRecovery(ctx, request.OperationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recoveryRecord); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	if err := service.config.Mirror.PublishRestoredMirror(ctx, finalized); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	recoveryRecord.MirrorPublished = true
	if _, err := service.config.Journal.CheckpointRecovery(ctx, request.OperationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recoveryRecord); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	completed, err := service.config.Journal.Advance(ctx, request.OperationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, tammyv1.RestoreState_RESTORE_STATE_COMPLETE, finalized.AuditHead)
	if err != nil || !statusBinds(completed, tammyv1.RestoreState_RESTORE_STATE_COMPLETE, finalized.AuditHead) {
		return nil, errors.Join(ErrRestore, err)
	}
	if err := service.config.Swapper.CommitSwap(ctx, receipt); err != nil {
		return nil, errors.Join(ErrRestore, err)
	}
	return &RestoreResult{ManifestHash: append([]byte(nil), opened.ManifestHash...), Generation: finalized.Generation,
		AuditHead: append([]byte(nil), finalized.AuditHead...), PreRestoreArchiveID: preArchive.ArchiveID}, nil
}

func cloneRestoreProof(proof RestoreProof) (RestoreProof, bool) {
	switch value := proof.(type) {
	case *AdminTOTPProof:
		if value == nil || !ids.IsCanonicalV7(value.AdminUserID) || !ids.IsCanonicalV7(value.ReplayKey) ||
			len(value.Password) == 0 || len(value.Password) > 4096 || !totpPattern.MatchString(value.TOTP) || value.IssuedAt.IsZero() {
			return nil, false
		}
		return &AdminTOTPProof{AdminUserID: value.AdminUserID, Password: append([]byte(nil), value.Password...),
			TOTP: value.TOTP, IssuedAt: value.IssuedAt, ReplayKey: value.ReplayKey}, true
	case *RecoveryProof:
		if value == nil || !ids.IsCanonicalV7(value.ReplayKey) || len(value.RecoverySecret) == 0 ||
			len(value.RecoverySecret) > 4096 || value.IssuedAt.IsZero() {
			return nil, false
		}
		return &RecoveryProof{RecoverySecret: append([]byte(nil), value.RecoverySecret...), IssuedAt: value.IssuedAt,
			ReplayKey: value.ReplayKey}, true
	default:
		return nil, false
	}
}

func zeroRestoreProof(proof RestoreProof) {
	switch value := proof.(type) {
	case *AdminTOTPProof:
		zeroBytes(value.Password)
	case *RecoveryProof:
		zeroBytes(value.RecoverySecret)
	}
}

func validAuthorization(authorization *RestoreAuthorization, workspaceID string) bool {
	return authorization != nil && ids.IsCanonicalV7(authorization.AuthorizationID) &&
		authorization.WorkspaceID == workspaceID && authorization.CurrentGeneration > 0 &&
		len(authorization.CurrentAuditHead) == sha256.Size
}

func validPreRestoreArchive(archive *PreRestoreArchive) bool {
	return archive != nil && ids.IsCanonicalV7(archive.ArchiveID) && len(archive.SHA256) == sha256.Size
}

func validFinalizedRestore(finalized *FinalizedRestore, workspaceID string, generation uint64, staged *StagedWorkspace) bool {
	return finalized != nil && finalized.WorkspaceID == workspaceID && finalized.Generation == generation &&
		len(finalized.AuditHead) == sha256.Size && finalized.EventType == tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED &&
		finalized.Staged == staged
}

func statusBinds(status *tammyv1.RestoreStatus, state tammyv1.RestoreState, head []byte) bool {
	return status != nil && status.State == state && len(head) == sha256.Size &&
		subtle.ConstantTimeCompare(status.NewAuditHead, head) == 1
}

func cloneObjects(objects []backup.Object) []backup.Object {
	cloned := make([]backup.Object, len(objects))
	for index, object := range objects {
		cloned[index] = object
		cloned[index].Bytes = append([]byte(nil), object.Bytes...)
	}
	return cloned
}

func zeroObjects(objects []backup.Object) {
	for index := range objects {
		zeroBytes(objects[index].Bytes)
	}
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
