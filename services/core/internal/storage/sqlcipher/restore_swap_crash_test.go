//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	testRestoreOperationID = "018f0000-0000-7000-8000-000000000011"
	testRestoreWorkspaceID = "018f0000-0000-7000-8000-000000000012"
	testRestoreReceiptID   = "018f0000-0000-7000-8000-000000000013"
)

func TestRollbackWorkspaceRestoreResumesAfterEveryDurableSideEffect(t *testing.T) {
	boundaries := []string{
		restoreBoundaryRollbackActiveToStageRename,
		restoreBoundaryRollbackActiveToStageSync,
		restoreBoundaryRollbackPredecessorToActiveRename,
		restoreBoundaryRollbackPredecessorToActiveSync,
		restoreBoundaryRollbackStageRemove,
		restoreBoundaryRollbackStageRemoveSync,
		restoreBoundaryStageMarkerRemove,
		restoreBoundaryStageMarkerRemoveSync,
		restoreBoundaryRollbackMarkerRemove,
		restoreBoundaryRollbackMarkerRemoveSync,
	}
	injected := errors.New("injected process death")
	for _, boundary := range boundaries {
		t.Run(boundary, func(t *testing.T) {
			fixture := newRestoreRollbackCrashFixture(t)
			fired := false
			fixture.receipt.hooks = &restoreSwapHooks{afterSideEffect: func(actual string) error {
				if actual == boundary && !fired {
					fired = true
					return injected
				}
				return nil
			}}
			if err := RollbackWorkspaceRestore(context.Background(), fixture.receipt); !errors.Is(err, injected) {
				t.Fatalf("RollbackWorkspaceRestore() error=%v, want injected death", err)
			}
			if !fired {
				t.Fatal("requested crash boundary was not reached")
			}
			for restart := 1; restart <= 2; restart++ {
				if err := RecoverInterruptedWorkspaceRestore(context.Background(), fixture.activePath,
					testRestoreOperationID, testRestoreWorkspaceID, fixture.stageName, fixture.rollbackName,
					fixture.ownershipDigest[:], fixture.stageMarkerHash[:], fixture.rollbackMarkerHash[:],
					fixture.predecessorHash[:]); err != nil {
					t.Fatalf("restart %d error=%v", restart, err)
				}
			}
			active, err := os.ReadFile(fixture.activePath)
			if err != nil || !bytes.Equal(active, fixture.predecessorBytes) {
				t.Fatalf("recovered active=%q error=%v", active, err)
			}
			for _, name := range []string{fixture.stageName, fixture.rollbackName,
				fixture.stageName + ".owner", fixture.rollbackName + ".owner"} {
				if _, err := os.Lstat(filepath.Join(fixture.directory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owned residue %q error=%v", name, err)
				}
			}
			unrelated, err := os.ReadFile(filepath.Join(fixture.directory, "unrelated.bin"))
			if err != nil || string(unrelated) != "unrelated" {
				t.Fatalf("unrelated bytes=%q error=%v", unrelated, err)
			}
		})
	}
}

func TestCommitWorkspaceRestoreRejectsChangedActivatedBytesBeforeCleanup(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "truncated", mutate: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Truncate(path, 17); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mutated_in_place", mutate: func(t *testing.T, path string) {
			t.Helper()
			file, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteAt([]byte("changed"), 0); err != nil {
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
			fixture := newRestoreRollbackCrashFixture(t)
			test.mutate(t, fixture.activePath)
			before := snapshotRestoreCrashDirectory(t, fixture.directory)
			if err := CommitWorkspaceRestore(context.Background(), fixture.receipt); !errors.Is(err, ErrRestoreSwap) {
				t.Fatalf("CommitWorkspaceRestore() error=%v, want %v", err, ErrRestoreSwap)
			}
			after := snapshotRestoreCrashDirectory(t, fixture.directory)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("commit mutated directory\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestCommitWorkspaceRestoreRemovesAuthenticatedPredecessor(t *testing.T) {
	fixture := newRestoreRollbackCrashFixture(t)
	if err := CommitWorkspaceRestore(context.Background(), fixture.receipt); err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(fixture.activePath)
	if err != nil || !bytes.Equal(active, fixture.activatedBytes) {
		t.Fatalf("active=%x error=%v", active, err)
	}
	for _, name := range []string{fixture.rollbackName, fixture.stageName + ".owner", fixture.rollbackName + ".owner"} {
		if _, err := os.Lstat(filepath.Join(fixture.directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned residue %q error=%v", name, err)
		}
	}
}

type restoreRollbackCrashFixture struct {
	directory          string
	activePath         string
	stageName          string
	rollbackName       string
	predecessorBytes   []byte
	activatedBytes     []byte
	ownershipDigest    [sha256.Size]byte
	stageMarkerHash    [sha256.Size]byte
	rollbackMarkerHash [sha256.Size]byte
	predecessorHash    [sha256.Size]byte
	activatedHash      [sha256.Size]byte
	receipt            *RestoreSwapReceipt
}

func newRestoreRollbackCrashFixture(t *testing.T) restoreRollbackCrashFixture {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	stageName := ".tammy-restore-stage-" + testRestoreOperationID + "-" + testRestoreWorkspaceID + "-" +
		strings.Repeat("a", 64) + ".db"
	rollbackName := ".tammy-restore-rollback-" + testRestoreOperationID + "-" + testRestoreWorkspaceID + "-" +
		strings.Repeat("b", 64) + ".db"
	activePath := filepath.Join(directory, "workspace.db")
	activeBytes := bytes.Repeat([]byte("verified restored database"), 128)
	predecessorBytes := bytes.Repeat([]byte("predecessor database"), 128)
	if err := os.WriteFile(activePath, activeBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, rollbackName), predecessorBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unrelated.bin"), []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	stageMarker := bytes.Repeat([]byte{0x51}, 2*sha256.Size)
	rollbackMarker := bytes.Repeat([]byte{0x52}, 2*sha256.Size)
	if err := os.WriteFile(filepath.Join(directory, stageName+".owner"), stageMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, rollbackName+".owner"), rollbackMarker, 0o600); err != nil {
		t.Fatal(err)
	}
	activeIdentity, err := os.Lstat(activePath)
	if err != nil {
		t.Fatal(err)
	}
	rollbackIdentity, err := os.Lstat(filepath.Join(directory, rollbackName))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, _ = digest.Write(stageMarker)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(rollbackMarker)
	var ownershipDigest [sha256.Size]byte
	copy(ownershipDigest[:], digest.Sum(nil))
	stageMarkerHash := sha256.Sum256(stageMarker)
	rollbackMarkerHash := sha256.Sum256(rollbackMarker)
	predecessorHash := sha256.Sum256(predecessorBytes)
	activatedHash := sha256.Sum256(activeBytes)
	receipt := &RestoreSwapReceipt{activePath: activePath, stageName: stageName, rollbackName: rollbackName,
		operationID: testRestoreOperationID, receiptID: testRestoreReceiptID, activeIdentity: activeIdentity,
		rollbackIdentity: rollbackIdentity, ownershipDigest: ownershipDigest, stageMarkerHash: stageMarkerHash,
		rollbackMarkerHash: rollbackMarkerHash, predecessorHash: predecessorHash, activatedHash: activatedHash}
	return restoreRollbackCrashFixture{directory: directory, activePath: activePath, stageName: stageName,
		rollbackName: rollbackName, predecessorBytes: predecessorBytes, activatedBytes: activeBytes,
		ownershipDigest: ownershipDigest,
		stageMarkerHash: stageMarkerHash, rollbackMarkerHash: rollbackMarkerHash,
		predecessorHash: predecessorHash, activatedHash: activatedHash, receipt: receipt}
}

func snapshotRestoreCrashDirectory(t *testing.T, directory string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			snapshot[entry.Name()] = contents
		}
	}
	return snapshot
}
