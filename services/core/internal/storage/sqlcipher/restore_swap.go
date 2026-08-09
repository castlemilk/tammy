//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

var ErrRestoreSwap = errors.New("sqlcipher: restore swap failed")

const (
	restoreBoundaryRollbackActiveToStageRename       = "rollback_active_to_stage_rename"
	restoreBoundaryRollbackActiveToStageSync         = "rollback_active_to_stage_sync"
	restoreBoundaryRollbackPredecessorToActiveRename = "rollback_predecessor_to_active_rename"
	restoreBoundaryRollbackPredecessorToActiveSync   = "rollback_predecessor_to_active_sync"
	restoreBoundaryRollbackStageRemove               = "rollback_stage_remove"
	restoreBoundaryRollbackStageRemoveSync           = "rollback_stage_remove_sync"
	restoreBoundaryStageMarkerRemove                 = "stage_marker_remove"
	restoreBoundaryStageMarkerRemoveSync             = "stage_marker_remove_sync"
	restoreBoundaryRollbackMarkerRemove              = "rollback_marker_remove"
	restoreBoundaryRollbackMarkerRemoveSync          = "rollback_marker_remove_sync"
)

type restoreSwapHooks struct {
	afterSideEffect func(string) error
}

func runRestoreSwapHook(hooks *restoreSwapHooks, boundary string) error {
	if hooks == nil || hooks.afterSideEffect == nil {
		return nil
	}
	if err := hooks.afterSideEffect(boundary); err != nil {
		return errors.Join(ErrRestoreSwap, err)
	}
	return nil
}

// RestoreSwapReceipt is issued only after both durable renames and binds
// rollback/cleanup to the exact file identities observed under the exclusive
// workspace lock.
type RestoreSwapReceipt struct {
	mu                 sync.Mutex
	activePath         string
	stageName          string
	rollbackName       string
	operationID        string
	receiptID          string
	activeIdentity     os.FileInfo
	rollbackIdentity   os.FileInfo
	ownershipDigest    [sha256.Size]byte
	stageMarkerHash    [sha256.Size]byte
	rollbackMarkerHash [sha256.Size]byte
	predecessorHash    [sha256.Size]byte
	activatedHash      [sha256.Size]byte
	hooks              *restoreSwapHooks
	terminal           bool
}

// RestoreReservationLock holds the exclusive workspace file lock from the
// predecessor byte-hash capture through the atomic swap. It is opaque and
// single-use so the capture cannot race a writer before active->rollback.
type RestoreReservationLock struct {
	mu              sync.Mutex
	activePath      string
	lock            *workspaceFileLock
	activeIdentity  os.FileInfo
	stagedIdentity  os.FileInfo
	predecessorHash [sha256.Size]byte
	activatedHash   [sha256.Size]byte
	transferred     bool
	terminal        bool
}

func ReserveWorkspaceRestore(
	ctx context.Context,
	activePath string,
	stagedPath string,
	verifiedStagedIdentity os.FileInfo,
	verifiedStagedHash []byte,
) (*RestoreReservationLock, error) {
	if ctx == nil || ctx.Err() != nil || verifiedStagedIdentity == nil || len(verifiedStagedHash) != sha256.Size {
		return nil, ErrRestoreSwap
	}
	active, err := validateDatabasePath(activePath)
	if err != nil {
		return nil, ErrRestoreSwap
	}
	staged, err := validateDatabasePath(stagedPath)
	if err != nil || filepath.Dir(staged) != filepath.Dir(active) || filepath.Base(staged) == filepath.Base(active) {
		return nil, ErrRestoreSwap
	}
	lock, err := acquireWorkspaceFileLock(active, true)
	if err != nil {
		return nil, errors.Join(ErrRestoreSwap, err)
	}
	root, _, err := openVerifiedSwapRoot(filepath.Dir(active))
	if err != nil {
		_ = lock.close()
		return nil, err
	}
	defer root.Close()
	activeIdentity, err := root.Lstat(filepath.Base(active))
	if err != nil || validateDatabaseFileSecurity(activeIdentity) != nil {
		_ = lock.close()
		return nil, ErrRestoreSwap
	}
	predecessorHash, err := hashRestoreDatabase(root, filepath.Base(active), activeIdentity)
	if err != nil {
		_ = lock.close()
		return nil, err
	}
	stagedIdentity, err := root.Lstat(filepath.Base(staged))
	if err != nil || validateDatabaseFileSecurity(stagedIdentity) != nil || !os.SameFile(stagedIdentity, verifiedStagedIdentity) {
		_ = lock.close()
		zeroRestoreHash(&predecessorHash)
		return nil, ErrRestoreSwap
	}
	activatedHash, err := hashRestoreDatabase(root, filepath.Base(staged), stagedIdentity)
	if err != nil || subtle.ConstantTimeCompare(activatedHash[:], verifiedStagedHash) != 1 {
		_ = lock.close()
		zeroRestoreHash(&predecessorHash)
		zeroRestoreHash(&activatedHash)
		return nil, ErrRestoreSwap
	}
	return &RestoreReservationLock{activePath: active, lock: lock, activeIdentity: activeIdentity,
		stagedIdentity: stagedIdentity, predecessorHash: predecessorHash, activatedHash: activatedHash}, nil
}

