//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/audit"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type concreteProofVerifierFunc func(context.Context, string, RestoreProof) (*RestoreAuthorization, error)

func (function concreteProofVerifierFunc) AuthorizeRestore(ctx context.Context, workspaceID string, proof RestoreProof) (*RestoreAuthorization, error) {
	return function(ctx, workspaceID, proof)
}

type concreteValidatorFunc func(context.Context, ValidationInput) error

func (function concreteValidatorFunc) Validate(ctx context.Context, input ValidationInput) error {
	return function(ctx, input)
}

func reserveSQLCipherTestArtifacts(t *testing.T, ctx context.Context, adapter *SQLCipherWorkspaceAdapter,
	operationID, workspaceID string,
) *RestoreArtifactReservation {
	t.Helper()
	artifacts, err := adapter.ReserveRestoreArtifacts(ctx, operationID, workspaceID)
	if err != nil {
		t.Fatalf("ReserveRestoreArtifacts() error=%v", err)
	}
	return artifacts
}

func reserveSQLCipherTestSwap(t *testing.T, ctx context.Context, adapter *SQLCipherWorkspaceAdapter,
	operationID, workspaceID string, verified *VerifiedStagedWorkspace,
) *RestoreSwapReservation {
	t.Helper()
	reservation, err := adapter.ReserveSwap(ctx, operationID, workspaceID, verified)
	if err != nil {
		t.Fatalf("ReserveSwap() error=%v", err)
	}
	return reservation
}

func verifiedSQLCipherStageForSwapTest(
	t *testing.T,
	adapter *SQLCipherWorkspaceAdapter,
	finalized *FinalizedRestore,
) *VerifiedStagedWorkspace {
	t.Helper()
	stageName := filepath.Base(finalized.Staged.stagedPath)
	identity, err := adapter.root.Lstat(stageName)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashVerifiedStagedDatabase(context.Background(), adapter.root, stageName, identity)
	if err != nil {
		t.Fatal(err)
	}
	return &VerifiedStagedWorkspace{Finalized: finalized, verificationAuthority: adapter,
		stagedIdentity: identity, stagedSHA256: hash}
}

type concretePreRestoreArchiver struct {
	created int
	aborted int
}

func (archiver *concretePreRestoreArchiver) PrepareVerifiedPreRestoreArchive(context.Context, PreRestoreArchiveRequest) (*PreRestoreArchive, error) {
	archiver.created++
	createdAt := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	return &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
		Version: 1, SHA256: bytes.Repeat([]byte{0x61}, sha256.Size), CreatedAt: createdAt,
		DeletionEligibleAt: createdAt.AddDate(1, 0, 0), SourceGeneration: 5, EncryptedByteLength: 4096}, nil
}

func (archiver *concretePreRestoreArchiver) PublishPreRestoreArchive(context.Context, *PreRestoreArchive, *PreparedArchiveBinding) error {
	return nil
}

func (archiver *concretePreRestoreArchiver) AbortPreRestoreArchive(context.Context, *PreRestoreArchive) error {
	archiver.aborted++
	return nil
}

type concretePostRestoreEffects struct {
	revoked  int
	mirrored int
}

func (effects *concretePostRestoreEffects) RevokeMachineCredentials(context.Context, MachineCredentialRevocationRequest) error {
	effects.revoked++
	return nil
}

func (effects *concretePostRestoreEffects) PublishRestoredMirror(context.Context, *FinalizedRestore) error {
	effects.mirrored++
	return nil
}

func TestSQLCipherWorkspaceAdapterStagesAndAtomicallySwaps(t *testing.T) {
	ctx := context.Background()
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	restoredSourcePath := filepath.Join(t.TempDir(), "restored.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active")
	restored := createRestoreDatabaseFixture(t, ctx, restoredSourcePath, key, "restored")
	schemaVersion, migrationHash := restoreSchemaMetadata(t, ctx, restored)
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	restoredBytes, err := os.ReadFile(restoredSourcePath)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
		StagingDirectory: directory, Key: key,
		NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
		NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
		NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
		Now:                    func() time.Time { return time.Unix(1_720_000_000, 0).UTC() },
		Random:                 bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)),
		AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size)})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1,
		WorkspaceId: "018f0000-0000-7000-8000-000000000001", SchemaVersion: schemaVersion,
		MigrationManifestHash: migrationHash}
	operationID := "018f0000-0000-7000-8000-000000000099"
	artifacts := reserveSQLCipherTestArtifacts(t, ctx, adapter, operationID, manifest.WorkspaceId)
	staged, err := adapter.Stage(ctx, StageRequest{OperationID: operationID,
		WorkspaceID: manifest.WorkspaceId, Manifest: manifest, ManifestHash: make([]byte, sha256.Size),
		Artifacts: artifacts,
		Objects:   []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: restoredBytes}}})
	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	finalized := &FinalizedRestore{WorkspaceID: manifest.WorkspaceId, Generation: 2,
		AuditHead: make([]byte, sha256.Size), EventType: tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED, Staged: staged}
	verified := verifiedSQLCipherStageForSwapTest(t, adapter, finalized)
	swapReservation := reserveSQLCipherTestSwap(t, ctx, adapter, operationID, manifest.WorkspaceId, verified)
	receipt, err := adapter.Swap(ctx, SwapRequest{OperationID: operationID,
		Verified: verified, Reservation: swapReservation, PreRestoreArchive: &PreRestoreArchive{
			ArchiveID: "018f0000-0000-7000-8000-000000000088", SHA256: make([]byte, sha256.Size)}})
	if err != nil {
		t.Fatalf("Swap() error = %v", err)
	}
	assertRestoreMarker(t, ctx, activePath, key, "restored")
	if err := adapter.Rollback(ctx, receipt); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	assertRestoreMarker(t, ctx, activePath, key, "active")
}

