//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package identity

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

type enrolMutationBarrier struct {
	Repository
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newEnrolMutationBarrier(repository Repository) *enrolMutationBarrier {
	return &enrolMutationBarrier{Repository: repository, entered: make(chan struct{}), release: make(chan struct{})}
}

func (repository *enrolMutationBarrier) Mutate(ctx context.Context,
	work func(context.Context, workspace.MutationExecutor, *repositoryState) error,
) error {
	repository.enterOnce.Do(func() { close(repository.entered) })
	select {
	case <-repository.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return repository.Repository.Mutate(ctx, work)
}

func (repository *enrolMutationBarrier) releaseMutation() {
	repository.releaseOnce.Do(func() { close(repository.release) })
}

type observedAuthorizeExecutor struct {
	workspace.MutationExecutor
	queryStarted chan struct{}
	queryOnce    sync.Once
}

func (executor *observedAuthorizeExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	executor.queryOnce.Do(func() { close(executor.queryStarted) })
	return executor.MutationExecutor.QueryContext(ctx, query, args...)
}

type enrolOutcome struct {
	response *connect.Response[tammyv1.EnrolTOTPResponse]
	err      error
}

// SBR's sqlUnitOfWork opens a serializable transaction before calling
// identity.AuthorizeWithin. Keep that writer open, then pause the real
// EnrolTOTP after it owns Service.mu and before its repository write. The
// caller-owned authorization must still read, authorize, touch the session,
// and commit; only then may TOTP enrollment proceed.
func TestSQLCipherAuthorizeWithinProgressesWhileEnrolTOTPOwnsIdentityMutex(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx := context.Background()
	writer, err := storage.Database().BeginEncryptedTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	writerOpen := true

	originalRepository := harness.service.repository
	barrier := newEnrolMutationBarrier(originalRepository)
	harness.service.repository = barrier
	enrolCtx, cancelEnrol := context.WithTimeout(ctx, 15*time.Second)
	cancelAuthorize := func() {}
	var enrolStarted, authorizeStarted, enrolReceived, authorizeReceived bool
	enrolResult := make(chan enrolOutcome, 1)
	authorizeResult := make(chan error, 1)
	cleanup := func() {
		if writerOpen {
			_ = writer.Rollback()
			writerOpen = false
		}
		barrier.releaseMutation()
		cancelEnrol()
		cancelAuthorize()
		if enrolStarted && !enrolReceived {
			select {
			case <-enrolResult:
			case <-time.After(8 * time.Second):
			}
		}
		if authorizeStarted && !authorizeReceived {
			select {
			case <-authorizeResult:
			case <-time.After(5 * time.Second):
			}
		}
		harness.service.repository = originalRepository
	}
	defer cleanup()

	enrolStarted = true
	go func() {
		response, enrolErr := harness.service.EnrolTOTP(enrolCtx, connect.NewRequest(&tammyv1.EnrolTOTPRequest{
			CommandContext: &tammyv1.CommandContext{
				IdempotencyKey: "018f5f48-7f01-7b6e-86df-8b89f45e1a01",
				Authentication: authentication,
			},
			CurrentPassword: secret("admin-password-long-enough"),
		}))
		enrolResult <- enrolOutcome{response: response, err: enrolErr}
	}()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("EnrolTOTP did not reach its pre-persistence barrier")
	}
	if harness.service.mu.TryLock() {
		harness.service.mu.Unlock()
		t.Fatal("EnrolTOTP reached its repository barrier without holding identity mutex")
	}

	authorizeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	cancelAuthorize = cancel
	executor := &observedAuthorizeExecutor{MutationExecutor: writer, queryStarted: make(chan struct{})}
	authorizeStarted = true
	go func() {
		authorizeResult <- harness.service.AuthorizeWithin(authorizeCtx, executor, authentication, authorisation.ActionInspectSBR)
	}()
	select {
	case <-executor.queryStarted:
	case <-authorizeCtx.Done():
		t.Fatalf("caller-owned SBR authorization did not reach its first SQL query before its context deadline (deadline=%t)",
			authorizeCtx.Err() == context.DeadlineExceeded)
	case <-time.After(2 * time.Second):
		t.Fatal("caller-owned authorization neither progressed nor reached its bounded deadline")
	}

	select {
	case authorizeErr := <-authorizeResult:
		authorizeReceived = true
		if authorizeErr != nil {
			t.Fatalf("caller-owned SBR authorization failed: %v", authorizeErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller-owned authorization did not finish after its SQL query began")
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("commit SBR authorization transaction: %v", err)
	}
	writerOpen = false
	barrier.releaseMutation()
	select {
	case enrol := <-enrolResult:
		enrolReceived = true
		if enrol.err != nil || enrol.response == nil || enrol.response.Msg.GetFactor().GetState() != tammyv1.FactorState_FACTOR_STATE_PENDING_CONFIRMATION {
			t.Fatalf("EnrolTOTP did not persist a pending factor after authorization committed: error=%v", enrol.err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("EnrolTOTP did not finish after the caller-owned authorization committed")
	}

	if got := identityAuditCount(t, storage.Database(), "session_touched"); got != 1 {
		t.Fatalf("committed SBR authorization session_touched audits = %d, want 1", got)
	}
	var factors int
	if err := storage.Database().QueryRowContext(ctx, `SELECT count(*) FROM totp_factors WHERE user_id = ?`, authentication.ActorUserId).Scan(&factors); err != nil {
		t.Fatal(err)
	}
	if factors != 1 {
		t.Fatalf("persisted TOTP factors = %d, want 1", factors)
	}
}
