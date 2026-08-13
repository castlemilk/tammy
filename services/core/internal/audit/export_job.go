package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"regexp"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

var (
	ErrExportJob                    = errors.New("audit: invalid export job")
	ErrExportJobConflict            = errors.New("audit: export job version conflict")
	ErrExportCommitAlreadyCompleted = errors.New("COMMIT_ALREADY_COMPLETED")
)

type exportCommitPointError struct {
	job ExportJob
}

func (err *exportCommitPointError) Error() string { return ErrExportCommitAlreadyCompleted.Error() }
func (err *exportCommitPointError) Unwrap() error { return ErrExportCommitAlreadyCompleted }

func exportCommitPointJob(err error) (ExportJob, bool) {
	var commitPoint *exportCommitPointError
	if !errors.As(err, &commitPoint) || commitPoint == nil {
		return ExportJob{}, false
	}
	return commitPoint.job, true
}

var exportReferencePattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type ExportJobSpec struct {
	ID                    string
	WorkspaceID           string
	OperationKey          string
	OperationHash         []byte
	InputHash             []byte
	Filter                *tammyv1.AuditEventFilter
	SnapshotGeneration    uint64
	SnapshotSequence      uint64
	SnapshotHead          []byte
	DestinationProvider   string
	EvidenceProvider      string
	DestinationCapability string
	Progress              *tammyv1.JobProgress
	CreatedAt             time.Time
}

// ExportJob is the persisted internal lifecycle, including the fencing
// version and restart checkpoint not exposed by the public projection.
type ExportJob struct {
	ID                    string
	WorkspaceID           string
	Version               uint64
	OperationKey          string
	OperationHash         []byte
	InputHash             []byte
	Filter                *tammyv1.AuditEventFilter
	FilterProto           []byte
	SnapshotGeneration    uint64
	SnapshotSequence      uint64
	SnapshotHead          []byte
	DestinationProvider   string
	EvidenceProvider      string
	DestinationCapability string
	State                 tammyv1.AuditExportJobState
	Attempt               uint32
	Stage                 string
	CheckpointProto       []byte
	CheckpointHash        []byte
	ArchiveHash           []byte
	Progress              *tammyv1.JobProgress
	ResultRef             string
	DestinationHash       []byte
	SigningKeyID          string
	CancellationRequested bool
	RenameCommitted       bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

// ExportDestination is an already-approved opaque destination capability.
// Reference must be a UUIDv7 handle, never a filesystem path.
type ExportDestination interface {
	Reference() string
	AtomicCommit(context.Context, []byte) error
	ReadCommitted(context.Context) ([]byte, error)
}

type ExportDestinationResolver interface {
	Resolve(reference string) (ExportDestination, error)
}

type ExportTransactions interface {
	Read(context.Context, func(ServiceTransaction) error) error
	Mutate(context.Context, func(ServiceTransaction) error) error
}

func EnqueueExportJob(ctx context.Context, executor Executor, spec ExportJobSpec) (ExportJob, error) {
	if executor == nil || !exportReferencePattern.MatchString(spec.ID) || !exportReferencePattern.MatchString(spec.WorkspaceID) ||
		!exportReferencePattern.MatchString(spec.OperationKey) || len(spec.OperationHash) != sha256.Size ||
		len(spec.InputHash) != sha256.Size || spec.Filter == nil || spec.SnapshotGeneration == 0 ||
		len(spec.SnapshotHead) != sha256.Size || spec.DestinationProvider != "approved_file" || spec.EvidenceProvider != "audit_chain" ||
		len(spec.DestinationCapability) < 16 || len(spec.DestinationCapability) > 256 ||
		spec.CreatedAt.IsZero() || !validJobProgress(spec.Progress) {
		return ExportJob{}, ErrExportJob
	}
	progress, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec.Progress)
	if err != nil {
		return ExportJob{}, ErrExportJob
	}
	filterProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec.Filter)
	if err != nil || len(spec.Filter.ProtoReflect().GetUnknown()) != 0 {
		return ExportJob{}, ErrExportJob
	}
	instant := formatTimestamp(spec.CreatedAt)
	if _, err := executor.ExecContext(ctx, `INSERT INTO audit_export_jobs_v1(
		id, workspace_id, version, operation_key, operation_hash, input_hash, filter_proto,
		snapshot_generation, snapshot_sequence, snapshot_head, destination_provider, evidence_provider, destination_capability, state, attempt,
		stage, progress_proto, cancellation_requested, rename_committed, created_at, updated_at
	) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'QUEUED', 0, ?, ?, 0, 0, ?, ?)`, spec.ID, spec.WorkspaceID,
		spec.OperationKey, spec.OperationHash, spec.InputHash, filterProto, spec.SnapshotGeneration, spec.SnapshotSequence,
		spec.SnapshotHead, spec.DestinationProvider, spec.EvidenceProvider, spec.DestinationCapability, spec.Progress.Stage, progress, instant, instant); err != nil {
		return ExportJob{}, ErrExportJob
	}
	return LoadExportJob(ctx, executor, spec.ID)
}