func TestSQLCipherReserveSwapRejectsStageChangedAfterSemanticVerification(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "replaced_with_identical_bytes", mutate: func(t *testing.T, path string) {
			t.Helper()
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mutated_in_place", mutate: func(t *testing.T, path string) {
			t.Helper()
			file, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteAt([]byte("changed-after-verification"), 64); err != nil {
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newVerifiedSQLCipherStageFixture(t)
			test.mutate(t, fixture.staged.stagedPath)
			mutated, err := os.ReadFile(fixture.staged.stagedPath)
			if err != nil {
				t.Fatal(err)
			}
			reservation, reserveErr := fixture.adapter.ReserveSwap(context.Background(), fixture.operationID,
				fixture.workspaceID, fixture.verified)
			if reservation != nil {
				_ = fixture.adapter.ReleaseSwap(context.Background(), reservation)
			}
			if reservation != nil || !errors.Is(reserveErr, ErrSQLCipherWorkspace) {
				t.Fatalf("ReserveSwap() reservation=%#v error=%v, want fail closed", reservation, reserveErr)
			}
			after, err := os.ReadFile(fixture.staged.stagedPath)
			if err != nil || !bytes.Equal(after, mutated) {
				t.Fatalf("failed reservation mutated staged bytes: before=%x after=%x error=%v",
					sha256.Sum256(mutated), sha256.Sum256(after), err)
			}
		})
	}
}

func TestSQLCipherCommitSwapRejectsActivatedMutationAfterPostVerificationWithoutFilesystemAction(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "truncate", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Truncate(path, 31); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "same_inode_mutation", mutate: func(t *testing.T, path string) {
			t.Helper()
			file, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteAt([]byte("changed-after-post-verification"), 64); err != nil {
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newVerifiedSQLCipherStageFixture(t)
			ctx := context.Background()
			reservation, err := fixture.adapter.ReserveSwap(ctx, fixture.operationID, fixture.workspaceID, fixture.verified)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := fixture.adapter.Swap(ctx, SwapRequest{OperationID: fixture.operationID,
				Verified: fixture.verified, Reservation: reservation,
				PreRestoreArchive: &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
					SHA256: bytes.Repeat([]byte{0x61}, sha256.Size)}})
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.adapter.VerifyActivated(ctx, ActivatedVerificationRequest{OperationID: fixture.operationID,
				Verified: fixture.verified, Receipt: receipt}); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, fixture.adapter.activePath)
			before := snapshotRestoreDirectory(t, fixture.adapter.directory)
			if err := fixture.adapter.CommitSwap(ctx, receipt); !errors.Is(err, ErrSQLCipherWorkspace) {
				t.Fatalf("CommitSwap() error=%v, want %v", err, ErrSQLCipherWorkspace)
			}
			after := snapshotRestoreDirectory(t, fixture.adapter.directory)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("CommitSwap mutated directory\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestSQLCipherStableActiveLockBlocksCommitAcrossProcesses(t *testing.T) {
	if os.Getenv("TAMMY_ACTIVE_LOCK_HOLDER") == "1" {
		runSQLCipherActiveLockHolder(t)
		return
	}
	fixture := newVerifiedSQLCipherStageFixture(t)
	ctx := context.Background()
	reservation, err := fixture.adapter.ReserveSwap(ctx, fixture.operationID, fixture.workspaceID, fixture.verified)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := fixture.adapter.Swap(ctx, SwapRequest{OperationID: fixture.operationID,
		Verified: fixture.verified, Reservation: reservation,
		PreRestoreArchive: &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
			SHA256: bytes.Repeat([]byte{0x61}, sha256.Size)}})
	if err != nil {
		t.Fatal(err)
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	releaseRead, releaseWrite, err := os.Pipe()
	if err != nil {
		_ = readyRead.Close()
		_ = readyWrite.Close()
		t.Fatal(err)
	}
	childCtx, cancelChild := context.WithTimeout(ctx, 15*time.Second)
	defer cancelChild()
	command := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestSQLCipherStableActiveLockBlocksCommitAcrossProcesses$")
	command.Env = append(os.Environ(), "TAMMY_ACTIVE_LOCK_HOLDER=1", "TAMMY_ACTIVE_DATABASE_PATH="+fixture.adapter.activePath)
	command.ExtraFiles = []*os.File{readyWrite, releaseRead}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_ = readyWrite.Close()
	_ = releaseRead.Close()
	var ready [1]byte
	if _, err := io.ReadFull(readyRead, ready[:]); err != nil || ready[0] != 1 {
		_ = releaseWrite.Close()
		_ = command.Wait()
		t.Fatalf("lock holder readiness=%x error=%v", ready, err)
	}
	_ = readyRead.Close()
	if err := fixture.adapter.VerifyActivated(ctx, ActivatedVerificationRequest{OperationID: fixture.operationID,
		Verified: fixture.verified, Receipt: receipt}); err != nil {
		_ = releaseWrite.Close()
		_ = command.Wait()
		t.Fatal(err)
	}
	before := snapshotRestoreDirectory(t, fixture.adapter.directory)
	if err := fixture.adapter.CommitSwap(ctx, receipt); !errors.Is(err, ErrSQLCipherWorkspace) {
		_ = releaseWrite.Close()
		_ = command.Wait()
		t.Fatalf("CommitSwap while shared holder active error=%v", err)
	}
	after := snapshotRestoreDirectory(t, fixture.adapter.directory)
	if !reflect.DeepEqual(after, before) {
		_ = releaseWrite.Close()
		_ = command.Wait()
		t.Fatalf("blocked commit mutated directory\nbefore=%#v\nafter=%#v", before, after)
	}
	if err := releaseWrite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lock holder exit=%v", err)
	}
	if err := fixture.adapter.CommitSwap(ctx, receipt); err != nil {
		t.Fatalf("CommitSwap after holder close error=%v", err)
	}
	lockInfo, err := os.Lstat(fixture.adapter.activePath + ".lock")
	if err != nil || !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm() != 0o600 || lockInfo.Size() != 0 {
		t.Fatalf("stable active lock info=%#v error=%v", lockInfo, err)
	}
}

