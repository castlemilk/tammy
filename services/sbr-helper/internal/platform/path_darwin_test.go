//go:build darwin && arm64 && cgo

package platform

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPathGuardRejectsSymlinkInEveryPositionAndDegeneratePaths(t *testing.T) {
	root := secureTempDir(t)
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(realDirectory, "credential.p12")
	if err := os.WriteFile(realFile, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	linkedFile := filepath.Join(root, "linked-file.p12")
	if err := os.Symlink(realFile, linkedFile); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(linkedDirectory, "credential.p12"), linkedFile,
		root + "//real/credential.p12", filepath.Join(root, "real") + "/../real/credential.p12",
		realFile + "\nsecret", "relative.p12",
	} {
		guard, err := openRegularNoFollow(path)
		if guard != nil {
			_ = guard.Close()
		}
		if !errors.Is(err, ErrPathAuthorityInvalid) || err.Error() != "SBR_PATH_AUTHORITY_INVALID" {
			t.Fatalf("path %q error = %v", path, err)
		}
	}
}

func TestPathGuardRetainsOriginalLeafAndDetectsLeafSwap(t *testing.T) {
	root := secureTempDir(t)
	path := filepath.Join(root, "credential.p12")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := openRegularNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	retainedDescriptor := guard.components[len(guard.components)-1].descriptor
	var descriptorStat syscall.Stat_t
	if err := syscall.Fstat(retainedDescriptor, &descriptorStat); err != nil {
		t.Fatalf("retained descriptor not open: %v", err)
	}
	backup := filepath.Join(root, "credential-original.p12")
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Revalidate(); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("leaf swap revalidation error = %v", err)
	}
	if _, err := guard.ReadAll(64); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("leaf swap read was not rejected: %v", err)
	}
	backupInfo, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Fstat(retainedDescriptor, &descriptorStat); err != nil {
		t.Fatal(err)
	}
	backupStat := backupInfo.Sys().(*syscall.Stat_t)
	if descriptorStat.Ino != backupStat.Ino || descriptorStat.Dev != backupStat.Dev {
		t.Fatal("retained descriptor no longer identifies the original leaf")
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Fstat(retainedDescriptor, &descriptorStat); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("descriptor still open after Close: %v", err)
	}
	if _, err := guard.ReadAll(64); !errors.Is(err, ErrPathAuthorityClosed) {
		t.Fatalf("read after close = %v", err)
	}
	if err := guard.Revalidate(); !errors.Is(err, ErrPathAuthorityClosed) {
		t.Fatalf("validate after close = %v", err)
	}
}

func TestPathGuardRetainsOriginalLeafAndDetectsAncestorSwap(t *testing.T) {
	root := secureTempDir(t)
	ancestor := filepath.Join(root, "authority")
	if err := os.Mkdir(ancestor, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ancestor, "credential.p12")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := openRegularNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(ancestor, filepath.Join(root, "original-authority")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ancestor, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Revalidate(); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("ancestor swap revalidation error = %v", err)
	}
	got, err := guard.ReadAll(64)
	if err != nil || !bytes.Equal(got, []byte("inside")) {
		t.Fatalf("retained descriptor read = %q, %v", got, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPathGuardDetectsRetainedFileModeChange(t *testing.T) {
	path := filepath.Join(secureTempDir(t), "credential.p12")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := openRegularNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := guard.Revalidate(); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("mode change revalidation = %v", err)
	}
	if _, err := guard.ReadAll(64); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("mode change read = %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPathGuardDirectoryIdentityIgnoresUnrelatedSiblingChurn(t *testing.T) {
	root := secureTempDir(t)
	authority := filepath.Join(root, "authority")
	if err := os.Mkdir(authority, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(authority, "credential.p12")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := openRegularNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	sibling := filepath.Join(authority, "unrelated")
	if err := os.WriteFile(sibling, []byte("sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guard.Revalidate(); err != nil {
		t.Fatalf("sibling creation changed directory authority: %v", err)
	}
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	if err := guard.Revalidate(); err != nil {
		t.Fatalf("sibling removal changed directory authority: %v", err)
	}
}

func TestPathGuardRejectsAncestorModeChange(t *testing.T) {
	root := secureTempDir(t)
	authority := filepath.Join(root, "authority")
	if err := os.Mkdir(authority, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(authority, "credential.p12")
	if err := os.WriteFile(path, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, err := openRegularNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := os.Chmod(authority, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := guard.Revalidate(); !errors.Is(err, ErrPathAuthorityChanged) {
		t.Fatalf("ancestor mode change error=%v", err)
	}
}

func TestReadSecureRegularRejectsWritableCredentialAndBoundsRead(t *testing.T) {
	path := filepath.Join(secureTempDir(t), "credential.p12")
	if err := os.WriteFile(path, []byte("synthetic"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecureRegular(path, 64); !errors.Is(err, ErrPathAuthorityInvalid) {
		t.Fatalf("writable credential error = %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecureRegular(path, 4); !errors.Is(err, ErrPathAuthorityInvalid) {
		t.Fatalf("oversized credential error = %v", err)
	}
	got, err := ReadSecureRegular(path, 64)
	if err != nil || !bytes.Equal(got, []byte("synthetic")) {
		t.Fatalf("secure read = %q, %v", got, err)
	}
}

func TestReadSecureRegularRejectsAncestorSymlinkAtExactLiteralBoundary(t *testing.T) {
	root := secureTempDir(t)
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(realDirectory, "credential.p12")
	if err := os.WriteFile(credential, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecureRegular(filepath.Join(linkedDirectory, "credential.p12"), 64); !errors.Is(err, ErrPathAuthorityInvalid) {
		t.Fatalf("ancestor symlink error = %v", err)
	}
}

func secureTempDir(t *testing.T) string {
	t.Helper()
	path, err := os.MkdirTemp("/private/tmp", "tammy-sbr-helper-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	return path
}
