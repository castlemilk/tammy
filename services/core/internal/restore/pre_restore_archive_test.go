package restore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPreRestoreArchiveServiceHasNoUnjournaledPublishBypass(t *testing.T) {
	if _, exposed := reflect.TypeOf((*PreRestoreArchiveService)(nil)).MethodByName("CreateVerifiedPreRestoreArchive"); exposed {
		t.Fatal("PreRestoreArchiveService exposes a combined prepare-and-publish bypass")
	}
}

type preRestoreSnapshotSourceFunc func(context.Context, string, *RestoreAuthorization) ([]byte, error)

func TestPreRestoreArchiveNilRecoveryRemovesOnlyDEKAuthenticatedPreparedName(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	operationID := "018f0000-0000-7000-8000-000000000099"
	dek := bytes.Repeat([]byte{0x41}, 32)
	owned := filepath.Join(directory, testPreRestorePreparedName(dek, operationID))
	plainOperation := filepath.Join(directory, ".tammy-pre-restore-operation-"+operationID+".prepared")
	foreignUUIDTemp := filepath.Join(directory, ".tammy-pre-restore-018f0000-0000-7000-8000-000000000088.tmp")
	for path, contents := range map[string][]byte{owned: []byte("owned encrypted residue"),
		plainOperation: []byte("foreign plain operation"), foreignUUIDTemp: []byte("foreign UUID temp")} {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{Directory: directory, DEK: dek,
		Snapshots: preRestoreSnapshotSourceFunc(func(context.Context, string, *RestoreAuthorization) ([]byte, error) {
			return []byte("unused"), nil
		}), NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000088", nil },
		Now: time.Now, Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 256))})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, path := range []string{owned, plainOperation, foreignUUIDTemp} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("constructor changed %s: %v", path, err)
		}
	}
	status := &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
		BackupManifestHash: bytes.Repeat([]byte{0x51}, sha256.Size), UpdatedAt: timestamppb.Now()}
	if err := service.CleanupInterruptedPreRestoreArchive(context.Background(), status); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned residue remains: %v", err)
	}
	for path, want := range map[string]string{plainOperation: "foreign plain operation", foreignUUIDTemp: "foreign UUID temp"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("foreign residue %s=%q error=%v", path, got, err)
		}
	}
}

func testPreRestorePreparedName(dek []byte, operationID string) string {
	digest := hmac.New(sha256.New, dek)
	_, _ = digest.Write([]byte("tammy.pre-restore.prepared-name.v1\x00"))
	_, _ = digest.Write([]byte(operationID))
	return ".tammy-pre-restore-operation-" + operationID + "-" + hex.EncodeToString(digest.Sum(nil)[:16]) + ".prepared"
}

func testOwnedPreRestorePreparedName(operationID string) string {
	return testPreRestorePreparedName(bytes.Repeat([]byte{0x41}, 32), operationID)
}

func (function preRestoreSnapshotSourceFunc) CapturePreRestoreSnapshot(
	ctx context.Context,
	workspaceID string,
	authorization *RestoreAuthorization,
) ([]byte, error) {
	return function(ctx, workspaceID, authorization)
}