func LoadExportJob(ctx context.Context, executor Executor, jobID string) (ExportJob, error) {
	if executor == nil || !exportReferencePattern.MatchString(jobID) {
		return ExportJob{}, ErrExportJob
	}
	rows, err := executor.QueryContext(ctx, exportJobSelect+` WHERE id = ?`, jobID)
	if err != nil {
		return ExportJob{}, ErrExportJob
	}
	defer rows.Close()
	if !rows.Next() {
		return ExportJob{}, ErrExportJob
	}
	job, err := scanExportJob(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return ExportJob{}, ErrExportJob
	}
	return job, nil
}

func ListExportJobs(ctx context.Context, executor Executor, workspaceID string, state tammyv1.AuditExportJobState) ([]ExportJob, error) {
	if executor == nil || workspaceID == "" {
		return nil, ErrExportJob
	}
	query := exportJobSelect + ` WHERE workspace_id=?`
	arguments := []any{workspaceID}
	if state != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_UNSPECIFIED {
		stateName := exportJobStateName(state)
		if stateName == "" {
			return nil, ErrExportJob
		}
		query += ` AND state=?`
		arguments = append(arguments, stateName)
	}
	query += ` ORDER BY created_at, id`
	rows, err := executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, ErrExportJob
	}
	defer rows.Close()
	jobs := make([]ExportJob, 0)
	for rows.Next() {
		job, scanErr := scanExportJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	if rows.Err() != nil {
		return nil, ErrExportJob
	}
	return jobs, nil
}

func ClaimExportJob(ctx context.Context, executor Executor, jobID string, now time.Time) (ExportJob, error) {
	job, err := LoadExportJob(ctx, executor, jobID)
	if err != nil || now.IsZero() || job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED {
		return ExportJob{}, ErrExportJob
	}
	if err := execExportJobFence(ctx, executor, `UPDATE audit_export_jobs_v1 SET
		state='RUNNING', attempt=attempt+1, stage='COLLECTING', updated_at=?, version=version+1
		WHERE id=? AND version=? AND state='QUEUED' AND cancellation_requested=0`, formatTimestamp(now), job.ID, job.Version); err != nil {
		return ExportJob{}, err
	}
	return LoadExportJob(ctx, executor, job.ID)
}