func runSQLCipherActiveLockHolder(t *testing.T) {
	t.Helper()
	key := bytes.Repeat([]byte{0x29}, sqlcipher.KeySize)
	defer zeroBytes(key)
	database, err := sqlcipher.Open(context.Background(), os.Getenv("TAMMY_ACTIVE_DATABASE_PATH"), key)
	if err != nil {
		t.Fatal(err)
	}
	ready := os.NewFile(3, "ready")
	release := os.NewFile(4, "release")
	if ready == nil || release == nil {
		_ = database.Close()
		t.Fatal("missing lock-holder pipes")
	}
	if _, err := ready.Write([]byte{1}); err != nil || ready.Close() != nil {
		_ = release.Close()
		_ = database.Close()
		t.Fatalf("ready signal error=%v", err)
	}
	_, releaseErr := io.Copy(io.Discard, release)
	closeReleaseErr := release.Close()
	closeDatabaseErr := database.Close()
	if releaseErr != nil || closeReleaseErr != nil || closeDatabaseErr != nil {
		t.Fatalf("release=%v close-release=%v close-database=%v", releaseErr, closeReleaseErr, closeDatabaseErr)
	}
}

type verifiedSQLCipherStageFixture struct {
	adapter     *SQLCipherWorkspaceAdapter
	staged      *StagedWorkspace
	verified    *VerifiedStagedWorkspace
	operationID string
	workspaceID string
}

func newVerifiedSQLCipherStageFixture(t *testing.T) verifiedSQLCipherStageFixture {
	t.Helper()
	ctx := context.Background()
	const workspaceID = "018f0000-0000-7000-8000-000000000001"
	const operationID = "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	archivedPath := filepath.Join(t.TempDir(), "archived.db")
	key := bytes.Repeat([]byte{0x29}, sqlcipher.KeySize)
	t.Cleanup(func() { zeroBytes(key) })
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
	adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
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
	t.Cleanup(func() { _ = adapter.Close() })
	manifestHash := bytes.Repeat([]byte{0x62}, sha256.Size)
	auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: schemaVersion, MigrationManifestHash: migrationHash, AuditGeneration: 2,
		AuditHead: archivedHead[:], AuditRoot: auditRoot[:], SigningKeyId: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch}
	artifacts := reserveSQLCipherTestArtifacts(t, ctx, adapter, operationID, workspaceID)
	staged, err := adapter.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: manifestHash, Artifacts: artifacts, Objects: []backup.Object{{
			Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1, Bytes: archivedBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	preArchiveCreatedAt := createdAt.Add(30 * time.Minute)
	preArchive := &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088", Version: 1,
		SHA256: bytes.Repeat([]byte{0x61}, sha256.Size), CreatedAt: preArchiveCreatedAt,
		DeletionEligibleAt: preArchiveCreatedAt.AddDate(1, 0, 0), SourceGeneration: 5, EncryptedByteLength: 4096}
	authorization := &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
		WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)}
	finalized, err := adapter.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{OperationID: operationID,
		WorkspaceID: workspaceID, NewGeneration: 6, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, PreRestoreArchive: preArchive, Staged: staged})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := adapter.VerifyStaged(ctx, StagedVerificationRequest{OperationID: operationID,
		WorkspaceID: workspaceID, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, Finalized: finalized})
	if err != nil {
		t.Fatal(err)
	}
	return verifiedSQLCipherStageFixture{adapter: adapter, staged: staged, verified: verified,
		operationID: operationID, workspaceID: workspaceID}
}