func TestPreRestoreArchiveRecoveryRemovesOnlyAuthenticatedOwnedResidue(t *testing.T) {
	operationID := "018f0000-0000-7000-8000-000000000099"
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	archiveID := "018f0000-0000-7000-8000-000000000088"
	archiveBytes := bytes.Repeat([]byte("authenticated encrypted predecessor"), 128)
	digest := sha256.Sum256(archiveBytes)
	for _, test := range []struct {
		name     string
		bound    bool
		basename string
	}{
		{name: "file_fsync_before_journal_bind", basename: testOwnedPreRestorePreparedName(operationID)},
		{name: "journal_bind_before_publish_rename", bound: true, basename: testOwnedPreRestorePreparedName(operationID)},
		{name: "publish_rename_before_directory_fsync", bound: true, basename: preRestoreArchiveName(archiveID)},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			service, err := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{Directory: directory,
				DEK: bytes.Repeat([]byte{0x41}, 32), Snapshots: preRestoreSnapshotSourceFunc(func(context.Context, string, *RestoreAuthorization) ([]byte, error) {
					return []byte("unused"), nil
				}), NewID: func() (string, error) { return archiveID, nil }, Now: time.Now,
				Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 256))})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			ownedPath := filepath.Join(directory, test.basename)
			if err := os.WriteFile(ownedPath, archiveBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			unrelatedPath := filepath.Join(directory, ".tammy-pre-restore-unrelated.archive")
			if err := os.WriteFile(unrelatedPath, []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
			activePath := filepath.Join(directory, "workspace.db")
			activeBytes := bytes.Repeat([]byte("active encrypted bytes"), 256)
			if err := os.WriteFile(activePath, activeBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			before := sha256.Sum256(activeBytes)
			status := &tammyv1.RestoreStatus{OperationId: operationID, State: tammyv1.RestoreState_RESTORE_STATE_PREPARED,
				BackupManifestHash: bytes.Repeat([]byte{0x50}, sha256.Size), UpdatedAt: timestamppb.Now()}
			if test.bound {
				artifacts := testRestoreArtifactReservation(operationID, workspaceID)
				status.Recovery = &tammyv1.RestoreRecoveryRecord{WorkspaceId: workspaceID, PreRestoreArchiveId: archiveID,
					PreRestoreArchiveHash: append([]byte(nil), digest[:]...), StageBasename: artifacts.StageBasename(),
					RollbackBasename:                  artifacts.RollbackBasename(),
					ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
					StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
					RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
					PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
					PreRestoreArchiveFinalBasename:    preRestoreArchiveName(archiveID)}
			}
			if err := service.CleanupInterruptedPreRestoreArchive(context.Background(), status); err != nil {
				t.Fatalf("CleanupInterruptedPreRestoreArchive() error=%v", err)
			}
			if _, err := os.Lstat(ownedPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("owned residue remains: %v", err)
			}
			if foreign, err := os.ReadFile(unrelatedPath); err != nil || string(foreign) != "foreign" {
				t.Fatalf("unrelated file changed: %q %v", foreign, err)
			}
			after, err := os.ReadFile(activePath)
			if err != nil || sha256.Sum256(after) != before {
				t.Fatalf("active bytes changed: before=%x after=%x error=%v", before, sha256.Sum256(after), err)
			}
		})
	}
}

