package sbrhelper

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/sbrprofile"
)

type launcherLocator struct{ resources sbrprofile.ResourceSet }

func (l launcherLocator) Locate(sbrprofile.Profile) (sbrprofile.ResourceSet, error) {
	return l.resources, nil
}

func writeLauncherProfile(t *testing.T, root string, helper []byte, now time.Time) string {
	t.Helper()
	sum := sha256.Sum256(helper)
	raw := []byte(`{"component_manifest_sha256":"NONE","endpoint_profile_sha256":"NONE","environment":"SIMULATOR","expires_at":"` + now.Add(time.Hour).Format("2006-01-02T15:04:05Z") + `","helper_sha256":"` + hex.EncodeToString(sum[:]) + `","issued_at":"` + now.Add(-time.Hour).Format("2006-01-02T15:04:05Z") + `","registration_manifest_sha256":"NONE","schema_version":1,"target":"darwin/arm64"}`)
	parsed, err := sbrprofile.ParseProfile(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	signature := append([]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), parsed.Canonical))), '\n')
	path := filepath.Join(root, "profile.json")
	writeLauncherFile(t, path, raw, 0o600)
	writeLauncherFile(t, strings.TrimSuffix(path, filepath.Ext(path))+".sig", signature, 0o600)
	return path
}

func writeLauncherFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func launcherRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
}

func helperModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../..", "sbr-helper"))
}

func sandboxApplyUnavailable(stderr []byte, runErr error) bool {
	if runErr == nil {
		return false
	}
	lower := bytes.ToLower(stderr)
	if bytes.Contains(lower, []byte("sandbox_apply")) &&
		(bytes.Contains(lower, []byte("operation not permitted")) || bytes.Contains(lower, []byte("not supported"))) {
		return true
	}
	exitError, ok := runErr.(*exec.ExitError)
	return ok && exitError.ExitCode() == 71 && bytes.Contains(lower, []byte("sandbox-exec"))
}
