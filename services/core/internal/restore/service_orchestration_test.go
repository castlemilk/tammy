package restore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

var errInjectedRestoreBoundary = errors.New("injected restore boundary failure")

type restoreOrchestrationHarness struct {
	order          []string
	trust          backup.TrustAnchor
	active         string
	journalState   tammyv1.RestoreState
	stagerOverride Stager
	failAt         string
	stagedBytes    string
	databaseWrites int
	writesAtSwap   int
	recovery       *tammyv1.RestoreRecoveryRecord
}

func (harness *restoreOrchestrationHarness) mark(boundary string) {
	harness.order = append(harness.order, boundary)
}

func (harness *restoreOrchestrationHarness) boundary(boundary string) error {
	harness.mark(boundary)
	if harness.failAt == boundary {
		return errInjectedRestoreBoundary
	}
	return nil
}

func (harness *restoreOrchestrationHarness) AuthorizeRestore(_ context.Context, workspaceID string, _ RestoreProof) (*RestoreAuthorization, error) {
	if err := harness.boundary("proof"); err != nil {
		return nil, err
	}
	return &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077", WorkspaceID: workspaceID,
		CurrentGeneration: 4, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)}, nil
}

func (harness *restoreOrchestrationHarness) ResolveRestoreTrust(_ context.Context, _ string) (backup.TrustAnchor, error) {
	if err := harness.boundary("trust"); err != nil {
		return backup.TrustAnchor{}, err
	}
	return harness.trust, nil
}

func (harness *restoreOrchestrationHarness) Prepare(_ context.Context, operationID string, manifestHash []byte) (*tammyv1.RestoreStatus, error) {
	if err := harness.boundary("journal_prepared"); err != nil {
		return nil, err
	}
	harness.journalState = tammyv1.RestoreState_RESTORE_STATE_PREPARED
	return &tammyv1.RestoreStatus{OperationId: operationID, State: harness.journalState, BackupManifestHash: append([]byte(nil), manifestHash...)}, nil
}

func (harness *restoreOrchestrationHarness) BindPreparedRecovery(_ context.Context, operationID string, manifestHash []byte,
	recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, *PreparedArchiveBinding, error) {
	if err := harness.boundary("journal_prepared_recovery"); err != nil {
		return nil, nil, err
	}
	return &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			BackupManifestHash: append([]byte(nil), manifestHash...), Recovery: proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)},
		&PreparedArchiveBinding{}, nil
}

func (harness *restoreOrchestrationHarness) BindStagedRecovery(_ context.Context, operationID string,
	recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, error) {
	if err := harness.boundary("journal_staged"); err != nil {
		return nil, err
	}
	harness.journalState = tammyv1.RestoreState_RESTORE_STATE_STAGED
	harness.recovery = proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	return &tammyv1.RestoreStatus{OperationId: operationID, State: harness.journalState,
		NewAuditHead: append([]byte(nil), recovery.FinalizedAuditHead...),
		Recovery:     proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)}, nil
}

func (harness *restoreOrchestrationHarness) CheckpointRecovery(_ context.Context, operationID string,
	state tammyv1.RestoreState, recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, error) {
	if harness.journalState != state || state != tammyv1.RestoreState_RESTORE_STATE_SWAPPED {
		return nil, ErrJournal
	}
	boundary := ""
	switch {
	case recovery.PostSwapVerified && !recovery.MachineCredentialsRevoked && !recovery.MirrorPublished:
		boundary = "journal_postverified"
	case recovery.PostSwapVerified && recovery.MachineCredentialsRevoked && !recovery.MirrorPublished:
		boundary = "journal_revoked"
	case recovery.PostSwapVerified && recovery.MachineCredentialsRevoked && recovery.MirrorPublished:
		boundary = "journal_mirrored"
	default:
		return nil, ErrJournal
	}
	if err := harness.boundary(boundary); err != nil {
		return nil, err
	}
	harness.recovery = proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	return &tammyv1.RestoreStatus{OperationId: operationID, State: state,
		NewAuditHead: append([]byte(nil), recovery.FinalizedAuditHead...), Recovery: harness.recovery}, nil
}

