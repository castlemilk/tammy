//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package identity

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/faults"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type sessionReadBarrierRepository struct {
	Repository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (repository *sessionReadBarrierRepository) releaseRead() {
	repository.once.Do(func() { close(repository.release) })
}

func (repository *sessionReadBarrierRepository) pause() {
	close(repository.entered)
	<-repository.release
}

func (repository *sessionReadBarrierRepository) Load(ctx context.Context) (repositoryState, error) {
	state, err := repository.Repository.Load(ctx)
	if err == nil {
		repository.pause()
	}
	return state, err
}

func (repository *sessionReadBarrierRepository) MutateSerialized(ctx context.Context,
	work func(context.Context, workspace.MutationExecutor, *repositoryState) error) error {
	repository.pause()
	serialized, ok := repository.Repository.(interface {
		MutateSerialized(context.Context, func(context.Context, workspace.MutationExecutor, *repositoryState) error) error
	})
	if !ok {
		return ErrRepositoryIntegrity
	}
	return serialized.MutateSerialized(ctx, work)
}

func currentUserSQLCipherSession(t *testing.T) (identityHarness, workspace.StorageHandle, *tammyv1.AuthenticationContext) {
	t.Helper()
	harness, storage, _ := newSQLCipherIdentityHarness(t)
	t.Cleanup(func() { _ = storage.Close() })
	admin, err := harness.service.BootstrapAdministrator(context.Background(), "admin@example.test", "Admin",
		[]byte("admin-password-long-enough"))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := harness.service.SignIn(context.Background(), connect.NewRequest(&tammyv1.SignInRequest{
		Username: admin.Username, Password: secret("admin-password-long-enough"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	return harness, storage, &tammyv1.AuthenticationContext{ActorUserId: admin.Id, SessionId: signed.Msg.Session.Id}
}

func TestSQLCipherCurrentUserWaitsForWriterAndReadsCommittedIdentity(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx := context.Background()
	writer, err := storage.Database().BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(ctx, `UPDATE users SET display_name=?, repository_version=repository_version+1 WHERE id=?`,
		"Committed administrator", authentication.ActorUserId); err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(time.Minute)
	type result struct {
		response *connect.Response[tammyv1.GetCurrentUserResponse]
		err      error
	}
	started := make(chan struct{})
	completed := make(chan result, 1)
	go func() {
		close(started)
		response, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: authentication}))
		completed <- result{response, err}
	}()
	<-started
	var early *result
	select {
	case got := <-completed:
		early = &got
	case <-time.After(200 * time.Millisecond):
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	var got result
	if early != nil {
		got = *early
	} else {
		select {
		case got = <-completed:
		case <-time.After(5 * time.Second):
			t.Fatal("current-user read did not finish after the writer committed")
		}
	}
	if got.err != nil {
		t.Fatalf("current-user read failed during an ordinary overlapping writer: %v", got.err)
	}
	if early != nil {
		t.Fatal("current-user read completed before the pending identity update committed")
	}
	if got.response.Msg.User.DisplayName != "Committed administrator" {
		t.Fatalf("current-user read returned stale identity %q", got.response.Msg.User.DisplayName)
	}
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	session := state.Sessions[authentication.SessionId]
	if !session.LastActive.Equal(*harness.now) || !session.ExpiresAt.Equal(harness.now.Add(30*time.Minute)) {
		t.Fatalf("session was not touched atomically: %+v", session)
	}
	if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 1 {
		t.Fatalf("session touch audit rows = %d, want 1", got)
	}
}

func TestSQLCipherCurrentUserDoesNotBlockCallerOwnedAuthorization(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx := context.Background()
	barrier := &sessionReadBarrierRepository{Repository: harness.repository,
		entered: make(chan struct{}), release: make(chan struct{})}
	defer barrier.releaseRead()
	harness.service.repository = barrier
	read := make(chan error, 1)
	go func() {
		_, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: authentication}))
		read <- err
	}()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("current-user read did not reach the transaction boundary")
	}
	writer, err := storage.Database().BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		barrier.releaseRead()
		t.Fatal(err)
	}
	defer writer.Rollback()
	authorized := make(chan error, 1)
	go func() {
		authorized <- harness.service.AuthorizeWithin(ctx, writer, authentication, authorisation.ActionManageWorkspace)
	}()
	var authorizationErr error
	blocked := false
	select {
	case authorizationErr = <-authorized:
	case <-time.After(200 * time.Millisecond):
		blocked = true
	}
	barrier.releaseRead()
	if blocked {
		select {
		case authorizationErr = <-authorized:
		case <-time.After(5 * time.Second):
			t.Fatal("caller-owned authorization and current-user read deadlocked")
		}
	}
	if authorizationErr != nil {
		t.Fatalf("caller-owned authorization failed: %v", authorizationErr)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-read:
		if err != nil {
			t.Errorf("overlapping current-user read failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("current-user read did not complete")
	}
	if blocked {
		t.Error("current-user read held the identity mutex while awaiting the database writer")
	}
	if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 2 {
		t.Errorf("session touch audit rows = %d, want 2", got)
	}
}

func TestSQLCipherCurrentUserRejectsRevocationCommittedByPendingWriter(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx := context.Background()
	writer, err := storage.Database().BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(ctx, `UPDATE application_sessions SET state=?, ended_at=?,
		repository_version=repository_version+1 WHERE id=?`,
		int32(tammyv1.SessionState_SESSION_STATE_SIGNED_OUT), harness.now.Format(time.RFC3339Nano), authentication.SessionId); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() {
		response, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: authentication}))
		if response != nil {
			completed <- errors.New("revoked session returned a current user")
			return
		}
		completed <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-completed:
		if !errors.Is(err, faults.New(faults.CodeAuthenticationRequired, nil)) {
			t.Fatalf("revoked session returned %v, want authentication required", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("current-user read did not finish after revocation committed")
	}
	if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 0 {
		t.Fatalf("revoked session wrote %d touch audit rows", got)
	}
}

