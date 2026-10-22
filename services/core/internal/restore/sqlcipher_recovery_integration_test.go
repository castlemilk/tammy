//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSQLCipherStartupRecoveryWithNilRecordNeverDeletesOperationNamedFiles(t *testing.T) {
	ctx := context.Background()
	operationID := "018f0000-0000-7000-8000-000000000098"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	key := bytes.Repeat([]byte{0x40}, sqlcipher.KeySize)
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active-nil-recovery")
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	activeBefore, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	stagePath := filepath.Join(directory, ".tammy-restore-stage-"+operationID+".db")
	if err := os.WriteFile(stagePath, []byte("foreign operation-named stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
		ActivePath: activePath, StagingDirectory: directory, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	status := &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
		BackupManifestHash: bytes.Repeat([]byte{0x50}, sha256.Size), UpdatedAt: timestamppb.Now()}
	if err := adapter.RollbackInterruptedRestore(ctx, status); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(stagePath); err != nil || string(got) != "foreign operation-named stage" {
		t.Fatalf("nil recovery changed foreign stage=%q error=%v", got, err)
	}
	activeAfter, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(activeAfter, activeBefore) || sha256.Sum256(activeAfter) != sha256.Sum256(activeBefore) {
		t.Fatalf("nil recovery changed active bytes error=%v", err)
	}
	assertRestoreMarker(t, ctx, activePath, key, "active-nil-recovery")
}

func TestSQLCipherStageCollisionRestartNeverDeletesForeignArtifacts(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000097"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	key := bytes.Repeat([]byte{0x41}, sqlcipher.KeySize)
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active-collision")
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	activeBefore, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	stageName := ".tammy-restore-stage-" + operationID + ".db"
	foreign := map[string][]byte{
		stageName: []byte("foreign predictable stage"),
		".tammy-restore-stage-" + operationID + "-0.db":    []byte("foreign mutated stage tag"),
		".tammy-restore-rollback-" + operationID + "-0.db": []byte("foreign mutated rollback tag"),
	}
	for name, contents := range foreign {
		if err := os.WriteFile(filepath.Join(directory, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
		StagingDirectory: directory, Key: key,
		NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
		NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
		NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
		Now:                    time.Now,
		Random:                 bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)),
		AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: 1, MigrationManifestHash: bytes.Repeat([]byte{0x70}, sha256.Size)}
	if staged, stageErr := workspace.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: bytes.Repeat([]byte{0x71}, sha256.Size), Objects: []backup.Object{{
			Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: []byte("unused")}}}); staged != nil || !errors.Is(stageErr, ErrSQLCipherWorkspace) {
		t.Fatalf("Stage() = %#v, %v; want collision failure", staged, stageErr)
	}
	if err := workspace.Close(); err != nil {
		t.Fatal(err)
	}

	recovery, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
		ActivePath: activePath, StagingDirectory: directory, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := recoveryStatus(operationID, tammyv1.RestoreState_RESTORE_STATE_PREPARED, 0x52)
	_ = recovery.RollbackInterruptedRestore(ctx, status)
	if err := recovery.Close(); err != nil {
		t.Fatal(err)
	}
	for name, want := range foreign {
		got, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("restart changed foreign artifact %q: bytes=%q error=%v", name, got, readErr)
		}
	}
	activeAfter, err := os.ReadFile(activePath)
	if err != nil || !bytes.Equal(activeAfter, activeBefore) {
		t.Fatalf("restart changed active bytes: error=%v", err)
	}
}

