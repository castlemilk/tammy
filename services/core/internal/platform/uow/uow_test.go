package uow_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/tammyapp/tammy/services/core/internal/platform/uow"
)

var (
	errBegin    = errors.New("begin failed")
	errCommit   = errors.New("commit failed")
	errInjected = errors.New("injected failure")
)

type persistedState struct {
	domain      []string
	idempotency []string
	audit       []string
}

type fakeRepositories struct {
	staged *persistedState
}

func (repositories fakeRepositories) SaveDomain(value string) {
	repositories.staged.domain = append(repositories.staged.domain, value)
}

func (repositories fakeRepositories) SaveIdempotency(value string) {
	repositories.staged.idempotency = append(repositories.staged.idempotency, value)
}

func (repositories fakeRepositories) AppendAudit(value string) {
	repositories.staged.audit = append(repositories.staged.audit, value)
}

type fakeTransaction struct {
	store              *persistedState
	staged             persistedState
	commitErr          error
	commitCalls        int
	rollbackCalls      int
	rollbackContextErr error
	rollbackPanic      any
}

func (transaction *fakeTransaction) TransactionID() string { return "tx-1" }

func (transaction *fakeTransaction) Repositories() fakeRepositories {
	return fakeRepositories{staged: &transaction.staged}
}

func (transaction *fakeTransaction) Commit(context.Context) error {
	transaction.commitCalls++
	if transaction.commitErr != nil {
		return transaction.commitErr
	}
	transaction.store.domain = append(transaction.store.domain, transaction.staged.domain...)
	transaction.store.idempotency = append(transaction.store.idempotency, transaction.staged.idempotency...)
	transaction.store.audit = append(transaction.store.audit, transaction.staged.audit...)
	transaction.staged = persistedState{}
	return nil
}

func (transaction *fakeTransaction) Rollback(ctx context.Context) error {
	transaction.rollbackCalls++
	transaction.rollbackContextErr = ctx.Err()
	if transaction.rollbackPanic != nil {
		panic(transaction.rollbackPanic)
	}
	transaction.staged = persistedState{}
	return nil
}

type fakeStarter struct {
	store         *persistedState
	beginErr      error
	commitErr     error
	rollbackPanic any
	beginCalls    int
	modes         []uow.Mode
	last          *fakeTransaction
}

type fixedStarter struct {
	transaction uow.Transaction[fakeRepositories]
}

func (starter fixedStarter) Begin(context.Context, uow.Mode) (uow.Transaction[fakeRepositories], error) {
	return starter.transaction, nil
}

func (starter *fakeStarter) Begin(_ context.Context, mode uow.Mode) (uow.Transaction[fakeRepositories], error) {
	starter.beginCalls++
	starter.modes = append(starter.modes, mode)
	if starter.beginErr != nil {
		return nil, starter.beginErr
	}
	starter.last = &fakeTransaction{
		store: starter.store, commitErr: starter.commitErr, rollbackPanic: starter.rollbackPanic,
	}
	return starter.last, nil
}

func TestUnitOfWorkClaimsRollbackBeforeInvokingIt(t *testing.T) {
	panicMarker := &struct{}{}
	starter := &fakeStarter{store: &persistedState{}, rollbackPanic: panicMarker}
	unit := uow.New[fakeRepositories](starter)
	defer func() {
		if recovered := recover(); recovered != panicMarker {
			t.Fatalf("recovered = %v, want rollback panic marker", recovered)
		}
		if starter.last.rollbackCalls != 1 {
			t.Fatalf("rollback calls = %d, want exactly 1", starter.last.rollbackCalls)
		}
	}()
	_ = unit.Do(context.Background(), func(context.Context, uow.TxScope[fakeRepositories]) error {
		return errInjected
	})
	t.Fatal("unit of work returned after rollback panic")
}

func TestUnitOfWorkCommitsDomainIdempotencyAndAuditExactlyOnce(t *testing.T) {
	store := &persistedState{}
	starter := &fakeStarter{store: store}
	unit := uow.New[fakeRepositories](starter)

	callbackContext := context.WithValue(context.Background(), struct{}{}, "retained")
	err := unit.Do(callbackContext, func(ctx context.Context, scope uow.TxScope[fakeRepositories]) error {
		if ctx != callbackContext || scope.TransactionID() != "tx-1" {
			t.Fatalf("callback context/scope mismatch: context=%v transaction=%q", ctx, scope.TransactionID())
		}
		repositories := scope.Repositories()
		repositories.SaveDomain("journal-1")
		repositories.SaveIdempotency("result-1")
		repositories.AppendAudit("audit-1")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := &persistedState{
		domain:      []string{"journal-1"},
		idempotency: []string{"result-1"},
		audit:       []string{"audit-1"},
	}
	if !reflect.DeepEqual(store, want) {
		t.Fatalf("persisted state = %#v, want %#v", store, want)
	}
	if starter.beginCalls != 1 || !reflect.DeepEqual(starter.modes, []uow.Mode{uow.Write}) || starter.last.commitCalls != 1 || starter.last.rollbackCalls != 0 {
		t.Fatalf("calls = begin:%d commit:%d rollback:%d", starter.beginCalls, starter.last.commitCalls, starter.last.rollbackCalls)
	}
}

func TestUnitOfWorkRollsBackEveryStagedWriteOnFailure(t *testing.T) {
	steps := []struct {
		name string
		work func(fakeRepositories)
	}{
		{name: "after_domain", work: func(repositories fakeRepositories) {
			repositories.SaveDomain("journal-1")
		}},
		{name: "after_idempotency", work: func(repositories fakeRepositories) {
			repositories.SaveDomain("journal-1")
			repositories.SaveIdempotency("result-1")
		}},
		{name: "after_audit", work: func(repositories fakeRepositories) {
			repositories.SaveDomain("journal-1")
			repositories.SaveIdempotency("result-1")
			repositories.AppendAudit("audit-1")
		}},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			store := &persistedState{}
			starter := &fakeStarter{store: store}
			unit := uow.New[fakeRepositories](starter)
			err := unit.Do(context.Background(), func(_ context.Context, scope uow.TxScope[fakeRepositories]) error {
				step.work(scope.Repositories())
				return errInjected
			})
			if !errors.Is(err, errInjected) {
				t.Fatalf("error = %v, want %v", err, errInjected)
			}
			if !reflect.DeepEqual(store, &persistedState{}) {
				t.Fatalf("rollback leaked state: %#v", store)
			}
			if starter.last.commitCalls != 0 || starter.last.rollbackCalls != 1 {
				t.Fatalf("calls = commit:%d rollback:%d", starter.last.commitCalls, starter.last.rollbackCalls)
			}
		})
	}
}