func (reservation *RestoreReservationLock) PredecessorHash() []byte {
	if reservation == nil {
		return nil
	}
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.terminal || reservation.transferred {
		return nil
	}
	return append([]byte(nil), reservation.predecessorHash[:]...)
}

func (reservation *RestoreReservationLock) ActivatedHash() []byte {
	if reservation == nil {
		return nil
	}
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.terminal || reservation.transferred {
		return nil
	}
	return append([]byte(nil), reservation.activatedHash[:]...)
}

func ReleaseWorkspaceRestoreReservation(reservation *RestoreReservationLock) error {
	if reservation == nil {
		return ErrRestoreSwap
	}
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.terminal {
		return nil
	}
	if reservation.transferred || reservation.lock == nil {
		return ErrRestoreSwap
	}
	err := reservation.lock.close()
	reservation.lock = nil
	reservation.terminal = true
	zeroRestoreHash(&reservation.predecessorHash)
	zeroRestoreHash(&reservation.activatedHash)
	if err != nil {
		return ErrRestoreSwap
	}
	return nil
}

func (receipt *RestoreSwapReceipt) ReceiptID() string {
	if receipt == nil {
		return ""
	}
	return receipt.receiptID
}

func SwapWorkspaceForRestore(
	ctx context.Context,
	activePath string,
	stagedPath string,
	operationID string,
	workspaceID string,
	rollbackName string,
	receiptID string,
	ownershipDigest []byte,
	stageMarkerHash []byte,
	rollbackMarkerHash []byte,
	predecessorHash []byte,
	activatedHash []byte,
	reservation *RestoreReservationLock,
) (*RestoreSwapReceipt, error) {
	if ctx == nil || ctx.Err() != nil || !ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(workspaceID) ||
		!ids.IsCanonicalV7(receiptID) || len(ownershipDigest) != sha256.Size || len(stageMarkerHash) != sha256.Size ||
		len(rollbackMarkerHash) != sha256.Size || len(predecessorHash) != sha256.Size || len(activatedHash) != sha256.Size ||
		reservation == nil {
		return nil, ErrRestoreSwap
	}
	active, err := validateDatabasePath(activePath)
	if err != nil {
		return nil, ErrRestoreSwap
	}
	staged, err := validateDatabasePath(stagedPath)
	stageName := filepath.Base(staged)
	if err != nil || filepath.Dir(active) != filepath.Dir(staged) ||
		!validRestoreArtifactName(".tammy-restore-stage-", operationID, workspaceID, stageName) ||
		!validRestoreArtifactName(".tammy-restore-rollback-", operationID, workspaceID, rollbackName) {
		return nil, ErrRestoreSwap
	}
	reservation.mu.Lock()
	defer reservation.mu.Unlock()
	if reservation.terminal || reservation.transferred || reservation.lock == nil || reservation.activePath != active ||
		subtle.ConstantTimeCompare(reservation.predecessorHash[:], predecessorHash) != 1 ||
		subtle.ConstantTimeCompare(reservation.activatedHash[:], activatedHash) != 1 {
		return nil, ErrRestoreSwap
	}
	root, baseInfo, err := openVerifiedSwapRoot(filepath.Dir(active))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	activeName := filepath.Base(active)
	if _, err := root.Lstat(rollbackName); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrRestoreSwap
	}
	if !sameRootIdentity(root, activeName, reservation.activeIdentity) {
		return nil, ErrRestoreSwap
	}
	activeHash, err := hashRestoreDatabase(root, activeName, reservation.activeIdentity)
	if err != nil || subtle.ConstantTimeCompare(activeHash[:], predecessorHash) != 1 {
		zeroRestoreHash(&activeHash)
		return nil, ErrRestoreSwap
	}
	zeroRestoreHash(&activeHash)
	if !restoreArtifactMarkersMatch(root, stageName, rollbackName, ownershipDigest, stageMarkerHash, rollbackMarkerHash) {
		return nil, ErrRestoreSwap
	}
	stageIdentity := reservation.stagedIdentity
	if !sameRootIdentity(root, stageName, stageIdentity) {
		return nil, ErrRestoreSwap
	}
	currentActivatedHash, err := hashRestoreDatabase(root, stageName, stageIdentity)
	if err != nil || subtle.ConstantTimeCompare(currentActivatedHash[:], activatedHash) != 1 {
		zeroRestoreHash(&currentActivatedHash)
		return nil, ErrRestoreSwap
	}
	zeroRestoreHash(&currentActivatedHash)
	if err := root.Rename(activeName, rollbackName); err != nil || syncSwapRoot(root) != nil {
		return nil, ErrRestoreSwap
	}
	if err := root.Rename(stageName, activeName); err != nil {
		_ = root.Rename(rollbackName, activeName)
		_ = syncSwapRoot(root)
		return nil, ErrRestoreSwap
	}
	if err := syncSwapRoot(root); err != nil {
		return nil, ErrRestoreSwap
	}
	if !sameRootIdentity(root, activeName, stageIdentity) || !sameRootIdentity(root, rollbackName, reservation.activeIdentity) ||
		!sameDirectoryIdentity(filepath.Dir(active), baseInfo) {
		return nil, ErrRestoreSwap
	}
	if err := reservation.lock.close(); err != nil {
		return nil, ErrRestoreSwap
	}
	reservation.lock = nil
	reservation.transferred = true
	reservation.terminal = true
	zeroRestoreHash(&reservation.predecessorHash)
	zeroRestoreHash(&reservation.activatedHash)
	var boundOwnership [sha256.Size]byte
	copy(boundOwnership[:], ownershipDigest)
	var boundStageMarkerHash [sha256.Size]byte
	copy(boundStageMarkerHash[:], stageMarkerHash)
	var boundRollbackMarkerHash [sha256.Size]byte
	copy(boundRollbackMarkerHash[:], rollbackMarkerHash)
	var boundPredecessor [sha256.Size]byte
	copy(boundPredecessor[:], predecessorHash)
	var boundActivated [sha256.Size]byte
	copy(boundActivated[:], activatedHash)
	return &RestoreSwapReceipt{activePath: active, stageName: stageName, rollbackName: rollbackName, operationID: operationID,
		receiptID: receiptID, activeIdentity: stageIdentity, rollbackIdentity: reservation.activeIdentity,
		ownershipDigest: boundOwnership, stageMarkerHash: boundStageMarkerHash,
		rollbackMarkerHash: boundRollbackMarkerHash, predecessorHash: boundPredecessor,
		activatedHash: boundActivated}, nil
}

