package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"strconv"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

var (
	ErrPreRestoreExportJob         = errors.New("restore: pre-restore export job failed")
	ErrPreRestoreExportJobConflict = errors.New("restore: pre-restore export job conflict")
)

type preRestoreExportHooks struct {
	beforeAtomicCommitFence func(preRestoreExportJobRecord) error
	afterCommitPoint        func(preRestoreExportJobRecord) error
	afterDestinationRename  func() error
}

type preRestoreExportJobRecord struct {
	ID                    string
	WorkspaceID           string
	ArchiveID             string
	ArchiveVersion        uint64
	OperationKey          string
	RetryOperationKey     string
	Version               uint64
	State                 tammyv1.PreRestoreArchiveExportJobState
	InputHash             []byte
	DestinationCapability string
	DestinationHash       []byte
	Progress              *tammyv1.JobProgress
	CommitPointReached    bool
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

func (service *PreRestoreArchiveCommandService) ExportPreRestoreArchive(
	ctx context.Context,
	request *tammyv1.ExportPreRestoreArchiveRequest,
) (*tammyv1.ExportPreRestoreArchiveResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || request.AdministratorPassword == nil || request.Destination == nil ||
		nilInterface(service.config.Transactions) || nilInterface(service.config.Archives) ||
		nilInterface(service.config.Destinations) || service.config.NewJobID == nil || service.config.Now == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	password := append([]byte(nil), request.AdministratorPassword.Utf8...)
	defer zeroBytes(password)
	if err := service.authorizeMutation(ctx, request.CommandContext, password, "export_pre_restore_archive"); err != nil {
		return nil, err
	}
	now := service.config.Now().UTC()
	jobID, err := service.config.NewJobID()
	if err != nil || !ids.IsCanonicalV7(jobID) {
		return nil, ErrPreRestoreArchiveCommand
	}
	inputHash := preRestoreExportInputHash(service.workspaceID, request.ArchiveId, request.ExpectedVersion,
		request.Destination.CapabilityId)
	var job preRestoreExportJobRecord
	err = service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		archive, loadErr := loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, request.ArchiveId)
		if loadErr != nil || archive.Version != request.ExpectedVersion ||
			archive.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE {
			return ErrPreRestoreExportJobConflict
		}
		progress, progressErr := marshalPreRestoreProgress("QUEUED")
		if progressErr != nil {
			return progressErr
		}
		instant := formatPreRestoreTime(now)
		_, insertErr := executor.ExecContext(ctx, `INSERT INTO pre_restore_archive_export_jobs_v1(
			job_id,workspace_id,archive_id,archive_version,operation_key,version,state,input_hash,destination_capability,
			progress_proto,commit_point_reached,created_at,updated_at) VALUES(?,?,?,?,?,1,1,?,?,?,0,?,?)`,
			jobID, service.workspaceID, request.ArchiveId, archive.Version, request.CommandContext.IdempotencyKey, inputHash[:],
			request.Destination.CapabilityId, progress, instant, instant)
		if insertErr != nil {
			existing, existingErr := loadPreRestoreExportJobByOperation(ctx, executor, request.CommandContext.IdempotencyKey)
			if existingErr == nil && bytes.Equal(existing.InputHash, inputHash[:]) {
				job = existing
				return nil
			}
			return ErrPreRestoreExportJobConflict
		}
		job, insertErr = loadPreRestoreExportJob(ctx, executor, jobID)
		return insertErr
	})
	if err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	return &tammyv1.ExportPreRestoreArchiveResponse{Job: projectPreRestoreExportJob(job)}, nil
}

