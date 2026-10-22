//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

const (
	restoreStageNameDomain    = "tammy.restore.stage-name.v1\x00"
	restoreRollbackNameDomain = "tammy.restore.rollback-name.v1\x00"
	restoreMarkerDomain       = "tammy.restore.artifact-marker.v1\x00"
	restoreMarkerBytes        = 2 * sha256.Size
	maximumArtifactScan       = 4096
)

type sqlcipherArtifactReservation struct {
	stageFile              *os.File
	stageIdentity          os.FileInfo
	stageMarkerIdentity    os.FileInfo
	rollbackMarkerIdentity os.FileInfo
	stageMarker            [restoreMarkerBytes]byte
	rollbackMarker         [restoreMarkerBytes]byte
	populated              bool
}

func (adapter *SQLCipherWorkspaceAdapter) ReserveRestoreArtifacts(
	ctx context.Context,
	operationID string,
	workspaceID string,
) (*RestoreArtifactReservation, error) {
	if adapter == nil || ctx == nil || !ids.IsCanonicalV7(operationID) || !ids.IsCanonicalV7(workspaceID) {
		return nil, ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || adapter.root == nil || len(adapter.key) != sqlcipher.KeySize || ctx.Err() != nil ||
		!sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		return nil, errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	stageName := authenticatedRestoreArtifactName(restoreStagePrefix, restoreStageNameDomain,
		adapter.key, operationID, workspaceID)
	rollbackName := authenticatedRestoreArtifactName(restoreRollbackPrefix, restoreRollbackNameDomain,
		adapter.key, operationID, workspaceID)
	if !validRestoreArtifactBasenames(operationID, workspaceID, stageName, rollbackName) {
		return nil, ErrSQLCipherWorkspace
	}
	for _, name := range []string{stageName, rollbackName, stageName + ".owner", rollbackName + ".owner"} {
		if _, err := adapter.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrSQLCipherWorkspace
		}
	}
	var token [sha256.Size]byte
	if _, err := io.ReadFull(adapter.random, token[:]); err != nil {
		zeroBytes(token[:])
		return nil, ErrSQLCipherWorkspace
	}
	defer zeroBytes(token[:])
	stageMarker := artifactMarker(adapter.key, restoreStageNameDomain, operationID, workspaceID,
		stageName, rollbackName, token[:])
	rollbackMarker := artifactMarker(adapter.key, restoreRollbackNameDomain, operationID, workspaceID,
		stageName, rollbackName, token[:])
	stageFile, err := adapter.root.OpenFile(stageName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		zeroBytes(stageMarker[:])
		zeroBytes(rollbackMarker[:])
		return nil, ErrSQLCipherWorkspace
	}
	reservation := &sqlcipherArtifactReservation{stageFile: stageFile, stageMarker: stageMarker,
		rollbackMarker: rollbackMarker}
	created := []string{stageName}
	cleanup := func() {
		_ = stageFile.Close()
		for index := len(created) - 1; index >= 0; index-- {
			_ = adapter.root.Remove(created[index])
		}
		_ = syncRestoreRoot(adapter.root)
		zeroBytes(reservation.stageMarker[:])
		zeroBytes(reservation.rollbackMarker[:])
	}
	if reservation.stageIdentity, err = stageFile.Stat(); err != nil || stageFile.Sync() != nil {
		cleanup()
		return nil, ErrSQLCipherWorkspace
	}
	if reservation.stageMarkerIdentity, err = writeOwnedArtifactMarker(adapter.root, stageName+".owner", stageMarker[:]); err != nil {
		cleanup()
		return nil, ErrSQLCipherWorkspace
	}
	created = append(created, stageName+".owner")
	if reservation.rollbackMarkerIdentity, err = writeOwnedArtifactMarker(adapter.root, rollbackName+".owner", rollbackMarker[:]); err != nil {
		cleanup()
		return nil, ErrSQLCipherWorkspace
	}
	created = append(created, rollbackName+".owner")
	if syncRestoreRoot(adapter.root) != nil || !sameRestoreDirectory(adapter.directory, adapter.baseInfo) {
		cleanup()
		return nil, ErrSQLCipherWorkspace
	}
	digest := artifactOwnershipDigest(stageMarker[:], rollbackMarker[:])
	stageMarkerHash := sha256.Sum256(stageMarker[:])
	rollbackMarkerHash := sha256.Sum256(rollbackMarker[:])
	return &RestoreArtifactReservation{operationID: operationID, workspaceID: workspaceID,
		stageBasename: stageName, rollbackBasename: rollbackName, ownershipDigest: digest,
		stageMarkerHash: stageMarkerHash, rollbackMarkerHash: rollbackMarkerHash,
		artifactAuthority: adapter, storageReservation: reservation}, nil
}

