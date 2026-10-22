package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type recoveryJournalHarness struct {
	statuses []*tammyv1.RestoreStatus
	calls    *[]string
}

const rolledBackRestoreState = tammyv1.RestoreState_RESTORE_STATE_ROLLED_BACK

func (harness *recoveryJournalHarness) ListRecoveryRecords(
	_ context.Context,
	after string,
	limit uint32,
) (*RestoreJournalPage, error) {
	*harness.calls = append(*harness.calls, fmt.Sprintf("list:%s:%d", after, limit))
	actionable := make([]*tammyv1.RestoreStatus, 0, len(harness.statuses))
	for _, status := range harness.statuses {
		if status.State != rolledBackRestoreState {
			actionable = append(actionable, status)
		}
	}
	start := 0
	for start < len(actionable) && actionable[start].OperationId <= after {
		start++
	}
	end := min(start+int(limit), len(actionable))
	page := &RestoreJournalPage{Records: make([]*tammyv1.RestoreStatus, end-start)}
	for index := start; index < end; index++ {
		page.Records[index-start] = cloneStatus(actionable[index])
	}
	if end < len(actionable) {
		page.NextAfterOperationID = actionable[end-1].OperationId
	}
	return page, nil
}

func (harness *recoveryJournalHarness) CheckpointRecovery(
	_ context.Context,
	operationID string,
	_ tammyv1.RestoreState,
	recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, error) {
	*harness.calls = append(*harness.calls, fmt.Sprintf("checkpoint:%s:%t%t%t", operationID,
		recovery.PostSwapVerified, recovery.MachineCredentialsRevoked, recovery.MirrorPublished))
	for _, status := range harness.statuses {
		if status.OperationId == operationID {
			status.Recovery = proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
			return cloneStatus(status), nil
		}
	}
	return nil, ErrJournal
}

func (harness *recoveryJournalHarness) Advance(
	_ context.Context,
	operationID string,
	from tammyv1.RestoreState,
	to tammyv1.RestoreState,
	_ []byte,
) (*tammyv1.RestoreStatus, error) {
	boundary := "complete:"
	if to == rolledBackRestoreState {
		boundary = "rollback-terminal:"
	}
	*harness.calls = append(*harness.calls, boundary+operationID)
	for _, status := range harness.statuses {
		if status.OperationId == operationID && status.State == from {
			status.State = to
			return cloneStatus(status), nil
		}
	}
	return nil, ErrJournal
}

type recoveryWorkspaceHarness struct{ calls *[]string }

func (harness recoveryWorkspaceHarness) RollbackInterruptedRestore(_ context.Context, status *tammyv1.RestoreStatus) error {
	*harness.calls = append(*harness.calls, "rollback:"+status.OperationId)
	return nil
}

func (harness recoveryWorkspaceHarness) ReconstructAndVerifyActivatedRestore(
	_ context.Context,
	status *tammyv1.RestoreStatus,
) (*FinalizedRestore, error) {
	*harness.calls = append(*harness.calls, "verify:"+status.OperationId)
	recovery := status.Recovery
	return &FinalizedRestore{OperationID: status.OperationId, WorkspaceID: recovery.WorkspaceId,
		Generation: recovery.GetFinalizedGeneration(), AuditHead: append([]byte(nil), recovery.FinalizedAuditHead...),
		BackupManifestHash:  append([]byte(nil), status.BackupManifestHash...),
		PreRestoreArchiveID: recovery.PreRestoreArchiveId, PreRestoreArchiveHash: append([]byte(nil), recovery.PreRestoreArchiveHash...),
		SchemaVersion: recovery.GetSchemaVersion(), MigrationManifestHash: append([]byte(nil), recovery.MigrationManifestHash...)}, nil
}

func (harness recoveryWorkspaceHarness) CleanupCompletedRestore(_ context.Context, status *tammyv1.RestoreStatus) error {
	*harness.calls = append(*harness.calls, "cleanup:"+status.OperationId)
	return nil
}

type recoveryArchiveHarness struct{ calls *[]string }

func (harness recoveryArchiveHarness) CleanupInterruptedPreRestoreArchive(
	_ context.Context,
	status *tammyv1.RestoreStatus,
) error {
	archiveID := status.OperationId
	if status.Recovery != nil {
		archiveID = status.Recovery.PreRestoreArchiveId
	}
	*harness.calls = append(*harness.calls, "archive:"+archiveID)
	return nil
}

