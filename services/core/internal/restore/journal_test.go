package restore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestRestoreJournalListsAuthenticatedRecoveryRecordsInBoundedOrder(t *testing.T) {
	directory := t.TempDir()
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operationIDs := []string{
		"018f0000-0000-7000-8000-000000000013",
		"018f0000-0000-7000-8000-000000000011",
		"018f0000-0000-7000-8000-000000000012",
	}
	for _, operationID := range operationIDs {
		if _, err := store.Prepare(context.Background(), operationID, bytes.Repeat([]byte{0x31}, sha256.Size)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "unrelated.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := store.ListRecoveryRecords(context.Background(), "", 2)
	if err != nil || first == nil || len(first.Records) != 2 || first.NextAfterOperationID != operationIDs[2] {
		t.Fatalf("first page=%#v error=%v", first, err)
	}
	got := []string{first.Records[0].OperationId, first.Records[1].OperationId}
	if want := []string{operationIDs[1], operationIDs[2]}; !slices.Equal(got, want) {
		t.Fatalf("first operation IDs=%v, want %v", got, want)
	}
	second, err := store.ListRecoveryRecords(context.Background(), first.NextAfterOperationID, 2)
	if err != nil || second == nil || len(second.Records) != 1 || second.NextAfterOperationID != "" ||
		second.Records[0].OperationId != operationIDs[0] {
		t.Fatalf("second page=%#v error=%v", second, err)
	}
	if contents, err := os.ReadFile(filepath.Join(directory, "unrelated.txt")); err != nil || string(contents) != "preserve" {
		t.Fatalf("unrelated contents=%q error=%v", contents, err)
	}
}

func journalAuthKey() []byte { return bytes.Repeat([]byte{0x71}, sha256.Size) }

func TestRestoreJournalPersistsExactLifecycleAndRejectsChangedManifest(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewJournalStore() error = %v", err)
	}
	defer store.Close()
	operationID := "018f0000-0000-7000-8000-000000000011"
	manifestHash := sha256.Sum256([]byte("manifest one"))
	changedHash := sha256.Sum256([]byte("manifest two"))
	status, err := store.Prepare(context.Background(), operationID, manifestHash[:])
	if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED {
		t.Fatalf("Prepare() = (%#v, %v)", status, err)
	}
	if _, err := store.Prepare(context.Background(), operationID, changedHash[:]); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("changed manifest replay error = %v, want ErrJournalConflict", err)
	}
	newHead := bytes.Repeat([]byte{0x51}, sha256.Size)
	for _, transition := range []struct {
		from tammyv1.RestoreState
		to   tammyv1.RestoreState
	}{
		{tammyv1.RestoreState_RESTORE_STATE_PREPARED, tammyv1.RestoreState_RESTORE_STATE_STAGED},
		{tammyv1.RestoreState_RESTORE_STATE_STAGED, tammyv1.RestoreState_RESTORE_STATE_SWAPPED},
		{tammyv1.RestoreState_RESTORE_STATE_SWAPPED, tammyv1.RestoreState_RESTORE_STATE_COMPLETE},
	} {
		status, err = store.Advance(context.Background(), operationID, transition.from, transition.to, newHead)
		if err != nil || status.State != transition.to {
			t.Fatalf("Advance(%s -> %s) = (%#v, %v)", transition.from, transition.to, status, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.Get(context.Background(), operationID)
	if err != nil || loaded.State != tammyv1.RestoreState_RESTORE_STATE_COMPLETE ||
		!bytes.Equal(loaded.BackupManifestHash, manifestHash[:]) || !bytes.Equal(loaded.NewAuditHead, newHead) {
		t.Fatalf("Get() after restart = (%#v, %v)", loaded, err)
	}
}

func TestRestoreJournalPersistsRollbackTerminalAndExcludesItFromRecovery(t *testing.T) {
	directory := t.TempDir()
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operationID := "018f0000-0000-7000-8000-000000000015"
	manifestHash := sha256.Sum256([]byte("rollback terminal manifest"))
	if _, err := store.Prepare(context.Background(), operationID, manifestHash[:]); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := store.Advance(context.Background(), operationID,
		tammyv1.RestoreState_RESTORE_STATE_PREPARED, rolledBackRestoreState, nil)
	if err != nil || rolledBack == nil || rolledBack.State != rolledBackRestoreState || len(rolledBack.NewAuditHead) != 0 {
		t.Fatalf("prepared rollback terminal=(%#v, %v)", rolledBack, err)
	}
	replayed, err := store.Prepare(context.Background(), operationID, manifestHash[:])
	if err != nil || replayed.State != rolledBackRestoreState {
		t.Fatalf("same-operation replay=(%#v, %v)", replayed, err)
	}
	page, err := store.ListRecoveryRecords(context.Background(), "", 8)
	if err != nil || page == nil || len(page.Records) != 0 {
		t.Fatalf("terminal recovery page=(%#v, %v)", page, err)
	}
	newOperationID := "018f0000-0000-7000-8000-000000000016"
	newStatus, err := store.Prepare(context.Background(), newOperationID, manifestHash[:])
	if err != nil || newStatus.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED {
		t.Fatalf("new operation=(%#v, %v)", newStatus, err)
	}
}

func TestRestoreJournalV2BindsImmutableRecoveryRecord(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operationID := "018f0000-0000-7000-8000-000000000011"
	workspaceID := "018f0000-0000-7000-8000-000000000012"
	archiveID := "018f0000-0000-7000-8000-000000000013"
	manifestHash := sha256.Sum256([]byte("journal-v2 manifest"))
	if _, err := store.Prepare(context.Background(), operationID, manifestHash[:]); err != nil {
		t.Fatal(err)
	}
	artifacts := testRestoreArtifactReservation(operationID, workspaceID)
	recovery := &tammyv1.RestoreRecoveryRecord{WorkspaceId: workspaceID, PreRestoreArchiveId: archiveID,
		PreRestoreArchiveHash:             bytes.Repeat([]byte{0x31}, sha256.Size),
		StageBasename:                     artifacts.StageBasename(),
		RollbackBasename:                  artifacts.RollbackBasename(),
		ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
		StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
		RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
		PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
		PreRestoreArchiveFinalBasename:    preRestoreArchiveName(archiveID)}
	prepared, binding, err := store.BindPreparedRecovery(context.Background(), operationID, manifestHash[:], recovery)
	if err != nil || prepared.State != tammyv1.RestoreState_RESTORE_STATE_PREPARED || prepared.Recovery == nil || binding == nil {
		t.Fatalf("BindPreparedRecovery()=%#v error=%v", prepared, err)
	}
	finalizedGeneration := uint64(6)
	schemaVersion := uint64(4)
	recovery.FinalizedGeneration = &finalizedGeneration
	recovery.FinalizedAuditHead = bytes.Repeat([]byte{0x32}, sha256.Size)
	recovery.SchemaVersion = &schemaVersion
	recovery.MigrationManifestHash = bytes.Repeat([]byte{0x33}, sha256.Size)
	recovery.RollbackPredecessorHash = bytes.Repeat([]byte{0x34}, sha256.Size)
	recovery.ActivatedDatabaseSha256 = bytes.Repeat([]byte{0x35}, sha256.Size)
	staged, err := store.BindStagedRecovery(context.Background(), operationID, recovery)
	if err != nil || staged.State != tammyv1.RestoreState_RESTORE_STATE_STAGED || staged.Recovery == nil ||
		staged.Recovery.GetFinalizedGeneration() != 6 {
		t.Fatalf("BindStagedRecovery()=%#v error=%v", staged, err)
	}
	changed := proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	changed.PreRestoreArchiveHash = bytes.Repeat([]byte{0x41}, sha256.Size)
	if _, err := store.BindStagedRecovery(context.Background(), operationID, changed); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("changed staged recovery error=%v", err)
	}
}

func TestRestoreJournalV2CheckpointsPostSwapRecoveryMonotonically(t *testing.T) {
	directory := t.TempDir()
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operationID := "018f0000-0000-7000-8000-000000000011"
	manifestHash := sha256.Sum256([]byte("journal-v2 checkpoint manifest"))
	if _, err := store.Prepare(context.Background(), operationID, manifestHash[:]); err != nil {
		t.Fatal(err)
	}
	artifacts := testRestoreArtifactReservation(operationID, "018f0000-0000-7000-8000-000000000012")
	recovery := &tammyv1.RestoreRecoveryRecord{
		WorkspaceId:                       "018f0000-0000-7000-8000-000000000012",
		PreRestoreArchiveId:               "018f0000-0000-7000-8000-000000000013",
		PreRestoreArchiveHash:             bytes.Repeat([]byte{0x31}, sha256.Size),
		StageBasename:                     artifacts.StageBasename(),
		RollbackBasename:                  artifacts.RollbackBasename(),
		ArtifactOwnershipDigest:           artifacts.OwnershipDigest(),
		StageOwnerMarkerSha256:            artifacts.StageOwnerMarkerSHA256(),
		RollbackOwnerMarkerSha256:         artifacts.RollbackOwnerMarkerSHA256(),
		PreRestoreArchivePreparedBasename: testOwnedPreRestorePreparedName(operationID),
		PreRestoreArchiveFinalBasename:    preRestoreArchiveName("018f0000-0000-7000-8000-000000000013"),
	}
	if _, _, err := store.BindPreparedRecovery(context.Background(), operationID, manifestHash[:], recovery); err != nil {
		t.Fatal(err)
	}
	finalizedGeneration := uint64(6)
	schemaVersion := uint64(4)
	recovery.FinalizedGeneration = &finalizedGeneration
	recovery.FinalizedAuditHead = bytes.Repeat([]byte{0x32}, sha256.Size)
	recovery.SchemaVersion = &schemaVersion
	recovery.MigrationManifestHash = bytes.Repeat([]byte{0x33}, sha256.Size)
	recovery.RollbackPredecessorHash = bytes.Repeat([]byte{0x34}, sha256.Size)
	recovery.ActivatedDatabaseSha256 = bytes.Repeat([]byte{0x35}, sha256.Size)
	if _, err := store.BindStagedRecovery(context.Background(), operationID, recovery); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Advance(context.Background(), operationID, tammyv1.RestoreState_RESTORE_STATE_STAGED,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery.FinalizedAuditHead); err != nil {
		t.Fatal(err)
	}

	skipped := proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	skipped.PostSwapVerified = true
	skipped.MachineCredentialsRevoked = true
	if _, err := store.CheckpointRecovery(context.Background(), operationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, skipped); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("skipped checkpoint error=%v, want ErrJournalConflict", err)
	}

	for step, update := range []func(*tammyv1.RestoreRecoveryRecord){
		func(value *tammyv1.RestoreRecoveryRecord) { value.PostSwapVerified = true },
		func(value *tammyv1.RestoreRecoveryRecord) { value.MachineCredentialsRevoked = true },
		func(value *tammyv1.RestoreRecoveryRecord) { value.MirrorPublished = true },
	} {
		update(recovery)
		status, err := store.CheckpointRecovery(context.Background(), operationID,
			tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery)
		if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_SWAPPED || !proto.Equal(status.Recovery, recovery) {
			t.Fatalf("checkpoint %d=(%#v,%v)", step, status, err)
		}
		if _, err := store.CheckpointRecovery(context.Background(), operationID,
			tammyv1.RestoreState_RESTORE_STATE_SWAPPED, recovery); err != nil {
			t.Fatalf("idempotent checkpoint %d error=%v", step, err)
		}
	}

	regressed := proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	regressed.MachineCredentialsRevoked = false
	regressed.MirrorPublished = false
	if _, err := store.CheckpointRecovery(context.Background(), operationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, regressed); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("regressed checkpoint error=%v, want ErrJournalConflict", err)
	}
	changed := proto.Clone(recovery).(*tammyv1.RestoreRecoveryRecord)
	changed.PreRestoreArchiveHash = bytes.Repeat([]byte{0x41}, sha256.Size)
	if _, err := store.CheckpointRecovery(context.Background(), operationID,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, changed); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("changed checkpoint error=%v, want ErrJournalConflict", err)
	}
	if _, err := store.Advance(context.Background(), operationID, tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
		tammyv1.RestoreState_RESTORE_STATE_COMPLETE, recovery.FinalizedAuditHead); err != nil {
		t.Fatalf("complete after checkpoints error=%v", err)
	}
}

