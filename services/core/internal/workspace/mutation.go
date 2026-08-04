package workspace

import (
	"context"
	"database/sql"
	"errors"
)

// MutationExecutor is the common database contract used by workspace-owned
// units of work and their dependent domain services.
type MutationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type mutationTransaction struct {
	MutationExecutor
	afterCommit []func(context.Context) error
}

// MutationLifecycle is the post-commit boundary shared by workspace and
// identity-owned SQLCipher units of work.
type MutationLifecycle interface {
	MutationExecutor
	AfterCommit(func(context.Context) error) error
	Publish(context.Context) error
}

func NewMutationLifecycle(executor MutationExecutor) MutationLifecycle {
	if executor == nil {
		return nil
	}
	return &mutationTransaction{MutationExecutor: executor}
}

func (transaction *mutationTransaction) AfterCommit(callback func(context.Context) error) error {
	if transaction == nil || transaction.MutationExecutor == nil || callback == nil {
		return errors.New("workspace: invalid post-commit callback")
	}
	transaction.afterCommit = append(transaction.afterCommit, callback)
	return nil
}

func (transaction *mutationTransaction) publish(ctx context.Context) error {
	for _, callback := range transaction.afterCommit {
		if err := callback(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (transaction *mutationTransaction) Publish(ctx context.Context) error {
	return transaction.publish(ctx)
}