func RollbackWorkspaceRestore(ctx context.Context, receipt *RestoreSwapReceipt) error {
	return finishWorkspaceRestore(ctx, receipt, true)
}

func CommitWorkspaceRestore(ctx context.Context, receipt *RestoreSwapReceipt) error {
	return finishWorkspaceRestore(ctx, receipt, false)
}

// RecoverInterruptedWorkspaceRestore reverses only an authenticated restore
// operation's exact stage/rollback artifact set. It recognizes the durable
// rename boundaries used by SwapWorkspaceForRestore and rejects every
// ambiguous combination without touching any file.
func RecoverInterruptedWorkspaceRestore(
	ctx context.Context,
	activePath string,
	operationID string,
	workspaceID string,
	stageBasename string,
	rollbackBasename string,
	ownershipDigest []byte,
	stageMarkerHash []byte,
	rollbackMarkerHash []byte,
	predecessorHash []byte,
) error {
	if ctx == nil || ctx.Err() != nil || !ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(workspaceID) ||
		!validRestoreArtifactName(".tammy-restore-stage-", operationID, workspaceID, stageBasename) ||
		!validRestoreArtifactName(".tammy-restore-rollback-", operationID, workspaceID, rollbackBasename) ||
		len(ownershipDigest) != sha256.Size || len(stageMarkerHash) != sha256.Size ||
		len(rollbackMarkerHash) != sha256.Size || len(predecessorHash) != 0 && len(predecessorHash) != sha256.Size {
		return ErrRestoreSwap
	}
	active, err := validateDatabasePath(activePath)
	if err != nil || filepath.Base(active) == stageBasename || filepath.Base(active) == rollbackBasename {
		return ErrRestoreSwap
	}
	lock, err := acquireWorkspaceFileLock(active, true)
	if err != nil {
		return errors.Join(ErrRestoreSwap, err)
	}
	defer lock.close()
	root, baseInfo, err := openVerifiedSwapRoot(filepath.Dir(active))
	if err != nil {
		return err
	}
	defer root.Close()
	activeName := filepath.Base(active)
	activeInfo, activeExists, err := recoverableDatabaseIdentity(root, activeName)
	if err != nil {
		return err
	}
	_, stageExists, err := recoverableDatabaseIdentity(root, stageBasename)
	if err != nil {
		return err
	}
	rollbackInfo, rollbackExists, err := recoverableDatabaseIdentity(root, rollbackBasename)
	if err != nil {
		return err
	}
	markers, err := authenticateRestoreArtifactMarkers(root, stageBasename, rollbackBasename, ownershipDigest,
		stageMarkerHash, rollbackMarkerHash)
	if err != nil {
		return err
	}
	switch {
	case activeExists && stageExists && !rollbackExists:
		if !markers.both() {
			return ErrRestoreSwap
		}
		if err := removeRestoreDatabaseArtifacts(root, stageBasename); err != nil {
			return err
		}
		if err := removeRestoreArtifactMarkers(root, stageBasename, rollbackBasename, markers,
			stageMarkerHash, rollbackMarkerHash, nil); err != nil {
			return err
		}
	case !activeExists && stageExists && rollbackExists:
		if !markers.both() ||
			!restoreRollbackHashMatches(root, rollbackBasename, rollbackInfo, predecessorHash) {
			return ErrRestoreSwap
		}
		if err := root.Rename(rollbackBasename, activeName); err != nil || syncSwapRoot(root) != nil ||
			!sameRootIdentity(root, activeName, rollbackInfo) {
			return ErrRestoreSwap
		}
		if err := removeRestoreDatabaseArtifacts(root, stageBasename); err != nil {
			return err
		}
		if err := removeRestoreArtifactMarkers(root, stageBasename, rollbackBasename, markers,
			stageMarkerHash, rollbackMarkerHash, nil); err != nil {
			return err
		}
	case activeExists && !stageExists && rollbackExists:
		if !markers.both() ||
			!restoreRollbackHashMatches(root, rollbackBasename, rollbackInfo, predecessorHash) {
			return ErrRestoreSwap
		}
		if _, err := root.Lstat(stageBasename); !errors.Is(err, os.ErrNotExist) {
			return ErrRestoreSwap
		}
		if err := root.Rename(activeName, stageBasename); err != nil || syncSwapRoot(root) != nil {
			return ErrRestoreSwap
		}
		if err := root.Rename(rollbackBasename, activeName); err != nil {
			_ = root.Rename(stageBasename, activeName)
			_ = syncSwapRoot(root)
			return ErrRestoreSwap
		}
		if err := syncSwapRoot(root); err != nil || !sameRootIdentity(root, activeName, rollbackInfo) {
			return ErrRestoreSwap
		}
		if !sameRootIdentity(root, stageBasename, activeInfo) || root.Remove(stageBasename) != nil || syncSwapRoot(root) != nil {
			return ErrRestoreSwap
		}
		if err := removeRestoreArtifactMarkers(root, stageBasename, rollbackBasename, markers,
			stageMarkerHash, rollbackMarkerHash, nil); err != nil {
			return err
		}
	case activeExists && !stageExists && !rollbackExists:
		// A prior recovery already reached the safe terminal cleanup.
		if markers.any() {
			if err := removeRestoreArtifactMarkers(root, stageBasename, rollbackBasename, markers,
				stageMarkerHash, rollbackMarkerHash, nil); err != nil {
				return err
			}
		}
	default:
		return ErrRestoreSwap
	}
	if !sameDirectoryIdentity(filepath.Dir(active), baseInfo) {
		return ErrRestoreSwap
	}
	return nil
}