type recoveryCredentialHarness struct{ calls *[]string }

func (harness recoveryCredentialHarness) RevokeRecoveredMachineCredentials(
	_ context.Context,
	status *tammyv1.RestoreStatus,
) error {
	*harness.calls = append(*harness.calls, "revoke:"+status.OperationId)
	return nil
}

type recoveryMirrorHarness struct{ calls *[]string }

func (harness recoveryMirrorHarness) PublishRestoredMirror(_ context.Context, finalized *FinalizedRestore) error {
	*harness.calls = append(*harness.calls, "mirror:"+finalized.OperationID)
	return nil
}

type recoveryDeathJournal struct {
	*recoveryJournalHarness
	failPoint string
	failed    bool
	failure   error
}

func (journal *recoveryDeathJournal) CheckpointRecovery(ctx context.Context, operationID string,
	state tammyv1.RestoreState, recovery *tammyv1.RestoreRecoveryRecord,
) (*tammyv1.RestoreStatus, error) {
	point := fmt.Sprintf("checkpoint:%t%t%t", recovery.PostSwapVerified,
		recovery.MachineCredentialsRevoked, recovery.MirrorPublished)
	if !journal.failed && journal.failPoint == point {
		journal.failed = true
		return nil, journal.failure
	}
	return journal.recoveryJournalHarness.CheckpointRecovery(ctx, operationID, state, recovery)
}

func (journal *recoveryDeathJournal) Advance(ctx context.Context, operationID string, from tammyv1.RestoreState,
	to tammyv1.RestoreState, head []byte,
) (*tammyv1.RestoreStatus, error) {
	if !journal.failed && journal.failPoint == "complete" {
		journal.failed = true
		return nil, journal.failure
	}
	if !journal.failed && journal.failPoint == "rollback-terminal" && to == rolledBackRestoreState {
		journal.failed = true
		return nil, journal.failure
	}
	return journal.recoveryJournalHarness.Advance(ctx, operationID, from, to, head)
}

type idempotentRollbackWorkspace struct {
	recoveryWorkspaceHarness
	effects map[string]struct{}
}

func (workspace *idempotentRollbackWorkspace) RollbackInterruptedRestore(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) error {
	workspace.effects[status.OperationId] = struct{}{}
	return workspace.recoveryWorkspaceHarness.RollbackInterruptedRestore(ctx, status)
}

type idempotentRollbackArchives struct {
	recoveryArchiveHarness
	effects map[string]struct{}
}

func (archives *idempotentRollbackArchives) CleanupInterruptedPreRestoreArchive(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) error {
	archives.effects[status.OperationId] = struct{}{}
	return archives.recoveryArchiveHarness.CleanupInterruptedPreRestoreArchive(ctx, status)
}

type idempotentRecoveryWorkspace struct {
	recoveryWorkspaceHarness
	cleanupFailure error
	cleanupFailed  bool
	cleanupEffects map[string]struct{}
}

func (workspace *idempotentRecoveryWorkspace) CleanupCompletedRestore(
	ctx context.Context,
	status *tammyv1.RestoreStatus,
) error {
	if workspace.cleanupFailure != nil && !workspace.cleanupFailed {
		workspace.cleanupFailed = true
		return workspace.cleanupFailure
	}
	workspace.cleanupEffects[status.OperationId] = struct{}{}
	return workspace.recoveryWorkspaceHarness.CleanupCompletedRestore(ctx, status)
}

type idempotentRecoveryEffects struct {
	revocationAttempts int
	mirrorAttempts     int
	revocations        map[string]struct{}
	mirrors            map[string]struct{}
}

func (effects *idempotentRecoveryEffects) RevokeRecoveredMachineCredentials(
	_ context.Context,
	status *tammyv1.RestoreStatus,
) error {
	effects.revocationAttempts++
	effects.revocations[status.OperationId] = struct{}{}
	return nil
}

func (effects *idempotentRecoveryEffects) PublishRestoredMirror(
	_ context.Context,
	finalized *FinalizedRestore,
) error {
	effects.mirrorAttempts++
	effects.mirrors[finalized.OperationID] = struct{}{}
	return nil
}