func TestUnitOfWorkRollsBackOnCommitFailureAndReturnsBeginFailure(t *testing.T) {
	store := &persistedState{}
	starter := &fakeStarter{store: store, commitErr: errCommit}
	unit := uow.New[fakeRepositories](starter)
	err := unit.Do(context.Background(), func(_ context.Context, scope uow.TxScope[fakeRepositories]) error {
		scope.Repositories().SaveDomain("journal-1")
		return nil
	})
	if !errors.Is(err, errCommit) {
		t.Fatalf("commit error = %v, want %v", err, errCommit)
	}
	if !reflect.DeepEqual(store, &persistedState{}) || starter.last.commitCalls != 1 || starter.last.rollbackCalls != 1 {
		t.Fatalf("commit failure state=%#v commit=%d rollback=%d", store, starter.last.commitCalls, starter.last.rollbackCalls)
	}

	beginStarter := &fakeStarter{store: &persistedState{}, beginErr: errBegin}
	beginUnit := uow.New[fakeRepositories](beginStarter)
	called := false
	err = beginUnit.Do(context.Background(), func(context.Context, uow.TxScope[fakeRepositories]) error {
		called = true
		return nil
	})
	if !errors.Is(err, errBegin) || called {
		t.Fatalf("begin result = %v, callback called=%t", err, called)
	}
}

func TestUnitOfWorkRejectsNilCallback(t *testing.T) {
	unit := uow.New[fakeRepositories](&fakeStarter{store: &persistedState{}})
	if err := unit.Do(context.Background(), nil); !errors.Is(err, uow.ErrInvalidWork) {
		t.Fatalf("error = %v, want %v", err, uow.ErrInvalidWork)
	}
}

func TestUnitOfWorkRejectsNilAndTypedNilStarters(t *testing.T) {
	var typedNil *fakeStarter
	starters := []uow.Starter[fakeRepositories]{nil, typedNil}
	for index, starter := range starters {
		unit := uow.New[fakeRepositories](starter)
		err := unit.Do(context.Background(), func(context.Context, uow.TxScope[fakeRepositories]) error {
			return nil
		})
		if !errors.Is(err, uow.ErrInvalidStarter) {
			t.Fatalf("starter %d error = %v, want %v", index, err, uow.ErrInvalidStarter)
		}
	}
}

func TestUnitOfWorkRejectsNilAndTypedNilTransactions(t *testing.T) {
	var typedNil *fakeTransaction
	transactions := []uow.Transaction[fakeRepositories]{nil, typedNil}
	for index, transaction := range transactions {
		unit := uow.New[fakeRepositories](fixedStarter{transaction: transaction})
		err := unit.Do(context.Background(), func(context.Context, uow.TxScope[fakeRepositories]) error {
			return nil
		})
		if !errors.Is(err, uow.ErrInvalidTransaction) {
			t.Fatalf("transaction %d error = %v, want %v", index, err, uow.ErrInvalidTransaction)
		}
	}
}

func TestUnitOfWorkReadUsesReadTransaction(t *testing.T) {
	starter := &fakeStarter{store: &persistedState{}}
	unit := uow.New[fakeRepositories](starter)
	called := false
	if err := unit.Read(context.Background(), func(_ context.Context, scope uow.TxScope[fakeRepositories]) error {
		called = true
		if scope.TransactionID() != "tx-1" {
			t.Fatalf("transaction ID = %q", scope.TransactionID())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called || !reflect.DeepEqual(starter.modes, []uow.Mode{uow.ReadOnly}) || starter.last.commitCalls != 1 || starter.last.rollbackCalls != 0 {
		t.Fatalf("read called=%t modes=%v commit=%d rollback=%d", called, starter.modes, starter.last.commitCalls, starter.last.rollbackCalls)
	}
}

func TestUnitOfWorkRollbackIgnoresCallerCancellation(t *testing.T) {
	starter := &fakeStarter{store: &persistedState{}}
	unit := uow.New[fakeRepositories](starter)
	ctx, cancel := context.WithCancel(context.Background())
	err := unit.Do(ctx, func(context.Context, uow.TxScope[fakeRepositories]) error {
		cancel()
		return errInjected
	})
	if !errors.Is(err, errInjected) {
		t.Fatalf("error = %v, want %v", err, errInjected)
	}
	if starter.last.rollbackCalls != 1 || starter.last.rollbackContextErr != nil {
		t.Fatalf("rollback calls=%d context error=%v", starter.last.rollbackCalls, starter.last.rollbackContextErr)
	}
}