func TestSQLCipherCurrentUserAuditFailureRollsBackSessionTouch(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx := context.Background()
	before, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	*harness.now = harness.now.Add(time.Minute)
	auditFailure := errors.New("session audit unavailable")
	harness.config.Audit.(*sqlIdentityAudit).fail = auditFailure
	response, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: authentication}))
	if !errors.Is(err, auditFailure) || response != nil {
		t.Fatalf("current-user read returned response=%v error=%v, want audit failure", response, err)
	}
	after, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if beforeSession, afterSession := before.Sessions[authentication.SessionId], after.Sessions[authentication.SessionId]; !beforeSession.LastActive.Equal(afterSession.LastActive) || !beforeSession.ExpiresAt.Equal(afterSession.ExpiresAt) {
		t.Fatalf("failed audit changed the session: %+v -> %+v", beforeSession, afterSession)
	}
	if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 0 {
		t.Fatalf("failed audit committed %d session touch rows", got)
	}
}

type legacySessionSnapshotRepository struct {
	*DatabaseRepository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (repository *legacySessionSnapshotRepository) Load(ctx context.Context) (repositoryState, error) {
	state, err := repository.DatabaseRepository.Load(ctx)
	if err == nil {
		repository.once.Do(func() {
			close(repository.entered)
			<-repository.release
		})
	}
	return state, err
}

func TestSQLCipherCurrentUserPreservesConcurrentSessionRevocation(t *testing.T) {
	for _, command := range []string{"sign-out", "close"} {
		t.Run(command, func(t *testing.T) {
			harness, storage, authentication := currentUserSQLCipherSession(t)
			ctx := context.Background()
			barrier := &legacySessionSnapshotRepository{DatabaseRepository: harness.repository.(*DatabaseRepository),
				entered: make(chan struct{}), release: make(chan struct{})}
			harness.service.repository = barrier
			locked := make(chan struct{}, 1)
			harness.service.onWorkspaceLock = func() { locked <- struct{}{} }
			revoked := make(chan error, 1)
			go func() {
				if command == "close" {
					revoked <- harness.service.Close()
					return
				}
				_, err := harness.service.SignOut(ctx, connect.NewRequest(&tammyv1.SignOutRequest{Authentication: authentication}))
				revoked <- err
			}()
			select {
			case <-barrier.entered:
			case <-time.After(5 * time.Second):
				close(barrier.release)
				t.Fatal("revocation did not reach its loaded-snapshot boundary")
			}
			read := make(chan error, 1)
			go func() {
				_, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: authentication}))
				read <- err
			}()
			var earlyRead error
			readCompleted := false
			select {
			case earlyRead = <-read:
				readCompleted = true
			case <-time.After(200 * time.Millisecond):
			}
			close(barrier.release)
			select {
			case err := <-revoked:
				if err != nil {
					t.Errorf("overlapping current-user query defeated %s: %v", command, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("session revocation and current-user query deadlocked")
			}
			if !readCompleted {
				select {
				case earlyRead = <-read:
				case <-time.After(5 * time.Second):
					t.Fatal("current-user query did not complete after revocation")
				}
			}
			if !errors.Is(earlyRead, faults.New(faults.CodeAuthenticationRequired, nil)) {
				t.Errorf("query crossing %s returned %v, want authentication required", command, earlyRead)
			}
			state, err := harness.repository.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := tammyv1.SessionState_SESSION_STATE_SIGNED_OUT
			if command == "close" {
				want = tammyv1.SessionState_SESSION_STATE_INVALIDATED
			} else {
				select {
				case <-locked:
				default:
					t.Error("successful sign-out did not lock the workspace")
				}
			}
			if got := state.Sessions[authentication.SessionId].State; got != want {
				t.Errorf("session state = %v, want %v", got, want)
			}
			if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 0 {
				t.Errorf("query crossing revocation committed %d touch audits", got)
			}
		})
	}
}