func TestRestoreJournalV2AuthenticationRejectsWrongKeyPlainDigestAndV1(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]byte)
		key    []byte
	}{
		{name: "wrong_key", key: bytes.Repeat([]byte{0x72}, sha256.Size)},
		{name: "recomputed_plain_digest", key: journalAuthKey(), mutate: func(frame []byte) {
			frame[28] ^= 0x01
			digest := sha256.Sum256(frame[:len(frame)-sha256.Size])
			copy(frame[len(frame)-sha256.Size:], digest[:])
		}},
		{name: "legacy_v1", key: journalAuthKey(), mutate: func(frame []byte) {
			frame[23] = '1'
			digest := sha256.Sum256(frame[:len(frame)-sha256.Size])
			copy(frame[len(frame)-sha256.Size:], digest[:])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			operationID := "018f0000-0000-7000-8000-000000000011"
			store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Prepare(context.Background(), operationID, bytes.Repeat([]byte{0x33}, sha256.Size)); err != nil {
				t.Fatal(err)
			}
			ownedKey := store.auth
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			for _, value := range ownedKey {
				if value != 0 {
					t.Fatal("journal authentication key was not zeroed")
				}
			}
			path := filepath.Join(directory, journalName(operationID))
			if test.mutate != nil {
				frame, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				test.mutate(frame)
				if err := os.WriteFile(path, frame, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: test.key, Now: time.Now})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if status, err := reopened.Get(context.Background(), operationID); status != nil || !errors.Is(err, ErrJournal) {
				t.Fatalf("unauthenticated journal status=%#v error=%v", status, err)
			}
		})
	}
}

