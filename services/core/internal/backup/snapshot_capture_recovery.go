//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

const (
	snapshotCaptureNameDomain       = "tammy.backup.snapshot-stage-name.v1\x00"
	snapshotCaptureMarkerDomain     = "tammy.backup.snapshot-stage-marker.v1\x00"
	snapshotCaptureMarkerTempDomain = "tammy.backup.snapshot-stage-marker-temp.v1\x00"
	snapshotCapturePrefix           = ".tammy-snapshot-"
	snapshotCaptureSuffix           = ".db"
	maximumSnapshotCleanupBatch     = 256
	maximumSnapshotCleanupInspect   = 4097
	maximumSnapshotCleanupEntries   = maximumSnapshotCleanupInspect - 1
)

type SQLCipherStagedCaptureRecoveryReport struct {
	Inspected int
	Removed   int
}

func RecoverSQLCipherStagedCaptures(
	ctx context.Context,
	config SQLCipherStagedCaptureConfig,
) (SQLCipherStagedCaptureRecoveryReport, error) {
	var report SQLCipherStagedCaptureRecoveryReport
	if ctx == nil || ctx.Err() != nil || config.Directory == "" || !filepath.IsAbs(config.Directory) ||
		filepath.Clean(config.Directory) != config.Directory || len(config.AuthenticationKey) != sha256.Size {
		return report, errors.Join(ErrSnapshotExclusion, contextError(ctx))
	}
	baseInfo, err := os.Lstat(config.Directory)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 {
		return report, ErrSnapshotExclusion
	}
	root, err := os.OpenRoot(config.Directory)
	if err != nil {
		return report, ErrSnapshotExclusion
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(baseInfo, rootInfo) {
		return report, ErrSnapshotExclusion
	}
	directory, err := root.Open(".")
	if err != nil {
		return report, ErrSnapshotExclusion
	}
	entries, readErr := directory.ReadDir(maximumSnapshotCleanupInspect)
	closeErr := directory.Close()
	report.Inspected = len(entries)
	if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil || len(entries) > maximumSnapshotCleanupEntries {
		return report, ErrSnapshotExclusion
	}
	candidates := make(map[string]struct{})
	markerTemps := make(map[string]struct{})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, errors.Join(ErrSnapshotExclusion, err)
		}
		if base, ok := authenticatedSnapshotCaptureBase(entry.Name(), config.AuthenticationKey); ok {
			candidates[base] = struct{}{}
		}
		if name, ok := authenticatedSnapshotCaptureMarkerTemp(entry.Name(), config.AuthenticationKey); ok {
			markerTemps[name] = struct{}{}
		}
	}
	type cleanupCandidate struct {
		name       string
		markerTemp bool
	}
	work := make([]cleanupCandidate, 0, len(candidates)+len(markerTemps))
	for name := range candidates {
		work = append(work, cleanupCandidate{name: name})
	}
	for name := range markerTemps {
		work = append(work, cleanupCandidate{name: name, markerTemp: true})
	}
	sort.Slice(work, func(left, right int) bool {
		if work[left].name == work[right].name {
			return !work[left].markerTemp && work[right].markerTemp
		}
		return work[left].name < work[right].name
	})
	if len(work) > maximumSnapshotCleanupBatch {
		work = work[:maximumSnapshotCleanupBatch]
	}
	owned := make([]string, 0, len(work))
	type ownedMarkerTemp struct {
		name     string
		identity os.FileInfo
	}
	ownedTemps := make([]ownedMarkerTemp, 0, len(work))
	for _, candidate := range work {
		if candidate.markerTemp {
			identity, err := validateInterruptedSnapshotMarkerTemp(root, candidate.name, config.AuthenticationKey)
			if err != nil {
				return report, err
			}
			ownedTemps = append(ownedTemps, ownedMarkerTemp{name: candidate.name, identity: identity})
			continue
		}
		isOwned, err := validateInterruptedSnapshotCapture(root, candidate.name, config.AuthenticationKey)
		if err != nil {
			return report, err
		}
		if isOwned {
			owned = append(owned, candidate.name)
		}
	}
	for _, name := range owned {
		if err := removeAuthenticatedSnapshotCapture(root, name); err != nil {
			return report, err
		}
		report.Removed++
	}
	for _, temp := range ownedTemps {
		if !sameSnapshotCaptureIdentity(root, temp.name, temp.identity) || root.Remove(temp.name) != nil {
			return report, ErrSnapshotExclusion
		}
		report.Removed++
	}
	if report.Removed > 0 && syncCaptureDirectory(root) != nil {
		return report, ErrSnapshotExclusion
	}
	if !sameSnapshotCaptureDirectory(config.Directory, baseInfo) {
		return report, ErrSnapshotExclusion
	}
	return report, nil
}

func authenticatedSnapshotCaptureName(reference string, authenticationKey []byte) string {
	mac := hmac.New(sha256.New, authenticationKey)
	_, _ = mac.Write([]byte(snapshotCaptureNameDomain))
	_, _ = mac.Write([]byte(reference))
	tag := mac.Sum(nil)
	name := snapshotCapturePrefix + reference + "-" + hex.EncodeToString(tag) + snapshotCaptureSuffix
	zero(tag)
	return name
}