// AuthorizeExportDestination records a verified archive digest and an opaque
// destination capability before any external rename is attempted.
func AuthorizeExportDestination(
	ctx context.Context,
	executor Executor,
	job ExportJob,
	archive []byte,
	resultRef string,
	progress *tammyv1.JobProgress,
	now time.Time,
) (ExportJob, error) {
	if executor == nil || now.IsZero() || job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING ||
		!validDestinationReference(resultRef) || !validJobProgress(progress) {
		return ExportJob{}, ErrExportJob
	}
	verification, err := VerifyEvidenceArchive(archive)
	if err != nil || verification.Manifest.WorkspaceId != job.WorkspaceID {
		return ExportJob{}, ErrExportJob
	}
	archiveHash := sha256.Sum256(archive)
	checkpoint := append([]byte(nil), archiveHash[:]...)
	checkpointHash := sha256.Sum256(checkpoint)
	progressProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(progress)
	if err != nil {
		return ExportJob{}, ErrExportJob
	}
	if err := execExportJobFence(ctx, executor, `UPDATE audit_export_jobs_v1 SET
		stage='ARCHIVE_VERIFIED', checkpoint_proto=?, checkpoint_hash=?, progress_proto=?, result_ref=?,
		signing_key_id=?, updated_at=?, version=version+1
		WHERE id=? AND version=? AND state='RUNNING' AND cancellation_requested=0`, checkpoint, checkpointHash[:],
		progressProto, resultRef, verification.Manifest.SigningKeyId, formatTimestamp(now), job.ID, job.Version); err != nil {
		return ExportJob{}, err
	}
	return LoadExportJob(ctx, executor, job.ID)
}

func RequestExportCancellation(ctx context.Context, executor Executor, jobID string, now time.Time) error {
	if executor == nil || !exportReferencePattern.MatchString(jobID) || now.IsZero() {
		return ErrExportJob
	}
	job, err := LoadExportJob(ctx, executor, jobID)
	if err != nil {
		return err
	}
	result, err := executor.ExecContext(ctx, `UPDATE audit_export_jobs_v1 SET
		cancellation_requested=1, state='CANCELLED', stage='CANCELLED',
		completed_at=?, updated_at=?, version=version+1
		WHERE id=? AND version=?
		AND state IN ('QUEUED','RUNNING','WAITING_FOR_INPUT','FAILED_RETRYABLE')
		AND stage NOT IN ('DESTINATION_COMMITTING','COMMIT_DESTINATION_REAPPROVAL')
		AND rename_committed=0`, formatTimestamp(now), formatTimestamp(now), jobID, job.Version)
	if err != nil {
		return ErrExportJob
	}
	count, countErr := result.RowsAffected()
	if countErr != nil {
		return ErrExportJob
	}
	if count == 1 {
		return nil
	}
	if count != 0 {
		return ErrExportJob
	}
	current, loadErr := LoadExportJob(ctx, executor, jobID)
	if loadErr != nil {
		return loadErr
	}
	if current.RenameCommitted || current.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED ||
		current.Stage == "DESTINATION_COMMITTING" || current.Stage == "COMMIT_DESTINATION_REAPPROVAL" {
		return &exportCommitPointError{job: current}
	}
	return ErrExportJobConflict
}

func FailExportJob(
	ctx context.Context,
	executor Executor,
	jobID string,
	expectedVersion uint64,
	retryable bool,
	stage string,
	now time.Time,
) (ExportJob, error) {
	if executor == nil || !exportReferencePattern.MatchString(jobID) || expectedVersion == 0 || stage == "" || len(stage) > 64 || now.IsZero() {
		return ExportJob{}, ErrExportJob
	}
	state := "FAILED_TERMINAL"
	var completed any = formatTimestamp(now)
	if retryable {
		state = "FAILED_RETRYABLE"
		completed = nil
	}
	if err := execExportJobFence(ctx, executor, `UPDATE audit_export_jobs_v1 SET state=?, stage=?,
		completed_at=?, updated_at=?, version=version+1 WHERE id=? AND version=? AND state='RUNNING' AND rename_committed=0`,
		state, stage, completed, formatTimestamp(now), jobID, expectedVersion); err != nil {
		return ExportJob{}, err
	}
	return LoadExportJob(ctx, executor, jobID)
}