func TestRestoreJournalRejectsSymlinkAndBaseSwap(t *testing.T) {
	parent := t.TempDir()
	realDirectory := filepath.Join(parent, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(parent, "link")
	if err := os.Symlink(realDirectory, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJournalStore(JournalConfig{Directory: symlink, AuthenticationKey: journalAuthKey(), Now: time.Now}); !errors.Is(err, ErrJournal) {
		t.Fatalf("symlink base error = %v, want ErrJournal", err)
	}

	swapped := filepath.Join(parent, "swapped")
	_, err := NewJournalStore(JournalConfig{Directory: realDirectory, AuthenticationKey: journalAuthKey(), Now: time.Now, hooks: &journalHooks{
		openRoot: func(candidate string) (*os.Root, error) {
			if err := os.Rename(candidate, swapped); err != nil {
				return nil, err
			}
			if err := os.Mkdir(candidate, 0o700); err != nil {
				return nil, err
			}
			return os.OpenRoot(candidate)
		},
	}})
	if !errors.Is(err, ErrJournal) {
		t.Fatalf("base swap error = %v, want ErrJournal", err)
	}
}

func TestRestoreJournalBindsStagedAuditHead(t *testing.T) {
	directory := t.TempDir()
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operationID := "018f0000-0000-7000-8000-000000000012"
	manifestHash := sha256.Sum256([]byte("bound manifest"))
	if _, err := store.Prepare(context.Background(), operationID, manifestHash[:]); err != nil {
		t.Fatal(err)
	}
	boundHead := bytes.Repeat([]byte{0x61}, sha256.Size)
	if _, err := store.Advance(context.Background(), operationID, tammyv1.RestoreState_RESTORE_STATE_PREPARED,
		tammyv1.RestoreState_RESTORE_STATE_STAGED, boundHead); err != nil {
		t.Fatal(err)
	}
	replacement := bytes.Repeat([]byte{0x62}, sha256.Size)
	if _, err := store.Advance(context.Background(), operationID, tammyv1.RestoreState_RESTORE_STATE_STAGED,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED, replacement); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("replacement head error = %v, want ErrJournalConflict", err)
	}
	status, err := store.Get(context.Background(), operationID)
	if err != nil || status.State != tammyv1.RestoreState_RESTORE_STATE_STAGED || !bytes.Equal(status.NewAuditHead, boundHead) {
		t.Fatalf("status after replacement = (%#v, %v)", status, err)
	}
}

func TestRestoreJournalRejectsSymlinkedTargetAndPreservesForeignFiles(t *testing.T) {
	directory := t.TempDir()
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	operationID := "018f0000-0000-7000-8000-000000000013"
	manifestHash := sha256.Sum256([]byte("symlinked journal"))
	if _, err := store.Prepare(context.Background(), operationID, manifestHash[:]); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	name := journalName(operationID)
	realName := "retained-real-journal.pb"
	if err := os.Rename(filepath.Join(directory, name), filepath.Join(directory, realName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realName, filepath.Join(directory, name)); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(directory, ".foreign.tmp")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Get(context.Background(), operationID); !errors.Is(err, ErrJournal) {
		t.Fatalf("symlinked journal error = %v, want ErrJournal", err)
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "foreign" {
		t.Fatalf("foreign file = %q, %v", got, err)
	}
}

func TestRestoreJournalDeathBoundaries(t *testing.T) {
	operationID := "018f0000-0000-7000-8000-000000000014"
	manifestHash := sha256.Sum256([]byte("death boundary manifest"))
	head := bytes.Repeat([]byte{0x71}, sha256.Size)
	targets := []tammyv1.RestoreState{
		tammyv1.RestoreState_RESTORE_STATE_PREPARED,
		tammyv1.RestoreState_RESTORE_STATE_STAGED,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
		tammyv1.RestoreState_RESTORE_STATE_COMPLETE,
	}
	for _, target := range targets {
		for _, boundary := range []string{"write", "file_sync", "rename", "directory_sync"} {
			t.Run(target.String()+"/"+boundary, func(t *testing.T) {
				directory := t.TempDir()
				from := prepareJournalBefore(t, directory, operationID, manifestHash[:], head, target)
				injected := errors.New("injected process death")
				hooks := normalizedJournalHooks(nil)
				switch boundary {
				case "write":
					hooks.write = func(file *os.File, value []byte) (int, error) {
						written, _ := file.Write(value[:1])
						return written, injected
					}
				case "file_sync":
					hooks.syncFile = func(*os.File) error { return injected }
				case "rename":
					hooks.rename = func(*os.Root, string, string) error { return injected }
				case "directory_sync":
					hooks.syncDirectory = func(*os.Root) error { return injected }
				}
				store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now, hooks: &hooks})
				if err != nil {
					t.Fatal(err)
				}
				if target == tammyv1.RestoreState_RESTORE_STATE_PREPARED {
					_, err = store.Prepare(context.Background(), operationID, manifestHash[:])
				} else {
					_, err = store.Advance(context.Background(), operationID, from, target, head)
				}
				if !errors.Is(err, ErrJournal) {
					t.Fatalf("injected boundary error = %v, want ErrJournal", err)
				}
				_ = store.Close()

				reopened, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				status, getErr := reopened.Get(context.Background(), operationID)
				if boundary == "directory_sync" {
					if getErr != nil || status.State != target {
						t.Fatalf("rename-visible status = (%#v, %v), want %s", status, getErr, target)
					}
					var replay *tammyv1.RestoreStatus
					if target == tammyv1.RestoreState_RESTORE_STATE_PREPARED {
						replay, err = reopened.Prepare(context.Background(), operationID, manifestHash[:])
					} else {
						replay, err = reopened.Advance(context.Background(), operationID, from, target, head)
					}
					if err != nil || replay.State != target {
						t.Fatalf("idempotent rename-visible replay = (%#v, %v)", replay, err)
					}
					return
				}
				if target == tammyv1.RestoreState_RESTORE_STATE_PREPARED {
					if !errors.Is(getErr, os.ErrNotExist) {
						t.Fatalf("pre-rename Prepare residue = (%#v, %v), want not-exist", status, getErr)
					}
				} else if getErr != nil || status.State != from {
					t.Fatalf("pre-rename prior status = (%#v, %v), want %s", status, getErr, from)
				}
			})
		}
	}
}

func TestRestoreJournalStartupRemovesOnlyOwnedPartialTemp(t *testing.T) {
	directory := t.TempDir()
	auth := journalAuthKey()
	owned := make([]string, 257)
	for index := range owned {
		operationID := fmt.Sprintf("018f6500-0000-7000-8000-%012x", index+1)
		owned[index] = filepath.Join(directory, testJournalTempName(auth, operationID))
		if err := os.WriteFile(owned[index], []byte{byte(index)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plainUUID := filepath.Join(directory, ".018f6500-0000-7000-8000-000000000999.tmp")
	mutatedName := []byte(filepath.Base(owned[0]))
	if mutatedName[len(mutatedName)-len(".tmp")-1] == '0' {
		mutatedName[len(mutatedName)-len(".tmp")-1] = '1'
	} else {
		mutatedName[len(mutatedName)-len(".tmp")-1] = '0'
	}
	mutatedTag := filepath.Join(directory, string(mutatedName))
	foreign := filepath.Join(directory, ".foreign.tmp")
	for path, contents := range map[string][]byte{plainUUID: []byte("plain UUID is foreign"),
		mutatedTag: []byte("mutated tag is foreign"), foreign: []byte("foreign")} {
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	remaining := 0
	for _, path := range owned {
		if _, err := os.Lstat(path); err == nil {
			remaining++
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	if remaining != 1 {
		t.Fatalf("owned temps remaining after first bounded cleanup=%d, want 1", remaining)
	}
	reopened, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: auth, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, path := range owned {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned temp remains after second cleanup: %s: %v", path, err)
		}
	}
	for path, want := range map[string]string{plainUUID: "plain UUID is foreign", mutatedTag: "mutated tag is foreign", foreign: "foreign"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("foreign temp %s = %q, %v", path, got, err)
		}
	}
}

func testJournalTempName(auth []byte, operationID string) string {
	digest := hmac.New(sha256.New, auth)
	_, _ = digest.Write([]byte("tammy.restore.journal.temp.v1\x00"))
	_, _ = digest.Write([]byte(operationID))
	return ".tammy-restore-journal-" + operationID + "-" + hex.EncodeToString(digest.Sum(nil)) + ".tmp"
}

func prepareJournalBefore(t *testing.T, directory, operationID string, manifestHash, head []byte, target tammyv1.RestoreState) tammyv1.RestoreState {
	t.Helper()
	if target == tammyv1.RestoreState_RESTORE_STATE_PREPARED {
		return tammyv1.RestoreState_RESTORE_STATE_UNSPECIFIED
	}
	store, err := NewJournalStore(JournalConfig{Directory: directory, AuthenticationKey: journalAuthKey(), Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prepare(context.Background(), operationID, manifestHash); err != nil {
		t.Fatal(err)
	}
	current := tammyv1.RestoreState_RESTORE_STATE_PREPARED
	for _, next := range []tammyv1.RestoreState{
		tammyv1.RestoreState_RESTORE_STATE_STAGED,
		tammyv1.RestoreState_RESTORE_STATE_SWAPPED,
	} {
		if next == target {
			break
		}
		if _, err := store.Advance(context.Background(), operationID, current, next, head); err != nil {
			t.Fatal(err)
		}
		current = next
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return current
}