func (service *PreRestoreArchiveCommandService) RetryPreRestoreArchiveExport(
	ctx context.Context,
	request *tammyv1.RetryPreRestoreArchiveExportRequest,
) (*tammyv1.RetryPreRestoreArchiveExportResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || request.AdministratorPassword == nil || request.Destination == nil ||
		nilInterface(service.config.Transactions) || service.config.Now == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	password := append([]byte(nil), request.AdministratorPassword.Utf8...)
	defer zeroBytes(password)
	if err := service.authorizeMutation(ctx, request.CommandContext, password, "retry_pre_restore_archive_export"); err != nil {
		return nil, err
	}
	var job preRestoreExportJobRecord
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreExportJob(ctx, executor, request.JobId)
		if err != nil {
			return ErrPreRestoreExportJobConflict
		}
		inputHash := preRestoreRetryInputHash(service.workspaceID, loaded.ID, loaded.ArchiveID,
			loaded.ArchiveVersion, request.ExpectedVersion, request.Destination.CapabilityId)
		if loaded.RetryOperationKey == request.CommandContext.IdempotencyKey {
			if bytes.Equal(loaded.InputHash, inputHash[:]) {
				job = loaded
				return nil
			}
			return ErrPreRestoreExportJobConflict
		}
		if loaded.Version != request.ExpectedVersion || loaded.CommitPointReached ||
			loaded.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE ||
			loaded.DestinationCapability == request.Destination.CapabilityId {
			return ErrPreRestoreExportJobConflict
		}
		archive, archiveErr := loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, loaded.ArchiveID)
		if archiveErr != nil || archive.Version != loaded.ArchiveVersion ||
			archive.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE {
			return ErrPreRestoreExportJobConflict
		}
		progress, progressErr := marshalPreRestoreProgress("QUEUED")
		if progressErr != nil {
			return progressErr
		}
		result, updateErr := executor.ExecContext(ctx, `UPDATE pre_restore_archive_export_jobs_v1 SET
			version=version+1,state=?,input_hash=?,destination_capability=?,retry_operation_key=?,destination_hash=NULL,
			progress_proto=?,commit_point_reached=0,updated_at=?,completed_at=NULL
			WHERE job_id=? AND workspace_id=? AND version=? AND state=? AND commit_point_reached=0`,
			int32(tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED), inputHash[:],
			request.Destination.CapabilityId, request.CommandContext.IdempotencyKey, progress,
			formatPreRestoreTime(service.config.Now().UTC()), loaded.ID, service.workspaceID, loaded.Version,
			int32(tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE))
		if updateErr != nil || exactlyOnePreRestore(result) != nil {
			return ErrPreRestoreExportJobConflict
		}
		job, updateErr = loadPreRestoreExportJob(ctx, executor, loaded.ID)
		return updateErr
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	return &tammyv1.RetryPreRestoreArchiveExportResponse{Job: projectPreRestoreExportJob(job)}, nil
}

