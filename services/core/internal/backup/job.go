package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrBackupJob         = errors.New("backup: invalid job")
	ErrBackupJobConflict = errors.New("backup: job version conflict")
)

const (
	backupJobKind                 = "ENCRYPTED_WORKSPACE_BACKUP_V1"
	maximumBackupJobPayloadBytes  = 4096
	maximumBackupJobProgressBytes = 4096
	maximumBackupJobResultBytes   = 1024
	maximumBackupCheckpointBytes  = 1024
)

type BackupJobSpec struct {
	ID                    string
	WorkspaceID           string
	OperationKey          string
	OperationHash         []byte
	InputHash             []byte
	DestinationCapability string
	PassphraseCapability  string
	CreatedAt             time.Time
}

type BackupJobRecord struct {
	ID                 string
	Version            uint64
	OperationKey       string
	OperationHash      []byte
	Input              *tammyv1.BackupJobInput
	InputProto         []byte
	State              tammyv1.BackupJobState
	Progress           *tammyv1.JobProgress
	Result             *tammyv1.BackupJobResult
	CommitPointReached bool
	Attempt            uint32
	CreatedAt          time.Time
	UpdatedAt          time.Time
	CompletedAt        *time.Time
}

type BackupJobTransactions interface {
	Read(context.Context, func(SQLExecutor) error) error
	Mutate(context.Context, func(SQLExecutor) error) error
}

func EnqueueBackupJob(ctx context.Context, executor SQLExecutor, spec BackupJobSpec) (BackupJobRecord, error) {
	if ctx == nil || nilInterface(executor) || !ids.IsCanonicalV7(spec.ID) || !ids.IsCanonicalV7(spec.WorkspaceID) ||
		!ids.IsCanonicalV7(spec.OperationKey) || !ids.IsCanonicalV7(spec.DestinationCapability) ||
		!ids.IsCanonicalV7(spec.PassphraseCapability) || len(spec.OperationHash) != sha256.Size ||
		len(spec.InputHash) != sha256.Size || spec.CreatedAt.IsZero() || ctx.Err() != nil {
		return BackupJobRecord{}, ErrBackupJob
	}
	wantInputHash := backupJobInputHash(spec.WorkspaceID, spec.DestinationCapability)
	wantOperationHash := backupJobOperationHash(spec.OperationKey, wantInputHash)
	if !bytes.Equal(spec.InputHash, wantInputHash[:]) || !bytes.Equal(spec.OperationHash, wantOperationHash[:]) {
		return BackupJobRecord{}, ErrBackupJob
	}
	input := &tammyv1.BackupJobInput{Format: "tammy-backup-job-v1", WorkspaceId: spec.WorkspaceID,
		OperationHash: append([]byte(nil), spec.OperationHash...), InputHash: append([]byte(nil), spec.InputHash...),
		DestinationCapability: spec.DestinationCapability, PassphraseCapability: spec.PassphraseCapability}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil || len(payload) == 0 || len(payload) > 4096 || len(input.ProtoReflect().GetUnknown()) != 0 {
		return BackupJobRecord{}, ErrBackupJob
	}
	progress := &tammyv1.JobProgress{Stage: "QUEUED"}
	progressBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(progress)
	if err != nil {
		return BackupJobRecord{}, ErrBackupJob
	}
	instant := spec.CreatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := executor.ExecContext(ctx, `INSERT INTO jobs(
		id,kind,state,operation_key,semantic_sha256,payload_proto,progress_proto,
		lease_generation,attempt_count,commit_point_reached,version,created_at,updated_at
	) VALUES(?,?,'PENDING',?,?,?,?,0,0,0,1,?,?)`, spec.ID, backupJobKind, spec.OperationKey,
		hex.EncodeToString(spec.OperationHash), payload, progressBytes, instant, instant); err != nil {
		existing, loadErr := loadBackupJobByOperation(ctx, executor, spec.OperationKey)
		if loadErr == nil && existing.Input.WorkspaceId == spec.WorkspaceID &&
			existing.Input.DestinationCapability == spec.DestinationCapability &&
			bytes.Equal(existing.Input.InputHash, wantInputHash[:]) && bytes.Equal(existing.OperationHash, wantOperationHash[:]) {
			return existing, nil
		}
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, spec.ID)
}

// backupJobInputHash deliberately excludes the ephemeral passphrase capability
// so replay semantics remain stable after that one-use in-memory handle expires.
func backupJobInputHash(workspaceID, destinationCapability string) [sha256.Size]byte {
	return framedBackupJobHash("tammy.backup.job.semantic-input.v1\x00", []byte(workspaceID), []byte(destinationCapability))
}

func backupJobOperationHash(operationKey string, inputHash [sha256.Size]byte) [sha256.Size]byte {
	return framedBackupJobHash("tammy.backup.job.operation.v1\x00", []byte(operationKey), inputHash[:])
}