func (adapter *SQLCipherWorkspaceAdapter) ReleaseRestoreArtifacts(
	ctx context.Context,
	capability *RestoreArtifactReservation,
) error {
	if adapter == nil || ctx == nil || capability == nil {
		return ErrSQLCipherWorkspace
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.closed || ctx.Err() != nil || !adapter.validArtifactCapability(capability) {
		return errors.Join(ErrSQLCipherWorkspace, ctx.Err())
	}
	reservation := capability.storageReservation.(*sqlcipherArtifactReservation)
	if err := adapter.removeReservedArtifacts(capability, reservation, false); err != nil {
		return err
	}
	capability.artifactAuthority = nil
	capability.storageReservation = nil
	return nil
}

func (adapter *SQLCipherWorkspaceAdapter) validArtifactCapability(capability *RestoreArtifactReservation) bool {
	reservation, ok := capability.storageReservation.(*sqlcipherArtifactReservation)
	if !ok || reservation == nil || capability.artifactAuthority != adapter ||
		!validRestoreArtifactBasenames(capability.operationID, capability.workspaceID,
			capability.stageBasename, capability.rollbackBasename) {
		return false
	}
	wantStage := authenticatedRestoreArtifactName(restoreStagePrefix, restoreStageNameDomain,
		adapter.key, capability.operationID, capability.workspaceID)
	wantRollback := authenticatedRestoreArtifactName(restoreRollbackPrefix, restoreRollbackNameDomain,
		adapter.key, capability.operationID, capability.workspaceID)
	return capability.stageBasename == wantStage && capability.rollbackBasename == wantRollback &&
		capability.ownershipDigest == artifactOwnershipDigest(reservation.stageMarker[:], reservation.rollbackMarker[:]) &&
		capability.stageMarkerHash == sha256.Sum256(reservation.stageMarker[:]) &&
		capability.rollbackMarkerHash == sha256.Sum256(reservation.rollbackMarker[:])
}

func (adapter *SQLCipherWorkspaceAdapter) populateReservedStage(
	ctx context.Context,
	capability *RestoreArtifactReservation,
	databaseBytes []byte,
) error {
	if !adapter.validArtifactCapability(capability) || len(databaseBytes) == 0 || len(databaseBytes) > maximumStagedDatabaseBytes {
		return ErrSQLCipherWorkspace
	}
	reservation := capability.storageReservation.(*sqlcipherArtifactReservation)
	if reservation.populated || reservation.stageFile == nil ||
		!sameRootFile(adapter.root, capability.stageBasename, reservation.stageIdentity) ||
		!sameRootFile(adapter.root, capability.stageBasename+".owner", reservation.stageMarkerIdentity) ||
		!sameRootFile(adapter.root, capability.rollbackBasename+".owner", reservation.rollbackMarkerIdentity) ||
		!adapter.reservationMarkersMatch(capability, reservation) {
		return ErrSQLCipherWorkspace
	}
	if err := reservation.stageFile.Truncate(0); err != nil {
		return ErrSQLCipherWorkspace
	}
	if _, err := reservation.stageFile.Seek(0, io.SeekStart); err != nil {
		return ErrSQLCipherWorkspace
	}
	for offset := 0; offset < len(databaseBytes); {
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrSQLCipherWorkspace, err)
		}
		end := min(offset+32*1024, len(databaseBytes))
		written, writeErr := reservation.stageFile.Write(databaseBytes[offset:end])
		if written <= 0 || written > end-offset || writeErr != nil {
			return ErrSQLCipherWorkspace
		}
		offset += written
	}
	if reservation.stageFile.Sync() != nil || reservation.stageFile.Close() != nil || syncRestoreRoot(adapter.root) != nil {
		reservation.stageFile = nil
		return ErrSQLCipherWorkspace
	}
	reservation.stageFile = nil
	reservation.populated = true
	return nil
}