// CleanupCompletedWorkspaceRestore removes only the exact operation-derived
// stage/rollback residue after the caller has durably recorded COMPLETE.
func CleanupCompletedWorkspaceRestore(
	ctx context.Context,
	activePath string,
	operationID string,
	workspaceID string,
	stageBasename string,
	rollbackBasename string,
	ownershipDigest []byte,
	stageMarkerHash []byte,
	rollbackMarkerHash []byte,
	predecessorHash []byte,
	activatedHash []byte,
) error {
	if ctx == nil || ctx.Err() != nil || !ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(workspaceID) ||
		!validRestoreArtifactName(".tammy-restore-stage-", operationID, workspaceID, stageBasename) ||
		!validRestoreArtifactName(".tammy-restore-rollback-", operationID, workspaceID, rollbackBasename) ||
		len(ownershipDigest) != sha256.Size || len(stageMarkerHash) != sha256.Size ||
		len(rollbackMarkerHash) != sha256.Size || len(predecessorHash) != sha256.Size || len(activatedHash) != sha256.Size {
		return ErrRestoreSwap
	}
	active, err := validateDatabasePath(activePath)
	if err != nil {
		return ErrRestoreSwap
	}
	lock, err := acquireWorkspaceFileLock(active, true)
	if err != nil {
		return errors.Join(ErrRestoreSwap, err)
	}
	defer lock.close()
	root, baseInfo, err := openVerifiedSwapRoot(filepath.Dir(active))
	if err != nil {
		return err
	}
	defer root.Close()
	activeName := filepath.Base(active)
	activeInfo, exists, err := recoverableDatabaseIdentity(root, activeName)
	if err != nil || !exists {
		return ErrRestoreSwap
	}
	activeHash, err := hashRestoreDatabase(root, activeName, activeInfo)
	if err != nil || subtle.ConstantTimeCompare(activeHash[:], activatedHash) != 1 {
		zeroRestoreHash(&activeHash)
		return ErrRestoreSwap
	}
	zeroRestoreHash(&activeHash)
	if err := validateRestoreDatabaseArtifacts(root, stageBasename); err != nil {
		return err
	}
	if err := validateRestoreDatabaseArtifacts(root, rollbackBasename); err != nil {
		return err
	}
	stageArtifactsExist, err := restoreDatabaseArtifactsExist(root, stageBasename)
	if err != nil {
		return err
	}
	rollbackArtifactsExist, err := restoreDatabaseArtifactsExist(root, rollbackBasename)
	if err != nil {
		return err
	}
	rollbackIdentity, rollbackExists, err := recoverableDatabaseIdentity(root, rollbackBasename)
	if err != nil || stageArtifactsExist || rollbackArtifactsExist != rollbackExists {
		return ErrRestoreSwap
	}
	markers, err := authenticateRestoreArtifactMarkers(root, stageBasename, rollbackBasename, ownershipDigest,
		stageMarkerHash, rollbackMarkerHash)
	if err != nil {
		return ErrRestoreSwap
	}
	if rollbackExists {
		if !markers.both() ||
			!restoreRollbackHashMatches(root, rollbackBasename, rollbackIdentity, predecessorHash) {
			return ErrRestoreSwap
		}
	}

	// Every allowed COMPLETE combination is classified and authenticated above
	// before the first mutation. A stage artifact is never valid after swap.
	if rollbackExists {
		if err := removeRestoreDatabaseArtifacts(root, rollbackBasename); err != nil {
			return err
		}
		if err := removeRestoreArtifactMarkers(root, stageBasename, rollbackBasename, markers,
			stageMarkerHash, rollbackMarkerHash, nil); err != nil {
			return err
		}
	} else if markers.any() {
		if err := removeRestoreArtifactMarkers(root, stageBasename, rollbackBasename, markers,
			stageMarkerHash, rollbackMarkerHash, nil); err != nil {
			return err
		}
	}
	if !sameRootIdentity(root, activeName, activeInfo) || !sameDirectoryIdentity(filepath.Dir(active), baseInfo) {
		return ErrRestoreSwap
	}
	return nil
}

