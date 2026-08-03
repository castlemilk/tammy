//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOpenFailsClosedWithoutKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ledger.db")

	for _, key := range [][]byte{nil, {}, make([]byte, 31), make([]byte, 33)} {
		_, err := Open(context.Background(), path, key)
		if !errors.Is(err, ErrKeyRequired) {
			t.Fatalf("Open key length %d error = %v, want ErrKeyRequired", len(key), err)
		}
	}
}

func TestTypedBindingsPreparedStatementsAndTransactions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "types.db"), testKey(0x25))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.ExecContext(ctx, `CREATE TABLE typed(
		null_value,
		integer_value INTEGER,
		float_value REAL,
		bool_value INTEGER,
		text_value TEXT,
		blob_value BLOB,
		empty_blob BLOB,
		time_value TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	moment := time.Date(2026, time.August, 4, 3, 30, 0, 123, time.UTC)
	statement, err := database.PrepareContext(ctx, "INSERT INTO typed VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := statement.ExecContext(
		ctx,
		nil,
		int64(17),
		3.5,
		true,
		"text",
		[]byte{0, 1, 2},
		[]byte{},
		moment,
	); err != nil {
		t.Fatal(err)
	}
	if err := statement.Close(); err != nil {
		t.Fatal(err)
	}
	var (
		nullValue    any
		integerValue int64
		floatValue   float64
		boolValue    bool
		textValue    string
		blobValue    []byte
		emptyBlob    []byte
		timeValue    string
	)
	if err := database.QueryRowContext(ctx, "SELECT * FROM typed").Scan(
		&nullValue,
		&integerValue,
		&floatValue,
		&boolValue,
		&textValue,
		&blobValue,
		&emptyBlob,
		&timeValue,
	); err != nil {
		t.Fatal(err)
	}
	if nullValue != nil || integerValue != 17 || floatValue != 3.5 || !boolValue || textValue != "text" {
		t.Fatalf("unexpected scalar round trip: %#v %d %f %t %q", nullValue, integerValue, floatValue, boolValue, textValue)
	}
	if !bytes.Equal(blobValue, []byte{0, 1, 2}) || emptyBlob == nil || len(emptyBlob) != 0 {
		t.Fatalf("unexpected blob round trip: %v %#v", blobValue, emptyBlob)
	}
	if timeValue != moment.Format(time.RFC3339Nano) {
		t.Fatalf("time = %q", timeValue)
	}

	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO typed VALUES (NULL, 1, 1.0, 0, 'rollback', x'', x'', 'now')"); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}); err == nil {
		t.Fatal("read-only transaction was silently accepted")
	}
	if _, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadUncommitted}); err == nil {
		t.Fatal("unsupported isolation was silently accepted")
	}
	if _, err := database.ExecContext(ctx, "SELECT 1; SELECT 2"); err == nil {
		t.Fatal("multiple statements were silently accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := database.QueryRowContext(cancelled, "SELECT 1").Scan(new(int)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query error = %v", err)
	}
	interruptContext, interruptCancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer interruptCancel()
	var sum int64
	err = database.QueryRowContext(interruptContext, `
		WITH RECURSIVE counter(value) AS (
			VALUES(0)
			UNION ALL
			SELECT value + 1 FROM counter WHERE value < 100000000
		)
		SELECT sum(value) FROM counter
	`).Scan(&sum)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("mid-query cancellation error = %v", err)
	}
	var one int
	if err := database.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("interrupt leaked into next operation: value=%d error=%v", one, err)
	}
}

func TestOpenRejectsUnsafeOrUnavailablePaths(t *testing.T) {
	t.Parallel()
	key := make([]byte, KeySize)

	for _, path := range []string{"", "relative.db", ".", string([]byte{'/', 't', 'm', 'p', 0, 'x'})} {
		_, err := Open(context.Background(), path, key)
		if !errors.Is(err, ErrDatabasePath) {
			t.Fatalf("Open(%q) error = %v, want ErrDatabasePath", path, err)
		}
	}

	missingParent := filepath.Join(t.TempDir(), "missing", "ledger.db")
	if _, err := Open(context.Background(), missingParent, key); err == nil {
		t.Fatal("Open succeeded with a missing parent directory")
	}
}

func TestOpenRejectsInsecureExistingPermissionsWithoutMutatingThem(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("POSIX ownership and mode checks are meaningful on macOS")
	}
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "insecure.db")
	key := testKey(0x59)
	database, err := Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path, key)
	if err == nil {
		_ = reopened.Close()
		t.Fatal("existing database with group/world permissions was accepted")
	}
	stats, statErr := os.Lstat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := stats.Mode().Perm(); got != 0o644 {
		t.Fatalf("rejected database permissions were mutated to %04o", got)
	}
}

func TestOpenRejectsLeafReplacementAtFinalIdentityBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	probeTarget := filepath.Join(root, "symlink-probe-target")
	probeLink := filepath.Join(root, "symlink-probe-link")
	if err := os.WriteFile(probeTarget, []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(probeTarget, probeLink); err != nil {
		t.Skipf("file symlinks unavailable on this target: %v", err)
	}
	if err := os.Remove(probeLink); err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(root, "victim.db")
	displaced := filepath.Join(root, "displaced.db")
	attacker := filepath.Join(root, "attacker.txt")
	key := testKey(0x5a)
	database, err := Open(ctx, victim, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attacker, []byte("attacker-controlled"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hookErr error
	reopened, err := openDatabase(ctx, victim, key, openBoundaryHooks{
		beforeFinalIdentityCheck: func() {
			if hookErr = os.Rename(victim, displaced); hookErr != nil {
				return
			}
			hookErr = os.Symlink(attacker, victim)
		},
	})
	if reopened != nil {
		_ = reopened.Close()
	}
	if hookErr != nil {
		if runtime.GOOS == "windows" {
			contents, readErr := os.ReadFile(attacker)
			if readErr != nil || string(contents) != "attacker-controlled" {
				t.Fatalf("blocked replacement changed attacker target: content=%q error=%v", contents, readErr)
			}
			return
		}
		t.Fatalf("attack setup failed: %v", hookErr)
	}
	if err == nil {
		t.Fatal("database open accepted a leaf replaced by an attacker symlink")
	}
	contents, readErr := os.ReadFile(attacker)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(contents) != "attacker-controlled" {
		t.Fatalf("attacker target content changed to %q", contents)
	}
	if runtime.GOOS == "darwin" {
		stats, statErr := os.Lstat(attacker)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if got := stats.Mode().Perm(); got != 0o644 {
			t.Fatalf("attacker target permissions changed to %04o", got)
		}
	}
}

func TestConnectorRefusesDatabaseSymlinkAfterValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	target := filepath.Join(root, "target.db")
	key := testKey(0x57)
	database, err := Open(ctx, target, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "swapped.db")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("database symlinks unavailable on this target: %v", err)
	}
	connection, err := (&connector{key: append([]byte(nil), key...), path: link}).Connect(ctx)
	if err == nil {
		_ = connection.Close()
		t.Fatal("connector followed a database symlink")
	}
}

func TestOpenResolvesParentSymlinkBeforeCreatingDatabase(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Skipf("directory symlinks unavailable on this target: %v", err)
	}
	database, err := Open(context.Background(), filepath.Join(alias, "ledger.db"), testKey(0x58))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxIdleConns(0)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(context.Background(), "CREATE TABLE resolved(value INTEGER)"); err != nil {
		t.Fatalf("database retained unsafe dependence on parent symlink: %v", err)
	}
	if stats, err := os.Lstat(filepath.Join(realParent, "ledger.db")); err != nil || !stats.Mode().IsRegular() {
		t.Fatalf("resolved database missing: %v", err)
	}
}

func TestOpenOwnsKeyMaterial(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	key := testKey(0x31)
	database, err := Open(ctx, filepath.Join(t.TempDir(), "ledger.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	for index := range key {
		key[index] = 0
	}
	database.SetMaxIdleConns(0)
	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("connector could not key a replacement physical connection: %v", err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE key_copy_proof(value TEXT NOT NULL)"); err != nil {
		t.Fatalf("database stopped working after caller key was cleared: %v", err)
	}
}

func testKey(fill byte) []byte {
	key := make([]byte, KeySize)
	for index := range key {
		key[index] = fill
	}
	return key
}