func (service *PreRestoreArchiveCommandService) RunPreRestoreArchiveExport(
	ctx context.Context,
	jobID string,
) (*tammyv1.PreRestoreArchiveExportJob, error) {
	if service == nil || ctx == nil || !ids.IsCanonicalV7(jobID) || nilInterface(service.config.Transactions) ||
		nilInterface(service.config.Archives) || nilInterface(service.config.Destinations) || service.config.Now == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreExportJob
	}
	var job preRestoreExportJobRecord
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreExportJob(ctx, executor, jobID)
		if err != nil || loaded.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED ||
			loaded.CommitPointReached {
			return ErrPreRestoreExportJobConflict
		}
		job, err = transitionPreRestoreExportJob(ctx, executor, loaded,
			tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING, "WRITING", nil, false,
			service.config.Now().UTC())
		return err
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	var archive PreRestoreArchiveRecord
	if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
		var err error
		archive, err = loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, job.ArchiveID)
		return err
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	encrypted, err := service.config.Archives.ReadEncryptedPreRestoreArchive(ctx, service.workspaceID,
		archive.ArchiveID, archive.ContentHash)
	if err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	defer zeroBytes(encrypted)
	destination, err := service.config.Destinations.Resolve(job.DestinationCapability)
	if err != nil || nilInterface(destination) || destination.Reference() != job.DestinationCapability {
		return nil, ErrPreRestoreExportJob
	}
	if service.config.hooks != nil && service.config.hooks.beforeAtomicCommitFence != nil {
		if err := service.config.hooks.beforeAtomicCommitFence(job); err != nil {
			return nil, errors.Join(ErrPreRestoreExportJob, err)
		}
	}
	cancelled := false
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreExportJob(ctx, executor, job.ID)
		if err != nil {
			return ErrPreRestoreExportJobConflict
		}
		if loaded.State == tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED {
			job = loaded
			cancelled = true
			return nil
		}
		if loaded.Version != job.Version || loaded.State != job.State || loaded.CommitPointReached {
			return ErrPreRestoreExportJobConflict
		}
		job, err = transitionPreRestoreExportJob(ctx, executor, loaded,
			tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING,
			"COMMITTING", nil, true, service.config.Now().UTC())
		return err
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	if cancelled {
		return projectPreRestoreExportJob(job), nil
	}
	if service.config.hooks != nil && service.config.hooks.afterCommitPoint != nil {
		if err := service.config.hooks.afterCommitPoint(job); err != nil {
			return nil, errors.Join(ErrPreRestoreExportJob, err)
		}
	}
	if err := destination.AtomicCommit(ctx, encrypted); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	if service.config.hooks != nil && service.config.hooks.afterDestinationRename != nil {
		if err := service.config.hooks.afterDestinationRename(); err != nil {
			return nil, errors.Join(ErrPreRestoreExportJob, err)
		}
	}
	committed, err := destination.ReadCommitted(ctx)
	if err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	defer zeroBytes(committed)
	destinationHash := sha256.Sum256(committed)
	if !bytes.Equal(committed, encrypted) || !bytes.Equal(destinationHash[:], archive.ContentHash) {
		return nil, ErrPreRestoreExportJob
	}
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreExportJob(ctx, executor, job.ID)
		if err != nil || loaded.Version != job.Version || loaded.State != job.State {
			return ErrPreRestoreExportJobConflict
		}
		job, err = transitionPreRestoreExportJob(ctx, executor, loaded,
			tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED,
			"VERIFIED", destinationHash[:], true, service.config.Now().UTC())
		return err
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreExportJob(ctx, executor, job.ID)
		if err != nil || loaded.Version != job.Version || loaded.State != job.State {
			return ErrPreRestoreExportJobConflict
		}
		job, err = transitionPreRestoreExportJob(ctx, executor, loaded,
			tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED,
			"COMPLETED", destinationHash[:], true, service.config.Now().UTC())
		return err
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	return projectPreRestoreExportJob(job), nil
}

func (service *PreRestoreArchiveCommandService) CancelPreRestoreArchiveExport(
	ctx context.Context,
	request *tammyv1.CancelPreRestoreArchiveExportRequest,
) (*tammyv1.CancelPreRestoreArchiveExportResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || request.CommandContext == nil ||
		nilInterface(service.config.Transactions) || service.config.Now == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	if err := service.authorizeRead(ctx, request.CommandContext.Authentication); err != nil {
		return nil, err
	}
	var job preRestoreExportJobRecord
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreExportJob(ctx, executor, request.JobId)
		if err != nil || loaded.Version != request.ExpectedVersion || loaded.CommitPointReached ||
			(loaded.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED &&
				loaded.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING &&
				loaded.State != tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE) {
			return ErrPreRestoreExportJobConflict
		}
		job, err = transitionPreRestoreExportJob(ctx, executor, loaded,
			tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED,
			"CANCELLED", nil, false, service.config.Now().UTC())
		return err
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	return &tammyv1.CancelPreRestoreArchiveExportResponse{Job: projectPreRestoreExportJob(job)}, nil
}

func (service *PreRestoreArchiveCommandService) GetPreRestoreArchiveExportJob(
	ctx context.Context,
	request *tammyv1.GetPreRestoreArchiveExportJobRequest,
) (*tammyv1.GetPreRestoreArchiveExportJobResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || nilInterface(service.config.Transactions) || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	if err := service.authorizeRead(ctx, request.Authentication); err != nil {
		return nil, err
	}
	var job preRestoreExportJobRecord
	if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
		var loadErr error
		job, loadErr = loadPreRestoreExportJob(ctx, executor, request.JobId)
		if loadErr != nil || job.WorkspaceID != service.workspaceID {
			return ErrPreRestoreExportJob
		}
		return nil
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	return &tammyv1.GetPreRestoreArchiveExportJobResponse{Job: projectPreRestoreExportJob(job)}, nil
}

