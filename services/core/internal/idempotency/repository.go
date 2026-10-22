package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrConflict        = errors.New("idempotency: semantic request conflict")
	ErrAborted         = errors.New("idempotency: elected command remains in flight")
	ErrInvalidElection = errors.New("idempotency: invalid election")
	ErrRepository      = errors.New("idempotency: repository failure")
	ErrElectionBusy    = errors.New("idempotency: election writer is busy")
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// Executor intentionally excludes Commit and Rollback; election/result writes
// remain owned by the caller's transaction scope.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type Scope struct {
	WorkspaceID  string
	ActorUserID  string
	RPCName      string
	OperationKey string
}

type Record struct {
	Scope               Scope
	SemanticHashVersion string
	RequestType         string
	NormalizedHash      [sha256.Size]byte
	ResultType          string
	ResultProto         []byte
	Outcome             string
	FailureCode         string
	ResultResourceID    string
	Attempt             uint32
	RetentionPolicy     string
	CreatedAt           time.Time
	CompletedAt         *time.Time
}

type Repository struct{ executor Executor }

func NewRepository(executor Executor) (*Repository, error) {
	if executor == nil {
		return nil, ErrRepository
	}
	return &Repository{executor: executor}, nil
}

func (repository *Repository) insertElection(ctx context.Context, record Record) (bool, error) {
	if _, err := repository.executor.ExecContext(ctx, `PRAGMA busy_timeout=0`); err != nil {
		return false, fmt.Errorf("%w: configure election timeout", ErrRepository)
	}
	defer func() { _, _ = repository.executor.ExecContext(context.WithoutCancel(ctx), `PRAGMA busy_timeout=5000`) }()
	result, err := repository.executor.ExecContext(ctx, `INSERT INTO command_idempotency_v1(
		workspace_id, actor_user_id, fully_qualified_rpc_name, operation_key,
		semantic_hash_version, request_type, normalized_hash, outcome, attempt, retention_policy, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, 'ELECTED', 1, 'WORKSPACE_LIFETIME', ?)
	ON CONFLICT(workspace_id, actor_user_id, fully_qualified_rpc_name, operation_key) DO NOTHING`,
		record.Scope.WorkspaceID, record.Scope.ActorUserID, record.Scope.RPCName, record.Scope.OperationKey,
		record.SemanticHashVersion, record.RequestType, record.NormalizedHash[:], formatTimestamp(record.CreatedAt))
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "busy") || strings.Contains(message, "locked") {
			return false, ErrElectionBusy
		}
		return false, fmt.Errorf("%w: insert election", ErrRepository)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("%w: insert election result", ErrRepository)
	}
	return count == 1, nil
}

func (repository *Repository) load(ctx context.Context, scope Scope) (Record, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT semantic_hash_version, request_type, normalized_hash,
		COALESCE(result_type, ''), result_proto, outcome, COALESCE(failure_code, ''),
		COALESCE(result_resource_id, ''), attempt, retention_policy, created_at, completed_at
		FROM command_idempotency_v1
		WHERE workspace_id = ? AND actor_user_id = ? AND fully_qualified_rpc_name = ? AND operation_key = ?`,
		scope.WorkspaceID, scope.ActorUserID, scope.RPCName, scope.OperationKey)
	if err != nil {
		return Record{}, fmt.Errorf("%w: load election", ErrRepository)
	}
	defer rows.Close()
	if !rows.Next() {
		return Record{}, ErrRepository
	}
	record := Record{Scope: scope}
	var normalized []byte
	var resultProto []byte
	var created string
	var completed sql.NullString
	if err := rows.Scan(&record.SemanticHashVersion, &record.RequestType, &normalized, &record.ResultType,
		&resultProto, &record.Outcome, &record.FailureCode, &record.ResultResourceID, &record.Attempt,
		&record.RetentionPolicy, &created, &completed); err != nil || rows.Next() || rows.Err() != nil || len(normalized) != sha256.Size {
		return Record{}, fmt.Errorf("%w: malformed election", ErrRepository)
	}
	copy(record.NormalizedHash[:], normalized)
	record.ResultProto = append([]byte(nil), resultProto...)
	record.CreatedAt, err = time.Parse(timestampLayout, created)
	if err != nil {
		return Record{}, fmt.Errorf("%w: malformed created time", ErrRepository)
	}
	if completed.Valid {
		instant, parseErr := time.Parse(timestampLayout, completed.String)
		if parseErr != nil {
			return Record{}, fmt.Errorf("%w: malformed completion time", ErrRepository)
		}
		record.CompletedAt = &instant
	}
	return record, nil
}

func (repository *Repository) complete(
	ctx context.Context,
	record Record,
	resultType string,
	resultProto []byte,
	resourceID string,
	completedAt time.Time,
) error {
	result, err := repository.executor.ExecContext(ctx, `UPDATE command_idempotency_v1
		SET result_type = ?, result_proto = ?, outcome = 'COMMITTED', failure_code = NULL,
			result_resource_id = ?, completed_at = ?
		WHERE workspace_id = ? AND actor_user_id = ? AND fully_qualified_rpc_name = ? AND operation_key = ?
			AND outcome = 'ELECTED' AND attempt = ? AND normalized_hash = ?`,
		resultType, resultProto, nullString(resourceID), formatTimestamp(completedAt),
		record.Scope.WorkspaceID, record.Scope.ActorUserID, record.Scope.RPCName, record.Scope.OperationKey,
		record.Attempt, record.NormalizedHash[:])
	if err != nil {
		return fmt.Errorf("%w: complete election", ErrRepository)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrInvalidElection
	}
	return nil
}

func (repository *Repository) fail(ctx context.Context, record Record, failureCode string, completedAt time.Time) error {
	if failureCode == "" {
		return ErrInvalidElection
	}
	result, err := repository.executor.ExecContext(ctx, `UPDATE command_idempotency_v1
		SET outcome = 'FAILED', failure_code = ?, completed_at = ?
		WHERE workspace_id = ? AND actor_user_id = ? AND fully_qualified_rpc_name = ? AND operation_key = ?
			AND outcome = 'ELECTED' AND attempt = ? AND normalized_hash = ?`,
		failureCode, formatTimestamp(completedAt), record.Scope.WorkspaceID, record.Scope.ActorUserID,
		record.Scope.RPCName, record.Scope.OperationKey, record.Attempt, record.NormalizedHash[:])
	if err != nil {
		return fmt.Errorf("%w: fail election", ErrRepository)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrInvalidElection
	}
	return nil
}

func (repository *Repository) retryFailed(ctx context.Context, record Record) (Record, error) {
	result, err := repository.executor.ExecContext(ctx, `UPDATE command_idempotency_v1
		SET outcome = 'ELECTED', failure_code = NULL, completed_at = NULL, attempt = attempt + 1
		WHERE workspace_id = ? AND actor_user_id = ? AND fully_qualified_rpc_name = ? AND operation_key = ?
			AND outcome = 'FAILED' AND attempt = ? AND normalized_hash = ?`,
		record.Scope.WorkspaceID, record.Scope.ActorUserID, record.Scope.RPCName, record.Scope.OperationKey,
		record.Attempt, record.NormalizedHash[:])
	if err != nil {
		return Record{}, fmt.Errorf("%w: retry failed election", ErrRepository)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return Record{}, ErrInvalidElection
	}
	record.Outcome = "ELECTED"
	record.FailureCode = ""
	record.CompletedAt = nil
	record.Attempt++
	return record, nil
}

func formatTimestamp(value time.Time) string { return value.UTC().Format(timestampLayout) }

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