func (adapter *SQLCipherWorkspaceAdapter) reservationMarkersMatch(
	capability *RestoreArtifactReservation,
	reservation *sqlcipherArtifactReservation,
) bool {
	stageMarker, stageErr := readExactOwnedMarker(adapter.root, capability.stageBasename+".owner")
	rollbackMarker, rollbackErr := readExactOwnedMarker(adapter.root, capability.rollbackBasename+".owner")
	if stageErr != nil || rollbackErr != nil {
		zeroBytes(stageMarker)
		zeroBytes(rollbackMarker)
		return false
	}
	defer zeroBytes(stageMarker)
	defer zeroBytes(rollbackMarker)
	return subtle.ConstantTimeCompare(stageMarker, reservation.stageMarker[:]) == 1 &&
		subtle.ConstantTimeCompare(rollbackMarker, reservation.rollbackMarker[:]) == 1 &&
		artifactOwnershipDigest(stageMarker, rollbackMarker) == capability.ownershipDigest
}

func (adapter *SQLCipherWorkspaceAdapter) removeReservedArtifacts(
	capability *RestoreArtifactReservation,
	reservation *sqlcipherArtifactReservation,
	allowPopulated bool,
) error {
	if reservation == nil || reservation.populated && !allowPopulated ||
		!sameRootFile(adapter.root, capability.stageBasename, reservation.stageIdentity) ||
		!sameRootFile(adapter.root, capability.stageBasename+".owner", reservation.stageMarkerIdentity) ||
		!sameRootFile(adapter.root, capability.rollbackBasename+".owner", reservation.rollbackMarkerIdentity) ||
		!adapter.reservationMarkersMatch(capability, reservation) {
		return ErrSQLCipherWorkspace
	}
	if _, err := adapter.root.Lstat(capability.rollbackBasename); !errors.Is(err, os.ErrNotExist) {
		return ErrSQLCipherWorkspace
	}
	if reservation.stageFile != nil {
		if err := reservation.stageFile.Close(); err != nil {
			return ErrSQLCipherWorkspace
		}
		reservation.stageFile = nil
	}
	if err := adapter.removeStageFiles(capability.stageBasename); err != nil {
		return err
	}
	for _, name := range []string{capability.stageBasename + ".owner", capability.rollbackBasename + ".owner"} {
		if err := adapter.root.Remove(name); err != nil {
			return ErrSQLCipherWorkspace
		}
	}
	if syncRestoreRoot(adapter.root) != nil {
		return ErrSQLCipherWorkspace
	}
	zeroBytes(reservation.stageMarker[:])
	zeroBytes(reservation.rollbackMarker[:])
	return nil
}

func authenticatedRestoreArtifactName(prefix, domain string, key []byte, operationID, workspaceID string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte(operationID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(workspaceID))
	tag := mac.Sum(nil)
	name := prefix + operationID + "-" + workspaceID + "-" + hex.EncodeToString(tag) + restoreArtifactSuffix
	zeroBytes(tag)
	return name
}

func artifactMarker(key []byte, domain, operationID, workspaceID, stageName, rollbackName string, token []byte) [restoreMarkerBytes]byte {
	var marker [restoreMarkerBytes]byte
	copy(marker[:sha256.Size], token)
	mac := hmac.New(sha256.New, key)
	for _, field := range []string{restoreMarkerDomain, domain, operationID, workspaceID, stageName, rollbackName} {
		_, _ = mac.Write([]byte(field))
		_, _ = mac.Write([]byte{0})
	}
	_, _ = mac.Write(token)
	tag := mac.Sum(nil)
	copy(marker[sha256.Size:], tag)
	zeroBytes(tag)
	return marker
}

func artifactOwnershipDigest(stageMarker, rollbackMarker []byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write(stageMarker)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(rollbackMarker)
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func writeOwnedArtifactMarker(root *os.Root, name string, contents []byte) (os.FileInfo, error) {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	written, writeErr := file.Write(contents)
	syncErr := file.Sync()
	info, statErr := file.Stat()
	closeErr := file.Close()
	if written != len(contents) || writeErr != nil || syncErr != nil || statErr != nil || closeErr != nil ||
		!info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != int64(len(contents)) {
		_ = root.Remove(name)
		return nil, ErrSQLCipherWorkspace
	}
	return info, nil
}

func readExactOwnedMarker(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() != restoreMarkerBytes {
		return nil, ErrSQLCipherWorkspace
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrSQLCipherWorkspace
	}
	defer file.Close()
	contents := make([]byte, restoreMarkerBytes)
	if _, err := io.ReadFull(file, contents); err != nil {
		zeroBytes(contents)
		return nil, ErrSQLCipherWorkspace
	}
	var extra [1]byte
	if read, err := file.Read(extra[:]); read != 0 || err != io.EOF {
		zeroBytes(contents)
		return nil, ErrSQLCipherWorkspace
	}
	return contents, nil
}

func sameRootFile(root *os.Root, name string, expected os.FileInfo) bool {
	current, err := root.Lstat(name)
	return err == nil && expected != nil && current.Mode().IsRegular() && current.Mode().Perm() == 0o600 &&
		current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, expected)
}