func (service *PreRestoreArchiveCommandService) ListPreRestoreArchiveExportJobs(
	ctx context.Context,
	request *tammyv1.ListPreRestoreArchiveExportJobsRequest,
) (*tammyv1.ListPreRestoreArchiveExportJobsResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || request.Page == nil || nilInterface(service.config.Transactions) ||
		ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	if err := service.authorizeRead(ctx, request.Authentication); err != nil {
		return nil, err
	}
	state := tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_UNSPECIFIED
	if request.State != nil {
		state = *request.State
	}
	if state < tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_UNSPECIFIED ||
		state > tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE {
		return nil, ErrPreRestoreArchiveCommand
	}
	pageSize := request.Page.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		return nil, ErrPreRestoreArchiveCommand
	}
	queryHash := preRestoreExportListQueryHash(service.workspaceID, state)
	snapshot, position := "", ""
	if request.Page.Cursor != nil {
		cursor, err := service.repository.cursors.Decode(*request.Page.Cursor, queryHash)
		if err != nil {
			return nil, ErrPreRestoreArchiveCommand
		}
		snapshot, position = cursor.Snapshot, cursor.Position
	} else {
		if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
			rows, err := executor.QueryContext(ctx, `SELECT created_at,job_id FROM pre_restore_archive_export_jobs_v1
				WHERE workspace_id=? AND (?=0 OR state=?) ORDER BY created_at DESC,job_id DESC LIMIT 1`,
				service.workspaceID, int32(state), int32(state))
			if err != nil {
				return ErrPreRestoreExportJob
			}
			defer rows.Close()
			if !rows.Next() {
				return rows.Err()
			}
			var created, jobID string
			if rows.Scan(&created, &jobID) != nil || rows.Next() || rows.Err() != nil {
				return ErrPreRestoreExportJob
			}
			snapshot = created + "|" + jobID
			return nil
		}); err != nil {
			return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
		}
		if snapshot == "" {
			return &tammyv1.ListPreRestoreArchiveExportJobsResponse{Page: &tammyv1.PageInfo{}}, nil
		}
		position = "!|!"
	}
	snapshotCreated, snapshotID, ok := splitPreRestoreCursorComponent(snapshot)
	if !ok {
		return nil, ErrPreRestoreArchiveCommand
	}
	positionCreated, positionID := "", ""
	if position != "!|!" {
		positionCreated, positionID, ok = splitPreRestoreCursorComponent(position)
		if !ok {
			return nil, ErrPreRestoreArchiveCommand
		}
	}
	jobs := make([]preRestoreExportJobRecord, 0, pageSize+1)
	if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
		rows, err := executor.QueryContext(ctx, preRestoreExportSelect+` WHERE workspace_id=? AND (?=0 OR state=?)
			AND (created_at < ? OR (created_at=? AND job_id<=?))
			AND (?='' OR created_at > ? OR (created_at=? AND job_id>?)) ORDER BY created_at,job_id LIMIT ?`,
			service.workspaceID, int32(state), int32(state), snapshotCreated, snapshotCreated, snapshotID,
			positionCreated, positionCreated, positionCreated, positionID, pageSize+1)
		if err != nil {
			return ErrPreRestoreExportJob
		}
		defer rows.Close()
		for rows.Next() {
			job, scanErr := scanPreRestoreExportJob(rows)
			if scanErr != nil {
				return scanErr
			}
			jobs = append(jobs, job)
		}
		return rows.Err()
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	hasMore := len(jobs) > int(pageSize)
	if hasMore {
		jobs = jobs[:pageSize]
	}
	response := &tammyv1.ListPreRestoreArchiveExportJobsResponse{Jobs: make([]*tammyv1.PreRestoreArchiveExportJob, len(jobs)),
		Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(jobs))}}
	for index := range jobs {
		response.Jobs[index] = projectPreRestoreExportJob(jobs[index])
	}
	if hasMore && len(jobs) != 0 {
		last := jobs[len(jobs)-1]
		token, err := service.repository.cursors.Encode(paging.Cursor{Snapshot: snapshot,
			Position: formatPreRestoreTime(last.CreatedAt) + "|" + last.ID, QueryHash: queryHash})
		if err != nil {
			return nil, ErrPreRestoreArchiveCommand
		}
		response.Page.NextCursor = &token
	}
	return response, nil
}

