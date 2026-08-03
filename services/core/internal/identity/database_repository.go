//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package identity

import (
	"context"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
	"github.com/tammyapp/tammy/services/core/internal/workspace"
)

// DatabaseRepository persists identity aggregates in constrained normalized
// SQLCipher tables. Every repository-owned read or mutation uses one authenticated
// transaction; LoadFrom and SaveTo participate in a caller-owned unit of work.
type DatabaseRepository struct{ database *sqlcipher.Database }

func NewDatabaseRepository(database *sqlcipher.Database) (*DatabaseRepository, error) {
	if database == nil {
		return nil, ErrRepositoryIntegrity
	}
	return &DatabaseRepository{database: database}, nil
}

func (repository *DatabaseRepository) Load(ctx context.Context) (repositoryState, error) {
	transaction, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return repositoryState{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	state, err := repository.LoadFrom(ctx, transaction)
	if err != nil {
		return repositoryState{}, err
	}
	if err := transaction.Commit(); err != nil {
		return repositoryState{}, err
	}
	committed = true
	return state, nil
}

func (repository *DatabaseRepository) Save(ctx context.Context, state repositoryState) error {
	return repository.Mutate(ctx, func(_ context.Context, _ workspace.MutationExecutor, current *repositoryState) error {
		cloned, err := cloneState(state)
		if err != nil {
			return err
		}
		*current = cloned
		return nil
	})
}

func (repository *DatabaseRepository) Mutate(ctx context.Context, work func(context.Context, workspace.MutationExecutor, *repositoryState) error) error {
	if work == nil {
		return ErrRepositoryIntegrity
	}
	transaction, err := repository.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	state, err := repository.LoadFrom(ctx, transaction)
	if err != nil {
		return err
	}
	if err := work(ctx, transaction, &state); err != nil {
		if isRepositoryBusy(err) {
			return ErrRepositoryConflict
		}
		return err
	}
	if err := repository.SaveTo(ctx, transaction, state); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		if isRepositoryBusy(err) {
			return ErrRepositoryConflict
		}
		return err
	}
	committed = true
	return nil
}

func (repository *DatabaseRepository) LoadFrom(ctx context.Context, executor workspace.MutationExecutor) (repositoryState, error) {
	return loadRepositoryStateFrom(ctx, executor)
}

func (repository *DatabaseRepository) SaveTo(ctx context.Context, executor workspace.MutationExecutor, state repositoryState) error {
	return saveRepositoryStateTo(ctx, executor, state)
}