func authenticatedSnapshotCaptureMarker(name string, authenticationKey []byte) []byte {
	mac := hmac.New(sha256.New, authenticationKey)
	_, _ = mac.Write([]byte(snapshotCaptureMarkerDomain))
	_, _ = mac.Write([]byte(name))
	return mac.Sum(nil)
}

func authenticatedSnapshotCaptureMarkerTempName(name string, authenticationKey []byte) string {
	mac := hmac.New(sha256.New, authenticationKey)
	_, _ = mac.Write([]byte(snapshotCaptureMarkerTempDomain))
	_, _ = mac.Write([]byte(name))
	tag := mac.Sum(nil)
	temp := name + ".owner." + hex.EncodeToString(tag) + ".tmp"
	zero(tag)
	return temp
}

func authenticatedSnapshotCaptureMarkerTemp(entryName string, authenticationKey []byte) (string, bool) {
	const tailLength = len(".owner.") + 2*sha256.Size + len(".tmp")
	if len(entryName) <= tailLength || !strings.HasSuffix(entryName, ".tmp") {
		return "", false
	}
	base := entryName[:len(entryName)-tailLength]
	if authenticated, valid := authenticatedSnapshotCaptureBase(base, authenticationKey); !valid || authenticated != base {
		return "", false
	}
	want := authenticatedSnapshotCaptureMarkerTempName(base, authenticationKey)
	return entryName, hmac.Equal([]byte(entryName), []byte(want))
}

func authenticatedSnapshotCaptureBase(entryName string, authenticationKey []byte) (string, bool) {
	base := entryName
	for _, suffix := range []string{".owner", ".lock", "-journal", "-wal", "-shm"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	if !strings.HasPrefix(base, snapshotCapturePrefix) || !strings.HasSuffix(base, snapshotCaptureSuffix) {
		return "", false
	}
	remainder := strings.TrimSuffix(strings.TrimPrefix(base, snapshotCapturePrefix), snapshotCaptureSuffix)
	if len(remainder) != 36+1+2*sha256.Size || remainder[36] != '-' {
		return "", false
	}
	reference := remainder[:36]
	tag := remainder[37:]
	decoded, err := hex.DecodeString(tag)
	if err != nil || len(decoded) != sha256.Size || tag != strings.ToLower(tag) || !ids.IsCanonicalV7(reference) {
		return "", false
	}
	want := authenticatedSnapshotCaptureName(reference, authenticationKey)
	return base, hmac.Equal([]byte(base), []byte(want))
}

func validateInterruptedSnapshotCapture(root *os.Root, name string, authenticationKey []byte) (bool, error) {
	if base, valid := authenticatedSnapshotCaptureBase(name, authenticationKey); !valid || base != name {
		return false, ErrSnapshotExclusion
	}
	for _, artifact := range []string{name, name + ".lock", name + "-journal", name + "-wal", name + "-shm"} {
		info, err := root.Lstat(artifact)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 ||
			(artifact == name+".lock" && info.Size() != 0) {
			return false, ErrSnapshotExclusion
		}
	}
	markerName := name + ".owner"
	if _, err := root.Lstat(markerName); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, ErrSnapshotExclusion
	}
	marker, err := readSnapshotCaptureMarker(root, markerName)
	if err != nil {
		return false, err
	}
	defer zero(marker)
	want := authenticatedSnapshotCaptureMarker(name, authenticationKey)
	defer zero(want)
	if !hmac.Equal(marker, want) {
		return false, ErrSnapshotExclusion
	}
	return true, nil
}

func validateInterruptedSnapshotMarkerTemp(root *os.Root, name string, authenticationKey []byte) (os.FileInfo, error) {
	if authenticated, valid := authenticatedSnapshotCaptureMarkerTemp(name, authenticationKey); !valid || authenticated != name {
		return nil, ErrSnapshotExclusion
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 0 || info.Size() > sha256.Size {
		return nil, ErrSnapshotExclusion
	}
	return info, nil
}

func readSnapshotCaptureMarker(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() != sha256.Size {
		return nil, ErrSnapshotExclusion
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, ErrSnapshotExclusion
	}
	contents := make([]byte, sha256.Size)
	_, readErr := io.ReadFull(file, contents)
	var extra [1]byte
	extraCount, extraErr := file.Read(extra[:])
	closeErr := file.Close()
	if readErr != nil || extraCount != 0 || extraErr != io.EOF || closeErr != nil {
		zero(contents)
		return nil, ErrSnapshotExclusion
	}
	return contents, nil
}

func removeAuthenticatedSnapshotCapture(root *os.Root, name string) error {
	for _, artifact := range []string{name, name + ".lock", name + "-journal", name + "-wal", name + "-shm", name + ".owner"} {
		if err := root.Remove(artifact); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrSnapshotExclusion
		}
	}
	return nil
}

func sameSnapshotCaptureDirectory(path string, expected os.FileInfo) bool {
	current, err := os.Lstat(path)
	return err == nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(current, expected)
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
