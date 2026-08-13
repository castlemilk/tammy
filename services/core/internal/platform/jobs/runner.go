// Package jobs provides persistent, lease-elected background job execution.
package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

var (
	ErrCheckpointTampered = errors.New("jobs: checkpoint integrity failure")
	ErrInvalidConfig      = errors.New("jobs: invalid runner configuration")
	ErrInvalidOutcome     = errors.New("jobs: handler returned an invalid outcome")
	ErrLeaseLost          = errors.New("jobs: lease lost before finalization")
	ErrPayloadTampered    = errors.New("jobs: payload integrity failure")
	ErrDomainWriteDenied  = errors.New("jobs: domain write denied")
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

var sqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Job is the immutable input plus the latest authenticated checkpoint supplied
// to a handler after lease election.
type Job struct {
	ID                 string
	Kind               string
	PayloadProto       []byte
	Attempt            uint32
	CheckpointSequence uint64
	CheckpointProto    []byte
	LeaseGeneration    uint64
	cancelRequested    bool
}

// Outcome is one deterministic handler step. An incomplete step must provide a
// checkpoint; a complete step atomically records ResultProto as the commit point.
type Outcome struct {
	CheckpointProto []byte
	ProgressProto   []byte
	ResultProto     []byte
	Complete        bool
	// Commit runs only after the active lease is fenced and cancellation has
	// been checked. Its writes commit atomically with the COMPLETED job state.
	// Handlers must return domain writes through Commit, never commit them directly.
	Commit func(context.Context, *DomainTransaction) error
}

// DomainTransaction is a deliberately narrow capability for committing one
// job's domain result. It has no transaction-control or arbitrary-SQL methods,
// and it can insert only into tables allowlisted by the composition root.
type DomainTransaction struct {
	transaction *sql.Tx
	allowed     map[string]struct{}
}

// InsertRow inserts one row into an allowlisted domain table.
func (transaction *DomainTransaction) InsertRow(ctx context.Context, table string, columns []string, values ...any) error {
	if transaction == nil || transaction.transaction == nil || len(columns) == 0 || len(columns) != len(values) {
		return ErrDomainWriteDenied
	}
	if _, allowed := transaction.allowed[table]; !allowed || !sqlIdentifierPattern.MatchString(table) {
		return ErrDomainWriteDenied
	}
	quotedColumns := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for index, column := range columns {
		if !sqlIdentifierPattern.MatchString(column) {
			return ErrDomainWriteDenied
		}
		quotedColumns[index] = `"` + column + `"`
		placeholders[index] = "?"
	}
	statement := `INSERT INTO "` + table + `" (` + strings.Join(quotedColumns, ",") + `) VALUES (` + strings.Join(placeholders, ",") + `)`
	if _, err := transaction.transaction.ExecContext(ctx, statement, values...); err != nil {
		return fmt.Errorf("jobs: insert domain row: %w", err)
	}
	return nil
}

type Handler interface {
	Handle(context.Context, Job) (Outcome, error)
}

type HandlerFunc func(context.Context, Job) (Outcome, error)

func (function HandlerFunc) Handle(ctx context.Context, job Job) (Outcome, error) {
	return function(ctx, job)
}

type Backoff func(attempt uint32) time.Duration

type Config struct {
	Database      *sql.DB
	Clock         clock.Clock
	Handler       Handler
	LeaseDuration time.Duration
	MaxAttempts   uint32
	RetryBackoff  Backoff
	WorkerID      string
	DomainTables  []string
}

type Runner struct {
	config       Config
	domainTables map[string]struct{}
}

type RunResult struct {
	Claimed bool
	JobID   string
	State   string
	Attempt uint32
}

type JobSpec struct {
	ID           string
	Kind         string
	OperationKey string
	PayloadProto []byte
}

func NewRunner(config Config) (*Runner, error) {
	if config.Database == nil || config.Clock == nil || config.Handler == nil ||
		config.LeaseDuration <= 0 || config.MaxAttempts == 0 ||
		config.RetryBackoff == nil || config.WorkerID == "" {
		return nil, ErrInvalidConfig
	}
	allowed := make(map[string]struct{}, len(config.DomainTables))
	for _, table := range config.DomainTables {
		normalized := strings.ToLower(table)
		if table != normalized || !sqlIdentifierPattern.MatchString(table) ||
			normalized == "jobs" || normalized == "job_checkpoints" {
			return nil, ErrInvalidConfig
		}
		if _, duplicate := allowed[table]; duplicate {
			return nil, ErrInvalidConfig
		}
		allowed[table] = struct{}{}
	}
	config.DomainTables = append([]string(nil), config.DomainTables...)
	return &Runner{config: config, domainTables: allowed}, nil
}

// Enqueue persists one idempotently keyed protobuf job.
func Enqueue(ctx context.Context, database *sql.DB, now time.Time, spec JobSpec) error {
	if database == nil || spec.ID == "" || spec.Kind == "" || spec.OperationKey == "" || len(spec.PayloadProto) == 0 {
		return errors.New("jobs: invalid job specification")
	}
	digest := sha256.Sum256(spec.PayloadProto)
	instant := formatTime(now)
	_, err := database.ExecContext(ctx, `
		INSERT INTO jobs(
			id, kind, state, operation_key, semantic_sha256, payload_proto,
			attempt_count, version, created_at, updated_at
		) VALUES (?, ?, 'PENDING', ?, ?, ?, 0, 1, ?, ?)`,
		spec.ID, spec.Kind, spec.OperationKey, hex.EncodeToString(digest[:]),
		spec.PayloadProto, instant, instant)
	if err != nil {
		return fmt.Errorf("jobs: enqueue: %w", err)
	}
	return nil
}

// RequestCancel atomically cancels queued work or marks leased work so its
// runner observes cancellation immediately before the commit point.
func (runner *Runner) RequestCancel(ctx context.Context, jobID string) error {
	now := formatTime(runner.config.Clock.Now())
	result, err := runner.config.Database.ExecContext(ctx, `
		UPDATE jobs
		SET state = CASE
		      WHEN state IN ('PENDING','RETRY_WAIT') THEN 'CANCELLED'
		      WHEN state = 'RUNNING' THEN 'CANCEL_REQUESTED'
		      ELSE state END,
		    lease_owner = CASE WHEN state IN ('PENDING','RETRY_WAIT') THEN NULL ELSE lease_owner END,
		    lease_expires_at = CASE WHEN state IN ('PENDING','RETRY_WAIT') THEN NULL ELSE lease_expires_at END,
		    completed_at = CASE WHEN state IN ('PENDING','RETRY_WAIT') THEN ? ELSE completed_at END,
		    updated_at = CASE WHEN state IN ('PENDING','RETRY_WAIT','RUNNING') THEN ? ELSE updated_at END,
		    version = CASE WHEN state IN ('PENDING','RETRY_WAIT','RUNNING') THEN version + 1 ELSE version END
		WHERE id = ?`, now, now, jobID)
	if err != nil {
		return fmt.Errorf("jobs: request cancellation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("jobs: request cancellation result: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RunNext performs at most one election and one handler step. It never polls or sleeps.
func (runner *Runner) RunNext(ctx context.Context) (RunResult, error) {
	job, claimed, err := runner.claim(ctx)
	if err != nil || !claimed {
		return RunResult{Claimed: claimed}, err
	}
	if job.cancelRequested {
		state, finalizeErr := runner.finalize(ctx, job, Outcome{}, nil)
		return RunResult{Claimed: true, JobID: job.ID, State: state, Attempt: job.Attempt}, finalizeErr
	}
	outcome, handleErr := runner.config.Handler.Handle(ctx, job)
	if handleErr == nil && ((outcome.Complete && len(outcome.CheckpointProto) != 0) ||
		(!outcome.Complete && (len(outcome.CheckpointProto) == 0 || outcome.Commit != nil))) {
		handleErr = ErrInvalidOutcome
	}
	state, finalizeErr := runner.finalize(ctx, job, outcome, handleErr)
	result := RunResult{Claimed: true, JobID: job.ID, State: state, Attempt: job.Attempt}
	if finalizeErr != nil {
		return result, finalizeErr
	}
	if handleErr != nil {
		return result, fmt.Errorf("jobs: handler failed: %w", handleErr)
	}
	return result, nil
}

func (runner *Runner) claim(ctx context.Context) (Job, bool, error) {
	now := runner.config.Clock.Now().UTC()
	tx, err := runner.config.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: begin election: %w", err)
	}
	defer tx.Rollback()
	var job Job
	var checkpointHash []byte
	var checkpointHashHex string
	var electedState, payloadHashHex string
	leaseUntil := formatTime(now.Add(runner.config.LeaseDuration))
	err = tx.QueryRowContext(ctx, `
		UPDATE jobs
		SET state = CASE WHEN state = 'CANCEL_REQUESTED' THEN 'CANCEL_REQUESTED' ELSE 'RUNNING' END,
		    lease_owner = ?, lease_expires_at = ?,
		    lease_generation = lease_generation + 1,
		    attempt_count = attempt_count + CASE WHEN state = 'CANCEL_REQUESTED' THEN 0 ELSE 1 END,
		    updated_at = ?, version = version + 1
		WHERE id = (
			SELECT id FROM jobs
			WHERE (
				(state = 'PENDING' AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR
				(state = 'RETRY_WAIT' AND next_attempt_at <= ?) OR
				(state IN ('RUNNING','CANCEL_REQUESTED') AND lease_expires_at <= ?)
			)
			ORDER BY created_at, id LIMIT 1
		)
		RETURNING id, kind, payload_proto, semantic_sha256, attempt_count, state, lease_generation`,
		runner.config.WorkerID, leaseUntil, formatTime(now),
		formatTime(now), formatTime(now), formatTime(now)).Scan(
		&job.ID, &job.Kind, &job.PayloadProto, &payloadHashHex, &job.Attempt,
		&electedState, &job.LeaseGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: elect lease: %w", err)
	}
	payloadDigest := sha256.Sum256(job.PayloadProto)
	if payloadHashHex != hex.EncodeToString(payloadDigest[:]) {
		return Job{}, false, ErrPayloadTampered
	}
	job.cancelRequested = electedState == "CANCEL_REQUESTED"
	err = tx.QueryRowContext(ctx, `
		SELECT sequence, checkpoint_proto, checkpoint_sha256
		FROM job_checkpoints WHERE job_id = ? ORDER BY sequence DESC LIMIT 1`, job.ID).Scan(
		&job.CheckpointSequence, &job.CheckpointProto, &checkpointHashHex)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, fmt.Errorf("jobs: load checkpoint: %w", err)
	}
	if err == nil {
		digest := sha256.Sum256(job.CheckpointProto)
		checkpointHash, err = hex.DecodeString(checkpointHashHex)
		if err != nil || !bytes.Equal(checkpointHash, digest[:]) {
			return Job{}, false, ErrCheckpointTampered
		}
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("jobs: commit election: %w", err)
	}
	return job, true, nil
}

func (runner *Runner) finalize(ctx context.Context, job Job, outcome Outcome, handleErr error) (string, error) {
	now := runner.config.Clock.Now().UTC()
	tx, err := runner.config.Database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "RUNNING", fmt.Errorf("jobs: begin finalization: %w", err)
	}
	defer tx.Rollback()
	var state, leaseOwner, leaseExpiresAt string
	var leaseGeneration uint64
	if err := tx.QueryRowContext(ctx, `SELECT state, COALESCE(lease_owner, ''), COALESCE(lease_expires_at, ''), lease_generation FROM jobs WHERE id = ?`, job.ID).Scan(&state, &leaseOwner, &leaseExpiresAt, &leaseGeneration); err != nil {
		return "RUNNING", fmt.Errorf("jobs: reload lease: %w", err)
	}
	leaseExpiry, parseErr := time.Parse(timestampLayout, leaseExpiresAt)
	if leaseOwner != runner.config.WorkerID || leaseGeneration != job.LeaseGeneration ||
		parseErr != nil || !leaseExpiry.After(now) ||
		(state != "RUNNING" && state != "CANCEL_REQUESTED") {
		return state, ErrLeaseLost
	}
	if state == "CANCEL_REQUESTED" {
		if err := execFenced(ctx, tx, `
			UPDATE jobs SET state='CANCELLED', lease_owner=NULL, lease_expires_at=NULL,
			completed_at=?, updated_at=?, version=version+1
			WHERE id=? AND lease_owner=? AND lease_generation=?`,
			formatTime(now), formatTime(now), job.ID, runner.config.WorkerID,
			job.LeaseGeneration); err != nil {
			return state, fmt.Errorf("jobs: finalize cancellation: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return state, fmt.Errorf("jobs: commit cancellation: %w", err)
		}
		return "CANCELLED", nil
	}
	var domainCommitErr error
	if handleErr == nil && outcome.Complete && outcome.Commit != nil {
		if _, err := tx.ExecContext(ctx, `SAVEPOINT job_domain_commit`); err != nil {
			return state, fmt.Errorf("jobs: begin domain commit: %w", err)
		}
		if commitErr := outcome.Commit(ctx, &DomainTransaction{transaction: tx, allowed: runner.domainTables}); commitErr != nil {
			_, rollbackErr := tx.ExecContext(context.WithoutCancel(ctx), `ROLLBACK TO job_domain_commit`)
			_, releaseErr := tx.ExecContext(context.WithoutCancel(ctx), `RELEASE job_domain_commit`)
			domainCommitErr = errors.Join(commitErr, rollbackErr, releaseErr)
			handleErr = domainCommitErr
		} else if _, err := tx.ExecContext(ctx, `RELEASE job_domain_commit`); err != nil {
			return state, fmt.Errorf("jobs: release domain commit: %w", err)
		}
	}
	if handleErr != nil {
		state = "RETRY_WAIT"
		var nextAttempt any = formatTime(now.Add(runner.config.RetryBackoff(job.Attempt)))
		var completedAt any
		if job.Attempt >= runner.config.MaxAttempts {
			state = "FAILED"
			nextAttempt = nil
			completedAt = formatTime(now)
		}
		if err := execFenced(ctx, tx, `
			UPDATE jobs SET state=?, next_attempt_at=?, lease_owner=NULL,
			lease_expires_at=NULL, completed_at=?, updated_at=?, version=version+1
			WHERE id=? AND lease_owner=? AND lease_generation=?`, state, nextAttempt,
			completedAt, formatTime(now), job.ID, runner.config.WorkerID,
			job.LeaseGeneration); err != nil {
			return "RUNNING", fmt.Errorf("jobs: persist retry: %w", err)
		}
	} else if outcome.Complete {
		if err := execFenced(ctx, tx, `
			UPDATE jobs SET state='COMPLETED', result_proto=?, progress_proto=?,
			commit_point_reached=1, lease_owner=NULL, lease_expires_at=NULL,
			next_attempt_at=NULL, completed_at=?, updated_at=?, version=version+1
			WHERE id=? AND lease_owner=? AND lease_generation=?`, outcome.ResultProto,
			nullableBytes(outcome.ProgressProto), formatTime(now), formatTime(now),
			job.ID, runner.config.WorkerID, job.LeaseGeneration); err != nil {
			return "RUNNING", fmt.Errorf("jobs: persist completion: %w", err)
		}
		state = "COMPLETED"
	} else {
		digest := sha256.Sum256(outcome.CheckpointProto)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO job_checkpoints(job_id, sequence, checkpoint_proto, checkpoint_sha256, committed_at)
			VALUES (?, ?, ?, ?, ?)`, job.ID, job.CheckpointSequence+1,
			outcome.CheckpointProto, hex.EncodeToString(digest[:]), formatTime(now)); err != nil {
			return "RUNNING", fmt.Errorf("jobs: persist checkpoint: %w", err)
		}
		if err := execFenced(ctx, tx, `
			UPDATE jobs SET state='PENDING', progress_proto=?, lease_owner=NULL,
			lease_expires_at=NULL, next_attempt_at=NULL, updated_at=?, version=version+1
			WHERE id=? AND lease_owner=? AND lease_generation=?`,
			nullableBytes(outcome.ProgressProto), formatTime(now), job.ID,
			runner.config.WorkerID, job.LeaseGeneration); err != nil {
			return "RUNNING", fmt.Errorf("jobs: release checkpointed job: %w", err)
		}
		state = "PENDING"
	}
	if err := tx.Commit(); err != nil {
		return state, fmt.Errorf("jobs: commit finalization: %w", err)
	}
	if domainCommitErr != nil {
		return state, fmt.Errorf("jobs: domain commit failed: %w", domainCommitErr)
	}
	return state, nil
}

func execFenced(ctx context.Context, transaction *sql.Tx, statement string, arguments ...any) error {
	result, err := transaction.ExecContext(ctx, statement, arguments...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrLeaseLost
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(timestampLayout)
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
