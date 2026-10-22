//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

func TestSnapshotExclusionPolicySanitizesStagedCopyOnly(t *testing.T) {
	ctx := context.Background()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	openFixture := func(name string) *sqlcipher.Database {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
			t.Fatal(err)
		}
		database, err := sqlcipher.Open(ctx, path, key)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		seedSnapshotSessionRows(t, database)
		return database
	}
	live := openFixture("live.db")
	staged := openFixture("staged.db")

	tx, err := staged.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SanitizeSnapshot(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("SanitizeSnapshot() error = %v", err)
	}
	if err := VerifySnapshotExclusions(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("VerifySnapshotExclusions() before commit = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := VerifySnapshotExclusions(ctx, staged); err != nil {
		t.Fatalf("VerifySnapshotExclusions() after commit = %v", err)
	}
	assertSnapshotSessionState(t, live, 1, 1, 1, 1)
	assertSnapshotSessionState(t, staged, 0, 0, 0, 0)
}

func TestSQLCipherStagedCaptureCopiesBeforeSanitizingAndCleansUp(t *testing.T) {
	ctx := context.Background()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	livePath := filepath.Join(t.TempDir(), "live.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, livePath, key, 3); err != nil {
		t.Fatal(err)
	}
	live, err := sqlcipher.Open(ctx, livePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	seedSnapshotSessionRows(t, live)
	stagingDirectory := t.TempDir()
	bytes, err := CaptureSanitizedSQLCipherSnapshot(ctx, live, key, SQLCipherStagedCaptureConfig{
		Directory: stagingDirectory, AuthenticationKey: testSnapshotStagingAuthenticationKey(),
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000045", nil },
	})
	if err != nil {
		t.Fatalf("CaptureSanitizedSQLCipherSnapshot() error = %v", err)
	}
	if len(bytes) == 0 {
		t.Fatal("captured bytes are empty")
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restoredPath, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := sqlcipher.Open(ctx, restoredPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := VerifySnapshotExclusions(ctx, restored); err != nil {
		t.Fatal(err)
	}
	assertSnapshotSessionState(t, live, 1, 1, 1, 1)
	assertSnapshotSessionState(t, restored, 0, 0, 0, 0)
	assertMigrationCount(t, live, 3)
	assertMigrationCount(t, restored, 3)
	entries, err := os.ReadDir(stagingDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging residue = %#v, %v", entries, err)
	}
}

func TestSQLCipherStagedCaptureCannotWriteAfterBaseSwap(t *testing.T) {
	ctx := context.Background()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	livePath := filepath.Join(t.TempDir(), "live.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, livePath, key, 3); err != nil {
		t.Fatal(err)
	}
	live, err := sqlcipher.Open(ctx, livePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	stagingParent := t.TempDir()
	stagingDirectory := filepath.Join(stagingParent, "staging")
	if err := os.Mkdir(stagingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	movedDirectory := filepath.Join(stagingParent, "held-root")
	_, err = CaptureSanitizedSQLCipherSnapshot(ctx, live, key, SQLCipherStagedCaptureConfig{
		Directory: stagingDirectory, AuthenticationKey: testSnapshotStagingAuthenticationKey(),
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000046", nil },
		hooks: &captureHooks{beforeBackup: func() error {
			if err := os.Rename(stagingDirectory, movedDirectory); err != nil {
				return err
			}
			return os.Mkdir(stagingDirectory, 0o700)
		}},
	})
	if !errors.Is(err, ErrSnapshotExclusion) {
		t.Fatalf("base swap error = %v, want ErrSnapshotExclusion", err)
	}
	for _, directory := range []string{stagingDirectory, movedDirectory} {
		entries, readErr := os.ReadDir(directory)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("base swap residue in %s = %#v, %v", directory, entries, readErr)
		}
	}
}

func TestSQLCipherStagedCaptureReservationRacePreservesForeignExactName(t *testing.T) {
	ctx := context.Background()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	livePath := filepath.Join(t.TempDir(), "live.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, livePath, key, 3); err != nil {
		t.Fatal(err)
	}
	live, err := sqlcipher.Open(ctx, livePath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	stagingDirectory := t.TempDir()
	authenticationKey := testSnapshotStagingAuthenticationKey()
	const reference = "018f0000-0000-7000-8000-000000000096"
	name := authenticatedSnapshotCaptureName(reference, authenticationKey)
	foreign := []byte("foreign exact-name bytes created after preflight")
	_, err = CaptureSanitizedSQLCipherSnapshot(ctx, live, key, SQLCipherStagedCaptureConfig{
		Directory: stagingDirectory, AuthenticationKey: authenticationKey,
		NewID: func() (string, error) { return reference, nil },
		hooks: &captureHooks{afterPreflight: func() error {
			return os.WriteFile(filepath.Join(stagingDirectory, name), foreign, 0o600)
		}},
	})
	if !errors.Is(err, ErrSnapshotExclusion) {
		t.Fatalf("reservation race error=%v, want %v", err, ErrSnapshotExclusion)
	}
	contents, readErr := os.ReadFile(filepath.Join(stagingDirectory, name))
	if readErr != nil || string(contents) != string(foreign) {
		t.Fatalf("foreign contents=%q error=%v", contents, readErr)
	}
	if _, markerErr := os.Lstat(filepath.Join(stagingDirectory, name+".owner")); !errors.Is(markerErr, os.ErrNotExist) {
		t.Fatalf("foreign gained owner marker error=%v", markerErr)
	}
}

func TestSQLCipherStagedCaptureCancellationAfterPageStepCleansOwnedBytes(t *testing.T) {
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
	defer live.Close()
	if _, err := live.ExecContext(context.Background(),
		`CREATE TABLE backup_cancellation_pages(id INTEGER PRIMARY KEY, value BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := live.ExecContext(context.Background(),
		`INSERT INTO backup_cancellation_pages(value) VALUES(?)`, make([]byte, 2*1024*1024)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	steps := 0
	stagingDirectory := t.TempDir()
	contents, err := CaptureSanitizedSQLCipherSnapshot(ctx, live, key, SQLCipherStagedCaptureConfig{
		Directory: stagingDirectory, AuthenticationKey: testSnapshotStagingAuthenticationKey(),
		NewID: func() (string, error) { return "018f0000-0000-7000-8000-000000000047", nil },
		hooks: &captureHooks{afterBackupStep: func(_, _ int) {
			steps++
			cancel()
		}},
	})
	if !errors.Is(err, context.Canceled) || contents != nil || steps != 1 {
		t.Fatalf("cancelled capture contents=%d error=%v steps=%d", len(contents), err, steps)
	}
	entries, readErr := os.ReadDir(stagingDirectory)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("cancelled capture residue = %#v, %v", entries, readErr)
	}
}

func TestSnapshotExclusionPolicyRejectsForgedTriggerBeforeMutation(t *testing.T) {
	ctx := context.Background()
	key := make([]byte, sqlcipher.KeySize)
	for index := range key {
		key[index] = byte(index + 1)
	}
	defer zero(key)
	path := filepath.Join(t.TempDir(), "forged-trigger.db")
	if _, err := sqlcipher.MigrateWorkspace(ctx, path, key, 3); err != nil {
		t.Fatal(err)
	}
	database, err := sqlcipher.Open(ctx, path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	seedSnapshotSessionRows(t, database)
	if _, err := database.ExecContext(ctx, `DROP TRIGGER command_idempotency_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TRIGGER command_idempotency_no_update
		BEFORE UPDATE ON command_idempotency BEGIN SELECT RAISE(ABORT, 'forged trigger'); END`); err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := SanitizeSnapshot(ctx, tx); !errors.Is(err, ErrSnapshotExclusion) {
		_ = tx.Rollback()
		t.Fatalf("forged trigger SanitizeSnapshot() error = %v, want ErrSnapshotExclusion", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertSnapshotSessionState(t, database, 1, 1, 1, 1)
}

func seedSnapshotSessionRows(t *testing.T, executor SQLExecutor) {
	t.Helper()
	ctx := context.Background()
	const (
		userID    = "018f0000-0000-7000-8000-000000000041"
		sessionID = "018f0000-0000-7000-8000-000000000042"
	)
	if _, err := executor.ExecContext(ctx, `INSERT INTO users(
		id,email,display_name,status,activation_session_id,version,repository_version,created_at,updated_at
	) VALUES(?,?,?,?,?,1,1,?,?)`, userID, "admin@example.test", "Admin", "ACTIVE", sessionID,
		"2026-08-09T00:00:00Z", "2026-08-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO application_sessions(
		id,user_id,state,created_at,last_active_at,expires_at,repository_version
	) VALUES(?,?,1,?,?,?,1)`, sessionID, userID, "2026-08-09T00:00:00Z", "2026-08-09T00:00:00Z",
		"2026-08-09T01:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO factor_assertions(
		id,user_id,session_id,purpose,asserted_at,consumed,repository_version
	) VALUES(?,?,?,?,?,0,1)`, "018f0000-0000-7000-8000-000000000043", userID, sessionID,
		"backup", "2026-08-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO command_idempotency(
		operation_key,command_type,semantic_sha256,actor_user_id,user_id,session_id,repository_version,created_at
	) VALUES(?,?,?,?,?,?,1,?)`, "018f0000-0000-7000-8000-000000000044", "tammy.v1.WorkspaceService.BackupWorkspace",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", userID, userID, sessionID,
		"2026-08-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotSessionState(t *testing.T, executor SQLExecutor, sessions, factors, activations, commandSessions int) {
	t.Helper()
	queries := []string{
		`SELECT COUNT(*) FROM application_sessions`,
		`SELECT COUNT(*) FROM factor_assertions`,
		`SELECT COUNT(*) FROM users WHERE activation_session_id IS NOT NULL`,
		`SELECT COUNT(*) FROM command_idempotency WHERE session_id IS NOT NULL`,
	}
	want := []int{sessions, factors, activations, commandSessions}
	for index, query := range queries {
		rows, err := executor.QueryContext(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			_ = rows.Close()
			t.Fatal("count row missing")
		}
		var count int
		if err := rows.Scan(&count); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		if rows.Next() || rows.Err() != nil {
			_ = rows.Close()
			t.Fatal("unexpected count rows")
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if count != want[index] {
			t.Fatalf("query %q count = %d, want %d", query, count, want[index])
		}
	}
}

func assertMigrationCount(t *testing.T, executor SQLExecutor, want int) {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT COUNT(*) FROM schema_migrations`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("migration count missing")
	}
	var got int
	if err := rows.Scan(&got); err != nil || got != want || rows.Next() || rows.Err() != nil {
		t.Fatalf("migration count = %d, want %d, err=%v", got, want, err)
	}
}