func (service *PreRestoreArchiveCommandService) RecoverPreRestoreArchiveExports(
	ctx context.Context,
) ([]*tammyv1.PreRestoreArchiveExportJob, error) {
	if service == nil || ctx == nil || nilInterface(service.config.Transactions) ||
		nilInterface(service.config.Destinations) || service.config.Now == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreExportJob
	}
	pending := make([]preRestoreExportJobRecord, 0, 256)
	if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
		rows, err := executor.QueryContext(ctx, preRestoreExportSelect+`
			WHERE workspace_id=? AND state IN (?,?) ORDER BY created_at,job_id LIMIT 256`,
			service.workspaceID,
			int32(tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING),
			int32(tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED))
		if err != nil {
			return ErrPreRestoreExportJob
		}
		defer rows.Close()
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return errors.Join(ErrPreRestoreExportJob, err)
			}
			job, scanErr := scanPreRestoreExportJob(rows)
			if scanErr != nil {
				return scanErr
			}
			pending = append(pending, job)
		}
		if err := rows.Err(); err != nil {
			return errors.Join(ErrPreRestoreExportJob, err)
		}
		if err := rows.Close(); err != nil {
			return ErrPreRestoreExportJob
		}
		return nil
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreExportJob, err)
	}
	recovered := make([]*tammyv1.PreRestoreArchiveExportJob, 0, len(pending))
	for _, job := range pending {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(ErrPreRestoreExportJob, err)
		}
		var archive PreRestoreArchiveRecord
		if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
			var loadErr error
			archive, loadErr = loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, job.ArchiveID)
			return loadErr
		}); err != nil {
			return nil, errors.Join(ErrPreRestoreExportJob, err)
		}
		destination, resolveErr := service.config.Destinations.Resolve(job.DestinationCapability)
		var committed []byte
		var readErr error
		if resolveErr == nil && !nilInterface(destination) && destination.Reference() == job.DestinationCapability {
			committed, readErr = destination.ReadCommitted(ctx)
		} else {
			readErr = ErrPreRestoreExportJob
		}
		destinationHash := sha256.Sum256(committed)
		matches := readErr == nil && bytes.Equal(destinationHash[:], archive.ContentHash)
		zeroBytes(committed)
		if !matches {
			var retryable preRestoreExportJobRecord
			if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
				loaded, loadErr := loadPreRestoreExportJob(ctx, executor, job.ID)
				if loadErr != nil || loaded.Version != job.Version || loaded.State != job.State {
					return ErrPreRestoreExportJobConflict
				}
				var transitionErr error
				retryable, transitionErr = transitionPreRestoreExportJob(ctx, executor, loaded,
					tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE,
					"DESTINATION_REAPPROVAL_REQUIRED", nil, false, service.config.Now().UTC())
				return transitionErr
			}); err != nil {
				return nil, errors.Join(ErrPreRestoreExportJob, err)
			}
			recovered = append(recovered, projectPreRestoreExportJob(retryable))
			continue
		}
		if job.State == tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING {
			if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
				loaded, loadErr := loadPreRestoreExportJob(ctx, executor, job.ID)
				if loadErr != nil || loaded.Version != job.Version || loaded.State != job.State {
					return ErrPreRestoreExportJobConflict
				}
				var transitionErr error
				job, transitionErr = transitionPreRestoreExportJob(ctx, executor, loaded,
					tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED,
					"VERIFIED", destinationHash[:], true, service.config.Now().UTC())
				return transitionErr
			}); err != nil {
				return nil, errors.Join(ErrPreRestoreExportJob, err)
			}
		}
		if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
			loaded, loadErr := loadPreRestoreExportJob(ctx, executor, job.ID)
			if loadErr != nil || loaded.Version != job.Version || loaded.State != job.State ||
				!bytes.Equal(loaded.DestinationHash, destinationHash[:]) {
				return ErrPreRestoreExportJobConflict
			}
			var transitionErr error
			job, transitionErr = transitionPreRestoreExportJob(ctx, executor, loaded,
				tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED,
				"COMPLETED", destinationHash[:], true, service.config.Now().UTC())
			return transitionErr
		}); err != nil {
			return nil, errors.Join(ErrPreRestoreExportJob, err)
		}
		recovered = append(recovered, projectPreRestoreExportJob(job))
	}
	return recovered, nil
}