func TestSQLCipherStagedFinalizerCreatesGenerationAndRestoreEvent(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	restoredSourcePath := filepath.Join(t.TempDir(), "archived.db")
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active")
	archived := createRestoreDatabaseFixture(t, ctx, restoredSourcePath, key, "archived")
	createdAt := time.Unix(1_710_000_000, 0).UTC()
	archivedHead, signingKey := seedArchivedAuditAndSessions(t, ctx, archived, workspaceID, key, createdAt)
	schemaVersion, migrationHash := restoreSchemaMetadata(t, ctx, archived)
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archived.Close(); err != nil {
		t.Fatal(err)
	}
	archivedBytes, err := os.ReadFile(restoredSourcePath)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
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
	defer adapter.Close()
	manifestHash := bytes.Repeat([]byte{0x62}, sha256.Size)
	auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: schemaVersion, MigrationManifestHash: migrationHash, AuditGeneration: 2,
		AuditHead: archivedHead[:], AuditRoot: auditRoot[:], SigningKeyId: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch}
	artifacts := reserveSQLCipherTestArtifacts(t, ctx, adapter, operationID, workspaceID)
	staged, err := adapter.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: manifestHash, Artifacts: artifacts, Objects: []backup.Object{{Path: "database/workspace.db",
			Provider: "workspace", ProviderVersion: 1, Bytes: archivedBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	predecessorHead := bytes.Repeat([]byte{0x50}, sha256.Size)
	preArchive := &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
		Version: 1, SHA256: bytes.Repeat([]byte{0x61}, sha256.Size), CreatedAt: createdAt.Add(30 * time.Minute),
		DeletionEligibleAt: createdAt.Add(30*time.Minute).AddDate(1, 0, 0), SourceGeneration: 5,
		EncryptedByteLength: 4096}
	authorization := &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
		WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: predecessorHead}
	wrongLineageManifest := proto.Clone(manifest).(*tammyv1.BackupArchiveManifest)
	wrongLineageManifest.SigningKeyId = "018f0000-0000-7000-8000-000000000099"
	if finalized, err := adapter.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{OperationID: operationID,
		WorkspaceID: workspaceID, NewGeneration: 6, Manifest: wrongLineageManifest, ManifestHash: manifestHash,
		Authorization: authorization, PreRestoreArchive: preArchive, Staged: staged}); finalized != nil || !errors.Is(err, ErrSQLCipherWorkspace) {
		t.Fatalf("wrong signing lineage finalized=%#v error=%v, want fail closed", finalized, err)
	}
	finalized, err := adapter.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{OperationID: operationID,
		WorkspaceID: workspaceID, NewGeneration: 6, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, PreRestoreArchive: preArchive, Staged: staged})
	if err != nil {
		t.Fatalf("FinalizeStagedWorkspace() error = %v", err)
	}
	verified, err := adapter.VerifyStaged(ctx, StagedVerificationRequest{OperationID: operationID,
		WorkspaceID: workspaceID, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, Finalized: finalized})
	if err != nil || verified == nil || verified.Finalized != finalized {
		t.Fatalf("VerifyStaged() = %#v, %v", verified, err)
	}
	stagedDatabase, err := sqlcipher.Open(ctx, staged.stagedPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedDatabase.Close()
	if err := backup.VerifySnapshotExclusions(ctx, stagedDatabase); err != nil {
		t.Fatalf("restored sessions were not invalidated: %v", err)
	}
	header, err := audit.LoadChainHeader(ctx, stagedDatabase, workspaceID, 0)
	if err != nil || header.Generation != 6 || header.CurrentSequence != 1 || !bytes.Equal(header.CurrentHead[:], finalized.AuditHead) {
		t.Fatalf("restored header=%#v error=%v", header, err)
	}
	events, err := audit.LoadStoredEvents(ctx, stagedDatabase, workspaceID, 6, 1, 1)
	if err != nil || len(events) != 1 || events[0].Event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_RESTORED {
		t.Fatalf("restored events=%#v error=%v", events, err)
	}
	if verification := audit.VerifyStoredChain(header, events); verification.Integrity != tammyv1.AuditChainIntegrity_AUDIT_CHAIN_INTEGRITY_VALID ||
		!bytes.Equal(verification.VerifiedHead, finalized.AuditHead) {
		t.Fatalf("audit verification=%#v", verification)
	}
	payload := events[0].Event.GetPayload().GetWorkspaceRestored()
	if payload == nil || payload.OperationId != operationID || payload.PredecessorGeneration != 5 ||
		payload.BackupGeneration != 2 || payload.RestoredGeneration != 6 || !bytes.Equal(payload.PredecessorHead, predecessorHead) ||
		!bytes.Equal(payload.ArchivedHead, archivedHead[:]) || !bytes.Equal(payload.BackupManifestHash, manifestHash) ||
		payload.PreRestoreArchiveId != preArchive.ArchiveID || !bytes.Equal(payload.PreRestoreArchiveHash, preArchive.SHA256) {
		t.Fatalf("restore payload=%#v", payload)
	}
	history, err := audit.LoadSigningKeyHistory(ctx, stagedDatabase, workspaceID)
	if err != nil || len(history) != 1 || history[0].RetiredAt != nil {
		t.Fatalf("signing history=%#v error=%v", history, err)
	}
	cursorCodec, err := paging.NewCodec(bytes.Repeat([]byte{0x46}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPreRestoreArchiveRepository(stagedDatabase, cursorCodec)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := repository.Get(ctx, workspaceID, preArchive.ArchiveID)
	if err != nil || retained.Version != 1 || retained.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE ||
		retained.SourceGeneration != 5 || !bytes.Equal(retained.ContentHash, preArchive.SHA256) {
		t.Fatalf("retained archive metadata=%#v error=%v", retained, err)
	}
}

func TestSQLCipherStagedFinalizerRollsBackMidMutation(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	archivedPath := filepath.Join(t.TempDir(), "archived.db")
	key := bytes.Repeat([]byte{0x21}, sqlcipher.KeySize)
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
	injected := errors.New("mid-staged-transaction")
	adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
		StagingDirectory: directory, Key: key,
		NewID:                  func() (string, error) { return "018f0000-0000-7000-8000-000000000055", nil },
		NewReceiptID:           func() (string, error) { return "018f0000-0000-7000-8000-000000000066", nil },
		NewEventID:             func() (string, error) { return "018f0000-0000-7000-8000-000000000067", nil },
		Now:                    func() time.Time { return createdAt.Add(time.Hour) },
		Random:                 bytes.NewReader(bytes.Repeat([]byte{0x73}, 128)),
		AuditSchemaFingerprint: bytes.Repeat([]byte{0x72}, sha256.Size),
		hooks:                  &sqlcipherWorkspaceHooks{afterStagedMutation: func() error { return injected }}})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	manifestHash := bytes.Repeat([]byte{0x62}, sha256.Size)
	auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: schemaVersion, MigrationManifestHash: migrationHash, AuditGeneration: 2, AuditHead: archivedHead[:],
		AuditRoot: auditRoot[:], SigningKeyId: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch}
	artifacts := reserveSQLCipherTestArtifacts(t, ctx, adapter, operationID, workspaceID)
	staged, err := adapter.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: manifestHash, Artifacts: artifacts, Objects: []backup.Object{{Path: "database/workspace.db",
			Provider: "workspace", ProviderVersion: 1, Bytes: archivedBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	preArchiveCreatedAt := createdAt.Add(30 * time.Minute)
	before, err := os.ReadFile(staged.stagedPath)
	if err != nil {
		t.Fatal(err)
	}
	authorization := &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
		WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)}
	finalized, err := adapter.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{OperationID: operationID,
		WorkspaceID: workspaceID, NewGeneration: 6, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: authorization, PreRestoreArchive: &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
			Version: 1, SHA256: bytes.Repeat([]byte{0x61}, sha256.Size), CreatedAt: preArchiveCreatedAt,
			DeletionEligibleAt: preArchiveCreatedAt.AddDate(1, 0, 0), SourceGeneration: 5,
			EncryptedByteLength: 4096}, Staged: staged})
	if finalized != nil || !errors.Is(err, injected) {
		t.Fatalf("finalized=%#v error=%v, want injected rollback", finalized, err)
	}
	after, err := os.ReadFile(staged.stagedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("rolled-back staged bytes changed: before=%x after=%x", sha256.Sum256(before), sha256.Sum256(after))
	}
	database, err := sqlcipher.Open(ctx, staged.stagedPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if header, err := audit.LoadChainHeader(ctx, database, workspaceID, 0); err != nil || header.Generation != 2 {
		t.Fatalf("latest header after rollback=%#v error=%v", header, err)
	}
	var sessions int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM application_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions after rollback=%d error=%v", sessions, err)
	}
}