func TestStartupRecoveryInjectedPreRestoreArchiveDeathBoundaries(t *testing.T) {
	operationID := "018f0000-0000-7000-8000-000000000099"
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	archiveID := "018f0000-0000-7000-8000-000000000088"
	manifestHash := bytes.Repeat([]byte{0x60}, sha256.Size)
	activeBytes := bytes.Repeat([]byte("active encrypted bytes"), 256)
	activeHash := sha256.Sum256(activeBytes)
	injectedDeath := errors.New("injected process death")
	for _, boundary := range []string{"file_fsync_before_journal_bind", "journal_bind_before_publish_rename", "publish_rename_before_directory_fsync"} {
		t.Run(boundary, func(t *testing.T) {
			root := t.TempDir()
			archiveDirectory := filepath.Join(root, "archives")
			journalDirectory := filepath.Join(root, "journal")
			if err := os.Mkdir(archiveDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(journalDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			activePath := filepath.Join(root, "workspace.db")
			if err := os.WriteFile(activePath, activeBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			foreignPath := filepath.Join(archiveDirectory, "foreign-evidence.archive")
			if err := os.WriteFile(foreignPath, []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
			journal, err := NewJournalStore(JournalConfig{Directory: journalDirectory,
				AuthenticationKey: journalAuthKey(), Now: time.Now})
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if _, err := journal.Prepare(context.Background(), operationID, manifestHash); err != nil {
				t.Fatal(err)
			}
			hooks := &preRestoreArchiveHooks{}
			if boundary == "file_fsync_before_journal_bind" {
				hooks.afterPreparedFileSync = func() error { return injectedDeath }
			}
			if boundary == "publish_rename_before_directory_fsync" {
				hooks.afterPublishRename = func() error { return injectedDeath }
			}
			newArchiveService := func(hooks *preRestoreArchiveHooks) *PreRestoreArchiveService {
				service, createErr := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{Directory: archiveDirectory,
					DEK: bytes.Repeat([]byte{0x41}, 32), Snapshots: preRestoreSnapshotSourceFunc(func(context.Context, string, *RestoreAuthorization) ([]byte, error) {
						return append([]byte(nil), activeBytes...), nil
					}), NewID: func() (string, error) { return archiveID, nil }, Now: time.Now,
					Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 256)), hooks: hooks})
				if createErr != nil {
					t.Fatal(createErr)
				}
				return service
			}
			interrupted := newArchiveService(hooks)
			archive, prepareErr := interrupted.PrepareVerifiedPreRestoreArchive(context.Background(), PreRestoreArchiveRequest{
				OperationID: operationID, WorkspaceID: workspaceID,
				Authorization: &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
					WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)},
				ManifestHash: manifestHash})
			if boundary == "file_fsync_before_journal_bind" {
				if !errors.Is(prepareErr, injectedDeath) || archive != nil {
					t.Fatalf("file-sync death prepare=%#v error=%v", archive, prepareErr)
				}
			} else {
				if prepareErr != nil {
					t.Fatal(prepareErr)
				}
				artifacts := testRestoreArtifactReservation(operationID, workspaceID)
				recovery := &tammyv1.RestoreRecoveryRecord{WorkspaceId: workspaceID, PreRestoreArchiveId: archive.ArchiveID,
					PreRestoreArchiveHash: append([]byte(nil), archive.SHA256...), StageBasename: artifacts.StageBasename(),
					RollbackBasename:                  artifacts.RollbackBasename(),
					ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
					StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
					RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
					PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
					PreRestoreArchiveFinalBasename:    preRestoreArchiveName(archive.ArchiveID)}
				_, binding, err := journal.BindPreparedRecovery(context.Background(), operationID, manifestHash, recovery)
				if err != nil {
					t.Fatal(err)
				}
				if boundary == "publish_rename_before_directory_fsync" {
					if err := interrupted.PublishPreRestoreArchive(context.Background(), archive, binding); !errors.Is(err, injectedDeath) {
						t.Fatalf("rename death error=%v", err)
					}
				}
			}
			restarted := newArchiveService(nil)
			defer restarted.Close()
			actions := []string{}
			coordinator, err := NewStartupRecoveryCoordinator(StartupRecoveryConfig{Journal: journal,
				Workspace: recoveryWorkspaceHarness{calls: &actions}, Archives: restarted,
				MachineCredentials: recoveryCredentialHarness{calls: &actions}, Mirror: recoveryMirrorHarness{calls: &actions}, BatchSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			for restart := 0; restart < 2; restart++ {
				report, recoveryErr := coordinator.Recover(context.Background())
				wantProcessed, wantRolledBack := uint32(1), uint32(1)
				if restart == 1 {
					wantProcessed, wantRolledBack = 0, 0
				}
				if recoveryErr != nil || report.Processed != wantProcessed || report.RolledBack != wantRolledBack {
					t.Fatalf("restart %d report=%#v error=%v", restart, report, recoveryErr)
				}
			}
			if err := interrupted.Close(); err != nil {
				t.Fatal(err)
			}
			for _, owned := range []string{testOwnedPreRestorePreparedName(operationID), preRestoreArchiveName(archiveID)} {
				if _, err := os.Lstat(filepath.Join(archiveDirectory, owned)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owned residue %q remains: %v", owned, err)
				}
			}
			if foreign, err := os.ReadFile(foreignPath); err != nil || string(foreign) != "foreign" {
				t.Fatalf("foreign evidence changed: %q %v", foreign, err)
			}
			activeAfter, err := os.ReadFile(activePath)
			if err != nil || sha256.Sum256(activeAfter) != activeHash {
				t.Fatalf("active bytes changed: before=%x after=%x error=%v", activeHash, sha256.Sum256(activeAfter), err)
			}
		})
	}
}

func TestPreRestoreArchiveFormatRoundTripAndTamper(t *testing.T) {
	createdAt := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	predecessor := bytes.Repeat([]byte("encrypted-sqlcipher-page"), 10_000)
	dek := bytes.Repeat([]byte{0x41}, 32)
	input := PreRestoreArchiveFormatInput{
		ArchiveID:        "018f0000-0000-7000-8000-000000000088",
		WorkspaceID:      "018f0000-0000-7000-8000-000000000001",
		SourceGeneration: 5,
		CreatedAt:        createdAt,
		DeleteEligibleAt: createdAt.AddDate(1, 0, 0),
		Predecessor:      predecessor,
	}
	archive, err := SealPreRestoreArchive(input, dek, bytes.NewReader(bytes.Repeat([]byte{0x72}, 256)))
	if err != nil {
		t.Fatalf("SealPreRestoreArchive() error = %v", err)
	}
	opened, err := OpenPreRestoreArchive(archive, dek, input.WorkspaceID, input.ArchiveID)
	if err != nil {
		t.Fatalf("OpenPreRestoreArchive() error = %v", err)
	}
	wantHash := sha256.Sum256(predecessor)
	if opened.Manifest == nil || opened.Manifest.Format != PreRestoreFormatV1 ||
		opened.Manifest.ArchiveId != input.ArchiveID || opened.Manifest.SourceGeneration != input.SourceGeneration ||
		!opened.Manifest.CreatedAt.AsTime().Equal(createdAt) ||
		!opened.Manifest.DeletionEligibleAt.AsTime().Equal(input.DeleteEligibleAt) ||
		!bytes.Equal(opened.Manifest.PredecessorSha256, wantHash[:]) || !bytes.Equal(opened.Predecessor, predecessor) {
		t.Fatalf("opened archive = %#v", opened)
	}

	tampered := append([]byte(nil), archive...)
	tampered[len(tampered)-1] ^= 0x01
	_, tamperErr := OpenPreRestoreArchive(tampered, dek, input.WorkspaceID, input.ArchiveID)
	_, wrongKeyErr := OpenPreRestoreArchive(archive, bytes.Repeat([]byte{0x99}, 32), input.WorkspaceID, input.ArchiveID)
	if !errors.Is(tamperErr, ErrPreRestoreArchiveSecret) || !errors.Is(wrongKeyErr, ErrPreRestoreArchiveSecret) ||
		tamperErr.Error() != wrongKeyErr.Error() {
		t.Fatalf("authenticated-decrypt oracle tamper=%v wrong-key=%v", tamperErr, wrongKeyErr)
	}
}

func TestPreRestoreArchiveServiceAtomicallyPublishesAndVerifies(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	directory := filepath.Join(root, "private")
	journalDirectory := filepath.Join(root, "journal")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journalDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	operationID := "018f0000-0000-7000-8000-000000000099"
	archiveID := "018f0000-0000-7000-8000-000000000088"
	secondArchiveID := "018f0000-0000-7000-8000-000000000089"
	createdAt := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	predecessor := bytes.Repeat([]byte("active encrypted page"), 5_000)
	dek := bytes.Repeat([]byte{0x41}, 32)
	archiveIDs := []string{archiveID, secondArchiveID}
	nextArchive := 0
	service, err := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{
		Directory: directory,
		DEK:       dek,
		Snapshots: preRestoreSnapshotSourceFunc(func(_ context.Context, gotWorkspaceID string, authorization *RestoreAuthorization) ([]byte, error) {
			if gotWorkspaceID != workspaceID || authorization.CurrentGeneration != 5 {
				t.Fatalf("snapshot scope workspace=%q auth=%#v", gotWorkspaceID, authorization)
			}
			return append([]byte(nil), predecessor...), nil
		}),
		NewID: func() (string, error) {
			identifier := archiveIDs[nextArchive]
			nextArchive++
			return identifier, nil
		},
		Now:    func() time.Time { return createdAt },
		Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 256)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	journal, err := NewJournalStore(JournalConfig{Directory: journalDirectory, AuthenticationKey: journalAuthKey(), Now: func() time.Time { return createdAt }})
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	manifestHash := bytes.Repeat([]byte{0x60}, sha256.Size)
	bindArchive := func(operationID string, archive *PreRestoreArchive) *PreparedArchiveBinding {
		if _, err := journal.Prepare(ctx, operationID, manifestHash); err != nil {
			t.Fatal(err)
		}
		artifacts := testRestoreArtifactReservation(operationID, workspaceID)
		recovery := &tammyv1.RestoreRecoveryRecord{WorkspaceId: workspaceID, PreRestoreArchiveId: archive.ArchiveID,
			PreRestoreArchiveHash: append([]byte(nil), archive.SHA256...), StageBasename: artifacts.StageBasename(),
			RollbackBasename:                  artifacts.RollbackBasename(),
			ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
			StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
			RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
			PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
			PreRestoreArchiveFinalBasename:    preRestoreArchiveName(archive.ArchiveID)}
		_, binding, err := journal.BindPreparedRecovery(ctx, operationID, manifestHash, recovery)
		if err != nil || binding == nil {
			t.Fatalf("BindPreparedRecovery() binding=%#v error=%v", binding, err)
		}
		return binding
	}
	archive, err := service.PrepareVerifiedPreRestoreArchive(ctx, PreRestoreArchiveRequest{OperationID: operationID,
		WorkspaceID: workspaceID, Authorization: &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
			WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)},
		ManifestHash: manifestHash})
	if err != nil {
		t.Fatalf("PrepareVerifiedPreRestoreArchive() error=%v", err)
	}
	if archive == nil || archive.ArchiveID != archiveID || archive.Version != 1 ||
		archive.SourceGeneration != 5 || !archive.CreatedAt.Equal(createdAt) ||
		!archive.DeletionEligibleAt.Equal(createdAt.AddDate(1, 0, 0)) || len(archive.SHA256) != sha256.Size {
		t.Fatalf("archive=%#v", archive)
	}
	preparedPath := filepath.Join(directory, testOwnedPreRestorePreparedName(operationID))
	path := filepath.Join(directory, ".tammy-pre-restore-"+archiveID+".archive")
	if _, err := os.Lstat(preparedPath); err != nil {
		t.Fatalf("prepared archive is absent: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive published before journal binding: %v", err)
	}
	if err := service.PublishPreRestoreArchive(ctx, archive, nil); !errors.Is(err, ErrPreRestoreArchive) {
		t.Fatalf("nil journal binding publish error=%v", err)
	}
	binding := bindArchive(operationID, archive)
	capabilityCopy := *archive
	if err := service.PublishPreRestoreArchive(ctx, &capabilityCopy, binding); !errors.Is(err, ErrPreRestoreArchive) {
		t.Fatalf("copied prepared capability publish error=%v", err)
	}
	if err := service.PublishPreRestoreArchive(ctx, archive, binding); err != nil {
		t.Fatalf("PublishPreRestoreArchive() error=%v", err)
	}
	if err := service.PublishPreRestoreArchive(ctx, archive, binding); !errors.Is(err, ErrPreRestoreArchive) {
		t.Fatalf("reused prepared capability publish error=%v", err)
	}
	if err := service.PublishPreRestoreArchive(ctx, archive, bindArchive(operationID, archive)); err != nil {
		t.Fatalf("exact journal-bound publication replay error=%v", err)
	}
	if _, err := os.Lstat(preparedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared archive remains after publish: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || sha256.Sum256(contents) != *(*[32]byte)(archive.SHA256) {
		t.Fatalf("published info=%#v hash=%x error=%v", info, sha256.Sum256(contents), err)
	}
	opened, err := OpenPreRestoreArchive(contents, dek, workspaceID, archiveID)
	if err != nil || !bytes.Equal(opened.Predecessor, predecessor) {
		t.Fatalf("published open=%#v error=%v", opened, err)
	}
	readBack, err := service.ReadEncryptedPreRestoreArchive(ctx, workspaceID, archiveID, archive.SHA256)
	if err != nil || !bytes.Equal(readBack, contents) {
		t.Fatalf("ReadEncryptedPreRestoreArchive() bytes=%d error=%v", len(readBack), err)
	}
	zeroBytes(readBack)
	if err := service.AbortPreRestoreArchive(ctx, archive); err != nil {
		t.Fatalf("AbortPreRestoreArchive() error=%v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("aborted archive still exists: %v", err)
	}
	secondOperationID := "018f0000-0000-7000-8000-000000000098"
	second, err := service.PrepareVerifiedPreRestoreArchive(ctx, PreRestoreArchiveRequest{OperationID: secondOperationID,
		WorkspaceID: workspaceID, Authorization: &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
			WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)},
		ManifestHash: manifestHash})
	if err != nil || second.ArchiveID != secondArchiveID {
		t.Fatalf("second archive=%#v error=%v", second, err)
	}
	if err := service.PublishPreRestoreArchive(ctx, second, bindArchive(secondOperationID, second)); err != nil {
		t.Fatalf("second PublishPreRestoreArchive() error=%v", err)
	}
	if err := service.DeleteEncryptedPreRestoreArchive(ctx, secondArchiveID, second.SHA256); err != nil {
		t.Fatalf("DeleteEncryptedPreRestoreArchive() error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, preRestoreArchiveName(secondArchiveID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted archive still exists: %v", err)
	}
}

