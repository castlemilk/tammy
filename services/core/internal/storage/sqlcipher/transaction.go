//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package sqlcipher

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

var ErrUnauthenticatedTransaction = errors.New("sqlcipher: unauthenticated transaction")

type transactionAuthority struct{}

// Transaction is a concrete capability issued only by an authenticated
// SQLCipher Database. Its authority and underlying transaction are unexported,
// so embedding or constructing a zero value cannot forge a usable capability.
type Transaction struct {
	mu          sync.Mutex
	transaction *sql.Tx
	authority   *transactionAuthority
	self        *Transaction
	active      bool
}

func (database *Database) BeginEncryptedTx(ctx context.Context, options *sql.TxOptions) (*Transaction, error) {
	if database == nil || database.DB == nil || database.DB != database.authenticatedDB ||
		database.connector == nil || database.identity == nil {
		return nil, errors.New("sqlcipher: database is required")
	}
	transaction, err := database.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	capability := &Transaction{
		transaction: transaction,
		authority:   &transactionAuthority{},
		active:      true,
	}
	capability.self = capability
	return capability, nil
}

// Authenticated reports whether the capability was issued by SQLCipher and is
// still active. Repository constructors use it as a fail-closed boundary check.
func (transaction *Transaction) Authenticated() bool {
	if transaction == nil {
		return false
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	return transaction.authenticatedLocked()
}

func (transaction *Transaction) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	underlying, err := transaction.underlying()
	if err != nil {
		return nil, err
	}
	return underlying.ExecContext(ctx, query, arguments...)
}

func (transaction *Transaction) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	underlying, err := transaction.underlying()
	if err != nil {
		return nil, err
	}
	return underlying.QueryContext(ctx, query, arguments...)
}

func (transaction *Transaction) QueryRowContext(ctx context.Context, query string, arguments ...any) *Row {
	underlying, err := transaction.underlying()
	if err != nil {
		return &Row{err: err}
	}
	return &Row{row: underlying.QueryRowContext(ctx, query, arguments...)}
}

func (transaction *Transaction) Commit() error {
	underlying, err := transaction.finish()
	if err != nil {
		return err
	}
	return underlying.Commit()
}

func (transaction *Transaction) Rollback() error {
	underlying, err := transaction.finish()
	if err != nil {
		return err
	}
	return underlying.Rollback()
}

func (transaction *Transaction) underlying() (*sql.Tx, error) {
	if transaction == nil {
		return nil, ErrUnauthenticatedTransaction
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if !transaction.authenticatedLocked() {
		return nil, ErrUnauthenticatedTransaction
	}
	return transaction.transaction, nil
}

func (transaction *Transaction) finish() (*sql.Tx, error) {
	if transaction == nil {
		return nil, ErrUnauthenticatedTransaction
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if !transaction.authenticatedLocked() {
		return nil, ErrUnauthenticatedTransaction
	}
	transaction.active = false
	return transaction.transaction, nil
}

func (transaction *Transaction) authenticatedLocked() bool {
	return transaction != nil && transaction.self == transaction && transaction.active &&
		transaction.authority != nil && transaction.transaction != nil
}

// Row preserves database/sql's deferred QueryRow error contract while allowing
// invalid capabilities to fail without panicking.
type Row struct {
	row *sql.Row
	err error
}

func (row *Row) Scan(destinations ...any) error {
	if row == nil {
		return ErrUnauthenticatedTransaction
	}
	if row.err != nil {
		return row.err
	}
	return row.row.Scan(destinations...)
}