func TestSQLCipherAuthenticatedArtifactReservationCollisionAndUnboundDeath(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000096"
	key := bytes.Repeat([]byte{0x44}, sqlcipher.KeySize)
	defer zeroBytes(key)

	newWorkspace := func(t *testing.T, directory string) (*SQLCipherWorkspaceAdapter, string) {
		t.Helper()
		activePath := filepath.Join(directory, "workspace.db")
		active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active-reservation")
		if err := active.Close(); err != nil {
			t.Fatal(err)
		}
		adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
			StagingDirectory: directory, Key: key,
			NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
			NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
			NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
			Now:                    time.Now,
			Random:                 bytes.NewReader(bytes.Repeat([]byte{0x75}, 128)),
			AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size)})
		if err != nil {
			t.Fatal(err)
		}
		return adapter, activePath
	}

	t.Run("exact_hmac_name_without_ownership_marker_is_foreign", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "private")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		workspace, activePath := newWorkspace(t, directory)
		stageName := authenticatedRestoreArtifactName(restoreStagePrefix, restoreStageNameDomain,
			key, operationID, workspaceID)
		rollbackName := authenticatedRestoreArtifactName(restoreRollbackPrefix, restoreRollbackNameDomain,
			key, operationID, workspaceID)
		mutation := byte('0')
		if stageName[len(stageName)-4] == mutation {
			mutation = '1'
		}
		mutated := stageName[:len(stageName)-4] + string(mutation) + stageName[len(stageName)-3:]
		foreign := map[string][]byte{stageName: []byte("foreign exact tagged stage"),
			rollbackName + ".foreign": []byte("foreign rollback variant"), mutated: []byte("foreign mutated tag")}
		for name, contents := range foreign {
			if err := os.WriteFile(filepath.Join(directory, name), contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if reservation, err := workspace.ReserveRestoreArtifacts(ctx, operationID, workspaceID); reservation != nil ||
			!errors.Is(err, ErrSQLCipherWorkspace) {
			t.Fatalf("reservation=%#v error=%v, want fail-closed collision", reservation, err)
		}
		if err := workspace.Close(); err != nil {
			t.Fatal(err)
		}
		recovery, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
			ActivePath: activePath, StagingDirectory: directory, Key: key})
		if err != nil {
			t.Fatal(err)
		}
		status := &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			BackupManifestHash: bytes.Repeat([]byte{0x50}, sha256.Size), UpdatedAt: timestamppb.Now()}
		if err := recovery.RollbackInterruptedRestore(ctx, status); err != nil {
			t.Fatal(err)
		}
		_ = recovery.Close()
		for name, want := range foreign {
			if got, err := os.ReadFile(filepath.Join(directory, name)); err != nil || !bytes.Equal(got, want) {
				t.Fatalf("foreign %q changed: %q %v", name, got, err)
			}
		}
	})

	t.Run("death_after_reservation_before_bind_cleans_only_authenticated_empty_reservation", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "private")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		workspace, activePath := newWorkspace(t, directory)
		reservation, err := workspace.ReserveRestoreArtifacts(ctx, operationID, workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		storageReservation := reservation.storageReservation.(*sqlcipherArtifactReservation)
		if err := storageReservation.stageFile.Close(); err != nil {
			t.Fatal(err)
		}
		storageReservation.stageFile = nil
		foreignName := reservation.StageBasename() + ".tag-mutated"
		if err := os.WriteFile(filepath.Join(directory, foreignName), []byte("preserve foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := workspace.Close(); err != nil {
			t.Fatal(err)
		}
		recovery, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
			ActivePath: activePath, StagingDirectory: directory, Key: key})
		if err != nil {
			t.Fatal(err)
		}
		status := &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
			BackupManifestHash: bytes.Repeat([]byte{0x50}, sha256.Size), UpdatedAt: timestamppb.Now()}
		if err := recovery.RollbackInterruptedRestore(ctx, status); err != nil {
			t.Fatal(err)
		}
		_ = recovery.Close()
		for _, name := range []string{reservation.StageBasename(), reservation.StageBasename() + ".owner",
			reservation.RollbackBasename() + ".owner"} {
			if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("owned unbound residue %q remains: %v", name, err)
			}
		}
		if got, err := os.ReadFile(filepath.Join(directory, foreignName)); err != nil || string(got) != "preserve foreign" {
			t.Fatalf("foreign changed: %q %v", got, err)
		}
	})
}

