//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const sentinel = "TAMMY-PLAINTEXT-SENTINEL-4A5F39D8"

func TestEncryptedDatabaseBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")
	key := testKey(0x42)

	database, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	version, err := database.CipherVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version != PinnedVersion {
		t.Fatalf("cipher_version = %q, want %q", version, PinnedVersion)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE proof(value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO proof(value) VALUES (?)", sentinel); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(contents, []byte("SQLite format 3\x00")) {
		t.Fatal("encrypted database exposes the ordinary SQLite header")
	}
	if bytes.Contains(contents, []byte(sentinel)) {
		t.Fatal("encrypted database exposes plaintext row content")
	}
	assertOrdinarySQLiteRejects(t, path)

	if wrong, err := Open(ctx, path, testKey(0x43)); err == nil {
		_ = wrong.Close()
		t.Fatal("wrong key opened encrypted database")
	}

	reopened, err := Open(ctx, path, key)
	if err != nil {
		t.Fatalf("correct key failed to reopen database: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	var got string
	if err := reopened.QueryRowContext(ctx, "SELECT value FROM proof").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != sentinel {
		t.Fatalf("reopened value = %q", got)
	}
	if err := reopened.IntegrityCheck(ctx); err != nil {
		t.Fatalf("cipher_integrity_check failed: %v", err)
	}
}

func TestConnectionPolicyAndWALCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "policy.db")
	database, err := Open(ctx, path, testKey(0x67))
	if err != nil {
		t.Fatal(err)
	}

	for pragma, want := range map[string]int{
		"busy_timeout":  BusyTimeoutMilliseconds,
		"foreign_keys":  1,
		"secure_delete": 1,
	} {
		var got int
		if err := database.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if got != want {
			t.Fatalf("PRAGMA %s = %d, want %d", pragma, got, want)
		}
	}
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	if _, err := database.ExecContext(ctx, "CREATE TABLE parent(id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE child(parent_id INTEGER REFERENCES parent(id))"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO child(parent_id) VALUES (999)"); err == nil {
		t.Fatal("foreign key violation was accepted")
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE wal_proof(value INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT INTO wal_proof(value) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("WAL file was not created: %v", err)
	}
	if err := database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-journal", "-shm", "-wal"} {
		waitForAbsent(t, path+suffix)
	}
}

func TestPolicyAppliesToEveryPhysicalConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "connections.db"), testKey(0x74))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	first, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for index, connection := range []*sql.Conn{first, second} {
		for pragma, want := range map[string]int{
			"busy_timeout":  BusyTimeoutMilliseconds,
			"foreign_keys":  1,
			"secure_delete": 1,
		} {
			var got int
			if err := connection.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("connection %d PRAGMA %s: %v", index, pragma, err)
			}
			if got != want {
				t.Fatalf("connection %d PRAGMA %s = %d, want %d", index, pragma, got, want)
			}
		}
	}
}

func TestAbruptExitRecoveryAndCleanup(t *testing.T) {
	if os.Getenv("TAMMY_SQLCIPHER_CRASH_HELPER") == "1" {
		crashHelper()
		return
	}
	t.Parallel()
	path := filepath.Join(t.TempDir(), "crash.db")
	command := exec.Command(os.Args[0], "-test.run=^TestAbruptExitRecoveryAndCleanup$")
	command.Env = append(os.Environ(),
		"TAMMY_SQLCIPHER_CRASH_HELPER=1",
		"TAMMY_SQLCIPHER_CRASH_PATH="+path,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("crash helper failed: %v: %s", err, output)
	}
	ctx := context.Background()
	database, err := Open(ctx, path, testKey(0x7a))
	if err != nil {
		t.Fatalf("recovery open failed: %v", err)
	}
	var value string
	if err := database.QueryRowContext(ctx, "SELECT value FROM crash_proof").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "committed-before-crash" {
		t.Fatalf("recovered value = %q", value)
	}
	if err := database.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-journal", "-shm", "-wal"} {
		waitForAbsent(t, path+suffix)
	}
}

func crashHelper() {
	path := os.Getenv("TAMMY_SQLCIPHER_CRASH_PATH")
	database, err := Open(context.Background(), path, testKey(0x7a))
	if err != nil {
		os.Exit(91)
	}
	if _, err := database.ExecContext(context.Background(), "CREATE TABLE crash_proof(value TEXT NOT NULL)"); err != nil {
		os.Exit(92)
	}
	if _, err := database.ExecContext(context.Background(), "INSERT INTO crash_proof(value) VALUES ('committed-before-crash')"); err != nil {
		os.Exit(93)
	}
	os.Exit(0)
}

func assertOrdinarySQLiteRejects(t *testing.T, databasePath string) {
	t.Helper()
	reader := "/usr/bin/sqlite3"
	arguments := []string{databasePath, "PRAGMA schema_version;"}
	if runtime.GOOS == "windows" {
		reader = os.Getenv("TAMMY_ORDINARY_SQLITE3")
		if reader == "" || !filepath.IsAbs(reader) {
			t.Fatal("TAMMY_ORDINARY_SQLITE3 must name the absolute authenticated Windows probe")
		}
		stats, err := os.Lstat(reader)
		if err != nil || !stats.Mode().IsRegular() || stats.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("ordinary SQLite probe is unavailable or unsafe: %v", err)
		}
		arguments = []string{databasePath}
	}
	command := exec.Command(reader, arguments...)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("ordinary SQLite unexpectedly read encrypted file: %s", output)
	}
}

func waitForAbsent(t *testing.T, candidate string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("crash residue remains at %s: %v", candidate, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