func RetryExportJob(
	ctx context.Context,
	executor Executor,
	jobID string,
	expectedVersion uint64,
	progress *tammyv1.JobProgress,
	now time.Time,
) (ExportJob, error) {
	if executor == nil || !exportReferencePattern.MatchString(jobID) || expectedVersion == 0 || !validJobProgress(progress) || now.IsZero() {
		return ExportJob{}, ErrExportJob
	}
	progressProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(progress)
	if err != nil {
		return ExportJob{}, ErrExportJob
	}
	if err := execExportJobFence(ctx, executor, `UPDATE audit_export_jobs_v1 SET state='QUEUED', stage='COLLECTING',
		checkpoint_proto=NULL, checkpoint_hash=NULL, progress_proto=?, result_ref=NULL, destination_hash=NULL,
		signing_key_id=NULL, cancellation_requested=0, completed_at=NULL, updated_at=?, version=version+1
		WHERE id=? AND version=? AND state IN ('FAILED_RETRYABLE','WAITING_FOR_INPUT') AND rename_committed=0`,
		progressProto, formatTimestamp(now), jobID, expectedVersion); err != nil {
		return ExportJob{}, err
	}
	return LoadExportJob(ctx, executor, jobID)
}

func ReapproveExportDestination(
	ctx context.Context,
	executor Executor,
	jobID string,
	expectedVersion uint64,
	approvedReference string,
	progress *tammyv1.JobProgress,
	now time.Time,
) (ExportJob, error) {
	if executor == nil || !exportReferencePattern.MatchString(jobID) || expectedVersion == 0 ||
		!validDestinationReference(approvedReference) || !validJobProgress(progress) || now.IsZero() {
		return ExportJob{}, ErrExportJob
	}
	progressProto, err := proto.MarshalOptions{Deterministic: true}.Marshal(progress)
	if err != nil {
		return ExportJob{}, ErrExportJob
	}
	if err := execExportJobFence(ctx, executor, `UPDATE audit_export_jobs_v1 SET
		state='QUEUED', stage='COLLECTING',
		progress_proto=?, result_ref=?, destination_hash=NULL,
		cancellation_requested=0, completed_at=NULL, updated_at=?, version=version+1
		WHERE id=? AND version=? AND state='WAITING_FOR_INPUT' AND rename_committed=0`,
		progressProto, approvedReference, formatTimestamp(now), jobID, expectedVersion); err != nil {
		return ExportJob{}, err
	}
	return LoadExportJob(ctx, executor, jobID)
}

