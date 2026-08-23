// Package sbr owns durable, redacted SBR readiness state.
package sbr

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrRepository = errors.New("sbr: repository failure")
	ErrInvalid    = errors.New("sbr: invalid redacted state")
)

// SanitizeBackupState disconnects a fixed backup copy from helper-owned
// pending items. It operates only on caller-owned staged storage and performs
// no helper, Keychain, file, or network operation.
func SanitizeBackupState(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	if ctx == nil || executor == nil || ctx.Err() != nil {
		return ErrInvalid
	}
	present, err := sbrSchemaPresent(ctx, executor)
	if err != nil || !present {
		return err
	}
	for _, statement := range []string{
		`UPDATE sbr_mutations_v1 SET mutation_state='ABORTED',pending_id=NULL WHERE mutation_state IN ('PREPARED','STAGED','CORE_COMMITTED','RECONCILE_REQUIRED','ABORT_REQUIRED','ABORTING')`,
		`UPDATE sbr_simulator_transports_v1 SET state='UNKNOWN' WHERE state IN ('DISPATCHING','MAYBE_SENT')`,
	} {
		if _, err := executor.ExecContext(ctx, statement); err != nil {
			return ErrRepository
		}
	}
	return nil
}

func VerifyBackupState(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	if ctx == nil || executor == nil || ctx.Err() != nil {
		return ErrInvalid
	}
	present, err := sbrSchemaPresent(ctx, executor)
	if err != nil || !present {
		return err
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM sbr_mutations_v1 WHERE mutation_state IN ('PREPARED','STAGED','CORE_COMMITTED','RECONCILE_REQUIRED','ABORT_REQUIRED','ABORTING') OR (mutation_state IN ('ABORTED','HELPER_COMMITTED') AND pending_id IS NOT NULL)`,
		`SELECT COUNT(*) FROM sbr_simulator_transports_v1 WHERE state IN ('DISPATCHING','MAYBE_SENT')`,
	} {
		rows, queryErr := executor.QueryContext(ctx, query)
		if queryErr != nil {
			return ErrRepository
		}
		var count int64
		valid := rows.Next() && rows.Scan(&count) == nil && count == 0 && !rows.Next() && rows.Err() == nil
		_ = rows.Close()
		if !valid {
			return ErrRepository
		}
	}
	return nil
}

// MarkRestoredState invalidates restored opaque bindings without opening the
// helper or vault. A user must explicitly re-import before SBR can be ready.
func MarkRestoredState(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, updatedAt string) error {
	if ctx == nil || executor == nil || ctx.Err() != nil || !validTimestamp(updatedAt) {
		return ErrInvalid
	}
	present, err := sbrSchemaPresent(ctx, executor)
	if err != nil || !present {
		return err
	}
	if err := SanitizeBackupState(ctx, executor); err != nil {
		return err
	}
	if _, err := executor.ExecContext(ctx, `UPDATE sbr_credential_bindings_v1
SET binding_state='REIMPORT_REQUIRED',revision=revision+1,updated_at=? WHERE binding_state='ACTIVE'`, updatedAt); err != nil {
		return ErrRepository
	}
	return nil
}

func sbrSchemaPresent(ctx context.Context, executor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (bool, error) {
	rows, err := executor.QueryContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name IN (
'sbr_credential_bindings_v1','sbr_authenticated_profiles_v1','sbr_readiness_transitions_v1',
'sbr_mutations_v1','sbr_idempotency_v1','sbr_simulator_transports_v1')`)
	if err != nil {
		return false, ErrRepository
	}
	defer rows.Close()
	var count int64
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil || (count != 0 && count != 6) {
		return false, ErrRepository
	}
	return count == 6, nil
}

func validTimestamp(value string) bool {
	if len(value) != 30 {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.UTC().Format("2006-01-02T15:04:05.000000000Z") == value
}