func framedBackupJobHash(domain string, values ...[]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(domain))
	for _, value := range values {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func LoadBackupJob(ctx context.Context, executor SQLExecutor, jobID string) (BackupJobRecord, error) {
	if ctx == nil || nilInterface(executor) || !ids.IsCanonicalV7(jobID) {
		return BackupJobRecord{}, ErrBackupJob
	}
	metadata, err := loadBackupJobBlobMetadata(ctx, executor, `id=? AND kind=?`, jobID, backupJobKind)
	if err != nil {
		return BackupJobRecord{}, err
	}
	return loadBoundedBackupJob(ctx, executor, `id=? AND kind=?`, metadata, jobID, backupJobKind)
}

func loadBackupJobByOperation(ctx context.Context, executor SQLExecutor, operationKey string) (BackupJobRecord, error) {
	if ctx == nil || nilInterface(executor) || !ids.IsCanonicalV7(operationKey) {
		return BackupJobRecord{}, ErrBackupJob
	}
	metadata, err := loadBackupJobBlobMetadata(ctx, executor, `operation_key=? AND kind=?`, operationKey, backupJobKind)
	if err != nil {
		return BackupJobRecord{}, err
	}
	return loadBoundedBackupJob(ctx, executor, `operation_key=? AND kind=?`, metadata, operationKey, backupJobKind)
}

func claimBackupJob(ctx context.Context, executor SQLExecutor, jobID string, now time.Time) (BackupJobRecord, error) {
	job, err := LoadBackupJob(ctx, executor, jobID)
	if err != nil || now.IsZero() || (job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_QUEUED &&
		job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_RETRYABLE) || job.CommitPointReached {
		return BackupJobRecord{}, ErrBackupJob
	}
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,
		progress_proto=?,updated_at=?,version=version+1 WHERE id=? AND kind=? AND version=?
		AND state IN ('PENDING','RETRY_WAIT') AND commit_point_reached=0`, marshalJobProgress("PREPARING"),
		now.UTC().Format(time.RFC3339Nano), job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, job.ID)
}

func persistPreparedBackupJob(
	ctx context.Context,
	executor SQLExecutor,
	job BackupJobRecord,
	prepared *preparedBackup,
	now time.Time,
) (BackupJobRecord, error) {
	if nilInterface(executor) || job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING ||
		job.CommitPointReached || prepared == nil || len(prepared.manifestHash) != sha256.Size ||
		sha256.Sum256(prepared.archive) != prepared.archiveHash || now.IsZero() {
		return BackupJobRecord{}, ErrBackupJob
	}
	checkpoint := &tammyv1.BackupJobCheckpoint{Format: "tammy-backup-job-checkpoint-v1",
		ManifestHash: append([]byte(nil), prepared.manifestHash...), ArchiveHash: append([]byte(nil), prepared.archiveHash[:]...)}
	checkpointBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(checkpoint)
	if err != nil || len(checkpointBytes) > 1024 {
		return BackupJobRecord{}, ErrBackupJob
	}
	digest := sha256.Sum256(checkpointBytes)
	instant := now.UTC().Format(time.RFC3339Nano)
	if _, err := executor.ExecContext(ctx, `INSERT INTO job_checkpoints(job_id,sequence,checkpoint_proto,checkpoint_sha256,committed_at)
		VALUES(?,1,?,?,?)`, job.ID, checkpointBytes, hex.EncodeToString(digest[:]), instant); err != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET progress_proto=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state='RUNNING' AND commit_point_reached=0`,
		marshalJobProgress("PREPARED"), instant, job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, job.ID)
}

func CancelBackupJob(
	ctx context.Context,
	executor SQLExecutor,
	jobID string,
	expectedVersion uint64,
	now time.Time,
) (BackupJobRecord, error) {
	if ctx == nil || nilInterface(executor) || !ids.IsCanonicalV7(jobID) || expectedVersion == 0 || now.IsZero() {
		return BackupJobRecord{}, ErrBackupJob
	}
	instant := now.UTC().Format(time.RFC3339Nano)
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET state='CANCELLED',progress_proto=?,completed_at=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state IN ('PENDING','RUNNING','RETRY_WAIT') AND commit_point_reached=0`,
		marshalJobProgress("CANCELLED"), instant, instant, jobID, backupJobKind, expectedVersion)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, jobID)
}

func beginBackupPublish(ctx context.Context, executor SQLExecutor, jobID string, now time.Time) (BackupJobRecord, bool, error) {
	job, err := LoadBackupJob(ctx, executor, jobID)
	if err != nil {
		return BackupJobRecord{}, false, err
	}
	if job.State == tammyv1.BackupJobState_BACKUP_JOB_STATE_CANCELLED {
		return job, false, nil
	}
	if job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING || job.CommitPointReached || now.IsZero() {
		return BackupJobRecord{}, false, ErrBackupJob
	}
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET commit_point_reached=1,progress_proto=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state='RUNNING' AND commit_point_reached=0`, marshalJobProgress("PUBLISHING"),
		now.UTC().Format(time.RFC3339Nano), job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, false, ErrBackupJobConflict
	}
	job, err = LoadBackupJob(ctx, executor, job.ID)
	return job, err == nil, err
}