func restoreDatabaseArtifactsExist(root *os.Root, databaseName string) (bool, error) {
	anyExists := false
	mainExists := false
	for _, name := range []string{databaseName + "-wal", databaseName + "-shm", databaseName + ".lock", databaseName} {
		_, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, ErrRestoreSwap
		}
		anyExists = true
		if name == databaseName {
			mainExists = true
		}
	}
	if anyExists != mainExists {
		return false, ErrRestoreSwap
	}
	return anyExists, nil
}

func finishWorkspaceRestore(ctx context.Context, receipt *RestoreSwapReceipt, rollback bool) error {
	if ctx == nil || receipt == nil || ctx.Err() != nil {
		return ErrRestoreSwap
	}
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	if receipt.terminal || !ids.IsCanonicalV7(receipt.operationID) || !ids.IsCanonicalV7(receipt.receiptID) {
		return ErrRestoreSwap
	}
	lock, err := acquireWorkspaceFileLock(receipt.activePath, true)
	if err != nil {
		return errors.Join(ErrRestoreSwap, err)
	}
	defer lock.close()
	root, baseInfo, err := openVerifiedSwapRoot(filepath.Dir(receipt.activePath))
	if err != nil {
		return err
	}
	defer root.Close()
	activeName := filepath.Base(receipt.activePath)
	if !sameRootIdentity(root, activeName, receipt.activeIdentity) ||
		!sameRootIdentity(root, receipt.rollbackName, receipt.rollbackIdentity) {
		return ErrRestoreSwap
	}
	activeHash, err := hashRestoreDatabase(root, activeName, receipt.activeIdentity)
	if err != nil || subtle.ConstantTimeCompare(activeHash[:], receipt.activatedHash[:]) != 1 {
		zeroRestoreHash(&activeHash)
		return ErrRestoreSwap
	}
	zeroRestoreHash(&activeHash)
	rollbackHash, err := hashRestoreDatabase(root, receipt.rollbackName, receipt.rollbackIdentity)
	if err != nil || subtle.ConstantTimeCompare(rollbackHash[:], receipt.predecessorHash[:]) != 1 ||
		!restoreArtifactMarkersMatch(root, receipt.stageName, receipt.rollbackName, receipt.ownershipDigest[:],
			receipt.stageMarkerHash[:], receipt.rollbackMarkerHash[:]) {
		zeroRestoreHash(&rollbackHash)
		return ErrRestoreSwap
	}
	zeroRestoreHash(&rollbackHash)
	if rollback {
		if _, err := root.Lstat(receipt.stageName); !errors.Is(err, os.ErrNotExist) {
			return ErrRestoreSwap
		}
		if err := root.Rename(activeName, receipt.stageName); err != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(receipt.hooks, restoreBoundaryRollbackActiveToStageRename); err != nil {
			return err
		}
		if syncSwapRoot(root) != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(receipt.hooks, restoreBoundaryRollbackActiveToStageSync); err != nil {
			return err
		}
		if err := root.Rename(receipt.rollbackName, activeName); err != nil {
			_ = root.Rename(receipt.stageName, activeName)
			_ = syncSwapRoot(root)
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(receipt.hooks, restoreBoundaryRollbackPredecessorToActiveRename); err != nil {
			return err
		}
		if err := syncSwapRoot(root); err != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(receipt.hooks, restoreBoundaryRollbackPredecessorToActiveSync); err != nil {
			return err
		}
		if !sameRootIdentity(root, activeName, receipt.rollbackIdentity) {
			return ErrRestoreSwap
		}
		if err := root.Remove(receipt.stageName); err != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(receipt.hooks, restoreBoundaryRollbackStageRemove); err != nil {
			return err
		}
		if syncSwapRoot(root) != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(receipt.hooks, restoreBoundaryRollbackStageRemoveSync); err != nil {
			return err
		}
	} else {
		if err := root.Remove(receipt.rollbackName); err != nil || syncSwapRoot(root) != nil {
			return ErrRestoreSwap
		}
	}
	markers := restoreArtifactMarkerState{stage: true, rollback: true}
	if err := removeRestoreArtifactMarkers(root, receipt.stageName, receipt.rollbackName, markers,
		receipt.stageMarkerHash[:], receipt.rollbackMarkerHash[:], receipt.hooks); err != nil {
		return err
	}
	if !sameDirectoryIdentity(filepath.Dir(receipt.activePath), baseInfo) {
		return ErrRestoreSwap
	}
	receipt.terminal = true
	zeroRestoreHash(&receipt.ownershipDigest)
	zeroRestoreHash(&receipt.stageMarkerHash)
	zeroRestoreHash(&receipt.rollbackMarkerHash)
	zeroRestoreHash(&receipt.predecessorHash)
	zeroRestoreHash(&receipt.activatedHash)
	return nil
}

func recoverableDatabaseIdentity(root *os.Root, name string) (os.FileInfo, bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || validateDatabaseFileSecurity(info) != nil {
		return nil, false, ErrRestoreSwap
	}
	return info, true, nil
}

func removeRestoreDatabaseArtifacts(root *os.Root, databaseName string) error {
	if err := validateRestoreDatabaseArtifacts(root, databaseName); err != nil {
		return err
	}
	for _, name := range []string{databaseName + "-wal", databaseName + "-shm", databaseName + ".lock", databaseName} {
		_, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := root.Remove(name); err != nil {
			return ErrRestoreSwap
		}
	}
	if err := syncSwapRoot(root); err != nil {
		return ErrRestoreSwap
	}
	return nil
}

func validateRestoreDatabaseArtifacts(root *os.Root, databaseName string) error {
	for _, name := range []string{databaseName + "-wal", databaseName + "-shm", databaseName + ".lock", databaseName} {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 ||
			(name == databaseName+".lock" && info.Size() != 0) {
			return ErrRestoreSwap
		}
	}
	return nil
}

func openVerifiedSwapRoot(directory string) (*os.Root, os.FileInfo, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrRestoreSwap
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, nil, ErrRestoreSwap
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, rootInfo) {
		_ = root.Close()
		return nil, nil, ErrRestoreSwap
	}
	return root, info, nil
}

