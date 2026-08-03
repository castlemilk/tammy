// Package uow provides one transaction boundary for application commands.
package uow

import (
	"context"
	"errors"
)

var ErrInvalidWork = errors.New("invalid unit of work callback")

// Mode selects a write transaction or a stable read snapshot.
type Mode uint8

const (
	Write Mode = iota + 1
	ReadOnly
)

// Transaction owns a typed repository bundle and its commit/rollback controls.
type Transaction[Repositories any] interface {
	TransactionID() string
	Repositories() Repositories
	Commit(context.Context) error
	Rollback(context.Context) error
}

// Starter begins one transaction with transaction-owned repositories.
type Starter[Repositories any] interface {
	Begin(context.Context, Mode) (Transaction[Repositories], error)
}

// TxScope exposes only repositories bound to the active transaction.
type TxScope[Repositories any] interface {
	TransactionID() string
	Repositories() Repositories
}

type transactionScope[Repositories any] struct {
	transactionID string
	repositories  Repositories
}

func (scope transactionScope[Repositories]) TransactionID() string {
	return scope.transactionID
}

func (scope transactionScope[Repositories]) Repositories() Repositories {
	return scope.repositories
}

// UnitOfWork executes an application command under exactly one transaction.
type UnitOfWork[Repositories any] struct {
	starter Starter[Repositories]
}

// New creates a unit of work for a typed transaction starter.
func New[Repositories any](starter Starter[Repositories]) *UnitOfWork[Repositories] {
	return &UnitOfWork[Repositories]{starter: starter}
}

// Do executes a command in one write transaction.
func (unit *UnitOfWork[Repositories]) Do(
	ctx context.Context,
	work func(context.Context, TxScope[Repositories]) error,
) (runErr error) {
	return unit.run(ctx, Write, work)
}

// Read executes a query against one stable read transaction.
func (unit *UnitOfWork[Repositories]) Read(
	ctx context.Context,
	work func(context.Context, TxScope[Repositories]) error,
) error {
	return unit.run(ctx, ReadOnly, work)
}

func (unit *UnitOfWork[Repositories]) run(
	ctx context.Context,
	mode Mode,
	work func(context.Context, TxScope[Repositories]) error,
) (runErr error) {
	if work == nil {
		return ErrInvalidWork
	}
	transaction, err := unit.starter.Begin(ctx, mode)
	if err != nil {
		return err
	}
	rollbackContext := context.WithoutCancel(ctx)
	finished := false
	defer func() {
		if !finished {
			_ = transaction.Rollback(rollbackContext)
		}
	}()

	scope := transactionScope[Repositories]{
		transactionID: transaction.TransactionID(),
		repositories:  transaction.Repositories(),
	}
	if err := work(ctx, scope); err != nil {
		rollbackErr := transaction.Rollback(rollbackContext)
		finished = true
		return errors.Join(err, rollbackErr)
	}
	if err := transaction.Commit(ctx); err != nil {
		rollbackErr := transaction.Rollback(rollbackContext)
		finished = true
		return errors.Join(err, rollbackErr)
	}
	finished = true
	return nil
}