func TestSQLCipherStagedFinalizerStreamsLargeArchivedChain(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	archivedPath := filepath.Join(t.TempDir(), "archived.db")
	key := bytes.Repeat([]byte{0x31}, sqlcipher.KeySize)
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active")
	archived := createRestoreDatabaseFixture(t, ctx, archivedPath, key, "archived")
	createdAt := time.Unix(1_710_000_000, 0).UTC()
	genesis, signingKey := seedArchivedAuditAndSessions(t, ctx, archived, workspaceID, key, createdAt)
	archivedHead := appendArchivedAuditEvents(t, ctx, archived, workspaceID, 2, genesis, createdAt, 300)
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
	adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
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
	defer adapter.Close()
	manifestHash := bytes.Repeat([]byte{0x62}, sha256.Size)
	auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &tammyv1.BackupArchiveManifest{Format: backup.FormatV1, WorkspaceId: workspaceID,
		SchemaVersion: schemaVersion, MigrationManifestHash: migrationHash, AuditGeneration: 2,
		AuditSequence: 300, AuditHead: archivedHead[:], AuditRoot: auditRoot[:], SigningKeyId: signingKey.KeyID,
		SigningKeyEpoch: signingKey.Epoch}
	artifacts := reserveSQLCipherTestArtifacts(t, ctx, adapter, operationID, workspaceID)
	staged, err := adapter.Stage(ctx, StageRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Manifest: manifest, ManifestHash: manifestHash, Artifacts: artifacts, Objects: []backup.Object{{Path: "database/workspace.db",
			Provider: "workspace", ProviderVersion: 1, Bytes: archivedBytes}}})
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := adapter.FinalizeStagedWorkspace(ctx, FinalizeStagedRestoreRequest{OperationID: operationID,
		WorkspaceID: workspaceID, NewGeneration: 6, Manifest: manifest, ManifestHash: manifestHash,
		Authorization: &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
			WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)},
		PreRestoreArchive: &PreRestoreArchive{ArchiveID: "018f0000-0000-7000-8000-000000000088",
			Version: 1, SHA256: bytes.Repeat([]byte{0x61}, sha256.Size), CreatedAt: createdAt.Add(30 * time.Minute),
			DeletionEligibleAt: createdAt.Add(30*time.Minute).AddDate(1, 0, 0), SourceGeneration: 5,
			EncryptedByteLength: 4096}, Staged: staged})
	if err != nil || finalized == nil || finalized.Generation != 6 {
		t.Fatalf("large-chain finalize=%#v error=%v", finalized, err)
	}
}