func sameRootIdentity(root *os.Root, name string, expected os.FileInfo) bool {
	current, err := root.Lstat(name)
	return err == nil && expected != nil && validateDatabaseFileSecurity(current) == nil && os.SameFile(current, expected)
}

func sameDirectoryIdentity(path string, expected os.FileInfo) bool {
	current, err := os.Lstat(path)
	return err == nil && expected != nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, expected)
}

func hashRestoreDatabase(root *os.Root, name string, expected os.FileInfo) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if expected == nil || validateDatabaseFileSecurity(expected) != nil || !sameRootIdentity(root, name, expected) {
		return result, ErrRestoreSwap
	}
	file, err := root.Open(name)
	if err != nil {
		return result, ErrRestoreSwap
	}
	digest := sha256.New()
	buffer := make([]byte, 32*1024)
	_, copyErr := io.CopyBuffer(digest, file, buffer)
	for index := range buffer {
		buffer[index] = 0
	}
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || !sameRootIdentity(root, name, expected) {
		return result, ErrRestoreSwap
	}
	copy(result[:], digest.Sum(nil))
	return result, nil
}

func zeroRestoreHash(hash *[sha256.Size]byte) {
	if hash == nil {
		return
	}
	for index := range hash {
		hash[index] = 0
	}
}