func (harness *restoreOrchestrationHarness) Advance(_ context.Context, operationID string, from, to tammyv1.RestoreState, head []byte) (*tammyv1.RestoreStatus, error) {
	if harness.journalState != from {
		return nil, ErrJournal
	}
	switch to {
	case tammyv1.RestoreState_RESTORE_STATE_SWAPPED:
		if err := harness.boundary("journal_swapped"); err != nil {
			return nil, err
		}
	case tammyv1.RestoreState_RESTORE_STATE_COMPLETE:
		if err := harness.boundary("journal_complete"); err != nil {
			return nil, err
		}
	}
	harness.journalState = to
	return &tammyv1.RestoreStatus{OperationId: operationID, State: to, NewAuditHead: append([]byte(nil), head...)}, nil
}

func (harness *restoreOrchestrationHarness) PrepareVerifiedPreRestoreArchive(_ context.Context, _ PreRestoreArchiveRequest) (*PreRestoreArchive, error) {
	if err := harness.boundary("pre_restore_archive"); err != nil {
		return nil, err
	}
	return &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088", SHA256: bytes.Repeat([]byte{0x61}, sha256.Size)}, nil
}

func (harness *restoreOrchestrationHarness) PublishPreRestoreArchive(_ context.Context, _ *PreRestoreArchive, _ *PreparedArchiveBinding) error {
	return harness.boundary("publish_pre_restore_archive")
}

func (harness *restoreOrchestrationHarness) AbortPreRestoreArchive(_ context.Context, _ *PreRestoreArchive) error {
	harness.mark("abort_pre_restore_archive")
	return nil
}

func testRestoreArtifactReservation(operationID, workspaceID string) *RestoreArtifactReservation {
	return &RestoreArtifactReservation{operationID: operationID, workspaceID: workspaceID,
		stageBasename:    restoreStagePrefix + operationID + "-" + workspaceID + "-" + strings.Repeat("a", 64) + restoreArtifactSuffix,
		rollbackBasename: restoreRollbackPrefix + operationID + "-" + workspaceID + "-" + strings.Repeat("b", 64) + restoreArtifactSuffix,
		ownershipDigest:  [sha256.Size]byte{1}, stageMarkerHash: [sha256.Size]byte{2}, rollbackMarkerHash: [sha256.Size]byte{3}}
}

func (harness *restoreOrchestrationHarness) ReserveRestoreArtifacts(ctx context.Context, operationID, workspaceID string) (*RestoreArtifactReservation, error) {
	if harness.stagerOverride != nil {
		return harness.stagerOverride.ReserveRestoreArtifacts(ctx, operationID, workspaceID)
	}
	if err := harness.boundary("reserve_artifacts"); err != nil {
		return nil, err
	}
	return testRestoreArtifactReservation(operationID, workspaceID), nil
}

func (harness *restoreOrchestrationHarness) ReleaseRestoreArtifacts(ctx context.Context, reservation *RestoreArtifactReservation) error {
	if harness.stagerOverride != nil {
		return harness.stagerOverride.ReleaseRestoreArtifacts(ctx, reservation)
	}
	harness.mark("release_artifacts")
	return nil
}

func (harness *restoreOrchestrationHarness) Stage(ctx context.Context, request StageRequest) (*StagedWorkspace, error) {
	if harness.stagerOverride != nil {
		return harness.stagerOverride.Stage(ctx, request)
	}
	if err := harness.boundary("stage"); err != nil {
		return nil, err
	}
	harness.stagedBytes = "created"
	return &StagedWorkspace{Handle: "owned-stage"}, nil
}

func (harness *restoreOrchestrationHarness) DiscardStaged(_ context.Context, _ *StagedWorkspace) error {
	harness.mark("discard_staged")
	harness.stagedBytes = "discarded"
	return nil
}

func (harness *restoreOrchestrationHarness) VerifyStaged(_ context.Context, request StagedVerificationRequest) (*VerifiedStagedWorkspace, error) {
	if err := harness.boundary("staged_verify"); err != nil {
		return nil, err
	}
	return &VerifiedStagedWorkspace{Finalized: request.Finalized}, nil
}

func (harness *restoreOrchestrationHarness) Swap(_ context.Context, _ SwapRequest) (*SwapReceipt, error) {
	if err := harness.boundary("swap"); err != nil {
		return nil, err
	}
	harness.active = "restored"
	harness.stagedBytes = "active"
	harness.writesAtSwap = harness.databaseWrites
	return &SwapReceipt{ReceiptID: "018f0000-0000-7000-8000-000000000066"}, nil
}

func (harness *restoreOrchestrationHarness) ReserveSwap(_ context.Context, operationID, workspaceID string,
	_ *VerifiedStagedWorkspace,
) (*RestoreSwapReservation, error) {
	if err := harness.boundary("reserve_swap"); err != nil {
		return nil, err
	}
	return &RestoreSwapReservation{operationID: operationID, workspaceID: workspaceID,
		predecessorHash: [sha256.Size]byte{2}, activatedHash: [sha256.Size]byte{3}, swapAuthority: harness}, nil
}