func TestRestoreServiceActivatesVerifiedSQLCipherWorkspaceEndToEnd(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(directory, "workspace.db")
	archivedPath := filepath.Join(t.TempDir(), "archived.db")
	key := bytes.Repeat([]byte{0x41}, sqlcipher.KeySize)
	defer zeroBytes(key)
	active := createRestoreDatabaseFixture(t, ctx, activePath, key, "active")
	archived := createRestoreDatabaseFixture(t, ctx, archivedPath, key, "archived")
	createdAt := time.Unix(1_710_000_000, 0).UTC()
	archivedHead, signingKey := seedArchivedAuditAndSessions(t, ctx, archived, workspaceID, key, createdAt)
	schemaVersion, migrationHash := restoreSchemaMetadata(t, ctx, archived)
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	activeBytes, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := archived.Close(); err != nil {
		t.Fatal(err)
	}
	archivedBytes, err := os.ReadFile(archivedPath)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := audit.DecryptSigningKey(signingKey, key)
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Zero(privateKey)
	auditRoot, err := audit.SigningLineageRootFingerprint(workspaceID, signingKey.KeyID, signingKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("correct horse battery staple")
	archive, err := backup.Seal(backup.ArchiveInput{WorkspaceID: workspaceID, SchemaVersion: schemaVersion,
		AppVersion: "0.1.0", AuditGeneration: 2, AuditSequence: 0, AuditHead: archivedHead[:], AuditRoot: auditRoot[:],
		SigningKeyID: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch,
		WorkspaceHeaderHash: bytes.Repeat([]byte{0x44}, sha256.Size), MigrationManifestHash: migrationHash,
		Objects: []backup.Object{{Path: "database/workspace.db", Provider: "workspace", ProviderVersion: 1,
			Bytes: archivedBytes}}}, passphrase, privateKey, bytes.NewReader(bytes.Repeat([]byte{0x7d}, 256)))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewSQLCipherWorkspaceAdapter(SQLCipherWorkspaceAdapterConfig{ActivePath: activePath,
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
	defer adapter.Close()
	journal, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: bytes.Repeat([]byte{0x71}, sha256.Size),
		Now: func() time.Time { return createdAt.Add(time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	registry, err := NewProviderRegistry([]ProviderRegistration{{Name: "workspace", Version: 1,
		Validator: concreteValidatorFunc(func(_ context.Context, input ValidationInput) error {
			if len(input.Objects) != 1 || input.Objects[0].Path != "database/workspace.db" {
				return ErrProviderRegistry
			}
			return nil
		})}})
	if err != nil {
		t.Fatal(err)
	}
	preArchiveCreatedAt := createdAt.Add(30 * time.Minute)
	preArchives, err := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{Directory: directory, DEK: key,
		Snapshots: preRestoreSnapshotSourceFunc(func(_ context.Context, gotWorkspaceID string, authorization *RestoreAuthorization) ([]byte, error) {
			if gotWorkspaceID != workspaceID || authorization.CurrentGeneration != 5 {
				return nil, ErrPreRestoreArchive
			}
			return append([]byte(nil), activeBytes...), nil
		}),
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000088", nil },
		Now:   func() time.Time { return preArchiveCreatedAt }, Random: bytes.NewReader(bytes.Repeat([]byte{0x74}, 256))})
	if err != nil {
		t.Fatal(err)
	}
	defer preArchives.Close()
	effects := &concretePostRestoreEffects{}
	predecessorHead := bytes.Repeat([]byte{0x50}, sha256.Size)
	service, err := NewService(ServiceConfig{
		Proofs: concreteProofVerifierFunc(func(_ context.Context, gotWorkspaceID string, _ RestoreProof) (*RestoreAuthorization, error) {
			return &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
				WorkspaceID: gotWorkspaceID, CurrentGeneration: 5, CurrentAuditHead: predecessorHead}, nil
		}),
		Trust: trustResolverFunc(func(context.Context, string) (backup.TrustAnchor, error) {
			return backup.TrustAnchor{WorkspaceID: workspaceID, AuditGeneration: 2, AuditRoot: auditRoot[:],
				SigningKeyID: signingKey.KeyID, SigningKeyEpoch: signingKey.Epoch, PublicKey: signingKey.PublicKey}, nil
		}),
		Providers: registry, Journal: journal, PreRestoreArchives: preArchives, Stager: adapter,
		StagedFinalizer: adapter, StagedVerifier: adapter, Swapper: adapter, PostSwapVerifier: adapter,
		MachineCredentials: effects, Mirror: effects,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Restore(ctx, RestoreRequest{OperationID: operationID, WorkspaceID: workspaceID,
		Archive: archive, Passphrase: passphrase, Proof: &AdminTOTPProof{
			AdminUserID: "018f0000-0000-7000-8000-000000000010", Password: []byte("administrator-password"),
			TOTP: "123456", IssuedAt: createdAt, ReplayKey: "018f0000-0000-7000-8000-000000000011"}})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if result == nil || result.Generation != 6 || result.PreRestoreArchiveID != "018f0000-0000-7000-8000-000000000088" ||
		len(result.AuditHead) != sha256.Size || effects.revoked != 1 || effects.mirrored != 1 {
		t.Fatalf("result=%#v effects=%#v", result, effects)
	}
	activated, err := sqlcipher.Open(ctx, activePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer activated.Close()
	if err := backup.VerifySnapshotExclusions(ctx, activated); err != nil {
		t.Fatalf("activated exclusions: %v", err)
	}
	header, err := audit.LoadChainHeader(ctx, activated, workspaceID, 0)
	if err != nil || header.Generation != 6 || header.CurrentSequence != 1 || !bytes.Equal(header.CurrentHead[:], result.AuditHead) {
		t.Fatalf("activated header=%#v error=%v", header, err)
	}
	status, err := journal.Get(ctx, operationID)
	if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_COMPLETE || !bytes.Equal(status.NewAuditHead, result.AuditHead) {
		t.Fatalf("journal status=%#v error=%v", status, err)
	}
	cursorCodec, err := paging.NewCodec(bytes.Repeat([]byte{0x46}, 32))
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPreRestoreArchiveRepository(activated, cursorCodec)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := repository.Get(ctx, workspaceID, result.PreRestoreArchiveID)
	if err != nil || retained.SourceGeneration != 5 || !retained.CreatedAt.AsTime().Equal(preArchiveCreatedAt) {
		t.Fatalf("retained prearchive=%#v error=%v", retained, err)
	}
	retainedBytes, err := os.ReadFile(filepath.Join(directory, preRestoreArchiveName(result.PreRestoreArchiveID)))
	if err != nil || len(retainedBytes) == 0 {
		t.Fatalf("retained prearchive file bytes=%d error=%v", len(retainedBytes), err)
	}
	retainedHash := sha256.Sum256(retainedBytes)
	if !bytes.Equal(retained.ContentHash, retainedHash[:]) {
		t.Fatalf("retained file hash=%x metadata=%x", retainedHash, retained.ContentHash)
	}
	openedPreArchive, err := OpenPreRestoreArchive(retainedBytes, key, workspaceID, result.PreRestoreArchiveID)
	if err != nil || !bytes.Equal(openedPreArchive.Predecessor, activeBytes) {
		t.Fatalf("retained predecessor=%#v error=%v", openedPreArchive, err)
	}
	zeroBytes(openedPreArchive.Predecessor)
	for _, pattern := range []string{".tammy-restore-stage-*", ".tammy-restore-rollback-*", ".tammy-restore-stage-*.lock"} {
		if matches, err := filepath.Glob(filepath.Join(directory, pattern)); err != nil || len(matches) != 0 {
			t.Fatalf("restore residue pattern=%q matches=%q error=%v", pattern, matches, err)
		}
	}
}

func TestRestoredSigningHistoryAllowsMoreThan256RetainedKeys(t *testing.T) {
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	rootSeed := sha256.Sum256([]byte("retained signing root"))
	rootPublic := ed25519.NewKeyFromSeed(rootSeed[:]).Public().(ed25519.PublicKey)
	retiredAt := time.Unix(1_710_000_000, 0).UTC()
	history := make([]audit.SigningKeyRecord, 300)
	for index := range history {
		history[index] = audit.SigningKeyRecord{KeyID: fmt.Sprintf("retained-key-%03d", index+1), WorkspaceID: workspaceID,
			Generation: 2, Epoch: uint64(index + 1), PublicKey: append(ed25519.PublicKey(nil), rootPublic...)}
		if index > 0 {
			history[index].PreviousKeyID = history[index-1].KeyID
		}
		if index < len(history)-1 {
			history[index].RetiredAt = &retiredAt
		}
	}
	root, err := audit.SigningLineageRootFingerprint(workspaceID, history[0].KeyID, history[0].PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if !validRestoredSigningHistory(history, workspaceID, 2, history[len(history)-1].KeyID,
		history[len(history)-1].Epoch, root[:]) {
		t.Fatal("300-key authenticated retained history was rejected by restore preflight")
	}
}

func TestVerifyArchivedAuditStateRejectsCorruptEventBody(t *testing.T) {
	ctx := context.Background()
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	path := filepath.Join(t.TempDir(), "corrupt-archived.db")
	key := bytes.Repeat([]byte{0x52}, sqlcipher.KeySize)
	defer zeroBytes(key)
	database := createRestoreDatabaseFixture(t, ctx, path, key, "archived")
	defer database.Close()
	createdAt := time.Unix(1_710_000_000, 0).UTC()
	genesis, _ := seedArchivedAuditAndSessions(t, ctx, database, workspaceID, key, createdAt)
	head := appendArchivedAuditEvents(t, ctx, database, workspaceID, 2, genesis, createdAt, 1, 1)
	err := verifyArchivedAuditState(ctx, database, &tammyv1.BackupArchiveManifest{WorkspaceId: workspaceID,
		AuditGeneration: 2, AuditSequence: 1, AuditHead: head[:]})
	if !errors.Is(err, ErrSQLCipherWorkspace) {
		t.Fatalf("verify corrupt archived body error=%v, want generic SQLCipher restore failure", err)
	}
}

func createRestoreDatabaseFixture(t *testing.T, ctx context.Context, path string, key []byte, marker string) *sqlcipher.Database {
	t.Helper()
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 4); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE restore_fixture_marker(value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO restore_fixture_marker(value) VALUES(?)`, marker); err != nil {
		t.Fatal(err)
	}
	if err := database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	return database
}

func seedArchivedAuditAndSessions(t *testing.T, ctx context.Context, database *sqlcipher.Database,
	workspaceID string, key []byte, createdAt time.Time) ([sha256.Size]byte, audit.SigningKeyRecord) {
	t.Helper()
	transaction, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	salt := bytes.Repeat([]byte{0x22}, sha256.Size)
	genesis, err := audit.Genesis(workspaceID, salt)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.InitializeChain(ctx, transaction, audit.ChainHeader{WorkspaceID: workspaceID, Generation: 2,
		ChainSalt: salt, GenesisHash: genesis, CurrentHead: genesis, CreatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}
	signingKey, _, err := audit.GenerateSigningKey(workspaceID, key, createdAt,
		bytes.NewReader(bytes.Repeat([]byte{0x64}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.PersistSigningKey(ctx, transaction, signingKey); err != nil {
		t.Fatal(err)
	}
	if err := audit.InitializeSigningKeyState(ctx, transaction, signingKey); err != nil {
		t.Fatal(err)
	}
	userID := "018f0000-0000-7000-8000-000000000010"
	sessionID := "018f0000-0000-7000-8000-000000000012"
	assertionID := "018f0000-0000-7000-8000-000000000013"
	stamp := createdAt.Format(time.RFC3339Nano)
	if _, err := transaction.ExecContext(ctx, `INSERT INTO users(id,email,display_name,status,created_at,updated_at)
		VALUES(?, 'admin@example.test', 'Admin', 'ACTIVE', ?, ?)`, userID, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO application_sessions(id,user_id,state,created_at,last_active_at,expires_at)
		VALUES(?,?,1,?,?,?)`, sessionID, userID, stamp, stamp, createdAt.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE users SET activation_session_id=? WHERE id=?`, sessionID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO factor_assertions(id,user_id,session_id,purpose,asserted_at,consumed)
		VALUES(?,?,?,'restore',?,0)`, assertionID, userID, sessionID, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO command_idempotency(operation_key,command_type,semantic_sha256,
		actor_user_id,session_id,repository_version,created_at) VALUES(?, 'test', ?, ?, ?, 1, ?)`,
		"018f0000-0000-7000-8000-000000000014", hex.EncodeToString(bytes.Repeat([]byte{0x15}, sha256.Size)),
		userID, sessionID, stamp); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return genesis, signingKey
}

func appendArchivedAuditEvents(t *testing.T, ctx context.Context, database *sqlcipher.Database, workspaceID string,
	generation uint64, previous [sha256.Size]byte, createdAt time.Time, count int, corruptAt ...int) [sha256.Size]byte {
	t.Helper()
	transaction, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	fingerprint := bytes.Repeat([]byte{0x44}, sha256.Size)
	for index := 1; index <= count; index++ {
		eventID := fmt.Sprintf("018f0000-0000-7%03x-8000-%012x", index, index+0x100)
		payload := &tammyv1.WorkspaceStateChangedEvent{WorkspaceId: workspaceID,
			FromState: tammyv1.WorkspaceState_WORKSPACE_STATE_UNAUTHENTICATED,
			ToState:   tammyv1.WorkspaceState_WORKSPACE_STATE_LOCKED, ReasonCode: "ARCHIVED_EVENT"}
		source := &tammyv1.SourceRef{Type: "workspace", Id: workspaceID, Revision: uint64(index), ContentHash: previous[:]}
		event := &tammyv1.AuditEvent{Id: eventID, WorkspaceId: workspaceID, Generation: generation, Sequence: uint64(index),
			Type:        tammyv1.AuditEventType_AUDIT_EVENT_TYPE_WORKSPACE_STATE_CHANGED,
			OccurredAt:  timestamppb.New(createdAt.Add(time.Duration(index) * time.Second)),
			CommandType: "tammy.v1.Test.ArchivedEvent", Source: source,
			Result: &tammyv1.AuditResultMetadata{TypeName: "tammy.v1.WorkspaceStateChangedEvent",
				DeterministicSha256: bytes.Repeat([]byte{byte(index)}, sha256.Size), OutcomeCode: "ARCHIVED"},
			PayloadSchemaFingerprint: fingerprint,
			Payload:                  &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_WorkspaceStateChanged{WorkspaceStateChanged: payload}}}
		stored, err := audit.PrepareEvent(previous, event, nil)
		if err != nil {
			t.Fatalf("prepare archived event %d: %v", index, err)
		}
		if len(corruptAt) == 1 && index == corruptAt[0] {
			stored.EventProto = append([]byte(nil), stored.EventProto...)
			stored.EventProto[len(stored.EventProto)-1] ^= 0x01
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO audit_events_v1(
			workspace_id,generation,sequence,event_id,event_type,occurred_at,command_type,
			payload_type,payload_schema_fingerprint,payload_proto,payload_json,canonical_event,event_proto,previous_hash,event_hash
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, workspaceID, generation, index, eventID, int32(event.Type),
			event.OccurredAt.AsTime().UTC().Format(time.RFC3339Nano), event.CommandType, stored.PayloadType,
			fingerprint, stored.PayloadProto, stored.PayloadJSON, stored.CanonicalEvent, stored.EventProto,
			stored.Event.PreviousHash, stored.Event.EventHash); err != nil {
			t.Fatalf("insert archived event %d: %v", index, err)
		}
		updated, err := transaction.ExecContext(ctx, `UPDATE audit_chain_headers_v1 SET current_sequence=?,current_head=?
			WHERE workspace_id=? AND generation=? AND current_sequence=? AND current_head=?`, index,
			stored.Event.EventHash, workspaceID, generation, index-1, previous[:])
		if err != nil {
			t.Fatalf("advance archived head %d: %v", index, err)
		}
		rows, _ := updated.RowsAffected()
		if rows != 1 {
			t.Fatalf("advance archived head rows=%d", rows)
		}
		copy(previous[:], stored.Event.EventHash)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return previous
}

func restoreSchemaMetadata(t *testing.T, ctx context.Context, database *sqlcipher.Database) (uint64, []byte) {
	t.Helper()
	rows, err := database.QueryContext(ctx, `SELECT version,name,sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	digest := sha256.New()
	var count uint64
	for rows.Next() {
		var version uint64
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil || version != count+1 || len(checksum) != hex.EncodedLen(sha256.Size) {
			t.Fatalf("migration row %d/%q/%q error=%v", version, name, checksum, err)
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
		t.Fatalf("migration rows count=%d error=%v", count, rows.Err())
	}
	return count, digest.Sum(nil)
}

func assertRestoreMarker(t *testing.T, ctx context.Context, path string, key []byte, want string) {
	t.Helper()
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var got string
	if err := database.QueryRowContext(ctx, `SELECT value FROM restore_fixture_marker`).Scan(&got); err != nil || got != want {
		t.Fatalf("marker=%q error=%v, want %q", got, err, want)
	}
}