func validRestoreArtifactName(prefix, operationID, workspaceID, name string) bool {
	wantPrefix := prefix + operationID + "-" + workspaceID + "-"
	if !strings.HasPrefix(name, wantPrefix) || !strings.HasSuffix(name, ".db") {
		return false
	}
	tag := strings.TrimSuffix(strings.TrimPrefix(name, wantPrefix), ".db")
	decoded, err := hex.DecodeString(tag)
	return err == nil && len(decoded) == sha256.Size && tag == strings.ToLower(tag)
}

type restoreArtifactMarkerState struct {
	stage    bool
	rollback bool
}

func (state restoreArtifactMarkerState) any() bool  { return state.stage || state.rollback }
func (state restoreArtifactMarkerState) both() bool { return state.stage && state.rollback }

func restoreArtifactMarkersMatch(
	root *os.Root,
	stageName string,
	rollbackName string,
	expectedDigest []byte,
	expectedStageHash []byte,
	expectedRollbackHash []byte,
) bool {
	state, err := authenticateRestoreArtifactMarkers(root, stageName, rollbackName, expectedDigest,
		expectedStageHash, expectedRollbackHash)
	return err == nil && state.both()
}

func authenticateRestoreArtifactMarkers(
	root *os.Root,
	stageName string,
	rollbackName string,
	expectedDigest []byte,
	expectedStageHash []byte,
	expectedRollbackHash []byte,
) (restoreArtifactMarkerState, error) {
	var state restoreArtifactMarkerState
	if len(expectedDigest) != sha256.Size || len(expectedStageHash) != sha256.Size ||
		len(expectedRollbackHash) != sha256.Size {
		return state, ErrRestoreSwap
	}
	stageMarker, stageExists, err := readOptionalRestoreArtifactMarker(root, stageName+".owner", expectedStageHash)
	if err != nil {
		return state, err
	}
	defer zeroRestoreBytes(stageMarker)
	rollbackMarker, rollbackExists, err := readOptionalRestoreArtifactMarker(root, rollbackName+".owner", expectedRollbackHash)
	if err != nil {
		return state, err
	}
	defer zeroRestoreBytes(rollbackMarker)
	state = restoreArtifactMarkerState{stage: stageExists, rollback: rollbackExists}
	if !state.both() {
		return state, nil
	}
	digest := sha256.New()
	_, _ = digest.Write(stageMarker)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(rollbackMarker)
	actual := digest.Sum(nil)
	valid := subtle.ConstantTimeCompare(actual, expectedDigest) == 1
	zeroRestoreBytes(actual)
	if !valid {
		return restoreArtifactMarkerState{}, ErrRestoreSwap
	}
	return state, nil
}