func completeBackupJob(
	ctx context.Context,
	executor SQLExecutor,
	job BackupJobRecord,
	result CreateResult,
	now time.Time,
) (BackupJobRecord, error) {
	if job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING || !job.CommitPointReached ||
		len(result.ManifestHash) != sha256.Size || len(result.DestinationHash) != sha256.Size || now.IsZero() {
		return BackupJobRecord{}, ErrBackupJob
	}
	stored := &tammyv1.BackupJobResult{Format: "tammy-backup-job-result-v1", ManifestHash: append([]byte(nil), result.ManifestHash...),
		DestinationHash: append([]byte(nil), result.DestinationHash...), DestinationCapability: job.Input.DestinationCapability}
	resultBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(stored)
	if err != nil || len(resultBytes) > 1024 {
		return BackupJobRecord{}, ErrBackupJob
	}
	instant := now.UTC().Format(time.RFC3339Nano)
	updated, err := executor.ExecContext(ctx, `UPDATE jobs SET state='COMPLETED',result_proto=?,progress_proto=?,completed_at=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state='RUNNING' AND commit_point_reached=1`, resultBytes,
		marshalJobProgress("COMPLETED"), instant, instant, job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(updated) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, job.ID)
}

func failBackupJob(ctx context.Context, executor SQLExecutor, job BackupJobRecord, stage string, now time.Time) (BackupJobRecord, error) {
	if job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING || job.CommitPointReached ||
		stage == "" || len(stage) > 64 || now.IsZero() {
		return BackupJobRecord{}, ErrBackupJob
	}
	instant := now.UTC().Format(time.RFC3339Nano)
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET state='FAILED',progress_proto=?,completed_at=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state='RUNNING' AND commit_point_reached=0`, marshalJobProgress(stage),
		instant, instant, job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, job.ID)
}

func (job BackupJobRecord) Projection() *tammyv1.BackupJob {
	projection := &tammyv1.BackupJob{Id: job.ID, Version: job.Version, OperationKey: job.OperationKey,
		State: job.State, Progress: proto.Clone(job.Progress).(*tammyv1.JobProgress), CreatedAt: timestamppb.New(job.CreatedAt)}
	if job.CompletedAt != nil {
		projection.CompletedAt = timestamppb.New(*job.CompletedAt)
	}
	if job.Result != nil {
		projection.ManifestHash = append([]byte(nil), job.Result.ManifestHash...)
	}
	return projection
}

const backupJobSelect = `SELECT id,version,operation_key,semantic_sha256,payload_proto,state,progress_proto,
	result_proto,commit_point_reached,attempt_count,created_at,updated_at,completed_at FROM jobs`

type backupJobBlobMetadata struct {
	ID           string
	Version      uint64
	State        string
	SemanticHash string
	Payload      int64
	Progress     int64
	Result       int64
}

func loadBackupJobBlobMetadata(
	ctx context.Context,
	executor SQLExecutor,
	predicate string,
	arguments ...any,
) (backupJobBlobMetadata, error) {
	rows, err := executor.QueryContext(ctx, `SELECT id,version,state,semantic_sha256,length(payload_proto),COALESCE(length(progress_proto),0),
		COALESCE(length(result_proto),0) FROM jobs WHERE `+predicate, arguments...)
	if err != nil {
		return backupJobBlobMetadata{}, ErrBackupJob
	}
	defer rows.Close()
	var metadata backupJobBlobMetadata
	if !rows.Next() || rows.Scan(&metadata.ID, &metadata.Version, &metadata.State, &metadata.SemanticHash,
		&metadata.Payload, &metadata.Progress, &metadata.Result) != nil ||
		rows.Next() || rows.Err() != nil || !validBackupJobBlobMetadata(metadata) {
		return backupJobBlobMetadata{}, ErrBackupJob
	}
	return metadata, nil
}

func validBackupJobBlobMetadata(metadata backupJobBlobMetadata) bool {
	decodedHash, err := hex.DecodeString(metadata.SemanticHash)
	return err == nil && len(decodedHash) == sha256.Size && ids.IsCanonicalV7(metadata.ID) && metadata.Version > 0 &&
		parseBackupJobState(metadata.State) != tammyv1.BackupJobState_BACKUP_JOB_STATE_UNSPECIFIED &&
		metadata.Payload > 0 && metadata.Payload <= maximumBackupJobPayloadBytes &&
		metadata.Progress > 0 && metadata.Progress <= maximumBackupJobProgressBytes &&
		metadata.Result >= 0 && metadata.Result <= maximumBackupJobResultBytes
}

func loadBoundedBackupJob(
	ctx context.Context,
	executor SQLExecutor,
	predicate string,
	metadata backupJobBlobMetadata,
	arguments ...any,
) (BackupJobRecord, error) {
	if !validBackupJobBlobMetadata(metadata) {
		return BackupJobRecord{}, ErrBackupJob
	}
	boundedArguments := append([]any(nil), arguments...)
	boundedArguments = append(boundedArguments, metadata.ID, metadata.Version, metadata.State, metadata.SemanticHash,
		metadata.Payload, metadata.Progress, metadata.Result)
	rows, err := executor.QueryContext(ctx, backupJobSelect+` WHERE `+predicate+`
		AND id=? AND version=? AND state=? AND semantic_sha256=?
		AND length(payload_proto)=? AND COALESCE(length(progress_proto),0)=?
		AND COALESCE(length(result_proto),0)=?`, boundedArguments...)
	if err != nil {
		return BackupJobRecord{}, ErrBackupJob
	}
	defer rows.Close()
	if !rows.Next() {
		return BackupJobRecord{}, ErrBackupJob
	}
	job, err := scanBackupJob(rows)
	if err != nil || rows.Next() || rows.Err() != nil || job.ID != metadata.ID {
		return BackupJobRecord{}, ErrBackupJob
	}
	return job, nil
}

func scanBackupJob(scanner interface{ Scan(...any) error }) (BackupJobRecord, error) {
	var job BackupJobRecord
	var operationHashHex, state, created, updated string
	var payload, progress, result []byte
	var completed sql.NullString
	var commitPoint int
	if err := scanner.Scan(&job.ID, &job.Version, &job.OperationKey, &operationHashHex, &payload, &state, &progress,
		&result, &commitPoint, &job.Attempt, &created, &updated, &completed); err != nil {
		return BackupJobRecord{}, ErrBackupJob
	}
	if len(payload) == 0 || len(payload) > maximumBackupJobPayloadBytes || len(progress) == 0 ||
		len(progress) > maximumBackupJobProgressBytes || len(result) > maximumBackupJobResultBytes {
		return BackupJobRecord{}, ErrBackupJob
	}
	operationHash, err := hex.DecodeString(operationHashHex)
	job.Input = &tammyv1.BackupJobInput{}
	job.Progress = &tammyv1.JobProgress{}
	if err != nil || len(operationHash) != sha256.Size ||
		(proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, job.Input) != nil ||
		(proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(progress, job.Progress) != nil ||
		len(job.Input.ProtoReflect().GetUnknown()) != 0 || len(job.Progress.ProtoReflect().GetUnknown()) != 0 {
		return BackupJobRecord{}, ErrBackupJob
	}
	canonicalPayload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(job.Input)
	canonicalProgress, _ := proto.MarshalOptions{Deterministic: true}.Marshal(job.Progress)
	wantInputHash := backupJobInputHash(job.Input.WorkspaceId, job.Input.DestinationCapability)
	wantOperationHash := backupJobOperationHash(job.OperationKey, wantInputHash)
	if !bytes.Equal(payload, canonicalPayload) || !bytes.Equal(progress, canonicalProgress) ||
		job.Input.Format != "tammy-backup-job-v1" || !ids.IsCanonicalV7(job.ID) ||
		!ids.IsCanonicalV7(job.OperationKey) || !ids.IsCanonicalV7(job.Input.WorkspaceId) ||
		!ids.IsCanonicalV7(job.Input.DestinationCapability) || !ids.IsCanonicalV7(job.Input.PassphraseCapability) ||
		len(job.Input.OperationHash) != sha256.Size || len(job.Input.InputHash) != sha256.Size ||
		!bytes.Equal(job.Input.InputHash, wantInputHash[:]) || !bytes.Equal(job.Input.OperationHash, wantOperationHash[:]) ||
		!bytes.Equal(operationHash, wantOperationHash[:]) || job.Progress.Stage == "" || len(job.Progress.Stage) > 64 {
		return BackupJobRecord{}, ErrBackupJob
	}
	job.OperationHash = operationHash
	job.InputProto = append([]byte(nil), payload...)
	job.State = parseBackupJobState(state)
	job.CommitPointReached = commitPoint == 1
	job.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	if err == nil && completed.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, completed.String)
		err = parseErr
		job.CompletedAt = &value
	}
	if err != nil || job.Version == 0 || job.State == tammyv1.BackupJobState_BACKUP_JOB_STATE_UNSPECIFIED ||
		commitPoint < 0 || commitPoint > 1 {
		return BackupJobRecord{}, ErrBackupJob
	}
	if len(result) != 0 {
		job.Result = &tammyv1.BackupJobResult{}
		if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(result, job.Result) != nil ||
			len(job.Result.ProtoReflect().GetUnknown()) != 0 || job.Result.Format != "tammy-backup-job-result-v1" ||
			len(job.Result.ManifestHash) != sha256.Size || len(job.Result.DestinationHash) != sha256.Size ||
			job.Result.DestinationCapability != job.Input.DestinationCapability {
			return BackupJobRecord{}, ErrBackupJob
		}
		canonical, _ := proto.MarshalOptions{Deterministic: true}.Marshal(job.Result)
		if !bytes.Equal(result, canonical) {
			return BackupJobRecord{}, ErrBackupJob
		}
	}
	if !validBackupJobPersistedState(job) {
		return BackupJobRecord{}, ErrBackupJob
	}
	return job, nil
}

func validBackupJobPersistedState(job BackupJobRecord) bool {
	hasResult := job.Result != nil
	hasCompletedAt := job.CompletedAt != nil
	switch job.State {
	case tammyv1.BackupJobState_BACKUP_JOB_STATE_QUEUED:
		return !job.CommitPointReached && !hasResult && !hasCompletedAt && job.Attempt == 0
	case tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING:
		return !hasResult && !hasCompletedAt && job.Attempt > 0
	case tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_RETRYABLE:
		return !hasResult && !hasCompletedAt && job.Attempt > 0
	case tammyv1.BackupJobState_BACKUP_JOB_STATE_CANCELLED:
		return !job.CommitPointReached && !hasResult && hasCompletedAt
	case tammyv1.BackupJobState_BACKUP_JOB_STATE_COMPLETED:
		return job.CommitPointReached && hasResult && hasCompletedAt && job.Attempt > 0
	case tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_TERMINAL:
		return !hasResult && hasCompletedAt && job.Attempt > 0
	default:
		return false
	}
}

func parseBackupJobState(state string) tammyv1.BackupJobState {
	switch state {
	case "PENDING":
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_QUEUED
	case "RUNNING", "CANCEL_REQUESTED":
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING
	case "RETRY_WAIT":
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_RETRYABLE
	case "CANCELLED":
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_CANCELLED
	case "COMPLETED":
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_COMPLETED
	case "FAILED":
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_FAILED_TERMINAL
	default:
		return tammyv1.BackupJobState_BACKUP_JOB_STATE_UNSPECIFIED
	}
}

func marshalJobProgress(stage string) []byte {
	encoded, _ := proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.JobProgress{Stage: stage})
	return encoded
}

func exactlyOne(result sql.Result) error {
	if result == nil {
		return ErrBackupJobConflict
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return ErrBackupJobConflict
	}
	return nil
}

type BackupPassphraseProvider interface {
	WithPassphrase(context.Context, string, func([]byte) error) error
}

type PassphraseCapability struct {
	ID         string
	Passphrase []byte
	ExpiresAt  time.Time
}

type inMemoryPassphraseEntry struct {
	passphrase []byte
	expiresAt  time.Time
}

// InMemoryPassphraseCapabilities is immutable after construction and consumes
// each copied UUIDv7 capability exactly once.
type InMemoryPassphraseCapabilities struct {
	mu      sync.Mutex
	entries map[string]inMemoryPassphraseEntry
	now     func() time.Time
}

func NewInMemoryPassphraseCapabilities(
	capabilities []PassphraseCapability,
	now func() time.Time,
) (*InMemoryPassphraseCapabilities, error) {
	if len(capabilities) == 0 || len(capabilities) > 1024 || now == nil {
		return nil, ErrBackupJob
	}
	provider := &InMemoryPassphraseCapabilities{entries: make(map[string]inMemoryPassphraseEntry, len(capabilities)), now: now}
	for _, capability := range capabilities {
		if !ids.IsCanonicalV7(capability.ID) || len(capability.Passphrase) == 0 || len(capability.Passphrase) > 4096 ||
			capability.ExpiresAt.IsZero() || !capability.ExpiresAt.After(now()) {
			provider.Close()
			return nil, ErrBackupJob
		}
		if _, duplicate := provider.entries[capability.ID]; duplicate {
			provider.Close()
			return nil, ErrBackupJob
		}
		provider.entries[capability.ID] = inMemoryPassphraseEntry{passphrase: append([]byte(nil), capability.Passphrase...),
			expiresAt: capability.ExpiresAt.UTC()}
	}
	return provider, nil
}

func (provider *InMemoryPassphraseCapabilities) WithPassphrase(
	ctx context.Context,
	capabilityID string,
	callback func([]byte) error,
) error {
	if provider == nil || ctx == nil || !ids.IsCanonicalV7(capabilityID) || callback == nil {
		return ErrBackupJob
	}
	provider.mu.Lock()
	entry, exists := provider.entries[capabilityID]
	delete(provider.entries, capabilityID)
	provider.mu.Unlock()
	if !exists {
		return ErrBackupJob
	}
	defer zero(entry.passphrase)
	if !provider.now().Before(entry.expiresAt) {
		return ErrBackupJob
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return errors.Join(ErrBackupJob, contextErr)
	}
	working := append([]byte(nil), entry.passphrase...)
	defer zero(working)
	if err := callback(working); err != nil {
		return errors.Join(ErrBackupJob, err)
	}
	return nil
}

func (provider *InMemoryPassphraseCapabilities) Close() {
	if provider == nil {
		return
	}
	provider.mu.Lock()
	for key, entry := range provider.entries {
		zero(entry.passphrase)
		delete(provider.entries, key)
	}
	provider.mu.Unlock()
}

type BackupJobWorkerConfig struct {
	Transactions BackupJobTransactions
	Backups      *Service
	Passphrases  BackupPassphraseProvider
	Now          func() time.Time
}

type backupJobWorkerHooks struct {
	afterPrepared func() error
}

type BackupJobWorker struct {
	transactions BackupJobTransactions
	backups      *Service
	passphrases  BackupPassphraseProvider
	now          func() time.Time
	hooks        *backupJobWorkerHooks
}

func NewBackupJobWorker(config BackupJobWorkerConfig) (*BackupJobWorker, error) {
	if nilInterface(config.Transactions) || config.Backups == nil || nilInterface(config.Passphrases) || config.Now == nil {
		return nil, ErrBackupJob
	}
	return &BackupJobWorker{transactions: config.Transactions, backups: config.Backups,
		passphrases: config.Passphrases, now: config.Now}, nil
}

func (worker *BackupJobWorker) Run(ctx context.Context, jobID string) (*tammyv1.BackupJob, error) {
	if worker == nil || ctx == nil || !ids.IsCanonicalV7(jobID) {
		return nil, ErrBackupJob
	}
	var job BackupJobRecord
	if err := worker.transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var claimErr error
		job, claimErr = claimBackupJob(ctx, executor, jobID, worker.now())
		return claimErr
	}); err != nil {
		return nil, errors.Join(ErrBackupJob, err)
	}
	var prepared *preparedBackup
	callbackCalls := 0
	err := worker.passphrases.WithPassphrase(ctx, job.Input.PassphraseCapability, func(passphrase []byte) error {
		callbackCalls++
		if callbackCalls != 1 || len(passphrase) == 0 || ctx.Err() != nil {
			return ErrBackupJob
		}
		owned := append([]byte(nil), passphrase...)
		defer zero(owned)
		var prepareErr error
		prepared, prepareErr = worker.backups.prepare(ctx, CreateRequest{WorkspaceID: job.Input.WorkspaceId,
			DestinationCapability: job.Input.DestinationCapability, Passphrase: owned})
		return prepareErr
	})
	if err != nil || callbackCalls != 1 || prepared == nil {
		var failed BackupJobRecord
		_ = worker.transactions.Mutate(context.WithoutCancel(ctx), func(executor SQLExecutor) error {
			var failErr error
			failed, failErr = failBackupJob(context.WithoutCancel(ctx), executor, job, "SECRET_REAUTHORIZATION_UNAVAILABLE", worker.now())
			return failErr
		})
		if failed.ID != "" {
			return failed.Projection(), ErrBackupJob
		}
		return nil, ErrBackupJob
	}
	defer prepared.close()
	if err := worker.transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var persistErr error
		job, persistErr = persistPreparedBackupJob(ctx, executor, job, prepared, worker.now())
		return persistErr
	}); err != nil {
		return nil, err
	}
	if worker.hooks != nil && worker.hooks.afterPrepared != nil {
		if err := worker.hooks.afterPrepared(); err != nil {
			return nil, err
		}
	}
	shouldPublish := false
	if err := worker.transactions.Mutate(ctx, func(executor SQLExecutor) error {
		var beginErr error
		job, shouldPublish, beginErr = beginBackupPublish(ctx, executor, job.ID, worker.now())
		return beginErr
	}); err != nil {
		return nil, err
	}
	if !shouldPublish {
		return job.Projection(), nil
	}
	result, err := worker.backups.publish(context.WithoutCancel(ctx), prepared)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(result.ManifestHash, prepared.manifestHash) || !bytes.Equal(result.DestinationHash, prepared.archiveHash[:]) {
		return nil, ErrBackupJob
	}
	if err := worker.transactions.Mutate(context.WithoutCancel(ctx), func(executor SQLExecutor) error {
		var completeErr error
		job, completeErr = completeBackupJob(context.WithoutCancel(ctx), executor, job, result, worker.now())
		return completeErr
	}); err != nil {
		return nil, err
	}
	return job.Projection(), nil
}

// ReconstructBackupJobs fails closed on work that lost its transient one-use
// passphrase capability. Publish-fenced recovery is handled separately from
// this pre-commit branch and never rebuilds with invented credentials.
func ReconstructBackupJobs(
	ctx context.Context,
	transactions BackupJobTransactions,
	destinations DestinationResolver,
	now time.Time,
) ([]*tammyv1.BackupJob, error) {
	if ctx == nil || nilInterface(transactions) || nilInterface(destinations) || now.IsZero() {
		return nil, ErrBackupJob
	}
	running := make([]BackupJobRecord, 0, 256)
	if err := transactions.Read(ctx, func(executor SQLExecutor) error {
		metadataRecords := make([]backupJobBlobMetadata, 0, 256)
		rows, err := executor.QueryContext(ctx, `SELECT id,version,state,semantic_sha256,length(payload_proto),COALESCE(length(progress_proto),0),
			COALESCE(length(result_proto),0) FROM jobs
			WHERE kind=? AND state='RUNNING' ORDER BY created_at,id LIMIT 256`, backupJobKind)
		if err != nil {
			return ErrBackupJob
		}
		defer rows.Close()
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrBackupJob, err)
			}
			var metadata backupJobBlobMetadata
			if scanErr := rows.Scan(&metadata.ID, &metadata.Version, &metadata.State, &metadata.SemanticHash,
				&metadata.Payload, &metadata.Progress, &metadata.Result); scanErr != nil ||
				!validBackupJobBlobMetadata(metadata) {
				return ErrBackupJob
			}
			metadataRecords = append(metadataRecords, metadata)
		}
		if err := rows.Err(); err != nil {
			return errors.Join(ErrBackupJob, err)
		}
		if err := rows.Close(); err != nil {
			return ErrBackupJob
		}
		for _, metadata := range metadataRecords {
			loaded, loadErr := loadBoundedBackupJob(ctx, executor, `id=? AND kind=?`, metadata, metadata.ID, backupJobKind)
			if loadErr != nil {
				return loadErr
			}
			running = append(running, loaded)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	reconstructed := make([]*tammyv1.BackupJob, 0, len(running))
	for _, runningJob := range running {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrBackupJob, err)
		}
		if runningJob.CommitPointReached {
			var checkpoint *tammyv1.BackupJobCheckpoint
			if err := transactions.Read(ctx, func(executor SQLExecutor) error {
				var loadErr error
				checkpoint, loadErr = loadBackupJobCheckpoint(ctx, executor, runningJob.ID)
				return loadErr
			}); err != nil {
				var terminal BackupJobRecord
				if transitionErr := transactions.Mutate(ctx, func(executor SQLExecutor) error {
					var failErr error
					terminal, failErr = failBackupRecoveryState(ctx, executor, runningJob, now)
					return failErr
				}); transitionErr != nil {
					return nil, transitionErr
				}
				reconstructed = append(reconstructed, terminal.Projection())
				continue
			}
			destination, resolveErr := destinations.Resolve(runningJob.Input.DestinationCapability)
			var committed []byte
			var readErr error
			if resolveErr == nil && !nilInterface(destination) && destination.Reference() == runningJob.Input.DestinationCapability {
				committed, readErr = destination.ReadCommitted(ctx)
			} else {
				readErr = ErrBackupJob
			}
			destinationHash := sha256.Sum256(committed)
			matches := readErr == nil && bytes.Equal(destinationHash[:], checkpoint.ArchiveHash)
			zero(committed)
			if !matches {
				var terminal BackupJobRecord
				if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
					var transitionErr error
					terminal, transitionErr = terminalizeBackupDestinationRecovery(ctx, executor, runningJob, now)
					return transitionErr
				}); err != nil {
					return nil, err
				}
				reconstructed = append(reconstructed, terminal.Projection())
				continue
			}
			var completed BackupJobRecord
			if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
				var completeErr error
				completed, completeErr = completeBackupJob(ctx, executor, runningJob, CreateResult{
					ManifestHash:    append([]byte(nil), checkpoint.ManifestHash...),
					DestinationHash: append([]byte(nil), destinationHash[:]...)}, now)
				return completeErr
			}); err != nil {
				return nil, err
			}
			reconstructed = append(reconstructed, completed.Projection())
			continue
		}
		var updated BackupJobRecord
		if err := transactions.Mutate(ctx, func(executor SQLExecutor) error {
			var updateErr error
			updated, updateErr = failBackupJob(ctx, executor, runningJob, "SECRET_REAUTHORIZATION_UNAVAILABLE", now)
			return updateErr
		}); err != nil {
			return nil, err
		}
		reconstructed = append(reconstructed, updated.Projection())
	}
	return reconstructed, nil
}

func failBackupRecoveryState(
	ctx context.Context,
	executor SQLExecutor,
	job BackupJobRecord,
	now time.Time,
) (BackupJobRecord, error) {
	if ctx == nil || nilInterface(executor) || job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING ||
		!job.CommitPointReached || now.IsZero() {
		return BackupJobRecord{}, ErrBackupJob
	}
	instant := now.UTC().Format(time.RFC3339Nano)
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET state='FAILED',progress_proto=?,completed_at=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state='RUNNING' AND commit_point_reached=1`,
		marshalJobProgress("RECOVERY_STATE_INVALID"), instant, instant, job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, job.ID)
}