func (harness *restoreOrchestrationHarness) ReleaseSwap(_ context.Context, _ *RestoreSwapReservation) error {
	harness.mark("release_swap")
	return nil
}

func (harness *restoreOrchestrationHarness) Rollback(_ context.Context, _ *SwapReceipt) error {
	harness.mark("rollback")
	harness.active = "original"
	harness.stagedBytes = "rolled_back"
	return nil
}

func (harness *restoreOrchestrationHarness) CommitSwap(_ context.Context, _ *SwapReceipt) error {
	return harness.boundary("commit_swap")
}

func (harness *restoreOrchestrationHarness) VerifyActivated(_ context.Context, _ ActivatedVerificationRequest) error {
	return harness.boundary("post_swap_verify")
}

func (harness *restoreOrchestrationHarness) RevokeMachineCredentials(_ context.Context, _ MachineCredentialRevocationRequest) error {
	return harness.boundary("revoke_machine_credentials")
}

func (harness *restoreOrchestrationHarness) FinalizeStagedWorkspace(_ context.Context, request FinalizeStagedRestoreRequest) (*FinalizedRestore, error) {
	if err := harness.boundary("finalize_staged_restore"); err != nil {
		return nil, err
	}
	harness.databaseWrites++
	harness.stagedBytes = "finalized"
	return &FinalizedRestore{WorkspaceID: request.WorkspaceID, Generation: request.NewGeneration,
		AuditHead: bytes.Repeat([]byte{0x51}, sha256.Size), EventType: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED,
		SchemaVersion: request.Manifest.SchemaVersion, MigrationManifestHash: append([]byte(nil), request.Manifest.MigrationManifestHash...),
		Staged: request.Staged}, nil
}

func (harness *restoreOrchestrationHarness) PublishRestoredMirror(_ context.Context, _ *FinalizedRestore) error {
	return harness.boundary("mirror")
}

func (harness *restoreOrchestrationHarness) config(providers *ProviderRegistry) ServiceConfig {
	return ServiceConfig{Proofs: harness, Trust: harness, Providers: providers, Journal: harness,
		PreRestoreArchives: harness, Stager: harness, StagedFinalizer: harness, StagedVerifier: harness, Swapper: harness,
		PostSwapVerifier: harness, MachineCredentials: harness, Mirror: harness}
}