// cleanupUnboundRestoreArtifacts removes only an empty, cryptographically
// authenticated reservation left after durable reservation but before journal
// binding. Unknown, partial, mutated, or populated artifacts remain untouched.
func cleanupUnboundRestoreArtifacts(ctx context.Context, directory, operationID string, key []byte) error {
	if ctx == nil || ctx.Err() != nil || !ids.IsCanonicalV7(operationID) || len(key) != sqlcipher.KeySize {
		return ErrSQLCipherWorkspace
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return ErrSQLCipherWorkspace
	}
	defer root.Close()
	directoryHandle, err := root.Open(".")
	if err != nil {
		return ErrSQLCipherWorkspace
	}
	defer directoryHandle.Close()
	var candidates []string
	inspected := 0
	for inspected < maximumArtifactScan {
		entries, readErr := directoryHandle.ReadDir(256)
		for _, entry := range entries {
			inspected++
			if inspected > maximumArtifactScan {
				return ErrSQLCipherWorkspace
			}
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), restoreStagePrefix+operationID+"-") &&
				strings.HasSuffix(entry.Name(), restoreArtifactSuffix) {
				candidates = append(candidates, entry.Name())
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ErrSQLCipherWorkspace
		}
	}
	sort.Strings(candidates)
	for _, stageName := range candidates {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(stageName, restoreStagePrefix), restoreArtifactSuffix), "-")
		if len(parts) != 11 {
			continue
		}
		workspaceID := strings.Join(parts[5:10], "-")
		if !ids.IsCanonicalV7(workspaceID) {
			continue
		}
		wantStage := authenticatedRestoreArtifactName(restoreStagePrefix, restoreStageNameDomain, key, operationID, workspaceID)
		rollbackName := authenticatedRestoreArtifactName(restoreRollbackPrefix, restoreRollbackNameDomain, key, operationID, workspaceID)
		if stageName != wantStage || !validUnboundMarkers(root, key, operationID, workspaceID, stageName, rollbackName) {
			continue
		}
		stageInfo, stageErr := root.Lstat(stageName)
		_, rollbackErr := root.Lstat(rollbackName)
		if stageErr != nil || !stageInfo.Mode().IsRegular() || stageInfo.Mode().Perm() != 0o600 || stageInfo.Size() != 0 ||
			!errors.Is(rollbackErr, os.ErrNotExist) {
			continue
		}
		for _, name := range []string{stageName, stageName + ".owner", rollbackName + ".owner"} {
			if err := root.Remove(name); err != nil {
				return ErrSQLCipherWorkspace
			}
		}
		return syncRestoreRoot(root)
	}
	return nil
}

func validUnboundMarkers(root *os.Root, key []byte, operationID, workspaceID, stageName, rollbackName string) bool {
	stageMarker, stageErr := readExactOwnedMarker(root, stageName+".owner")
	rollbackMarker, rollbackErr := readExactOwnedMarker(root, rollbackName+".owner")
	if stageErr != nil || rollbackErr != nil {
		zeroBytes(stageMarker)
		zeroBytes(rollbackMarker)
		return false
	}
	defer zeroBytes(stageMarker)
	defer zeroBytes(rollbackMarker)
	wantStage := artifactMarker(key, restoreStageNameDomain, operationID, workspaceID, stageName, rollbackName,
		stageMarker[:sha256.Size])
	wantRollback := artifactMarker(key, restoreRollbackNameDomain, operationID, workspaceID, stageName, rollbackName,
		stageMarker[:sha256.Size])
	valid := subtle.ConstantTimeCompare(stageMarker, wantStage[:]) == 1 &&
		subtle.ConstantTimeCompare(rollbackMarker, wantRollback[:]) == 1
	zeroBytes(wantStage[:])
	zeroBytes(wantRollback[:])
	return valid
}
