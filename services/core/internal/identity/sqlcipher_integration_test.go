//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
	"google.golang.org/protobuf/proto"
)

type sqlIdentityAudit struct {
	database workspace.MutationExecutor
	fail     error
}

type preflightBarrierRepository struct {
	Repository
	mu      sync.Mutex
	armed   bool
	arrived int
	release chan struct{}
}

func (repository *preflightBarrierRepository) Load(ctx context.Context) (repositoryState, error) {
	state, err := repository.Repository.Load(ctx)
	if err != nil {
		return repositoryState{}, err
	}
	repository.mu.Lock()
	if !repository.armed {
		repository.mu.Unlock()
		return state, nil
	}
	repository.arrived++
	if repository.arrived == 2 {
		close(repository.release)
	}
	release := repository.release
	repository.mu.Unlock()
	<-release
	return state, nil
}

func (repository *preflightBarrierRepository) arm() {
	repository.mu.Lock()
	repository.armed = true
	repository.mu.Unlock()
}

func (audit *sqlIdentityAudit) Record(ctx context.Context, executor workspace.MutationExecutor, mutation, subject string) error {
	if executor == nil {
		return ErrRepositoryIntegrity
	}
	if _, err := executor.ExecContext(ctx, `INSERT INTO identity_test_audit(mutation, subject) VALUES (?, ?)`, mutation, subject); err != nil {
		return err
	}
	return audit.fail
}