func TestPreRestoreArchiveServiceCloseRemovesOwnedFileSyncResidue(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	operationID := "018f0000-0000-7000-8000-000000000099"
	workspaceID := "018f0000-0000-7000-8000-000000000001"
	injectedDeath := errors.New("injected process death after prepared file fsync")
	service, err := NewPreRestoreArchiveService(PreRestoreArchiveServiceConfig{Directory: directory,
		DEK: bytes.Repeat([]byte{0x41}, 32), Snapshots: preRestoreSnapshotSourceFunc(func(context.Context, string, *RestoreAuthorization) ([]byte, error) {
			return bytes.Repeat([]byte("active encrypted page"), 128), nil
		}), NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000088", nil }, Now: time.Now,
		Random: bytes.NewReader(bytes.Repeat([]byte{0x73}, 256)),
		hooks:  &preRestoreArchiveHooks{afterPreparedFileSync: func() error { return injectedDeath }}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PrepareVerifiedPreRestoreArchive(context.Background(), PreRestoreArchiveRequest{OperationID: operationID,
		WorkspaceID: workspaceID, Authorization: &RestoreAuthorization{AuthorizationID: "018f0000-0000-7000-8000-000000000077",
			WorkspaceID: workspaceID, CurrentGeneration: 5, CurrentAuditHead: bytes.Repeat([]byte{0x50}, sha256.Size)},
		ManifestHash: bytes.Repeat([]byte{0x60}, sha256.Size)})
	if !errors.Is(err, injectedDeath) {
		t.Fatalf("prepared file sync injection error=%v", err)
	}
	preparedPath := filepath.Join(directory, testOwnedPreRestorePreparedName(operationID))
	if _, err := os.Lstat(preparedPath); err != nil {
		t.Fatalf("injected death did not retain owned residue: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(preparedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Close retained owned prepared residue: %v", err)
	}
}