// CommitAuthorizedExport first elects commit versus cancellation in one short
// SQL transaction, then releases SQL before touching the destination. Once the
// commit intent is durable, caller cancellation can no longer produce a
// CANCELLED job with destination bytes.
func CommitAuthorizedExport(
	ctx context.Context,
	transactions ExportTransactions,
	gate *WriteGate,
	jobID string,
	archive []byte,
	destination ExportDestination,
	now time.Time,
) (ExportJob, error) {
	if transactions == nil || gate == nil || destination == nil || now.IsZero() {
		return ExportJob{}, ErrExportJob
	}
	if !gate.EvidenceExportAllowed() {
		return ExportJob{}, ErrWriteGate
	}
	archiveHash := sha256.Sum256(archive)
	if _, err := VerifyEvidenceArchive(archive); err != nil {
		return ExportJob{}, ErrExportJob
	}
	var elected ExportJob
	err := transactions.Mutate(ctx, func(executor ServiceTransaction) error {
		job, loadErr := LoadExportJob(ctx, executor, jobID)
		if loadErr != nil {
			return loadErr
		}
		if job.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED {
			elected = job
			return nil
		}
		if job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING || job.Stage != "ARCHIVE_VERIFIED" ||
			destination.Reference() != job.ResultRef || len(job.ArchiveHash) != sha256.Size || !bytes.Equal(archiveHash[:], job.ArchiveHash) {
			return ErrExportJob
		}
		if err := execExportJobFence(ctx, executor, `UPDATE audit_export_jobs_v1 SET
			stage='DESTINATION_COMMITTING', updated_at=?, version=version+1
			WHERE id=? AND version=? AND state='RUNNING' AND stage='ARCHIVE_VERIFIED' AND cancellation_requested=0 AND rename_committed=0`,
			formatTimestamp(now), job.ID, job.Version); err != nil {
			return err
		}
		elected, loadErr = LoadExportJob(ctx, executor, job.ID)
		return loadErr
	})
	if err != nil {
		return ExportJob{}, err
	}
	if elected.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED {
		return elected, nil
	}
	if !gate.EvidenceExportAllowed() {
		return ExportJob{}, ErrWriteGate
	}
	commitContext := context.WithoutCancel(ctx)
	committed, err := destination.ReadCommitted(commitContext)
	if err != nil {
		if err := destination.AtomicCommit(commitContext, archive); err != nil {
			return ExportJob{}, ErrExportJob
		}
		committed, err = destination.ReadCommitted(commitContext)
		if err != nil {
			return ExportJob{}, ErrExportJob
		}
	}
	destinationHash := sha256.Sum256(committed)
	if destinationHash != archiveHash {
		return ExportJob{}, ErrExportJob
	}
	if _, err := VerifyEvidenceArchive(committed); err != nil {
		return ExportJob{}, ErrExportJob
	}
	var completedJob ExportJob
	err = transactions.Mutate(commitContext, func(executor ServiceTransaction) error {
		job, loadErr := LoadExportJob(commitContext, executor, elected.ID)
		if loadErr != nil || job.State != tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING ||
			job.Stage != "DESTINATION_COMMITTING" || job.CancellationRequested || job.RenameCommitted {
			return ErrExportJob
		}
		if err := execExportJobFence(commitContext, executor, `UPDATE audit_export_jobs_v1 SET
			state='COMPLETED', stage='COMPLETED', destination_hash=?, rename_committed=1,
			progress_proto=?, completed_at=?, updated_at=?, version=version+1
			WHERE id=? AND version=? AND state='RUNNING' AND stage='DESTINATION_COMMITTING' AND cancellation_requested=0 AND rename_committed=0`,
			destinationHash[:], completedProgress(job.Progress), formatTimestamp(now), formatTimestamp(now), job.ID, job.Version); err != nil {
			return err
		}
		completedJob, loadErr = LoadExportJob(commitContext, executor, job.ID)
		return loadErr
	})
	return completedJob, err
}