func loadPreRestoreExportJob(ctx context.Context, executor backup.SQLExecutor, jobID string) (preRestoreExportJobRecord, error) {
	return queryOnePreRestoreExportJob(ctx, executor, ` WHERE job_id=?`, jobID)
}

func loadPreRestoreExportJobByOperation(ctx context.Context, executor backup.SQLExecutor, operationKey string) (preRestoreExportJobRecord, error) {
	return queryOnePreRestoreExportJob(ctx, executor, ` WHERE operation_key=?`, operationKey)
}

func queryOnePreRestoreExportJob(ctx context.Context, executor backup.SQLExecutor, where string, argument any) (preRestoreExportJobRecord, error) {
	rows, err := executor.QueryContext(ctx, preRestoreExportSelect+where, argument)
	if err != nil {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	defer rows.Close()
	if !rows.Next() {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	job, err := scanPreRestoreExportJob(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	return job, nil
}

const preRestoreExportSelect = `SELECT job_id,workspace_id,archive_id,archive_version,operation_key,retry_operation_key,version,state,input_hash,
	destination_capability,destination_hash,progress_proto,commit_point_reached,created_at,updated_at,completed_at
	FROM pre_restore_archive_export_jobs_v1`

func scanPreRestoreExportJob(scanner interface{ Scan(...any) error }) (preRestoreExportJobRecord, error) {
	var job preRestoreExportJobRecord
	var state int32
	var destinationHash []byte
	var progressBytes []byte
	var commitPoint int
	var created, updated string
	var completed sql.NullString
	var retryOperation sql.NullString
	if err := scanner.Scan(&job.ID, &job.WorkspaceID, &job.ArchiveID, &job.ArchiveVersion, &job.OperationKey,
		&retryOperation, &job.Version, &state,
		&job.InputHash, &job.DestinationCapability, &destinationHash, &progressBytes, &commitPoint, &created, &updated,
		&completed); err != nil {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	job.State = tammyv1.PreRestoreArchiveExportJobState(state)
	if retryOperation.Valid {
		job.RetryOperationKey = retryOperation.String
	}
	job.CommitPointReached = commitPoint == 1
	job.DestinationHash = append([]byte(nil), destinationHash...)
	job.Progress = &tammyv1.JobProgress{}
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(progressBytes, job.Progress) != nil ||
		len(job.Progress.ProtoReflect().GetUnknown()) != 0 {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	canonical, _ := proto.MarshalOptions{Deterministic: true}.Marshal(job.Progress)
	var err error
	job.CreatedAt, err = time.Parse(preRestoreTimestampLayout, created)
	if err == nil {
		job.UpdatedAt, err = time.Parse(preRestoreTimestampLayout, updated)
	}
	if err == nil && completed.Valid {
		instant, parseErr := time.Parse(preRestoreTimestampLayout, completed.String)
		err = parseErr
		job.CompletedAt = &instant
	}
	if err != nil || !bytes.Equal(canonical, progressBytes) || !validPreRestoreExportJob(job) {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	return job, nil
}

func validPreRestoreExportJob(job preRestoreExportJobRecord) bool {
	if !ids.IsCanonicalV7(job.ID) || !ids.IsCanonicalV7(job.WorkspaceID) || !ids.IsCanonicalV7(job.ArchiveID) ||
		!ids.IsCanonicalV7(job.OperationKey) || job.ArchiveVersion == 0 || job.Version == 0 || len(job.InputHash) != sha256.Size ||
		job.DestinationCapability == "" || job.Progress == nil || job.Progress.Stage == "" || job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
		return false
	}
	if job.RetryOperationKey != "" && !ids.IsCanonicalV7(job.RetryOperationKey) {
		return false
	}
	switch job.State {
	case tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_QUEUED,
		tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_FAILED_RETRYABLE:
		return !job.CommitPointReached && len(job.DestinationHash) == 0 && job.CompletedAt == nil
	case tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_WRITING:
		return len(job.DestinationHash) == 0 && job.CompletedAt == nil
	case tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_VERIFIED:
		return job.CommitPointReached && len(job.DestinationHash) == sha256.Size && job.CompletedAt == nil
	case tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED:
		return job.CommitPointReached && len(job.DestinationHash) == sha256.Size && job.CompletedAt != nil
	case tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED:
		return !job.CommitPointReached && len(job.DestinationHash) == 0 && job.CompletedAt != nil
	default:
		return false
	}
}

func transitionPreRestoreExportJob(ctx context.Context, executor backup.SQLExecutor, job preRestoreExportJobRecord,
	to tammyv1.PreRestoreArchiveExportJobState, stage string, destinationHash []byte, commitPoint bool, now time.Time,
) (preRestoreExportJobRecord, error) {
	progress, err := marshalPreRestoreProgress(stage)
	if err != nil || now.IsZero() {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJob
	}
	var completed any
	if to == tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_COMPLETED ||
		to == tammyv1.PreRestoreArchiveExportJobState_PRE_RESTORE_ARCHIVE_EXPORT_JOB_STATE_CANCELLED {
		completed = formatPreRestoreTime(now)
	}
	result, err := executor.ExecContext(ctx, `UPDATE pre_restore_archive_export_jobs_v1 SET version=version+1,state=?,
		destination_hash=?,progress_proto=?,commit_point_reached=?,updated_at=?,completed_at=? WHERE job_id=? AND version=? AND state=?`,
		int32(to), nullablePreRestoreHash(destinationHash), progress, boolInteger(commitPoint), formatPreRestoreTime(now),
		completed, job.ID, job.Version, int32(job.State))
	if err != nil || exactlyOnePreRestore(result) != nil {
		return preRestoreExportJobRecord{}, ErrPreRestoreExportJobConflict
	}
	return loadPreRestoreExportJob(ctx, executor, job.ID)
}

func marshalPreRestoreProgress(stage string) ([]byte, error) {
	if stage == "" || len(stage) > 64 {
		return nil, ErrPreRestoreExportJob
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(&tammyv1.JobProgress{Stage: stage})
}

func projectPreRestoreExportJob(job preRestoreExportJobRecord) *tammyv1.PreRestoreArchiveExportJob {
	projection := &tammyv1.PreRestoreArchiveExportJob{Id: job.ID, Version: job.Version, ArchiveId: job.ArchiveID,
		OperationKey: job.OperationKey, State: job.State, Progress: proto.Clone(job.Progress).(*tammyv1.JobProgress)}
	if len(job.DestinationHash) == sha256.Size {
		projection.DestinationHash = append([]byte(nil), job.DestinationHash...)
	}
	return projection
}

func preRestoreExportInputHash(workspaceID, archiveID string, expectedVersion uint64, destination string) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.pre-restore.export.semantic.v1\x00"))
	for _, value := range [][]byte{[]byte(workspaceID), []byte(archiveID), []byte(strconv.FormatUint(expectedVersion, 10)), []byte(destination)} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func preRestoreExportListQueryHash(workspaceID string,
	state tammyv1.PreRestoreArchiveExportJobState,
) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.pre-restore.export.list.v1\x00"))
	for _, value := range []string{workspaceID, strconv.FormatInt(int64(state), 10)} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(value))
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func preRestoreRetryInputHash(workspaceID, jobID, archiveID string, archiveVersion, failedVersion uint64,
	destination string,
) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.pre-restore.export.retry.semantic.v1\x00"))
	for _, value := range [][]byte{[]byte(workspaceID), []byte(jobID), []byte(archiveID),
		[]byte(strconv.FormatUint(archiveVersion, 10)), []byte(strconv.FormatUint(failedVersion, 10)), []byte(destination)} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func nullablePreRestoreHash(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return append([]byte(nil), value...)
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func exactlyOnePreRestore(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrPreRestoreExportJobConflict
	}
	return nil
}