func TestStartupRecoveryCoordinatorProcessesAuthenticatedJournalInBoundedBatches(t *testing.T) {
	operationIDs := []string{
		"018f0000-0000-7000-8000-000000000011",
		"018f0000-0000-7000-8000-000000000012",
		"018f0000-0000-7000-8000-000000000013",
		"018f0000-0000-7000-8000-000000000014",
	}
	statuses := make([]*tammyv1.RestoreStatus, len(operationIDs))
	for index, operationID := range operationIDs {
		statuses[index] = recoveryStatus(operationID, tammyv1.RestoreState(index+1), byte(0x31+index))
	}
	calls := []string{}
	journal := &recoveryJournalHarness{statuses: statuses, calls: &calls}
	coordinator, err := NewStartupRecoveryCoordinator(StartupRecoveryConfig{Journal: journal,
		Workspace: recoveryWorkspaceHarness{calls: &calls}, Archives: recoveryArchiveHarness{calls: &calls},
		MachineCredentials: recoveryCredentialHarness{calls: &calls}, Mirror: recoveryMirrorHarness{calls: &calls}, BatchSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	report, err := coordinator.Recover(context.Background())
	if err != nil || report == nil || report.Processed != 4 || report.RolledBack != 2 || report.Completed != 1 || report.Cleaned != 2 {
		t.Fatalf("Recover() report=%#v error=%v", report, err)
	}
	want := []string{
		"list::2",
		"rollback:" + operationIDs[0], "archive:" + statuses[0].Recovery.PreRestoreArchiveId,
		"rollback-terminal:" + operationIDs[0],
		"rollback:" + operationIDs[1], "archive:" + statuses[1].Recovery.PreRestoreArchiveId,
		"rollback-terminal:" + operationIDs[1],
		"list:" + operationIDs[1] + ":2",
		"verify:" + operationIDs[2], "checkpoint:" + operationIDs[2] + ":truefalsefalse",
		"revoke:" + operationIDs[2], "checkpoint:" + operationIDs[2] + ":truetruefalse",
		"mirror:" + operationIDs[2], "checkpoint:" + operationIDs[2] + ":truetruetrue",
		"complete:" + operationIDs[2], "cleanup:" + operationIDs[2],
		"cleanup:" + operationIDs[3],
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("recovery calls=\n%q\nwant=\n%q", calls, want)
	}
}

func TestStartupRecoveryCoordinatorResumesAfterDeathAtEveryForwardBoundary(t *testing.T) {
	processDeath := errors.New("simulated process death")
	for _, failPoint := range []string{
		"checkpoint:truefalsefalse",
		"checkpoint:truetruefalse",
		"checkpoint:truetruetrue",
		"complete",
		"cleanup",
	} {
		t.Run(failPoint, func(t *testing.T) {
			operationID := "018f0000-0000-7000-8000-000000000031"
			calls := []string{}
			baseJournal := &recoveryJournalHarness{statuses: []*tammyv1.RestoreStatus{
				recoveryStatus(operationID, tammyv1.RestoreState_RESTORE_STATE_SWAPPED, 0x61),
			}, calls: &calls}
			journal := &recoveryDeathJournal{recoveryJournalHarness: baseJournal,
				failPoint: failPoint, failure: processDeath}
			workspace := &idempotentRecoveryWorkspace{recoveryWorkspaceHarness: recoveryWorkspaceHarness{calls: &calls},
				cleanupEffects: make(map[string]struct{})}
			if failPoint == "cleanup" {
				workspace.cleanupFailure = processDeath
				journal.failed = true
			}
			effects := &idempotentRecoveryEffects{revocations: make(map[string]struct{}), mirrors: make(map[string]struct{})}
			config := StartupRecoveryConfig{Journal: journal, Workspace: workspace,
				Archives: recoveryArchiveHarness{calls: &calls}, MachineCredentials: effects, Mirror: effects, BatchSize: 8}
			coordinator, err := NewStartupRecoveryCoordinator(config)
			if err != nil {
				t.Fatal(err)
			}
			if report, err := coordinator.Recover(context.Background()); report != nil || !errors.Is(err, processDeath) {
				t.Fatalf("first recovery report=%#v error=%v", report, err)
			}
			restarted, err := NewStartupRecoveryCoordinator(config)
			if err != nil {
				t.Fatal(err)
			}
			report, err := restarted.Recover(context.Background())
			if err != nil || report == nil || baseJournal.statuses[0].State != tammyv1.RestoreState_RESTORE_STATE_COMPLETE {
				t.Fatalf("restarted recovery report=%#v error=%v status=%#v", report, err, baseJournal.statuses[0])
			}
			if len(effects.revocations) != 1 || len(effects.mirrors) != 1 || len(workspace.cleanupEffects) != 1 {
				t.Fatalf("harmful effects revocations=%v mirrors=%v cleanups=%v", effects.revocations,
					effects.mirrors, workspace.cleanupEffects)
			}
			if failPoint == "checkpoint:truetruefalse" && effects.revocationAttempts != 2 {
				t.Fatalf("revocation retry attempts=%d, want 2", effects.revocationAttempts)
			}
			if failPoint == "checkpoint:truetruetrue" && effects.mirrorAttempts != 2 {
				t.Fatalf("mirror retry attempts=%d, want 2", effects.mirrorAttempts)
			}
		})
	}
}

func TestStartupRecoveryCoordinatorPersistsRollbackTerminalAndSkipsItOnLaterStartup(t *testing.T) {
	operationID := "018f0000-0000-7000-8000-000000000032"
	calls := []string{}
	journal := &recoveryJournalHarness{statuses: []*tammyv1.RestoreStatus{
		recoveryStatus(operationID, tammyv1.RestoreState_RESTORE_STATE_PREPARED, 0x62),
	}, calls: &calls}
	workspace := &idempotentRollbackWorkspace{recoveryWorkspaceHarness: recoveryWorkspaceHarness{calls: &calls},
		effects: make(map[string]struct{})}
	archives := &idempotentRollbackArchives{recoveryArchiveHarness: recoveryArchiveHarness{calls: &calls},
		effects: make(map[string]struct{})}
	config := StartupRecoveryConfig{Journal: journal, Workspace: workspace, Archives: archives,
		MachineCredentials: recoveryCredentialHarness{calls: &calls}, Mirror: recoveryMirrorHarness{calls: &calls}, BatchSize: 8}
	coordinator, err := NewStartupRecoveryCoordinator(config)
	if err != nil {
		t.Fatal(err)
	}
	report, err := coordinator.Recover(context.Background())
	if err != nil || report == nil || report.RolledBack != 1 || journal.statuses[0].State != rolledBackRestoreState {
		t.Fatalf("first recovery report=%#v error=%v state=%v", report, err, journal.statuses[0].State)
	}
	restarted, err := NewStartupRecoveryCoordinator(config)
	if err != nil {
		t.Fatal(err)
	}
	report, err = restarted.Recover(context.Background())
	if err != nil || report == nil || report.Processed != 0 || len(workspace.effects) != 1 || len(archives.effects) != 1 {
		t.Fatalf("second recovery report=%#v error=%v workspace=%v archives=%v", report, err,
			workspace.effects, archives.effects)
	}
}

func TestStartupRecoveryCoordinatorResumesRollbackAfterDeathBeforeTerminalJournalPersist(t *testing.T) {
	operationID := "018f0000-0000-7000-8000-000000000033"
	processDeath := errors.New("simulated rollback journal death")
	calls := []string{}
	base := &recoveryJournalHarness{statuses: []*tammyv1.RestoreStatus{
		recoveryStatus(operationID, tammyv1.RestoreState_RESTORE_STATE_STAGED, 0x63),
	}, calls: &calls}
	journal := &recoveryDeathJournal{recoveryJournalHarness: base, failPoint: "rollback-terminal", failure: processDeath}
	workspace := &idempotentRollbackWorkspace{recoveryWorkspaceHarness: recoveryWorkspaceHarness{calls: &calls},
		effects: make(map[string]struct{})}
	archives := &idempotentRollbackArchives{recoveryArchiveHarness: recoveryArchiveHarness{calls: &calls},
		effects: make(map[string]struct{})}
	config := StartupRecoveryConfig{Journal: journal, Workspace: workspace, Archives: archives,
		MachineCredentials: recoveryCredentialHarness{calls: &calls}, Mirror: recoveryMirrorHarness{calls: &calls}, BatchSize: 8}
	coordinator, err := NewStartupRecoveryCoordinator(config)
	if err != nil {
		t.Fatal(err)
	}
	if report, err := coordinator.Recover(context.Background()); report != nil || !errors.Is(err, processDeath) {
		t.Fatalf("first recovery report=%#v error=%v", report, err)
	}
	restarted, err := NewStartupRecoveryCoordinator(config)
	if err != nil {
		t.Fatal(err)
	}
	report, err := restarted.Recover(context.Background())
	if err != nil || report == nil || base.statuses[0].State != rolledBackRestoreState ||
		len(workspace.effects) != 1 || len(archives.effects) != 1 {
		t.Fatalf("restart report=%#v error=%v state=%v workspace=%v archives=%v", report, err,
			base.statuses[0].State, workspace.effects, archives.effects)
	}
}

func TestStartupRecoveryCoordinatorAuthenticatesEveryRecordBeforeFilesystemAction(t *testing.T) {
	directory := t.TempDir()
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	operationIDs := []string{
		"018f0000-0000-7000-8000-000000000041",
		"018f0000-0000-7000-8000-000000000042",
	}
	for _, operationID := range operationIDs {
		manifestHash := bytes.Repeat([]byte{0x71}, sha256.Size)
		if _, err := store.Prepare(context.Background(), operationID, manifestHash); err != nil {
			t.Fatal(err)
		}
		recovery := recoveryStatus(operationID, tammyv1.RestoreState_RESTORE_STATE_PREPARED, 0x72).Recovery
		if _, _, err := store.BindPreparedRecovery(context.Background(), operationID, manifestHash, recovery); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	tamperedPath := filepath.Join(directory, journalName(operationIDs[1]))
	frame, err := os.ReadFile(tamperedPath)
	if err != nil {
		t.Fatal(err)
	}
	frame[28] ^= 0x01
	if err := os.WriteFile(tamperedPath, frame, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err = NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	actions := []string{}
	coordinator, err := NewStartupRecoveryCoordinator(StartupRecoveryConfig{Journal: store,
		Workspace: recoveryWorkspaceHarness{calls: &actions}, Archives: recoveryArchiveHarness{calls: &actions},
		MachineCredentials: recoveryCredentialHarness{calls: &actions}, Mirror: recoveryMirrorHarness{calls: &actions}, BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report, err := coordinator.Recover(context.Background()); report != nil || !errors.Is(err, ErrStartupRecovery) {
		t.Fatalf("tampered recovery report=%#v error=%v", report, err)
	}
	if len(actions) != 0 {
		t.Fatalf("filesystem/external actions ran before complete authentication: %v", actions)
	}
}

func recoveryStatus(operationID string, state tammyv1.RestoreState, fill byte) *tammyv1.RestoreStatus {
	generation := uint64(6)
	schemaVersion := uint64(4)
	workspaceID := "018f0000-0000-7000-8000-000000000021"
	artifacts := testRestoreArtifactReservation(operationID, workspaceID)
	recovery := &tammyv1.RestoreRecoveryRecord{
		WorkspaceId:                       workspaceID,
		PreRestoreArchiveId:               "018f0000-0000-7000-8000-000000000099",
		PreRestoreArchiveHash:             bytes.Repeat([]byte{fill}, sha256.Size),
		StageBasename:                     artifacts.StageBasename(),
		RollbackBasename:                  artifacts.RollbackBasename(),
		ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
		StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
		RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
		PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
		PreRestoreArchiveFinalBasename:    preRestoreArchiveName("018f0000-0000-7000-8000-000000000099"),
	}
	status := &tammyv1.RestoreStatus{OperationId: operationID, State: state,
		BackupManifestHash: bytes.Repeat([]byte{fill + 1}, sha256.Size), Recovery: recovery,
		UpdatedAt: timestamppb.New(time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC))}
	if state != tammyv1.RestoreState_RESTORE_STATE_PREPARED {
		recovery.FinalizedGeneration = &generation
		recovery.FinalizedAuditHead = bytes.Repeat([]byte{fill + 2}, sha256.Size)
		recovery.SchemaVersion = &schemaVersion
		recovery.MigrationManifestHash = bytes.Repeat([]byte{fill + 3}, sha256.Size)
		recovery.RollbackPredecessorHash = bytes.Repeat([]byte{fill + 4}, sha256.Size)
		recovery.ActivatedDatabaseSha256 = bytes.Repeat([]byte{fill + 5}, sha256.Size)
		status.NewAuditHead = append([]byte(nil), recovery.FinalizedAuditHead...)
	}
	if state == tammyv1.RestoreState_RESTORE_STATE_COMPLETE {
		recovery.PostSwapVerified = true
		recovery.MachineCredentialsRevoked = true
		recovery.MirrorPublished = true
	}
	return status
}