// ReconstructExportJobs repairs RUNNING work after startup. Uncheckpointed work
// requeues; an authorised destination requires reapproval when unavailable; a
// matching already-renamed archive is recovered as completed.
func ReconstructExportJobs(
	ctx context.Context,
	transactions ExportTransactions,
	resolver ExportDestinationResolver,
	now time.Time,
) ([]ExportJob, error) {
	if transactions == nil || resolver == nil || now.IsZero() {
		return nil, ErrExportJob
	}
	running := make([]ExportJob, 0)
	if err := transactions.Read(ctx, func(executor ServiceTransaction) error {
		rows, queryErr := executor.QueryContext(ctx, exportJobSelect+` WHERE state='RUNNING' ORDER BY created_at, id`)
		if queryErr != nil {
			return ErrExportJob
		}
		defer rows.Close()
		for rows.Next() {
			job, scanErr := scanExportJob(rows)
			if scanErr != nil {
				return scanErr
			}
			running = append(running, job)
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	results := make([]ExportJob, 0, len(running))
	for _, job := range running {
		var updateQuery string
		var updateArgs []any
		if job.CancellationRequested && job.Stage != "DESTINATION_COMMITTING" {
			updateQuery = `UPDATE audit_export_jobs_v1 SET state='CANCELLED', stage='CANCELLED', completed_at=?, updated_at=?, version=version+1 WHERE id=? AND version=? AND state='RUNNING'`
			updateArgs = []any{formatTimestamp(now), formatTimestamp(now), job.ID, job.Version}
		} else if (job.Stage == "ARCHIVE_VERIFIED" || job.Stage == "DESTINATION_COMMITTING") && len(job.ArchiveHash) == sha256.Size && job.ResultRef != "" {
			destination, resolveErr := resolver.Resolve(job.ResultRef)
			committed, readErr := []byte(nil), error(nil)
			if resolveErr == nil && destination != nil && destination.Reference() == job.ResultRef {
				committed, readErr = destination.ReadCommitted(ctx)
			} else {
				readErr = ErrExportJob
			}
			if readErr != nil {
				waitingStage := "DESTINATION_REAPPROVAL"
				if job.Stage == "DESTINATION_COMMITTING" {
					waitingStage = "COMMIT_DESTINATION_REAPPROVAL"
				}
				updateQuery = `UPDATE audit_export_jobs_v1 SET state='WAITING_FOR_INPUT', stage=?, result_ref=NULL, updated_at=?, version=version+1 WHERE id=? AND version=? AND state='RUNNING'`
				updateArgs = []any{waitingStage, formatTimestamp(now), job.ID, job.Version}
			} else {
				digest := sha256.Sum256(committed)
				if !bytes.Equal(digest[:], job.ArchiveHash) {
					updateQuery = `UPDATE audit_export_jobs_v1 SET state='FAILED_TERMINAL', stage='DESTINATION_HASH_MISMATCH', completed_at=?, updated_at=?, version=version+1 WHERE id=? AND version=? AND state='RUNNING'`
					updateArgs = []any{formatTimestamp(now), formatTimestamp(now), job.ID, job.Version}
				} else if _, verifyErr := VerifyEvidenceArchive(committed); verifyErr != nil {
					return nil, ErrExportJob
				} else {
					updateQuery = `UPDATE audit_export_jobs_v1 SET state='COMPLETED', stage='COMPLETED', destination_hash=?, rename_committed=1, progress_proto=?, completed_at=?, updated_at=?, version=version+1 WHERE id=? AND version=? AND state='RUNNING' AND cancellation_requested=0`
					updateArgs = []any{digest[:], completedProgress(job.Progress), formatTimestamp(now), formatTimestamp(now), job.ID, job.Version}
				}
			}
		} else {
			updateQuery = `UPDATE audit_export_jobs_v1 SET state='QUEUED', stage='COLLECTING', checkpoint_proto=NULL, checkpoint_hash=NULL, result_ref=NULL, updated_at=?, version=version+1 WHERE id=? AND version=? AND state='RUNNING'`
			updateArgs = []any{formatTimestamp(now), job.ID, job.Version}
		}
		var updated ExportJob
		if err := transactions.Mutate(ctx, func(executor ServiceTransaction) error {
			if err := execExportJobFence(ctx, executor, updateQuery, updateArgs...); err != nil {
				return err
			}
			var loadErr error
			updated, loadErr = LoadExportJob(ctx, executor, job.ID)
			return loadErr
		}); err != nil {
			return nil, err
		}
		results = append(results, updated)
	}
	return results, nil
}

const exportJobSelect = `SELECT id, workspace_id, version, operation_key, operation_hash, input_hash,
	filter_proto, snapshot_generation, snapshot_sequence, snapshot_head, destination_provider, evidence_provider, destination_capability,
	state, attempt, stage, checkpoint_proto, checkpoint_hash, progress_proto, result_ref,
	destination_hash, signing_key_id, cancellation_requested, rename_committed, created_at, updated_at, completed_at
	FROM audit_export_jobs_v1`

type exportJobScanner interface {
	Scan(...any) error
}

func scanExportJob(scanner exportJobScanner) (ExportJob, error) {
	var job ExportJob
	var state, created, updated string
	var completed, resultRef, signingKeyID sql.NullString
	var progressProto []byte
	var cancellation, renamed int
	if err := scanner.Scan(&job.ID, &job.WorkspaceID, &job.Version, &job.OperationKey, &job.OperationHash, &job.InputHash,
		&job.FilterProto, &job.SnapshotGeneration, &job.SnapshotSequence, &job.SnapshotHead, &job.DestinationProvider, &job.EvidenceProvider, &job.DestinationCapability,
		&state, &job.Attempt, &job.Stage, &job.CheckpointProto, &job.CheckpointHash, &progressProto, &resultRef,
		&job.DestinationHash, &signingKeyID, &cancellation, &renamed, &created, &updated, &completed); err != nil {
		return ExportJob{}, ErrExportJob
	}
	job.State = parseExportJobState(state)
	job.ResultRef = resultRef.String
	job.SigningKeyID = signingKeyID.String
	job.CancellationRequested = cancellation == 1
	job.RenameCommitted = renamed == 1
	job.Progress = &tammyv1.JobProgress{}
	job.Filter = &tammyv1.AuditEventFilter{}
	var err error
	job.CreatedAt, err = time.Parse(timestampLayout, created)
	if err == nil {
		job.UpdatedAt, err = time.Parse(timestampLayout, updated)
	}
	if err == nil && completed.Valid {
		var value time.Time
		value, err = time.Parse(timestampLayout, completed.String)
		job.CompletedAt = &value
	}
	if err != nil || (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(progressProto, job.Progress) != nil ||
		(proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(job.FilterProto, job.Filter) != nil || len(job.Filter.ProtoReflect().GetUnknown()) != 0 ||
		!validJobProgress(job.Progress) || job.State == tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_UNSPECIFIED ||
		len(job.OperationHash) != sha256.Size || len(job.InputHash) != sha256.Size || len(job.SnapshotHead) != sha256.Size ||
		job.SnapshotGeneration == 0 || job.DestinationProvider != "approved_file" || job.EvidenceProvider != "audit_chain" || len(job.DestinationCapability) < 16 {
		return ExportJob{}, ErrExportJob
	}
	if (job.Stage == "ARCHIVE_VERIFIED" || job.Stage == "DESTINATION_COMMITTING") && len(job.CheckpointProto) == sha256.Size {
		digest := sha256.Sum256(job.CheckpointProto)
		if bytes.Equal(digest[:], job.CheckpointHash) {
			job.ArchiveHash = append([]byte(nil), job.CheckpointProto...)
		}
	}
	return job, nil
}

func execExportJobFence(ctx context.Context, executor Executor, query string, arguments ...any) error {
	result, err := executor.ExecContext(ctx, query, arguments...)
	if err != nil {
		return ErrExportJob
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrExportJobConflict
	}
	return nil
}

func validJobProgress(progress *tammyv1.JobProgress) bool {
	return progress != nil && progress.Stage != "" && len(progress.Stage) <= 64 &&
		(progress.TotalUnits == 0 || progress.CompletedUnits <= progress.TotalUnits) && len(progress.ProtoReflect().GetUnknown()) == 0
}

func validDestinationReference(reference string) bool {
	return len(reference) >= 16 && len(reference) <= 256
}

func completedProgress(progress *tammyv1.JobProgress) []byte {
	completed := proto.Clone(progress).(*tammyv1.JobProgress)
	completed.Stage = "COMPLETED"
	if completed.TotalUnits != 0 {
		completed.CompletedUnits = completed.TotalUnits
	}
	encoded, _ := proto.MarshalOptions{Deterministic: true}.Marshal(completed)
	return encoded
}

func parseExportJobState(state string) tammyv1.AuditExportJobState {
	switch state {
	case "QUEUED":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED
	case "RUNNING":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING
	case "WAITING_FOR_INPUT":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT
	case "COMPLETED":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED
	case "FAILED_RETRYABLE":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE
	case "FAILED_TERMINAL":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_TERMINAL
	case "CANCELLED":
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED
	default:
		return tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_UNSPECIFIED
	}
}

func exportJobStateName(state tammyv1.AuditExportJobState) string {
	switch state {
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_QUEUED:
		return "QUEUED"
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_RUNNING:
		return "RUNNING"
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_WAITING_FOR_INPUT:
		return "WAITING_FOR_INPUT"
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_COMPLETED:
		return "COMPLETED"
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_RETRYABLE:
		return "FAILED_RETRYABLE"
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_FAILED_TERMINAL:
		return "FAILED_TERMINAL"
	case tammyv1.AuditExportJobState_AUDIT_EXPORT_JOB_STATE_CANCELLED:
		return "CANCELLED"
	default:
		return ""
	}
}