func TestSQLCipherStartupRecoveryRollsBackSwapBeforeSwappedJournal(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000021"
	operationID := "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	stageSourcePath := filepath.Join(t.TempDir(), "stage-source.db")
	key := bytes.Repeat([]byte{0x42}, sqlcipher.KeySize)
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active-before-crash")
	stageSource := createRestoreDatabaseFixture(t, ctx, stageSourcePath, key, "staged-before-crash")
	schemaVersion, migrationHash := restoreSchemaMetadata(t, ctx, stageSource)
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stageSource.Close(); err != nil {
		t.Fatal(err)
	}
	stagedBytes, err := os.ReadFile(stageSourcePath)
	if err != nil {
		t.Fatal(err)
	}
	activeBefore, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	activeBeforeHash := sha256.Sum256(activeBefore)
	if err := os.WriteFile(filepath.Join(directory, "unrelated.db"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	normal, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
		StagingDirectory: directory, Key: key,
		NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
		NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
		NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
		Now:                    time.Now,
		Random:                 bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)),
		AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := normal.ReserveRestoreArtifacts(ctx, operationID, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: schemaVersion, MigrationManifestHash: migrationHash}
	staged, err := normal.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: bytes.Repeat([]byte{0x71}, sha256.Size), Artifacts: artifacts,
		Objects: []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: stagedBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	verified := verifiedSQLCipherStageForSwapTest(t, normal,
		&FinalizedRestore{WorkspaceID: workspaceID, Staged: staged})
	swapReservation, err := normal.ReserveSwap(ctx, operationID, workspaceID, verified)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := normal.Swap(ctx, SwapRequest{OperationID: operationID, Verified: verified,
		PreRestoreArchive: &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
			SHA256: bytes.Repeat([]byte{0x61}, sha256.Size)}, Reservation: swapReservation}); err != nil {
		t.Fatal(err)
	}
	assertRestoreMarker(t, ctx, activePath, key, "staged-before-crash")
	if err := normal.Close(); err != nil {
		t.Fatal(err)
	}

	adapter, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
		ActivePath: activePath, StagingDirectory: directory, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	status := recoveryStatus(operationID, tammyv1.RestoreState_RESTORE_STATE_STAGED, 0x51)
	status.Recovery.WorkspaceId = workspaceID
	status.Recovery.StageBasename = artifacts.StageBasename()
	status.Recovery.RollbackBasename = artifacts.RollbackBasename()
	status.Recovery.ArtifactOwnershipDigest = artifacts.OwnershipDigest()
	status.Recovery.StageOwnerMarkerSha256 = artifacts.StageOwnerMarkerSHA256()
	status.Recovery.RollbackOwnerMarkerSha256 = artifacts.RollbackOwnerMarkerSHA256()
	status.Recovery.RollbackPredecessorHash = swapReservation.PredecessorHash()
	status.Recovery.ActivatedDatabaseSha256 = swapReservation.ActivatedHash()
	for _, test := range []struct {
		name      string
		target    string
		ambiguous bool
	}{
		{name: "rollback_predecessor_sha", target: status.Recovery.RollbackBasename},
		{name: "ownership_marker_digest", target: status.Recovery.StageBasename + ".owner"},
		{name: "active_stage_rollback", ambiguous: true},
	} {
		t.Run(test.name+"_tamper_fails_before_filesystem_action", func(t *testing.T) {
			tamperedDirectory := filepath.Join(t.TempDir(), "private")
			copyRestoreDirectory(t, directory, tamperedDirectory)
			if test.ambiguous {
				contents, err := os.ReadFile(filepath.Join(tamperedDirectory, "workspace.db"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(tamperedDirectory, status.Recovery.StageBasename), contents, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				targetPath := filepath.Join(tamperedDirectory, test.target)
				contents, err := os.ReadFile(targetPath)
				if err != nil || len(contents) == 0 {
					t.Fatalf("read tamper target: %v", err)
				}
				contents[0] ^= 0x01
				if err := os.WriteFile(targetPath, contents, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotRestoreDirectory(t, tamperedDirectory)
			tampered, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
				ActivePath: filepath.Join(tamperedDirectory, "workspace.db"), StagingDirectory: tamperedDirectory, Key: key})
			if err != nil {
				t.Fatal(err)
			}
			if err := tampered.RollbackInterruptedRestore(ctx, status); !errors.Is(err, ErrSQLCipherWorkspace) {
				t.Fatalf("tampered recovery error=%v, want fail closed", err)
			}
			_ = tampered.Close()
			after := snapshotRestoreDirectory(t, tamperedDirectory)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("tampered recovery changed files: before=%v after=%v", before, after)
			}
		})
	}
	if err := adapter.RollbackInterruptedRestore(ctx, status); err != nil {
		t.Fatalf("RollbackInterruptedRestore() error=%v", err)
	}
	activeAfter, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if activeAfterHash := sha256.Sum256(activeAfter); activeAfterHash != activeBeforeHash || !bytes.Equal(activeAfter, activeBefore) {
		t.Fatalf("active bytes changed across crash rollback: before=%x after=%x", activeBeforeHash, activeAfterHash)
	}
	assertRestoreMarker(t, ctx, activePath, key, "active-before-crash")
	for _, name := range []string{status.Recovery.StageBasename, status.Recovery.RollbackBasename} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("owned residue %q error=%v", name, err)
		}
	}
	if contents, err := os.ReadFile(filepath.Join(directory, "unrelated.db")); err != nil || string(contents) != "preserve" {
		t.Fatalf("unrelated contents=%q error=%v", contents, err)
	}
}

