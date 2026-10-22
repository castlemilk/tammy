//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestSQLCipherStagedCaptureProcessDeathRecovery(t *testing.T) {
	if boundary := os.Getenv("TAMMY_SNAPSHOT_DEATH_BOUNDARY"); boundary != "" {
		runSQLCipherStagedCaptureDeathHelper(t, boundary)
		return
	}
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	livePath := filepath.Join(t.TempDir(), "live.db")
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), livePath, key, 3); err != nil {
		t.Fatal(err)
	}
	live, err := sqlcipher.Open(context.Background(), livePath, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.ExecContext(context.Background(),
		`CREATE TABLE capture_death_pages(id INTEGER PRIMARY KEY, value BLOB NOT NULL)`); err != nil {
		_ = live.Close()
		t.Fatal(err)
	}
	if _, err := live.ExecContext(context.Background(),
		`INSERT INTO capture_death_pages(value) VALUES(?)`, make([]byte, 2*1024*1024)); err != nil {
		_ = live.Close()
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{"create", "during_copy", "post_copy_pre_sanitize", "sanitize", "read"} {
		t.Run(boundary, func(t *testing.T) {
			staging := filepath.Join(t.TempDir(), "staging")
			if err := os.Mkdir(staging, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(staging, "foreign.bin"), []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestSQLCipherStagedCaptureProcessDeathRecovery$")
			command.Env = append(os.Environ(), "TAMMY_SNAPSHOT_DEATH_BOUNDARY="+boundary,
				"TAMMY_SNAPSHOT_LIVE_PATH="+livePath, "TAMMY_SNAPSHOT_STAGING_DIRECTORY="+staging)
			err := command.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
				t.Fatalf("death helper error=%v", err)
			}
			config := SQLCipherStagedCaptureConfig{Directory: staging,
				AuthenticationKey: testSnapshotStagingAuthenticationKey()}
			first, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
			if err != nil || first.Removed != 1 {
				t.Fatalf("first startup=%#v error=%v", first, err)
			}
			second, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
			if err != nil || second.Removed != 0 {
				t.Fatalf("second startup=%#v error=%v", second, err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil || len(entries) != 1 || entries[0].Name() != "foreign.bin" {
				t.Fatalf("post-recovery entries=%v error=%v", entries, err)
			}
			contents, err := os.ReadFile(filepath.Join(staging, "foreign.bin"))
			if err != nil || string(contents) != "foreign" {
				t.Fatalf("foreign contents=%q error=%v", contents, err)
			}
		})
	}
}

func TestSQLCipherStagedCaptureMarkerPublicationProcessDeathRecovery(t *testing.T) {
	if boundary := os.Getenv("TAMMY_SNAPSHOT_MARKER_DEATH_BOUNDARY"); boundary != "" {
		runSQLCipherMarkerPublicationDeathHelper(t, boundary)
		return
	}
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	livePath := filepath.Join(t.TempDir(), "live.db")
	if _, err := sqlcipher.MigrateWorkspace(context.Background(), livePath, key, 3); err != nil {
		t.Fatal(err)
	}
	const reference = "018f0000-0000-7000-8000-000000000095"
	for _, boundary := range []string{"temp_create", "temp_write", "temp_fsync", "publish", "directory_fsync"} {
		t.Run(boundary, func(t *testing.T) {
			staging := filepath.Join(t.TempDir(), "staging")
			if err := os.Mkdir(staging, 0o700); err != nil {
				t.Fatal(err)
			}
			foreignPath := filepath.Join(staging, "foreign.bin")
			if err := os.WriteFile(foreignPath, []byte("foreign"), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestSQLCipherStagedCaptureMarkerPublicationProcessDeathRecovery$")
			command.Env = append(os.Environ(), "TAMMY_SNAPSHOT_MARKER_DEATH_BOUNDARY="+boundary,
				"TAMMY_SNAPSHOT_LIVE_PATH="+livePath, "TAMMY_SNAPSHOT_STAGING_DIRECTORY="+staging)
			err := command.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
				t.Fatalf("death helper error=%v", err)
			}
			authenticationKey := testSnapshotStagingAuthenticationKey()
			name, marker := testAuthenticatedSnapshotCapture(reference, authenticationKey)
			mainInfo, err := os.Lstat(filepath.Join(staging, name))
			if err != nil || mainInfo.Size() != 0 || !mainInfo.Mode().IsRegular() || mainInfo.Mode().Perm() != 0o600 {
				t.Fatalf("pre-marker reservation info=%#v error=%v", mainInfo, err)
			}
			finalMarker, markerErr := os.ReadFile(filepath.Join(staging, name+".owner"))
			if markerErr == nil && !hmac.Equal(finalMarker, marker) {
				t.Fatalf("published marker was partial/tampered: %x", finalMarker)
			}
			if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
				t.Fatal(markerErr)
			}
			published := markerErr == nil
			config := SQLCipherStagedCaptureConfig{Directory: staging, AuthenticationKey: authenticationKey}
			first, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
			if err != nil || first.Removed == 0 {
				t.Fatalf("first recovery=%#v error=%v", first, err)
			}
			for restart := 2; restart <= 3; restart++ {
				report, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
				if err != nil || report.Removed != 0 {
					t.Fatalf("restart %d report=%#v error=%v", restart, report, err)
				}
			}
			_, mainErr := os.Lstat(filepath.Join(staging, name))
			if published && !errors.Is(mainErr, os.ErrNotExist) {
				t.Fatalf("published owned main survived recovery: %v", mainErr)
			}
			if !published {
				info, err := os.Lstat(filepath.Join(staging, name))
				if err != nil || info.Size() != 0 {
					t.Fatalf("unbound empty reservation was not preserved harmlessly: info=%#v error=%v", info, err)
				}
			}
			foreign, err := os.ReadFile(foreignPath)
			if err != nil || string(foreign) != "foreign" {
				t.Fatalf("foreign=%q error=%v", foreign, err)
			}
		})
	}
}

func runSQLCipherMarkerPublicationDeathHelper(t *testing.T, boundary string) {
	t.Helper()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	live, err := sqlcipher.Open(context.Background(), os.Getenv("TAMMY_SNAPSHOT_LIVE_PATH"), key)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	die := func() error {
		os.Exit(42)
		return nil
	}
	hooks := &captureHooks{}
	switch boundary {
	case "temp_create":
		hooks.afterMarkerTempCreate = die
	case "temp_write":
		hooks.afterMarkerTempWrite = die
	case "temp_fsync":
		hooks.afterMarkerTempSync = die
	case "publish":
		hooks.afterMarkerPublish = die
	case "directory_fsync":
		hooks.afterMarkerDirectorySync = die
	default:
		t.Fatalf("unknown marker boundary %q", boundary)
	}
	_, err = CaptureSanitizedSQLCipherSnapshot(context.Background(), live, key, SQLCipherStagedCaptureConfig{
		Directory: os.Getenv("TAMMY_SNAPSHOT_STAGING_DIRECTORY"), AuthenticationKey: testSnapshotStagingAuthenticationKey(),
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000095", nil }, hooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("capture returned without injected marker-publication death")
}

func runSQLCipherStagedCaptureDeathHelper(t *testing.T, boundary string) {
	t.Helper()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	live, err := sqlcipher.Open(context.Background(), os.Getenv("TAMMY_SNAPSHOT_LIVE_PATH"), key)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	die := func() error {
		os.Exit(42)
		return nil
	}
	dieDuringCopy := func(remaining, _ int) {
		if remaining > 0 {
			os.Exit(42)
		}
	}
	hooks := &captureHooks{}
	switch boundary {
	case "create":
		hooks.afterCreate = die
	case "during_copy":
		hooks.afterBackupStep = dieDuringCopy
	case "post_copy_pre_sanitize":
		hooks.afterBackup = die
	case "sanitize":
		hooks.afterSanitize = die
	case "read":
		hooks.afterRead = die
	default:
		t.Fatalf("unknown boundary %q", boundary)
	}
	_, err = CaptureSanitizedSQLCipherSnapshot(context.Background(), live, key, SQLCipherStagedCaptureConfig{
		Directory: os.Getenv("TAMMY_SNAPSHOT_STAGING_DIRECTORY"), AuthenticationKey: testSnapshotStagingAuthenticationKey(),
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000097", nil }, hooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("capture returned without injected death")
}

func TestSQLCipherStagedCaptureRecoveryIsAuthenticatedBoundedAndRepeatable(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "staging")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	authenticationKey := make([]byte, sha256.Size)
	for index := range authenticationKey {
		authenticationKey[index] = byte(index + 1)
	}
	defer zero(authenticationKey)
	owned := make([]string, 0, 257)
	for index := 0; index < 257; index++ {
		reference := fmt.Sprintf("018f0000-0000-7%03x-8000-%012x", index, index+1)
		name, marker := testAuthenticatedSnapshotCapture(reference, authenticationKey)
		owned = append(owned, name)
		if err := os.WriteFile(filepath.Join(directory, name), []byte("interrupted encrypted snapshot"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name+".owner"), marker, 0o600); err != nil {
			t.Fatal(err)
		}
		if index == 256 {
			for suffix, contents := range map[string][]byte{"-wal": []byte("wal"), "-shm": []byte("shm"), ".lock": nil} {
				if err := os.WriteFile(filepath.Join(directory, name+suffix), contents, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	plainForeign := ".tammy-snapshot-018f0000-0000-7000-8000-000000000999.db"
	mutatedForeign := owned[0][:len(owned[0])-4] + "0.db"
	for _, name := range []string{plainForeign, mutatedForeign, "foreign.bin"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := SQLCipherStagedCaptureConfig{Directory: directory, AuthenticationKey: authenticationKey,
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000998", nil }}
	first, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
	if err != nil || first.Removed != 256 || first.Inspected > maximumSnapshotCleanupInspect {
		t.Fatalf("first recovery=%#v error=%v", first, err)
	}
	second, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
	if err != nil || second.Removed != 1 || second.Inspected > maximumSnapshotCleanupInspect {
		t.Fatalf("second recovery=%#v error=%v", second, err)
	}
	third, err := RecoverSQLCipherStagedCaptures(context.Background(), config)
	if err != nil || third.Removed != 0 {
		t.Fatalf("third recovery=%#v error=%v", third, err)
	}
	for _, name := range []string{plainForeign, mutatedForeign, "foreign.bin"} {
		contents, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || string(contents) != "foreign" {
			t.Fatalf("foreign %q contents=%q error=%v", name, contents, err)
		}
	}
	for _, name := range owned {
		for _, artifact := range []string{name, name + ".owner", name + "-wal", name + "-shm", name + ".lock"} {
			if _, err := os.Lstat(filepath.Join(directory, artifact)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("owned artifact %q error=%v", artifact, err)
			}
		}
	}
}

func TestSQLCipherStagedCaptureRecoveryFailsClosedAtInspectCapAndOnMarkerTamper(t *testing.T) {
	t.Run("authenticated_name_without_owner_marker", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "staging")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		key := testSnapshotStagingAuthenticationKey()
		name, _ := testAuthenticatedSnapshotCapture("018f0000-0000-7000-8000-000000000098", key)
		before := map[string][]byte{
			name:           []byte("foreign exact-name main"),
			name + "-wal":  []byte("foreign exact-name wal"),
			name + ".lock": nil,
		}
		for artifact, contents := range before {
			if err := os.WriteFile(filepath.Join(directory, artifact), contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		report, err := RecoverSQLCipherStagedCaptures(context.Background(), SQLCipherStagedCaptureConfig{
			Directory: directory, AuthenticationKey: key})
		if err != nil || report.Removed != 0 {
			t.Fatalf("missing-marker report=%#v error=%v", report, err)
		}
		for artifact, want := range before {
			got, readErr := os.ReadFile(filepath.Join(directory, artifact))
			if readErr != nil || !hmac.Equal(got, want) {
				t.Fatalf("artifact %q got=%x want=%x error=%v", artifact, got, want, readErr)
			}
		}
	})

	t.Run("inspect_cap", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "staging")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		for index := 0; index < maximumSnapshotCleanupInspect; index++ {
			name := fmt.Sprintf("foreign-%04d.bin", index)
			if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		report, err := RecoverSQLCipherStagedCaptures(context.Background(), SQLCipherStagedCaptureConfig{
			Directory: directory, AuthenticationKey: testSnapshotStagingAuthenticationKey()})
		if !errors.Is(err, ErrSnapshotExclusion) || report.Inspected != maximumSnapshotCleanupInspect || report.Removed != 0 {
			t.Fatalf("over-cap report=%#v error=%v", report, err)
		}
		for _, name := range []string{"foreign-0000.bin", fmt.Sprintf("foreign-%04d.bin", maximumSnapshotCleanupInspect-1)} {
			if _, err := os.Lstat(filepath.Join(directory, name)); err != nil {
				t.Fatalf("foreign %q error=%v", name, err)
			}
		}
	})

	t.Run("tampered_marker", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "staging")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		key := testSnapshotStagingAuthenticationKey()
		name, marker := testAuthenticatedSnapshotCapture("018f0000-0000-7000-8000-000000000099", key)
		marker[0] ^= 0x01
		if err := os.WriteFile(filepath.Join(directory, name), []byte("owned-looking"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name+".owner"), marker, 0o600); err != nil {
			t.Fatal(err)
		}
		beforeMain, _ := os.ReadFile(filepath.Join(directory, name))
		beforeMarker, _ := os.ReadFile(filepath.Join(directory, name+".owner"))
		report, err := RecoverSQLCipherStagedCaptures(context.Background(), SQLCipherStagedCaptureConfig{
			Directory: directory, AuthenticationKey: key})
		if !errors.Is(err, ErrSnapshotExclusion) || report.Removed != 0 {
			t.Fatalf("tamper report=%#v error=%v", report, err)
		}
		afterMain, mainErr := os.ReadFile(filepath.Join(directory, name))
		afterMarker, markerErr := os.ReadFile(filepath.Join(directory, name+".owner"))
		if mainErr != nil || markerErr != nil || !hmac.Equal(afterMain, beforeMain) || !hmac.Equal(afterMarker, beforeMarker) {
			t.Fatalf("tamper cleanup mutated main=%q/%q marker=%x/%x errors=%v,%v",
				beforeMain, afterMain, beforeMarker, afterMarker, mainErr, markerErr)
		}
	})
}

func testAuthenticatedSnapshotCapture(reference string, authenticationKey []byte) (string, []byte) {
	nameMAC := hmac.New(sha256.New, authenticationKey)
	_, _ = nameMAC.Write([]byte("tammy.backup.snapshot-stage-name.v1\x00"))
	_, _ = nameMAC.Write([]byte(reference))
	nameTag := nameMAC.Sum(nil)
	name := ".tammy-snapshot-" + reference + "-" + hex.EncodeToString(nameTag) + ".db"
	zero(nameTag)
	markerMAC := hmac.New(sha256.New, authenticationKey)
	_, _ = markerMAC.Write([]byte("tammy.backup.snapshot-stage-marker.v1\x00"))
	_, _ = markerMAC.Write([]byte(name))
	return name, markerMAC.Sum(nil)
}

func testSnapshotStagingAuthenticationKey() []byte {
	key := make([]byte, sha256.Size)
	for index := range key {
		key[index] = byte(0xa0 + index)
	}
	return key
}