func newSQLCipherIdentityHarness(t *testing.T) (identityHarness, workspace.StorageHandle, *sqlIdentityAudit) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	source := clock.Func(func() time.Time { return now })
	storage, err := workspace.NewSQLCipherStorageFactory(2).Create(ctx, filepath.Join(t.TempDir(), "identity.db"), bytes.Repeat([]byte{0x44}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Database().ExecContext(ctx, `CREATE TABLE identity_test_audit(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		mutation TEXT NOT NULL,
		subject TEXT NOT NULL
	)`); err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	repository, err := NewDatabaseRepository(storage.Database())
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	policy, err := workspace.NewPasswordPolicy(nil, rand.Reader)
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	generator, err := ids.NewGenerator(source, rand.Reader)
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	anchors, err := workspace.NewSQLAnchorStore(storage.Database())
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	journal, err := workspace.NewAttemptJournal(filepath.Join(t.TempDir(), "attempts.journal"), bytes.Repeat([]byte{7}, 32), source,
		"identity/sqlcipher-test", anchors)
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	audit := &sqlIdentityAudit{database: storage.Database()}
	config := Config{Repository: repository, Passwords: policy, Clock: source, Random: rand.Reader, IDs: generator,
		Attempts: journal, FactorEncryptionKey: bytes.Repeat([]byte{9}, 32), Audit: audit,
		SessionLifecycle: &testWorkspaceSessionLifecycle{}}
	service, err := NewService(config)
	if err != nil {
		_ = storage.Close()
		t.Fatal(err)
	}
	return identityHarness{service: service, repository: repository, config: config, now: &now}, storage, audit
}

func identityAuditCount(t *testing.T, executor workspace.MutationExecutor, mutation string) int {
	t.Helper()
	rows, err := executor.QueryContext(context.Background(), `SELECT count(*) FROM identity_test_audit WHERE mutation = ?`, mutation)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("audit count row missing")
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

type normalizedIdentitySQLSnapshot struct {
	totalChanges int64
	tables       [7][3]uint64
}

func captureNormalizedIdentitySQL(t *testing.T, executor *sqlcipher.Database) normalizedIdentitySQLSnapshot {
	t.Helper()
	var snapshot normalizedIdentitySQLSnapshot
	if err := executor.QueryRowContext(context.Background(), `SELECT total_changes()`).Scan(&snapshot.totalChanges); err != nil {
		t.Fatal(err)
	}
	for index, table := range []string{"users", "user_roles", "user_password_history", "application_sessions",
		"totp_factors", "factor_assertions", "command_idempotency"} {
		if err := executor.QueryRowContext(context.Background(), `SELECT count(*), COALESCE(sum(repository_version), 0),
			COALESCE(max(repository_version), 0) FROM `+table).Scan(&snapshot.tables[index][0], &snapshot.tables[index][1],
			&snapshot.tables[index][2]); err != nil {
			t.Fatal(err)
		}
	}
	return snapshot
}

func identityRowCount(t *testing.T, executor workspace.MutationExecutor, table string) int {
	t.Helper()
	allowed := map[string]bool{
		"users": true, "user_roles": true, "user_password_history": true,
		"application_sessions": true, "totp_factors": true, "factor_assertions": true,
		"command_idempotency": true,
	}
	if !allowed[table] {
		t.Fatalf("identityRowCount called with unsupported table %q", table)
	}
	rows, err := executor.QueryContext(context.Background(), `SELECT count(*) FROM `+table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("row count missing")
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestSQLCipherRepositoryUsesNormalizedTables(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	if _, err := harness.service.BootstrapAdministratorWithin(ctx, storage.Database(),
		"01890f3c-7b2e-7cc4-98c4-dc0c0c073981", "admin@example.test", "Admin",
		[]byte("admin-password-long-enough")); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"users": 1, "user_roles": 1, "command_idempotency": 1} {
		if got := identityRowCount(t, storage.Database(), table); got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
	var metadataRows int
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT count(*) FROM workspace_metadata WHERE key = 'identity.repository.v1'`).Scan(&metadataRows); err != nil {
		t.Fatal(err)
	}
	if metadataRows != 0 {
		t.Fatalf("identity JSON metadata rows = %d, want 0", metadataRows)
	}
}

func TestSQLCipherRepositoryRejectsStaleStateAcrossInstances(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin",
		[]byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	first := harness.repository.(*DatabaseRepository)
	second, err := NewDatabaseRepository(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	firstState, err := first.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	staleState, err := second.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstState.Users[admin.Id].DisplayName = "First writer"
	staleState.Users[admin.Id].DisplayName = "Stale writer"
	if err := first.Save(ctx, firstState); err != nil {
		t.Fatal(err)
	}
	if err := second.Save(ctx, staleState); !errors.Is(err, ErrRepositoryConflict) {
		t.Fatalf("stale save returned %v, want repository conflict", err)
	}
	committed, err := first.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := committed.Users[admin.Id].DisplayName; got != "First writer" {
		t.Fatalf("committed display name = %q, want first writer", got)
	}
}

func TestSQLCipherRepositoryConcurrentMutationHasOneWinner(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin",
		[]byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	first := harness.repository.(*DatabaseRepository)
	second, err := NewDatabaseRepository(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	var ready sync.WaitGroup
	ready.Add(2)
	release := make(chan struct{})
	results := make(chan error, 2)
	mutate := func(repository *DatabaseRepository, displayName string) {
		results <- repository.Mutate(ctx, func(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
			state.Users[admin.Id].DisplayName = displayName
			ready.Done()
			<-release
			_, err := executor.ExecContext(ctx, `INSERT INTO identity_test_audit(mutation, subject) VALUES (?, ?)`,
				displayName, admin.Id)
			return err
		})
	}
	go mutate(first, "First concurrent writer")
	go mutate(second, "Second concurrent writer")
	ready.Wait()
	close(release)
	firstErr, secondErr := <-results, <-results
	winners, conflicts := 0, 0
	for _, mutationErr := range []error{firstErr, secondErr} {
		switch {
		case mutationErr == nil:
			winners++
		case errors.Is(mutationErr, ErrRepositoryConflict):
			conflicts++
		default:
			t.Fatalf("concurrent mutation returned %v", mutationErr)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes winners=%d conflicts=%d", winners, conflicts)
	}
	committed, err := first.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := committed.Users[admin.Id].DisplayName
	if got != "First concurrent writer" && got != "Second concurrent writer" {
		t.Fatalf("committed display name = %q", got)
	}
	auditRows := identityAuditCount(t, storage.Database(), "First concurrent writer") +
		identityAuditCount(t, storage.Database(), "Second concurrent writer")
	if auditRows != 1 {
		t.Fatalf("concurrent mutation committed %d audit rows, want 1", auditRows)
	}
}

func completeNormalizedRepositoryState(now time.Time) repositoryState {
	state := newRepositoryState()
	userID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073982"
	state.Users[userID] = &userRecord{
		ID: userID, Version: 1, Username: "admin@example.test", DisplayName: "Admin",
		State: tammyv1.UserState_USER_STATE_ACTIVE, Roles: []tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN},
		Password: workspace.PasswordVerifier{PolicyVersion: 1, MemoryKiB: 64 * 1024, Iterations: 3,
			Parallelism: 1, Salt: bytes.Repeat([]byte{0x12}, 16), Digest: bytes.Repeat([]byte{0x34}, 32)},
		PasswordHistory: []workspace.PasswordVerifier{{PolicyVersion: 1, MemoryKiB: 64 * 1024, Iterations: 3,
			Parallelism: 1, Salt: bytes.Repeat([]byte{0x56}, 16), Digest: bytes.Repeat([]byte{0x78}, 32)}},
	}
	state.Usernames[normalizedUsername("admin@example.test")] = userID
	sessionID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073983"
	state.Sessions[sessionID] = &sessionRecord{ID: sessionID, UserID: userID,
		State: tammyv1.SessionState_SESSION_STATE_ACTIVE, CreatedAt: now, LastActive: now, ExpiresAt: now.Add(30 * time.Minute)}
	factorID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073984"
	state.Factors[factorID] = &factorRecord{ID: factorID, UserID: userID, Version: 1,
		State: tammyv1.FactorState_FACTOR_STATE_ENABLED, CreatedAt: now,
		EncryptedSecret: []byte{0x01, 0x89, 0xf0, 0x3c, 0x7b, 0x2e}, LastCounter: 42}
	assertionID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073985"
	state.Assertions[assertionID] = &assertionRecord{ID: assertionID, UserID: userID, SessionID: sessionID,
		Purpose: "repository_test", Asserted: now, Consumed: true}
	state.Idempotency["01890f3c-7b2e-7cc4-98c4-dc0c0c073986"] = idempotencyRecord{
		Command: "repository_test", SemanticHash: strings.Repeat("a", 64), ActorUserID: userID,
		UserID: userID, FactorID: factorID, SessionID: sessionID,
		ResponseEncrypted: []byte{0xde, 0xad, 0xbe, 0xef},
	}
	return state
}

func TestSQLCipherRepositoryRoundTripsEveryNormalizedRecord(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	repository := harness.repository.(*DatabaseRepository)
	want := completeNormalizedRepositoryState(time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC))
	if err := repository.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "user_roles", "user_password_history", "application_sessions",
		"totp_factors", "factor_assertions", "command_idempotency"} {
		if got := identityRowCount(t, storage.Database(), table); got != 1 {
			t.Errorf("%s rows = %d, want 1", table, got)
		}
	}
	restarted, err := NewDatabaseRepository(storage.Database())
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for id, user := range want.Users {
		if got.Users[id] == nil || got.Users[id].DisplayName != user.DisplayName ||
			len(got.Users[id].PasswordHistory) != 1 || !bytes.Equal(got.Users[id].Password.Digest, user.Password.Digest) {
			t.Fatalf("restarted user mismatch: %#v", got.Users[id])
		}
	}
	for key, record := range want.Idempotency {
		if !bytes.Equal(got.Idempotency[key].ResponseEncrypted, record.ResponseEncrypted) {
			t.Fatalf("retained response changed across restart")
		}
		var stored []byte
		if err := storage.Database().QueryRowContext(ctx,
			`SELECT response_encrypted FROM command_idempotency WHERE operation_key = ?`, key).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stored, record.ResponseEncrypted) {
			t.Fatalf("stored retained response = %x, want %x", stored, record.ResponseEncrypted)
		}
	}
}

func TestSQLCipherRepositoryReplacesActiveSessionWithDescendingUUID(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	repository := harness.repository.(*DatabaseRepository)
	now := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)
	state := completeNormalizedRepositoryState(now)
	const (
		oldSessionID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073983"
		newSessionID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073900"
	)
	if err := repository.Save(ctx, state); err != nil {
		t.Fatal(err)
	}

	endedAt := now.Add(time.Minute)
	replace := func(state *repositoryState) {
		state.Sessions[oldSessionID].State = tammyv1.SessionState_SESSION_STATE_INVALIDATED
		state.Sessions[oldSessionID].EndedAt = endedAt
		state.Sessions[newSessionID] = &sessionRecord{
			ID: newSessionID, UserID: state.Sessions[oldSessionID].UserID,
			State: tammyv1.SessionState_SESSION_STATE_ACTIVE, CreatedAt: now,
			LastActive: now, ExpiresAt: now.Add(30 * time.Minute),
		}
	}
	transaction, err := storage.Database().BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := repository.LoadFrom(ctx, transaction)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	replace(&rolledBack)
	if err := repository.SaveTo(ctx, transaction, rolledBack); err != nil {
		_ = transaction.Rollback()
		t.Fatalf("stage descending session replacement: %v", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	rolledBack, err = repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Sessions[oldSessionID].State != tammyv1.SessionState_SESSION_STATE_ACTIVE || rolledBack.Sessions[newSessionID] != nil {
		t.Fatalf("session rollback old=%v new_present=%t", rolledBack.Sessions[oldSessionID].State, rolledBack.Sessions[newSessionID] != nil)
	}
	if err := repository.Mutate(ctx, func(_ context.Context, _ workspace.MutationExecutor, state *repositoryState) error {
		replace(state)
		return nil
	}); err != nil {
		t.Fatalf("replace active session: %v", err)
	}

	var activeSessions int
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT count(*) FROM application_sessions WHERE state = ?`,
		int32(tammyv1.SessionState_SESSION_STATE_ACTIVE)).Scan(&activeSessions); err != nil {
		t.Fatal(err)
	}
	if activeSessions != 1 {
		t.Fatalf("active sessions = %d, want 1", activeSessions)
	}
	var oldState int32
	var oldRepositoryVersion uint64
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT state, repository_version FROM application_sessions WHERE id = ?`, oldSessionID).
		Scan(&oldState, &oldRepositoryVersion); err != nil {
		t.Fatal(err)
	}
	if got, want := tammyv1.SessionState(oldState), tammyv1.SessionState_SESSION_STATE_INVALIDATED; got != want {
		t.Fatalf("old session state = %v, want %v", got, want)
	}
	if oldRepositoryVersion != 2 {
		t.Fatalf("old session repository version = %d, want 2", oldRepositoryVersion)
	}
}

func TestSQLCipherRepositorySupersedesFreshAssertionWithDescendingUUID(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	repository := harness.repository.(*DatabaseRepository)
	now := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC)
	state := completeNormalizedRepositoryState(now)
	const (
		oldAssertionID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073985"
		newAssertionID = "01890f3c-7b2e-7cc4-98c4-dc0c0c073900"
	)
	state.Assertions[oldAssertionID].Consumed = false
	if err := repository.Save(ctx, state); err != nil {
		t.Fatal(err)
	}

	supersede := func(state *repositoryState) {
		oldAssertion := state.Assertions[oldAssertionID]
		oldAssertion.Consumed = true
		state.Assertions[newAssertionID] = &assertionRecord{
			ID: newAssertionID, UserID: oldAssertion.UserID, SessionID: oldAssertion.SessionID,
			Purpose: oldAssertion.Purpose, Asserted: now.Add(time.Minute),
		}
	}
	transaction, err := storage.Database().BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := repository.LoadFrom(ctx, transaction)
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	supersede(&rolledBack)
	if err := repository.SaveTo(ctx, transaction, rolledBack); err != nil {
		_ = transaction.Rollback()
		t.Fatalf("stage descending assertion replacement: %v", err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	rolledBack, err = repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.Assertions[oldAssertionID].Consumed || rolledBack.Assertions[newAssertionID] != nil {
		t.Fatalf("assertion rollback old_consumed=%t new_present=%t", rolledBack.Assertions[oldAssertionID].Consumed,
			rolledBack.Assertions[newAssertionID] != nil)
	}
	if err := repository.Mutate(ctx, func(_ context.Context, _ workspace.MutationExecutor, state *repositoryState) error {
		supersede(state)
		return nil
	}); err != nil {
		t.Fatalf("supersede fresh assertion: %v", err)
	}

	var freshAssertions int
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT count(*) FROM factor_assertions WHERE user_id = ? AND session_id = ? AND purpose = ? AND consumed = 0`,
		state.Assertions[oldAssertionID].UserID, state.Assertions[oldAssertionID].SessionID,
		state.Assertions[oldAssertionID].Purpose).Scan(&freshAssertions); err != nil {
		t.Fatal(err)
	}
	if freshAssertions != 1 {
		t.Fatalf("fresh assertions = %d, want 1", freshAssertions)
	}
	var oldConsumed int
	var oldRepositoryVersion uint64
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT consumed, repository_version FROM factor_assertions WHERE id = ?`, oldAssertionID).
		Scan(&oldConsumed, &oldRepositoryVersion); err != nil {
		t.Fatal(err)
	}
	if oldConsumed != 1 {
		t.Fatalf("old assertion consumed = %d, want 1", oldConsumed)
	}
	if oldRepositoryVersion != 2 {
		t.Fatalf("old assertion repository version = %d, want 2", oldRepositoryVersion)
	}
}

func TestSQLCipherRepositorySaveRollsBackAllNormalizedRows(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	if _, err := storage.Database().ExecContext(ctx, `CREATE TRIGGER identity_test_fail_factor
		BEFORE INSERT ON totp_factors BEGIN SELECT RAISE(ABORT, 'injected factor failure'); END`); err != nil {
		t.Fatal(err)
	}
	state := completeNormalizedRepositoryState(time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC))
	if err := harness.repository.Save(ctx, state); !errors.Is(err, ErrRepositoryIntegrity) {
		t.Fatalf("injected save returned %v", err)
	}
	for _, table := range []string{"users", "user_roles", "user_password_history", "application_sessions",
		"totp_factors", "factor_assertions", "command_idempotency"} {
		if got := identityRowCount(t, storage.Database(), table); got != 0 {
			t.Errorf("rollback left %d rows in %s", got, table)
		}
	}
}

func TestSQLCipherRepositoryRejectsUnsupportedPasswordVerifier(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	state := completeNormalizedRepositoryState(time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC))
	if err := harness.repository.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Database().ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Database().ExecContext(ctx,
		`UPDATE users SET password_iterations = 4`); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Database().ExecContext(ctx, `PRAGMA ignore_check_constraints = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.repository.Load(ctx); !errors.Is(err, ErrRepositoryIntegrity) {
		t.Fatalf("unsupported verifier load returned %v, want repository integrity failure", err)
	}
}

func TestSQLCipherIdentityConstraintsRejectInvalidRelationshipsAndSecondActiveSession(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin",
		[]byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 2, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := storage.Database().ExecContext(ctx, `INSERT INTO application_sessions(
		id, user_id, state, created_at, last_active_at, expires_at) VALUES (?,?,?,?,?,?)`,
		"01890f3c-7b2e-7cc4-98c4-dc0c0c073987", admin.Id, int32(tammyv1.SessionState_SESSION_STATE_ACTIVE),
		now, now, now); err == nil {
		t.Fatal("second active session satisfied the unique index")
	}
	if _, err := storage.Database().ExecContext(ctx, `INSERT INTO factor_assertions(
		id, user_id, session_id, purpose, asserted_at, consumed) VALUES (?,?,?,?,?,?)`,
		"01890f3c-7b2e-7cc4-98c4-dc0c0c073988", "missing-user", signed.Msg.Session.Id,
		"constraint_test", now, 0); err == nil {
		t.Fatal("factor assertion with a missing user satisfied its foreign key")
	}
	if _, err := storage.Database().ExecContext(ctx, `INSERT INTO users(
		id, email, normalized_username, display_name, status, password_policy_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, "01890f3c-7b2e-7cc4-98c4-dc0c0c073989", "partial@example.test",
		"partial@example.test", "Partial", "ACTIVE", 1, now, now); err == nil {
		t.Fatal("partial password verifier satisfied the users check constraint")
	}
	if _, err := storage.Database().ExecContext(ctx, `INSERT INTO users(
		id, email, normalized_username, display_name, status, password_policy_version, password_memory_kib,
		password_iterations, password_parallelism, password_salt, password_digest, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, "01890f3c-7b2e-7cc4-98c4-dc0c0c073991", "unsupported@example.test",
		"unsupported@example.test", "Unsupported", "ACTIVE", 1, 64*1024, 4, 1,
		bytes.Repeat([]byte{0x12}, 16), bytes.Repeat([]byte{0x34}, 32), now, now); err == nil {
		t.Fatal("unsupported password verifier satisfied the users check constraint")
	}
	if _, err := storage.Database().ExecContext(ctx, `INSERT INTO totp_factors(
		id, user_id, version, state, created_at, encrypted_secret) VALUES (?,?,?,?,?,NULL)`,
		"01890f3c-7b2e-7cc4-98c4-dc0c0c073990", admin.Id, 1,
		int32(tammyv1.FactorState_FACTOR_STATE_ENABLED), now); err == nil {
		t.Fatal("enabled TOTP factor without ciphertext satisfied its check constraint")
	}
}

func TestSQLCipherBootstrapAdministratorRollsBackWithCreateAudit(t *testing.T) {
	harness, storage, audit := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	operationID := "01890f3c-7b2e-7cc4-98c4-dc0c0c073976"
	transaction, err := storage.Database().BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := harness.service.BootstrapAdministratorWithin(ctx, transaction, operationID,
		"admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(ctx, transaction, "workspace_created", rolledBack.Id); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Users) != 0 || len(state.Idempotency) != 0 || identityAuditCount(t, storage.Database(), "workspace_created") != 0 {
		t.Fatalf("rollback leaked users=%d idempotency=%d audits=%d", len(state.Users), len(state.Idempotency),
			identityAuditCount(t, storage.Database(), "workspace_created"))
	}
	transaction, err = storage.Database().BeginEncryptedTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := harness.service.BootstrapAdministratorWithin(ctx, transaction, operationID,
		"admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(ctx, transaction, "workspace_created", committed.Id); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	state, err = harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Users) != 1 || len(state.Idempotency) != 1 || identityAuditCount(t, storage.Database(), "workspace_created") != 1 {
		t.Fatalf("commit users=%d idempotency=%d audits=%d", len(state.Users), len(state.Idempotency),
			identityAuditCount(t, storage.Database(), "workspace_created"))
	}
}

func TestSQLCipherEnrolTOTPAuditRollbackRetryReplayAndRestart(t *testing.T) {
	harness, storage, audit := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := &tammyv1.EnrolTOTPRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073955",
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id},
	}, CurrentPassword: secret("admin-password-long-enough")}
	auditErr := errors.New("audit append failed")
	audit.fail = auditErr
	if _, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(request)); !errors.Is(err, auditErr) {
		t.Fatalf("audit failure returned %v", err)
	}
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Factors) != 0 || len(state.Idempotency) != 0 || identityAuditCount(t, storage.Database(), "totp_enrolled") != 0 {
		t.Fatalf("audit rollback leaked factors=%d idempotency=%d audits=%d", len(state.Factors), len(state.Idempotency), identityAuditCount(t, storage.Database(), "totp_enrolled"))
	}
	audit.fail = nil
	enrolled, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	var encryptedSecret, retainedResponse []byte
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT encrypted_secret FROM totp_factors WHERE user_id = ?`, admin.Id).Scan(&encryptedSecret); err != nil {
		t.Fatal(err)
	}
	if err := storage.Database().QueryRowContext(ctx,
		`SELECT response_encrypted FROM command_idempotency WHERE operation_key = ?`,
		request.CommandContext.IdempotencyKey).Scan(&retainedResponse); err != nil {
		t.Fatal(err)
	}
	for label, persisted := range map[string][]byte{"factor secret": encryptedSecret, "retained response": retainedResponse} {
		if len(persisted) == 0 || bytes.Equal(persisted, enrolled.Msg.ProvisioningSecret.Utf8) ||
			bytes.Contains(persisted, enrolled.Msg.ProvisioningSecret.Utf8) {
			t.Fatalf("%s leaked the provisioning secret", label)
		}
	}
	beforeReplaySQL := captureNormalizedIdentitySQL(t, storage.Database())
	beforeReplayState, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeReplaySession := *beforeReplayState.Sessions[request.CommandContext.Authentication.SessionId]
	beforeReplayAudit := identityAuditCount(t, storage.Database(), "totp_enrolled")
	*harness.now = harness.now.Add(time.Minute)
	replayed, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(request))
	if err != nil || replayed.Msg.Factor.Id != enrolled.Msg.Factor.Id || !bytes.Equal(replayed.Msg.ProvisioningSecret.Utf8, enrolled.Msg.ProvisioningSecret.Utf8) {
		t.Fatalf("exact replay after retry: %v", err)
	}
	conflict := protoCloneEnrol(request)
	conflict.CurrentPassword = secret("different-admin-password-long-enough")
	if _, err := harness.service.EnrolTOTP(ctx, connect.NewRequest(conflict)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("enrol conflict returned %v", err)
	}
	afterReplaySQL := captureNormalizedIdentitySQL(t, storage.Database())
	afterReplayState, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterReplaySession := *afterReplayState.Sessions[request.CommandContext.Authentication.SessionId]
	if beforeReplaySQL != afterReplaySQL || beforeReplaySession != afterReplaySession ||
		beforeReplayAudit != identityAuditCount(t, storage.Database(), "totp_enrolled") ||
		len(beforeReplayState.Factors) != len(afterReplayState.Factors) || len(beforeReplayState.Idempotency) != len(afterReplayState.Idempotency) {
		t.Fatalf("enrol replay/conflict wrote normalized SQL before=%+v after=%+v session=%+v→%+v audits=%d→%d factors=%d→%d idempotency=%d→%d",
			beforeReplaySQL, afterReplaySQL, beforeReplaySession, afterReplaySession, beforeReplayAudit,
			identityAuditCount(t, storage.Database(), "totp_enrolled"), len(beforeReplayState.Factors), len(afterReplayState.Factors),
			len(beforeReplayState.Idempotency), len(afterReplayState.Idempotency))
	}
	if got := identityAuditCount(t, storage.Database(), "totp_enrolled"); got != 1 {
		t.Fatalf("audit count after retry/replay = %d", got)
	}
	restarted, err := NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	resigned, err := restarted.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	restartRequest := protoCloneEnrol(request)
	restartRequest.CommandContext.Authentication.SessionId = resigned.Msg.Session.Id
	replayed, err = restarted.EnrolTOTP(ctx, connect.NewRequest(restartRequest))
	if err != nil || replayed.Msg.Factor.Id != enrolled.Msg.Factor.Id {
		t.Fatalf("restart replay: %v", err)
	}
	if got := identityAuditCount(t, storage.Database(), "totp_enrolled"); got != 1 {
		t.Fatalf("restart replay audit count = %d", got)
	}
}

func TestSQLCipherConcurrentEnrolTOTPCommitsOnePhysicalWinner(t *testing.T) {
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	barrier := &preflightBarrierRepository{Repository: harness.repository, release: make(chan struct{})}
	config := harness.config
	config.Repository = barrier
	first, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := first.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	barrier.arm()
	request := &tammyv1.EnrolTOTPRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073971",
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id},
	}, CurrentPassword: secret("admin-password-long-enough")}
	before := captureNormalizedIdentitySQL(t, storage.Database())
	beforeAudit := identityAuditCount(t, storage.Database(), "totp_enrolled")
	type result struct {
		response *connect.Response[tammyv1.EnrolTOTPResponse]
		err      error
	}
	results := make(chan result, 2)
	for _, service := range []*Service{first, second} {
		go func(service *Service) {
			response, err := service.EnrolTOTP(ctx, connect.NewRequest(protoCloneEnrol(request)))
			results <- result{response: response, err: err}
		}(service)
	}
	left, right := <-results, <-results
	if left.err != nil || right.err != nil || left.response.Msg.Factor.Id != right.response.Msg.Factor.Id ||
		!bytes.Equal(left.response.Msg.ProvisioningSecret.Utf8, right.response.Msg.ProvisioningSecret.Utf8) {
		t.Fatalf("concurrent enrol results left=%v/%v right=%v/%v", left.response, left.err, right.response, right.err)
	}
	after := captureNormalizedIdentitySQL(t, storage.Database())
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Factors) != 1 || len(state.Idempotency) != 1 ||
		identityAuditCount(t, storage.Database(), "totp_enrolled") != beforeAudit+1 {
		t.Fatalf("concurrent winner factors=%d idempotency=%d audits=%d", len(state.Factors), len(state.Idempotency),
			identityAuditCount(t, storage.Database(), "totp_enrolled")-beforeAudit)
	}
	for index := range after.tables {
		inserted := after.tables[index][0] - before.tables[index][0]
		updated := before.tables[index][0]
		if index == 1 || index == 6 { // retained roles and idempotency rows are immutable
			updated = 0
		}
		wantSum := before.tables[index][1] + updated + inserted
		if after.tables[index][1] != wantSum {
			t.Fatalf("table %d repository_version sum=%d want=%d", index, after.tables[index][1], wantSum)
		}
	}
}

func TestSQLCipherDisableTOTPAuditRollbackRetryReplayAndRestart(t *testing.T) {
	harness, storage, audit := newSQLCipherIdentityHarness(t)
	defer storage.Close()
	ctx := context.Background()
	admin, err := harness.service.BootstrapAdministrator(ctx, "admin@example.test", "Admin", []byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	authentication := &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
	secretBytes := enrolConfirmedFactor(t, harness, authentication, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c073956")
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var factor *factorRecord
	for _, candidate := range state.Factors {
		factor = candidate
	}
	if factor == nil {
		t.Fatal("enabled factor missing")
	}
	*harness.now = harness.now.Add(30 * time.Second)
	asserted, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
		Authentication: authentication,
		Code:           &tammyv1.TotpCodeInput{Value: TOTPCode(secretBytes, *harness.now)},
		Purpose:        "disable_totp",
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := &tammyv1.DisableTOTPRequest{CommandContext: &tammyv1.CommandContext{
		IdempotencyKey: "01890f3c-7b2e-7cc4-98c4-dc0c0c073957", Authentication: authentication, FreshFactor: asserted.Msg.FreshFactor,
	}, FactorId: factor.ID, ExpectedVersion: factor.Version, CurrentPassword: secret("admin-password-long-enough")}
	auditErr := errors.New("audit append failed")
	audit.fail = auditErr
	if _, err := harness.service.DisableTOTP(ctx, connect.NewRequest(request)); !errors.Is(err, auditErr) {
		t.Fatalf("audit failure returned %v", err)
	}
	state, err = harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Factors[factor.ID].State != tammyv1.FactorState_FACTOR_STATE_ENABLED ||
		state.Assertions[asserted.Msg.FreshFactor.AssertionId].Consumed ||
		state.Idempotency[request.CommandContext.IdempotencyKey].Command != "" ||
		identityAuditCount(t, storage.Database(), "totp_disabled") != 0 {
		t.Fatalf("disable rollback leaked factor=%v consumed=%t idempotency=%q audits=%d",
			state.Factors[factor.ID].State, state.Assertions[asserted.Msg.FreshFactor.AssertionId].Consumed,
			state.Idempotency[request.CommandContext.IdempotencyKey].Command, identityAuditCount(t, storage.Database(), "totp_disabled"))
	}
	audit.fail = nil
	disabled, err := harness.service.DisableTOTP(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	beforeReplaySQL := captureNormalizedIdentitySQL(t, storage.Database())
	beforeReplayState, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeReplaySession := *beforeReplayState.Sessions[authentication.SessionId]
	beforeReplayAudit := identityAuditCount(t, storage.Database(), "totp_disabled")
	*harness.now = harness.now.Add(time.Minute)
	replayed, err := harness.service.DisableTOTP(ctx, connect.NewRequest(request))
	if err != nil || replayed.Msg.Factor.Id != disabled.Msg.Factor.Id || replayed.Msg.Factor.Version != disabled.Msg.Factor.Version {
		t.Fatalf("disable replay after retry: %v", err)
	}
	conflict := proto.Clone(request).(*tammyv1.DisableTOTPRequest)
	conflict.ExpectedVersion++
	if _, err := harness.service.DisableTOTP(ctx, connect.NewRequest(conflict)); !errors.Is(err, faults.New(faults.CodeIdempotencyConflict, nil)) {
		t.Fatalf("disable conflict returned %v", err)
	}
	afterReplaySQL := captureNormalizedIdentitySQL(t, storage.Database())
	afterReplayState, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	afterReplaySession := *afterReplayState.Sessions[authentication.SessionId]
	if beforeReplaySQL != afterReplaySQL || beforeReplaySession != afterReplaySession ||
		beforeReplayAudit != identityAuditCount(t, storage.Database(), "totp_disabled") ||
		len(beforeReplayState.Factors) != len(afterReplayState.Factors) || len(beforeReplayState.Assertions) != len(afterReplayState.Assertions) ||
		len(beforeReplayState.Idempotency) != len(afterReplayState.Idempotency) {
		t.Fatalf("disable replay/conflict wrote normalized SQL before=%+v after=%+v session=%+v→%+v audits=%d→%d factors=%d→%d assertions=%d→%d idempotency=%d→%d",
			beforeReplaySQL, afterReplaySQL, beforeReplaySession, afterReplaySession, beforeReplayAudit,
			identityAuditCount(t, storage.Database(), "totp_disabled"), len(beforeReplayState.Factors), len(afterReplayState.Factors),
			len(beforeReplayState.Assertions), len(afterReplayState.Assertions), len(beforeReplayState.Idempotency), len(afterReplayState.Idempotency))
	}
	if got := identityAuditCount(t, storage.Database(), "totp_disabled"); got != 1 {
		t.Fatalf("disable audit count after retry/replay = %d", got)
	}
	restarted, err := NewService(harness.config)
	if err != nil {
		t.Fatal(err)
	}
	resigned, err := restarted.SignIn(ctx, connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	restartRequest := proto.Clone(request).(*tammyv1.DisableTOTPRequest)
	restartRequest.CommandContext.Authentication.SessionId = resigned.Msg.Session.Id
	replayed, err = restarted.DisableTOTP(ctx, connect.NewRequest(restartRequest))
	if err != nil || replayed.Msg.Factor.Id != disabled.Msg.Factor.Id {
		t.Fatalf("disable restart replay: %v", err)
	}
	if got := identityAuditCount(t, storage.Database(), "totp_disabled"); got != 1 {
		t.Fatalf("disable restart replay audit count = %d", got)
	}
}

func protoCloneEnrol(request *tammyv1.EnrolTOTPRequest) *tammyv1.EnrolTOTPRequest {
	return proto.Clone(request).(*tammyv1.EnrolTOTPRequest)
}
