package backup

import (
	"context"
	"database/sql"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/sbr"
)

var ErrSnapshotExclusion = errors.New("backup: snapshot exclusion policy failed")

const commandIdempotencyUpdateTrigger = `CREATE TRIGGER command_idempotency_no_update
BEFORE UPDATE ON command_idempotency
BEGIN
  SELECT RAISE(ABORT, 'command idempotency rows are immutable');
END`

// SQLExecutor is a caller-owned transaction over an already-created staged
// snapshot. SanitizeSnapshot never commits and must not be run on the live DB.
type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// SnapshotExcludedSQLState documents the live authentication/session state
// removed from a staged backup database. Historical audit event session IDs
// remain because they are non-secret immutable signed evidence, not an active
// authentication capability. Password verifiers/history and encrypted
// TOTP/recovery material remain because staged admin/recovery proof requires
// them; no plaintext password or factor material is stored by this schema.
var SnapshotExcludedSQLState = []string{
	"application_sessions.*",
	"factor_assertions.*",
	"users.activation_session_id",
	"command_idempotency.session_id",
}

func SanitizeSnapshot(ctx context.Context, executor SQLExecutor) error {
	if ctx == nil || nilInterface(executor) || ctx.Err() != nil {
		return ErrSnapshotExclusion
	}
	if err := verifyPinnedTrigger(ctx, executor, "command_idempotency_no_update", commandIdempotencyUpdateTrigger); err != nil {
		return err
	}
	statements := []string{
		`DROP TRIGGER command_idempotency_no_update`,
		`UPDATE command_idempotency SET session_id=NULL WHERE session_id IS NOT NULL`,
		commandIdempotencyUpdateTrigger,
		`UPDATE users SET activation_session_id=NULL WHERE activation_session_id IS NOT NULL`,
		`DELETE FROM factor_assertions`,
		`DELETE FROM application_sessions`,
	}
	for _, statement := range statements {
		if _, err := executor.ExecContext(ctx, statement); err != nil || ctx.Err() != nil {
			return ErrSnapshotExclusion
		}
	}
	if err := sbr.SanitizeBackupState(ctx, executor); err != nil {
		return ErrSnapshotExclusion
	}
	return VerifySnapshotExclusions(ctx, executor)
}

func VerifySnapshotExclusions(ctx context.Context, executor SQLExecutor) error {
	if ctx == nil || nilInterface(executor) || ctx.Err() != nil {
		return ErrSnapshotExclusion
	}
	queries := []string{
		`SELECT COUNT(*) FROM application_sessions`,
		`SELECT COUNT(*) FROM factor_assertions`,
		`SELECT COUNT(*) FROM users WHERE activation_session_id IS NOT NULL`,
		`SELECT COUNT(*) FROM command_idempotency WHERE session_id IS NOT NULL`,
		`SELECT COUNT(*) FROM application_sessions WHERE state=1`,
	}
	for _, query := range queries {
		count, err := queryCount(ctx, executor, query)
		if err != nil || count != 0 {
			return ErrSnapshotExclusion
		}
	}
	if err := sbr.VerifyBackupState(ctx, executor); err != nil {
		return ErrSnapshotExclusion
	}
	rows, err := executor.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return ErrSnapshotExclusion
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		return ErrSnapshotExclusion
	}
	return nil
}

func verifyPinnedTrigger(ctx context.Context, executor SQLExecutor, name, expected string) error {
	rows, err := executor.QueryContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, name)
	if err != nil {
		return ErrSnapshotExclusion
	}
	defer rows.Close()
	if !rows.Next() {
		return ErrSnapshotExclusion
	}
	var definition string
	if err := rows.Scan(&definition); err != nil || definition != expected || rows.Next() || rows.Err() != nil {
		return ErrSnapshotExclusion
	}
	return nil
}

func queryCount(ctx context.Context, executor SQLExecutor, query string) (int64, error) {
	rows, err := executor.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, ErrSnapshotExclusion
	}
	var count int64
	if err := rows.Scan(&count); err != nil || count < 0 || rows.Next() || rows.Err() != nil {
		return 0, ErrSnapshotExclusion
	}
	return count, nil
}
