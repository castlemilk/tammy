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

// assertPersistBarrierRepository pauses an identity-owned mutation after
// AssertTOTP has prepared its state and before it asks SQLCipher to persist it.
// AssertTOTP holds Service.mu through this point.
type assertPersistBarrierRepository struct {
	Repository
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func newAssertPersistBarrierRepository(repository Repository) *assertPersistBarrierRepository {
	return &assertPersistBarrierRepository{
		Repository: repository,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (repository *assertPersistBarrierRepository) Mutate(ctx context.Context,
	work func(context.Context, workspace.MutationExecutor, *repositoryState) error) error {
	repository.enterOnce.Do(func() { close(repository.entered) })
	<-repository.release
	return repository.Repository.Mutate(ctx, work)
}

func (repository *assertPersistBarrierRepository) releaseMutation() {
	repository.releaseOnce.Do(func() { close(repository.release) })
}

type queryObservingMutationExecutor struct {
	workspace.MutationExecutor
	queryStarted chan struct{}
	queryOnce    sync.Once
}

func (executor *queryObservingMutationExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	executor.queryOnce.Do(func() { close(executor.queryStarted) })
	return executor.MutationExecutor.QueryContext(ctx, query, args...)
}

func TestSQLCipherValidateAuthorizationProgressesWhileAssertTOTPOwnsIdentityMutex(t *testing.T) {
	harness, storage, authentication := currentUserSQLCipherSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	totpSecret := enrolConfirmedFactor(t, harness, authentication, "admin-password-long-enough", "01890f3c-7b2e-7cc4-98c4-dc0c0c0739a1")
	defer workspace.Zero(totpSecret)
	*harness.now = harness.now.Add(30 * time.Second)

	underlying := harness.service.repository
	barrier := newAssertPersistBarrierRepository(underlying)
	harness.service.repository = barrier
	t.Cleanup(func() {
		barrier.releaseMutation()
		harness.service.repository = underlying
	})

	assertionResult := make(chan error, 1)
	go func() {
		_, err := harness.service.AssertTOTP(ctx, connect.NewRequest(&tammyv1.AssertTOTPRequest{
			Authentication: authentication,
			Code:           &tammyv1.TotpCodeInput{Value: TOTPCode(totpSecret, *harness.now)},
			Purpose:        "identity_authorization_lock_test",
		}))
		assertionResult <- err
	}()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		barrier.releaseMutation()
		select {
		case <-assertionResult:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("TOTP assertion did not reach its pre-persistence barrier")
	}

	readTx, err := storage.Database().BeginEncryptedTx(ctx, nil)
	if err != nil {
		barrier.releaseMutation()
		select {
		case <-assertionResult:
		case <-time.After(5 * time.Second):
		}
		t.Fatalf("begin caller-owned authorization read: %v", err)
	}
	executor := &queryObservingMutationExecutor{
		MutationExecutor: readTx,
		queryStarted:     make(chan struct{}),
	}

	validationResult := make(chan error, 1)
	go func() {
		validationResult <- harness.service.ValidateAuthorizationWithin(ctx, executor, authentication, authorisation.ActionReadFinancial)
	}()

	progressedWhileTOTPWasPaused := false
	select {
	case <-executor.queryStarted:
		progressedWhileTOTPWasPaused = true
	case <-time.After(750 * time.Millisecond):
	}

	var validationErr error
	if progressedWhileTOTPWasPaused {
		select {
		case validationErr = <-validationResult:
		case <-time.After(5 * time.Second):
			_ = readTx.Rollback()
			barrier.releaseMutation()
			select {
			case <-assertionResult:
			case <-time.After(5 * time.Second):
			}
			t.Fatal("authorization transaction did not finish while TOTP persistence was paused")
		}
	}
	_ = readTx.Rollback()
	barrier.releaseMutation()

	select {
	case assertionErr := <-assertionResult:
		if assertionErr != nil {
			t.Fatalf("TOTP assertion failed after releasing persistence barrier: %v", assertionErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TOTP assertion did not finish after releasing persistence barrier")
	}

	if !progressedWhileTOTPWasPaused {
		select {
		case validationErr = <-validationResult:
		case <-time.After(5 * time.Second):
			t.Fatal("authorization transaction did not finish after TOTP released the identity mutex")
		}
		t.Fatal("pure authorization waited for the identity mutex held by TOTP persistence")
	}
	if validationErr != nil {
		t.Fatalf("authorization read failed while TOTP persistence was paused: %v", validationErr)
	}
}