func terminalizeBackupDestinationRecovery(
	ctx context.Context,
	executor SQLExecutor,
	job BackupJobRecord,
	now time.Time,
) (BackupJobRecord, error) {
	if ctx == nil || nilInterface(executor) || job.State != tammyv1.BackupJobState_BACKUP_JOB_STATE_RUNNING ||
		!job.CommitPointReached || now.IsZero() {
		return BackupJobRecord{}, ErrBackupJob
	}
	instant := now.UTC().Format(time.RFC3339Nano)
	result, err := executor.ExecContext(ctx, `UPDATE jobs SET state='FAILED',progress_proto=?,completed_at=?,updated_at=?,version=version+1
		WHERE id=? AND kind=? AND version=? AND state='RUNNING' AND commit_point_reached=1`,
		marshalJobProgress("DESTINATION_REAUTHORIZATION_UNAVAILABLE"), instant, instant,
		job.ID, backupJobKind, job.Version)
	if err != nil || exactlyOne(result) != nil {
		return BackupJobRecord{}, ErrBackupJobConflict
	}
	return LoadBackupJob(ctx, executor, job.ID)
}

func loadBackupJobCheckpoint(ctx context.Context, executor SQLExecutor, jobID string) (*tammyv1.BackupJobCheckpoint, error) {
	if ctx == nil || nilInterface(executor) || !ids.IsCanonicalV7(jobID) {
		return nil, ErrBackupJob
	}
	metadataRows, err := executor.QueryContext(ctx, `SELECT sequence,length(checkpoint_proto),checkpoint_sha256
		FROM job_checkpoints WHERE job_id=? ORDER BY sequence`, jobID)
	if err != nil {
		return nil, ErrBackupJob
	}
	defer metadataRows.Close()
	if !metadataRows.Next() {
		return nil, ErrBackupJob
	}
	var sequence uint64
	var encodedLength int64
	var expectedHash string
	if metadataRows.Scan(&sequence, &encodedLength, &expectedHash) != nil || sequence != 1 ||
		encodedLength <= 0 || encodedLength > maximumBackupCheckpointBytes || len(expectedHash) != 64 ||
		metadataRows.Next() || metadataRows.Err() != nil {
		return nil, ErrBackupJob
	}
	decodedHash, err := hex.DecodeString(expectedHash)
	if err != nil || len(decodedHash) != sha256.Size {
		return nil, ErrBackupJob
	}
	if err := metadataRows.Close(); err != nil {
		return nil, ErrBackupJob
	}
	rows, err := executor.QueryContext(ctx, `SELECT sequence,checkpoint_proto,checkpoint_sha256
		FROM job_checkpoints WHERE job_id=? AND sequence=? AND length(checkpoint_proto)=?
		AND checkpoint_sha256=? AND length(checkpoint_sha256)=64`, jobID, sequence, encodedLength, expectedHash)
	if err != nil {
		return nil, ErrBackupJob
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrBackupJob
	}
	var encoded []byte
	if rows.Scan(&sequence, &encoded, &expectedHash) != nil || sequence != 1 || len(encoded) != int(encodedLength) ||
		rows.Next() || rows.Err() != nil {
		return nil, ErrBackupJob
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != expectedHash {
		return nil, ErrBackupJob
	}
	checkpoint := &tammyv1.BackupJobCheckpoint{}
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(encoded, checkpoint) != nil ||
		len(checkpoint.ProtoReflect().GetUnknown()) != 0 || checkpoint.Format != "tammy-backup-job-checkpoint-v1" ||
		len(checkpoint.ManifestHash) != sha256.Size || len(checkpoint.ArchiveHash) != sha256.Size {
		return nil, ErrBackupJob
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(checkpoint)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return nil, ErrBackupJob
	}
	return checkpoint, nil
}