func copyRestoreDirectory(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshotRestoreDirectory(t *testing.T, directory string) map[string][sha256.Size]byte {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string][sha256.Size]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = sha256.Sum256(contents)
	}
	return result
}

func (effects *concretePostRestoreEffects) RevokeRecoveredMachineCredentials(
	context.Context,
	*tammyv1.RestoreStatus,
) error {
	effects.revoked++
	return nil
}

func TestSQLCipherStartupRecoveryFinishesSwappedRestoreIdempotently(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	archivedPath := filepath.Join(t.TempDir(), "archived.db")
	key := bytes.Repeat([]byte{0x43}, sqlcipher.KeySize)
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active")
	archived := createRestoreDatabaseFixture(t, ctx, archivedPath, key, "archived")
	createdAt := time.Unix(1_710_000_000, 0).UTC()
	archivedHead, signingKey := seedArchivedAuditAndSessions(t, ctx, archived, workspaceID, key, createdAt)
	schemaVersion, migrationHash := restoreSchemaMetadata(t, ctx, archived)
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archived.Close(); err != nil {
		t.Fatal(err)
	}
	archivedBytes, err := os.ReadFile(archivedPath)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
		StagingDirectory: directory, Key: key,
		NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
		NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
		NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
		Now:                    func() time.Time { return createdAt.Add(time.Hour) },
		Random:                 bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)),
		AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	manifestHash := bytes.Repeat([]byte{0x62}, sha256.Size)
	auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: schemaVersion, MigrationManifestHash: migrationHash, AuditGeneration: 2,
		AuditHead: archivedHead[:], AuditRoot: auditRoot[:], SigningKeyId: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch}
	artifacts := reserveSQLCipherTestArtifacts(t, ctx, normal, operationID, workspaceID)
	staged, err := normal.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: manifestHash, Artifacts: artifacts, Objects: []backup.Object{{Path: "database/workspace.db",
			Provider: "workspace", ProviderVersion: 1, Bytes: archivedBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	preArchive := &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088", Version: 1,
		SHA256: bytes.Repeat([]byte{0x61}, sha256.Size), CreatedAt: createdAt.Add(30 * time.Minute),
		DeletionEligibleAt: createdAt.Add(30*time.Minute).AddDate(1, 0, 0), SourceGeneration: 5,
		EncryptedByteLength: 4096}
	authorization := &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
		WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)}
	finalized, err := normal.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{OperationID: operationID,
		WorkspaceID: workspaceID, NewGeneration: 6, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, PreRestoreArchive: preArchive, Staged: staged})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := normal.VerifyStaged(ctx, StagedVerificationRequest{OperationID: operationID,
		WorkspaceID: workspaceID, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, Finalized: finalized})
	if err != nil {
		t.Fatal(err)
	}
	swapReservation := reserveSQLCipherTestSwap(t, ctx, normal, operationID, workspaceID, verified)
	if _, err := normal.Swap(ctx, SwapRequest{OperationID: operationID, Verified: verified,
		PreRestoreArchive: preArchive, Reservation: swapReservation}); err != nil {
		t.Fatal(err)
	}
	if err := normal.Close(); err != nil {
		t.Fatal(err)
	}

	journal, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(),
		Now: func() time.Time { return createdAt.Add(2 * time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.Prepare(ctx, operationID, manifestHash); err != nil {
		t.Fatal(err)
	}
	recovery := &tammyv1.RestoreRecoveryRecord{WorkspaceId: workspaceID, PreRestoreArchiveId: preArchive.ArchiveID,
		PreRestoreArchiveHash:             append([]byte(nil), preArchive.SHA256...),
		StageBasename:                     artifacts.StageBasename(),
		RollbackBasename:                  artifacts.RollbackBasename(),
		ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
		StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
		RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
		PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
		PreRestoreArchiveFinalBasename:    preRestoreArchiveName(preArchive.ArchiveID)}
	if _, _, err := journal.BindPreparedRecovery(ctx, operationID, manifestHash, recovery); err != nil {
		t.Fatal(err)
	}
	recovery.FinalizedGeneration = &finalized.Generation
	recovery.FinalizedAuditHead = append([]byte(nil), finalized.AuditHead...)
	recovery.SchemaVersion = &finalized.SchemaVersion
	recovery.MigrationManifestHash = append([]byte(nil), finalized.MigrationManifestHash...)
	recovery.RollbackPredecessorHash = swapReservation.PredecessorHash()
	recovery.ActivatedDatabaseSha256 = swapReservation.ActivatedHash()
	if _, err := journal.BindStagedRecovery(ctx, operationID, recovery); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Advance(ctx, operationID, tammyv1.RestoreState_RESTORE_STATE_STAGED,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, finalized.AuditHead); err != nil {
		t.Fatal(err)
	}
	completeRecovery := proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	completeRecovery.PostSwapVerified = true
	completeRecovery.MachineCredentialsRevoked = true
	completeRecovery.MirrorPublished = true
	completeStatus := &tammyv1.RestoreStatus{OperationId: operationID,
		State: tammyv1.RestoreState_RESTORE_STATE_COMPLETE, BackupManifestHash: append([]byte(nil), manifestHash...),
		NewAuditHead: append([]byte(nil), finalized.AuditHead...), UpdatedAt: timestamppb.Now(), Recovery: completeRecovery}
	auxiliaryOnly := func(databaseBasename, suffix string, removeMain bool) func(*testing.T, string) {
		return func(t *testing.T, clone string) {
			if removeMain {
				if err := os.Remove(filepath.Join(clone, databaseBasename)); err != nil {
					t.Fatal(err)
				}
			}
			contents := []byte("foreign auxiliary residue")
			if suffix == ".lock" {
				contents = nil
			}
			if err := os.WriteFile(filepath.Join(clone, databaseBasename+suffix), contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertRemovedOnly := func(t *testing.T, before, after map[string][sha256.Size]byte, removedNames []string) {
		t.Helper()
		removed := make(map[string]struct{}, len(removedNames))
		for _, name := range removedNames {
			removed[name] = struct{}{}
			if _, exists := before[name]; !exists {
				t.Fatalf("expected removal %q was absent before cleanup", name)
			}
			if _, exists := after[name]; exists {
				t.Fatalf("expected removal %q remains after cleanup", name)
			}
		}
		for name, beforeHash := range before {
			if _, wasRemoved := removed[name]; wasRemoved {
				continue
			}
			afterHash, exists := after[name]
			if !exists || afterHash != beforeHash {
				t.Fatalf("cleanup changed preserved file %q: before=%x after=%x exists=%t", name, beforeHash, afterHash, exists)
			}
		}
		for name := range after {
			if _, existed := before[name]; !existed {
				t.Fatalf("cleanup created unexpected file %q", name)
			}
		}
	}
	for _, test := range []struct {
		name            string
		prepare         func(*testing.T, string)
		valid           bool
		expectedRemoved []string
	}{
		{name: "foreign_exact_stage_without_markers", prepare: func(t *testing.T, clone string) {
			for _, name := range []string{completeRecovery.RollbackBasename, completeRecovery.StageBasename + ".owner",
				completeRecovery.RollbackBasename + ".owner"} {
				if err := os.Remove(filepath.Join(clone, name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(clone, completeRecovery.StageBasename), []byte("foreign exact HMAC stage"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "ambiguous_active_stage_rollback_with_valid_binding", prepare: func(t *testing.T, clone string) {
			activeBytes, err := os.ReadFile(filepath.Join(clone, "workspace.db"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(clone, completeRecovery.StageBasename), activeBytes, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "partial_marker", prepare: func(t *testing.T, clone string) {
			if err := os.Remove(filepath.Join(clone, completeRecovery.StageBasename+".owner")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "tampered_marker", prepare: func(t *testing.T, clone string) {
			path := filepath.Join(clone, completeRecovery.StageBasename+".owner")
			marker, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			marker[0] ^= 0x01
			if err := os.WriteFile(path, marker, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "stage_wal_without_main", prepare: auxiliaryOnly(completeRecovery.StageBasename, "-wal", false)},
		{name: "stage_shm_without_main", prepare: auxiliaryOnly(completeRecovery.StageBasename, "-shm", false)},
		{name: "stage_lock_without_main", prepare: auxiliaryOnly(completeRecovery.StageBasename, ".lock", false)},
		{name: "rollback_wal_without_main", prepare: auxiliaryOnly(completeRecovery.RollbackBasename, "-wal", true)},
		{name: "rollback_shm_without_main", prepare: auxiliaryOnly(completeRecovery.RollbackBasename, "-shm", true)},
		{name: "rollback_lock_without_main", prepare: auxiliaryOnly(completeRecovery.RollbackBasename, ".lock", true)},
		{name: "rollback_predecessor_sha_mismatch", prepare: func(t *testing.T, clone string) {
			path := filepath.Join(clone, completeRecovery.RollbackBasename)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(contents) == 0 {
				t.Fatal("rollback database is unexpectedly empty")
			}
			contents[len(contents)/2] ^= 0x01
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "active_truncated_after_verification", prepare: func(t *testing.T, clone string) {
			path := filepath.Join(clone, "workspace.db")
			info, err := os.Lstat(path)
			if err != nil || info.Size() < 2 {
				t.Fatalf("active info=%#v error=%v", info, err)
			}
			if err := os.Truncate(path, info.Size()/2); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "active_replaced_after_verification", prepare: func(t *testing.T, clone string) {
			path := filepath.Join(clone, "workspace.db")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("replacement active database"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "active_mutated_in_place_after_verification", prepare: func(t *testing.T, clone string) {
			path := filepath.Join(clone, "workspace.db")
			file, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteAt([]byte{0xff}, 128); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Sync(); err != nil {
				_ = file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "marker_only_crash_cleanup_is_idempotent", prepare: func(t *testing.T, clone string) {
			if err := os.Remove(filepath.Join(clone, completeRecovery.RollbackBasename)); err != nil {
				t.Fatal(err)
			}
		}, valid: true, expectedRemoved: []string{
			completeRecovery.StageBasename + ".owner",
			completeRecovery.RollbackBasename + ".owner",
		}},
		{name: "marker_only_crash_after_stage_marker_removal", prepare: func(t *testing.T, clone string) {
			for _, name := range []string{completeRecovery.RollbackBasename, completeRecovery.StageBasename + ".owner"} {
				if err := os.Remove(filepath.Join(clone, name)); err != nil {
					t.Fatal(err)
				}
			}
		}, valid: true, expectedRemoved: []string{completeRecovery.RollbackBasename + ".owner"}},
		{name: "marker_only_crash_after_rollback_marker_removal", prepare: func(t *testing.T, clone string) {
			for _, name := range []string{completeRecovery.RollbackBasename, completeRecovery.RollbackBasename + ".owner"} {
				if err := os.Remove(filepath.Join(clone, name)); err != nil {
					t.Fatal(err)
				}
			}
		}, valid: true, expectedRemoved: []string{completeRecovery.StageBasename + ".owner"}},
		{name: "normal_completed_cleanup_is_idempotent", valid: true, expectedRemoved: []string{
			completeRecovery.RollbackBasename,
			completeRecovery.StageBasename + ".owner",
			completeRecovery.RollbackBasename + ".owner",
		}},
	} {
		t.Run("complete_cleanup_"+test.name, func(t *testing.T) {
			clone := filepath.Join(t.TempDir(), "private")
			copyRestoreDirectory(t, directory, clone)
			if test.prepare != nil {
				test.prepare(t, clone)
			}
			before := snapshotRestoreDirectory(t, clone)
			cleanup, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
				ActivePath: filepath.Join(clone, "workspace.db"), StagingDirectory: clone, Key: key})
			if err != nil {
				t.Fatal(err)
			}
			cleanupErr := cleanup.CleanupCompletedRestore(ctx, completeStatus)
			if test.valid {
				if cleanupErr != nil {
					t.Fatalf("valid cleanup error=%v", cleanupErr)
				}
				afterFirst := snapshotRestoreDirectory(t, clone)
				assertRemovedOnly(t, before, afterFirst, test.expectedRemoved)
				if err := cleanup.CleanupCompletedRestore(ctx, completeStatus); err != nil {
					t.Fatalf("idempotent cleanup error=%v", err)
				}
				afterSecond := snapshotRestoreDirectory(t, clone)
				if !reflect.DeepEqual(afterSecond, afterFirst) {
					t.Fatalf("second cleanup mutated files: first=%v second=%v", afterFirst, afterSecond)
				}
				activeAfter, err := os.ReadFile(filepath.Join(clone, "workspace.db"))
				if err != nil || sha256.Sum256(activeAfter) != before["workspace.db"] {
					t.Fatalf("valid cleanup changed active: error=%v", err)
				}
			} else {
				if !errors.Is(cleanupErr, ErrSQLCipherWorkspace) {
					t.Fatalf("ambiguous cleanup error=%v, want fail closed", cleanupErr)
				}
				after := snapshotRestoreDirectory(t, clone)
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("ambiguous COMPLETE cleanup mutated files: before=%v after=%v", before, after)
				}
			}
			_ = cleanup.Close()
		})
	}
	recoveryAdapter, err := NewSQLCipherStartupRecoveryAdapter(SQLCipherStartupRecoveryAdapterConfig{
		ActivePath: activePath, StagingDirectory: directory, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	defer recoveryAdapter.Close()
	effects := &concretePostRestoreEffects{}
	calls := []string{}
	coordinator, err := NewStartupRecoveryCoordinator(StartupRecoveryConfig{Journal: journal,
		Workspace: recoveryAdapter, Archives: recoveryArchiveHarness{calls: &calls}, MachineCredentials: effects,
		Mirror: effects, BatchSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	if report, err := coordinator.Recover(ctx); err != nil || report.Completed != 1 || report.Cleaned != 1 {
		t.Fatalf("first recovery report=%#v error=%v", report, err)
	}
	if effects.revoked != 1 || effects.mirrored != 1 {
		t.Fatalf("effects after first recovery=%#v", effects)
	}
	if report, err := coordinator.Recover(ctx); err != nil || report.Completed != 0 || report.Cleaned != 1 {
		t.Fatalf("second recovery report=%#v error=%v", report, err)
	}
	if effects.revoked != 1 || effects.mirrored != 1 {
		t.Fatalf("effects replayed after COMPLETE=%#v", effects)
	}
	status, err := journal.Get(ctx, operationID)
	if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_COMPLETE ||
		!status.Recovery.PostSwapVerified || !status.Recovery.MachineCredentialsRevoked || !status.Recovery.MirrorPublished {
		t.Fatalf("completed status=%#v error=%v", status, err)
	}
	activated, err := sqlcipher.Open(ctx, activePath, key)
	if err != nil {
		t.Fatal(err)
	}
	header, err := audit.LoadChainHeader(ctx, activated, workspaceID, 0)
	closeErr := activated.Close()
	if err != nil || closeErr != nil || header.Generation != finalized.Generation || header.CurrentSequence != 1 ||
		!bytes.Equal(header.CurrentHead[:], status.NewAuditHead) {
		t.Fatalf("reopened active header=%#v load_error=%v close_error=%v journal_head=%x",
			header, err, closeErr, status.NewAuditHead)
	}
	if _, err := os.Lstat(filepath.Join(directory, recovery.RollbackBasename)); !os.IsNotExist(err) {
		t.Fatalf("rollback residue error=%v", err)
	}
}
