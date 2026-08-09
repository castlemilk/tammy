//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package restore

import (
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/storage/sqlcipher"
)

type SQLCipherRestoreAuthenticationTransactions struct{ database *sqlcipher.Database }

func NewSQLCipherRestoreAuthenticationTransactions(
	database *sqlcipher.Database,
) (*SQLCipherRestoreAuthenticationTransactions, error) {
	if database == nil || database.DB == nil {
		return nil, ErrRestoreAuthorization
	}
	return &SQLCipherRestoreAuthenticationTransactions{database: database}, nil
}

func (transactions *SQLCipherRestoreAuthenticationTransactions) WithinRestoreAuthentication(
	ctx context.Context,
	work func(context.Context, RestoreAuthenticationExecutor) error,
) error {
	if transactions == nil || transactions.database == nil || ctx == nil || work == nil {
		return ErrRestoreAuthorization
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	transaction, err := transactions.database.BeginEncryptedTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := work(ctx, transaction); err != nil {
		rollbackErr := transaction.Rollback()
		return errors.Join(err, rollbackErr)
	}
	return transaction.Commit()
}