type sessionReadCallbackBarrierRepository struct {
	*DatabaseRepository
	entered chan struct{}
	release chan struct{}
}

func (repository *sessionReadCallbackBarrierRepository) MutateSerialized(ctx context.Context,
	work func(context.Context, workspace.MutationExecutor, *repositoryState) error) error {
	return repository.DatabaseRepository.MutateSerialized(ctx, func(ctx context.Context, executor workspace.MutationExecutor, state *repositoryState) error {
		close(repository.entered)
		<-repository.release
		return work(ctx, executor, state)
	})
}

func TestSQLCipherCurrentUserWriterAdmissionCannotDefeatSignOut(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx := context.Background()
	barrier := &sessionReadCallbackBarrierRepository{DatabaseRepository: harness.repository.(*DatabaseRepository),
		entered: make(chan struct{}), release: make(chan struct{})}
	harness.service.repository = barrier
	read := make(chan error, 1)
	go func() {
		_, err := harness.service.GetCurrentUser(ctx, connect.NewRequest(&tammyv1.GetCurrentUserRequest{Authentication: authentication}))
		read <- err
	}()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		close(barrier.release)
		t.Fatal("query did not reserve its writer")
	}
	locked := make(chan struct{}, 1)
	harness.service.onWorkspaceLock = func() { locked <- struct{}{} }
	revoked := make(chan error, 1)
	go func() {
		_, err := harness.service.SignOut(ctx, connect.NewRequest(&tammyv1.SignOutRequest{Authentication: authentication}))
		revoked <- err
	}()
	var signOutErr error
	earlySignOut := false
	select {
	case signOutErr = <-revoked:
		earlySignOut = true
	case <-time.After(200 * time.Millisecond):
	}
	close(barrier.release)
	select {
	case err := <-read:
		if err != nil {
			t.Fatalf("current-user read failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("current-user read did not finish")
	}
	if !earlySignOut {
		select {
		case signOutErr = <-revoked:
		case <-time.After(5 * time.Second):
			t.Fatal("sign-out did not finish after query commit")
		}
	}
	if signOutErr != nil {
		t.Fatalf("query writer admission defeated sign-out: %v", signOutErr)
	}
	state, err := harness.repository.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Sessions[authentication.SessionId].State; got != tammyv1.SessionState_SESSION_STATE_SIGNED_OUT {
		t.Fatalf("session state = %v, want signed out", got)
	}
	select {
	case <-locked:
	default:
		t.Fatal("sign-out did not lock the workspace")
	}
	if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 1 {
		t.Fatalf("touch audit rows = %d, want 1", got)
	}
}