func TestRestoreServiceSuccessOrchestrationOrder(t *testing.T) {
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	seed := sha256.Sum256([]byte("restore orchestration signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	input := backup.ArchiveInput{WorkspaceID: workspaceID, SchemaVersion: 3, AppVersion: "0.1.0",
		AuditGeneration: 2, AuditSequence: 7, AuditHead: bytes.Repeat([]byte{0x42}, sha256.Size),
		AuditRoot: bytes.Repeat([]byte{0x43}, sha256.Size), SigningKeyID: "018f0000-0000-7000-8000-000000000002",
		SigningKeyEpoch: 1, WorkspaceHeaderHash: bytes.Repeat([]byte{0x44}, sha256.Size),
		MigrationManifestHash: bytes.Repeat([]byte{0x45}, sha256.Size),
		Objects:               []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: []byte("verified staged database")}}}
	passphrase := []byte("correct horse battery staple")
	archive, err := backup.Seal(input, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x7b}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	harness := &restoreOrchestrationHarness{active: "original"}
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
		Validator: validatorFunc(func(context.Context, ValidationInput) error { harness.mark("providers"); return nil })}})
	if err != nil {
		t.Fatal(err)
	}
	harness.trust = backup.TrustAnchor{WorkspaceID: workspaceID,
		AuditGeneration: input.AuditGeneration, AuditRoot: input.AuditRoot, SigningKeyID: input.SigningKeyID,
		SigningKeyEpoch: input.SigningKeyEpoch, PublicKey: privateKey.Public().(ed25519.PublicKey)}
	service, err := NewService(harness.config(registry))
	if err != nil {
		t.Fatal(err)
	}
	proof := &AdminTOTPProof{AdminUserID: "018f0000-0000-7000-8000-000000000010",
		Password: []byte("administrator-password"), TOTP: "123456", IssuedAt: time.Unix(1_720_000_000, 0).UTC(),
		ReplayKey: "018f0000-0000-7000-8000-000000000011"}
	result, err := service.Restore(context.Background(), RestoreRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Archive: archive, Passphrase: passphrase, Proof: proof})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	wantOrder := []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "reserve_artifacts", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore",
		"staged_verify", "reserve_swap", "journal_staged", "swap", "journal_swapped", "post_swap_verify",
		"journal_postverified", "revoke_machine_credentials", "journal_revoked", "mirror", "journal_mirrored",
		"journal_complete", "commit_swap"}
	if !reflect.DeepEqual(harness.order, wantOrder) {
		t.Fatalf("restore order = %q, want %q", harness.order, wantOrder)
	}
	if harness.active != "restored" || harness.journalState != tammyv1.RestoreState_RESTORE_STATE_COMPLETE {
		t.Fatalf("active=%q journal=%v", harness.active, harness.journalState)
	}
	if result == nil || result.Generation != 5 || !bytes.Equal(result.AuditHead, bytes.Repeat([]byte{0x51}, sha256.Size)) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRestoreServiceFailureBoundaryMatrix(t *testing.T) {
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	seed := sha256.Sum256([]byte("restore failure matrix signing key"))
	privateKey := ed25519.NewKeyFromSeed(seed[:])
	input := backup.ArchiveInput{WorkspaceID: workspaceID, SchemaVersion: 3, AppVersion: "0.1.0",
		AuditGeneration: 2, AuditSequence: 7, AuditHead: bytes.Repeat([]byte{0x42}, sha256.Size),
		AuditRoot: bytes.Repeat([]byte{0x43}, sha256.Size), SigningKeyID: "018f0000-0000-7000-8000-000000000002",
		SigningKeyEpoch: 1, WorkspaceHeaderHash: bytes.Repeat([]byte{0x44}, sha256.Size),
		MigrationManifestHash: bytes.Repeat([]byte{0x45}, sha256.Size),
		Objects: []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1,
			Bytes: []byte("verified staged database")}}}
	passphrase := []byte("correct horse battery staple")
	archive, err := backup.Seal(input, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x7c}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	trust := backup.TrustAnchor{WorkspaceID: workspaceID, AuditGeneration: input.AuditGeneration,
		AuditRoot: input.AuditRoot, SigningKeyID: input.SigningKeyID, SigningKeyEpoch: input.SigningKeyEpoch,
		PublicKey: privateKey.Public().(ed25519.PublicKey)}
	proof := &AdminTOTPProof{AdminUserID: "018f0000-0000-7000-8000-000000000010",
		Password: []byte("administrator-password"), TOTP: "123456", IssuedAt: time.Unix(1_720_000_000, 0).UTC(),
		ReplayKey: "018f0000-0000-7000-8000-000000000011"}
	tests := []struct {
		name, failAt, active, staged string
		journal                      tammyv1.RestoreState
		action                       RecoveryAction
		order                        []string
		wrongPassphrase              bool
	}{
		{name: "proof", failAt: "proof", active: "original", action: RecoveryNone, order: []string{"proof"}},
		{name: "trust", failAt: "trust", active: "original", action: RecoveryNone, order: []string{"proof", "trust"}},
		{name: "open", active: "original", action: RecoveryNone, wrongPassphrase: true, order: []string{"proof", "trust"}},
		{name: "provider", failAt: "providers", active: "original", action: RecoveryNone, order: []string{"proof", "trust", "providers"}},
		{name: "prepared", failAt: "journal_prepared", active: "original", action: RecoveryNone, order: []string{"proof", "trust", "providers", "journal_prepared"}},
		{name: "prearchive", failAt: "pre_restore_archive", active: "original", journal: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive"}},
		{name: "publish_prearchive", failAt: "publish_pre_restore_archive", active: "original", journal: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "abort_pre_restore_archive"}},
		{name: "stage", failAt: "stage", active: "original", journal: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "abort_pre_restore_archive"}},
		{name: "staged_mutate", failAt: "finalize_staged_restore", active: "original", staged: "discarded", journal: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "discard_staged", "abort_pre_restore_archive"}},
		{name: "staged_verify", failAt: "staged_verify", active: "original", staged: "discarded", journal: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "discard_staged", "abort_pre_restore_archive"}},
		{name: "staged_journal", failAt: "journal_staged", active: "original", staged: "discarded", journal: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "discard_staged", "abort_pre_restore_archive"}},
		{name: "swap", failAt: "swap", active: "original", staged: "discarded", journal: tammyv1.RestoreState_RESTORE_STATE_STAGED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "discard_staged", "abort_pre_restore_archive"}},
		{name: "swapped_journal", failAt: "journal_swapped", active: "original", staged: "rolled_back", journal: tammyv1.RestoreState_RESTORE_STATE_STAGED,
			action: RecoveryRollback, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "rollback", "abort_pre_restore_archive"}},
		{name: "postverify", failAt: "post_swap_verify", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify"}},
		{name: "postverify_journal", failAt: "journal_postverified", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified"}},
		{name: "revoke", failAt: "revoke_machine_credentials", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified", "revoke_machine_credentials"}},
		{name: "revoke_journal", failAt: "journal_revoked", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified", "revoke_machine_credentials", "journal_revoked"}},
		{name: "mirror", failAt: "mirror", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified", "revoke_machine_credentials", "journal_revoked", "mirror"}},
		{name: "mirror_journal", failAt: "journal_mirrored", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified", "revoke_machine_credentials", "journal_revoked", "mirror", "journal_mirrored"}},
		{name: "complete", failAt: "journal_complete", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
			action: RecoveryFinishForward, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified", "revoke_machine_credentials", "journal_revoked", "mirror", "journal_mirrored", "journal_complete"}},
		{name: "cleanup", failAt: "commit_swap", active: "restored", staged: "active", journal: tammyv1.RestoreState_RESTORE_STATE_COMPLETE,
			action: RecoveryCleanup, order: []string{"proof", "trust", "providers", "journal_prepared", "pre_restore_archive", "journal_prepared_recovery", "publish_pre_restore_archive", "stage", "finalize_staged_restore", "staged_verify", "journal_staged", "swap", "journal_swapped", "post_swap_verify", "journal_postverified", "revoke_machine_credentials", "journal_revoked", "mirror", "journal_mirrored", "journal_complete", "commit_swap"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := &restoreOrchestrationHarness{active: "original", trust: trust, failAt: test.failAt}
			registry, registryErr := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
				Validator: validatorFunc(func(context.Context, ValidationInput) error { return harness.boundary("providers") })}})
			if registryErr != nil {
				t.Fatal(registryErr)
			}
			service, serviceErr := NewService(harness.config(registry))
			if serviceErr != nil {
				t.Fatal(serviceErr)
			}
			attemptPassphrase := passphrase
			if test.wrongPassphrase {
				attemptPassphrase = []byte("wrong archive passphrase")
			}
			result, restoreErr := service.Restore(context.Background(), RestoreRequest{OperationID: operationID,
				WorkspaceID: workspaceID, Archive: archive, Passphrase: attemptPassphrase, Proof: proof})
			if result != nil || !errors.Is(restoreErr, ErrRestore) {
				t.Fatalf("result=%#v error=%v, want restore failure", result, restoreErr)
			}
			if !test.wrongPassphrase && test.failAt != "providers" && !errors.Is(restoreErr, errInjectedRestoreBoundary) {
				t.Fatalf("error=%v, want injected cause", restoreErr)
			}
			wantOrder := restoreOrderWithReservations(test.name, test.order)
			if !reflect.DeepEqual(harness.order, wantOrder) {
				t.Fatalf("order=%q, want %q", harness.order, wantOrder)
			}
			if harness.active != test.active || harness.stagedBytes != test.staged || harness.journalState != test.journal {
				t.Fatalf("active=%q staged=%q journal=%v, want %q/%q/%v", harness.active, harness.stagedBytes,
					harness.journalState, test.active, test.staged, test.journal)
			}
			if harness.active == "restored" && harness.databaseWrites != harness.writesAtSwap {
				t.Fatalf("post-swap database writes=%d, writes at swap=%d", harness.databaseWrites, harness.writesAtSwap)
			}
			if action := RequiredRecoveryAction(harness.journalState); action != test.action {
				t.Fatalf("recovery action=%v, want %v", action, test.action)
			}
		})
	}
}

func restoreOrderWithReservations(name string, original []string) []string {
	order := append([]string(nil), original...)
	insertBefore := func(boundary, inserted string) {
		for index, value := range order {
			if value == boundary {
				order = append(order[:index], append([]string{inserted}, order[index:]...)...)
				return
			}
		}
	}
	insertBefore("journal_prepared_recovery", "reserve_artifacts")
	insertBefore("journal_staged", "reserve_swap")
	switch name {
	case "publish_prearchive", "stage":
		insertBefore("abort_pre_restore_archive", "release_artifacts")
	case "staged_journal", "swap":
		insertBefore("discard_staged", "release_swap")
	}
	return order
}