func readOptionalRestoreArtifactMarker(root *os.Root, name string, expectedHash []byte) ([]byte, bool, error) {
	if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, ErrRestoreSwap
	}
	marker, err := readRestoreArtifactMarker(root, name)
	if err != nil {
		return nil, false, err
	}
	digest := sha256.Sum256(marker)
	valid := subtle.ConstantTimeCompare(digest[:], expectedHash) == 1
	zeroRestoreHash(&digest)
	if !valid {
		zeroRestoreBytes(marker)
		return nil, false, ErrRestoreSwap
	}
	return marker, true, nil
}

func readRestoreArtifactMarker(root *os.Root, name string) ([]byte, error) {
	const markerBytes = 2 * sha256.Size
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() != markerBytes {
		return nil, ErrRestoreSwap
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrRestoreSwap
	}
	contents := make([]byte, markerBytes)
	_, readErr := io.ReadFull(file, contents)
	var extra [1]byte
	extraCount, extraErr := file.Read(extra[:])
	closeErr := file.Close()
	if readErr != nil || extraCount != 0 || extraErr != io.EOF || closeErr != nil {
		zeroRestoreBytes(contents)
		return nil, ErrRestoreSwap
	}
	return contents, nil
}

func zeroRestoreBytes(bytes []byte) {
	for index := range bytes {
		bytes[index] = 0
	}
}

func restoreRollbackHashMatches(root *os.Root, name string, identity os.FileInfo, expected []byte) bool {
	if len(expected) != sha256.Size {
		return false
	}
	actual, err := hashRestoreDatabase(root, name, identity)
	if err != nil {
		zeroRestoreHash(&actual)
		return false
	}
	valid := subtle.ConstantTimeCompare(actual[:], expected) == 1
	zeroRestoreHash(&actual)
	return valid
}

func removeRestoreArtifactMarkers(
	root *os.Root,
	stageName string,
	rollbackName string,
	state restoreArtifactMarkerState,
	expectedStageHash []byte,
	expectedRollbackHash []byte,
	hooks *restoreSwapHooks,
) error {
	for _, marker := range []struct {
		name           string
		exists         bool
		expected       []byte
		removeBoundary string
		syncBoundary   string
	}{
		{name: stageName + ".owner", exists: state.stage, expected: expectedStageHash,
			removeBoundary: restoreBoundaryStageMarkerRemove, syncBoundary: restoreBoundaryStageMarkerRemoveSync},
		{name: rollbackName + ".owner", exists: state.rollback, expected: expectedRollbackHash,
			removeBoundary: restoreBoundaryRollbackMarkerRemove, syncBoundary: restoreBoundaryRollbackMarkerRemoveSync},
	} {
		if !marker.exists {
			continue
		}
		contents, exists, err := readOptionalRestoreArtifactMarker(root, marker.name, marker.expected)
		zeroRestoreBytes(contents)
		if err != nil || !exists || root.Remove(marker.name) != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(hooks, marker.removeBoundary); err != nil {
			return err
		}
		if syncSwapRoot(root) != nil {
			return ErrRestoreSwap
		}
		if err := runRestoreSwapHook(hooks, marker.syncBoundary); err != nil {
			return err
		}
	}
	return nil
}

func syncSwapRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
