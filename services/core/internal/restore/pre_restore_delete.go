package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPreRestoreArchiveRetention = errors.New("restore: pre-restore archive retention period has not elapsed")

var errPreRestoreDeleteIntentNotFound = errors.New("restore: pre-restore delete intent not found")

type preRestoreDeleteIntent struct {
	OperationKey           string
	WorkspaceID            string
	ArchiveID              string
	ExpectedArchiveVersion uint64
	DeletionReason         string
	InputHash              []byte
	AuditEvent             *tammyv1.AuditEvent
	Status                 int
	Version                uint64
	Result                 *tammyv1.DeletePreRestoreArchiveResponse
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type preRestoreDeleteHooks struct {
	afterPendingCommit func() error
	afterFileRemoval   func() error
}

func (service *PreRestoreArchiveCommandService) DeletePreRestoreArchive(
	ctx context.Context,
	request *tammyv1.DeletePreRestoreArchiveRequest,
) (*tammyv1.DeletePreRestoreArchiveResponse, error) {
	if service == nil || ctx == nil || request == nil || len(request.ProtoReflect().GetUnknown()) != 0 ||
		protovalidate.Validate(request) != nil || request.AdministratorPassword == nil ||
		nilInterface(service.config.Transactions) || nilInterface(service.config.Archives) ||
		nilInterface(service.config.Audit) || service.config.Now == nil || ctx.Err() != nil ||
		strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.Reason) != request.Reason {
		return nil, ErrPreRestoreArchiveCommand
	}
	password := append([]byte(nil), request.AdministratorPassword.Utf8...)
	defer zeroBytes(password)
	if err := service.authorizeMutation(ctx, request.CommandContext, password, "delete_pre_restore_archive"); err != nil {
		return nil, err
	}
	now := service.config.Now().UTC()
	inputHash := preRestoreDeleteInputHash(service.workspaceID, request.ArchiveId, request.ExpectedVersion, request.Reason)
	var intent preRestoreDeleteIntent
	var completedReplay bool
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		existing, loadErr := loadPreRestoreDeleteIntent(ctx, executor, request.CommandContext.IdempotencyKey)
		if loadErr == nil {
			if !bytes.Equal(existing.InputHash, inputHash[:]) {
				return ErrPreRestoreExportJobConflict
			}
			archive, archiveErr := loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, existing.ArchiveID)
			if archiveErr != nil || !validPreRestoreDeleteArchiveBinding(existing, archive) ||
				(existing.Status == 2 && !validPreRestoreDeleteResultBinding(existing, archive)) {
				return ErrPreRestoreArchiveCommand
			}
			intent = existing
			completedReplay = existing.Status == 2
			return nil
		}
		if !errors.Is(loadErr, errPreRestoreDeleteIntentNotFound) {
			return loadErr
		}
		archive, archiveErr := loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, request.ArchiveId)
		if archiveErr != nil || archive.Version != request.ExpectedVersion ||
			archive.State != tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE {
			return ErrPreRestoreExportJobConflict
		}
		if now.Before(archive.DeletionEligibleAt) {
			return ErrPreRestoreArchiveRetention
		}
		active, activeErr := countActivePreRestoreExports(ctx, executor, archive.ArchiveID)
		if activeErr != nil || active != 0 {
			return ErrPreRestoreExportJobConflict
		}
		reasonHash := sha256.Sum256([]byte(request.Reason))
		operationKey := request.CommandContext.IdempotencyKey
		event := &tammyv1.AuditEvent{WorkspaceId: service.workspaceID,
			Type:       tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PRE_RESTORE_ARCHIVE_CHANGED,
			OccurredAt: timestamppb.New(now), Actor: proto.Clone(request.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
			CommandType: "DELETE_PRE_RESTORE_ARCHIVE", IdempotencyKey: &operationKey,
			BeforeSemanticHash: append([]byte(nil), archive.ContentHash...), AfterSemanticHash: append([]byte(nil), reasonHash[:]...),
			Payload: &tammyv1.AuditEventPayload{Payload: &tammyv1.AuditEventPayload_PreRestoreArchiveChanged{
				PreRestoreArchiveChanged: &tammyv1.PreRestoreArchiveChangedEvent{ArchiveId: archive.ArchiveID,
					FromState:   tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE.Enum(),
					ToState:     tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED,
					ContentHash: append([]byte(nil), archive.ContentHash...)}}}}
		eventBytes, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(event)
		if marshalErr != nil {
			return ErrPreRestoreArchiveCommand
		}
		result, updateErr := executor.ExecContext(ctx, `UPDATE pre_restore_archives_v1 SET version=version+1,state=?,
			deletion_reason_hash=? WHERE archive_id=? AND workspace_id=? AND version=? AND state=?`,
			int32(tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING), reasonHash[:], archive.ArchiveID,
			service.workspaceID, archive.Version, int32(tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE))
		if updateErr != nil || exactlyOnePreRestore(result) != nil {
			return ErrPreRestoreExportJobConflict
		}
		instant := formatPreRestoreTime(now)
		_, insertErr := executor.ExecContext(ctx, `INSERT INTO pre_restore_archive_commands_v1(
			operation_key,workspace_id,archive_id,expected_archive_version,command_type,deletion_reason,input_hash,audit_event_proto,
			status,version,created_at,updated_at) VALUES(?,?,?,?, 'DELETE',?,?,?,1,1,?,?)`, operationKey,
			service.workspaceID, archive.ArchiveID, archive.Version, request.Reason, inputHash[:], eventBytes, instant, instant)
		if insertErr != nil {
			return ErrPreRestoreExportJobConflict
		}
		intent, insertErr = loadPreRestoreDeleteIntent(ctx, executor, operationKey)
		if insertErr != nil || !validPreRestoreDeleteArchiveBinding(intent, PreRestoreArchiveRecord{
			WorkspaceID: archive.WorkspaceID, OperationID: archive.OperationID, ArchiveID: archive.ArchiveID,
			Version: archive.Version + 1, State: tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING,
			CreatedAt: archive.CreatedAt, DeletionEligibleAt: archive.DeletionEligibleAt, ContentHash: archive.ContentHash,
			SourceGeneration: archive.SourceGeneration, EncryptedByteLength: archive.EncryptedByteLength,
			DeletionReasonHash: reasonHash[:],
		}) {
			return ErrPreRestoreArchiveCommand
		}
		return nil
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	if completedReplay {
		return proto.Clone(intent.Result).(*tammyv1.DeletePreRestoreArchiveResponse), nil
	}
	if service.config.deleteHooks != nil && service.config.deleteHooks.afterPendingCommit != nil {
		if err := service.config.deleteHooks.afterPendingCommit(); err != nil {
			return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
		}
	}
	if err := service.config.Archives.DeleteEncryptedPreRestoreArchive(ctx, intent.ArchiveID,
		intent.AuditEvent.Payload.GetPreRestoreArchiveChanged().ContentHash); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	if service.config.deleteHooks != nil && service.config.deleteHooks.afterFileRemoval != nil {
		if err := service.config.deleteHooks.afterFileRemoval(); err != nil {
			return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
		}
	}
	response, err := service.finalizePreRestoreArchiveDelete(ctx, intent, now)
	if err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	return response, nil
}

func (service *PreRestoreArchiveCommandService) RecoverPreRestoreArchiveDeletes(
	ctx context.Context,
) ([]*tammyv1.DeletePreRestoreArchiveResponse, error) {
	if service == nil || ctx == nil || nilInterface(service.config.Transactions) ||
		nilInterface(service.config.Archives) || nilInterface(service.config.Audit) ||
		service.config.Now == nil || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveCommand
	}
	intents := make([]preRestoreDeleteIntent, 0)
	if err := service.config.Transactions.Read(ctx, func(executor backup.SQLExecutor) error {
		rows, err := executor.QueryContext(ctx, `SELECT operation_key FROM pre_restore_archive_commands_v1
			WHERE workspace_id=? AND command_type='DELETE' AND status=1 ORDER BY created_at,operation_key LIMIT 256`, service.workspaceID)
		if err != nil {
			return ErrPreRestoreArchiveCommand
		}
		defer rows.Close()
		keys := make([]string, 0)
		for rows.Next() {
			var key string
			if rows.Scan(&key) != nil {
				return ErrPreRestoreArchiveCommand
			}
			keys = append(keys, key)
		}
		if rows.Err() != nil {
			return ErrPreRestoreArchiveCommand
		}
		for _, key := range keys {
			intent, loadErr := loadPreRestoreDeleteIntent(ctx, executor, key)
			if loadErr != nil || intent.Status != 1 || intent.WorkspaceID != service.workspaceID {
				return ErrPreRestoreArchiveCommand
			}
			archive, archiveErr := loadPreRestoreArchiveRecord(ctx, executor, service.workspaceID, intent.ArchiveID)
			payload := intent.AuditEvent.Payload.GetPreRestoreArchiveChanged()
			if archiveErr != nil || payload == nil || !validPreRestoreDeleteArchiveBinding(intent, archive) {
				return ErrPreRestoreArchiveCommand
			}
			intents = append(intents, intent)
		}
		return nil
	}); err != nil {
		return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
	}
	results := make([]*tammyv1.DeletePreRestoreArchiveResponse, 0, len(intents))
	for _, intent := range intents {
		if err := service.config.Archives.DeleteEncryptedPreRestoreArchive(ctx, intent.ArchiveID,
			intent.AuditEvent.Payload.GetPreRestoreArchiveChanged().ContentHash); err != nil {
			return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
		}
		response, err := service.finalizePreRestoreArchiveDelete(ctx, intent, service.config.Now().UTC())
		if err != nil {
			return nil, errors.Join(ErrPreRestoreArchiveCommand, err)
		}
		results = append(results, response)
	}
	return results, nil
}

func (service *PreRestoreArchiveCommandService) finalizePreRestoreArchiveDelete(ctx context.Context,
	intent preRestoreDeleteIntent, now time.Time,
) (*tammyv1.DeletePreRestoreArchiveResponse, error) {
	var response *tammyv1.DeletePreRestoreArchiveResponse
	if err := service.config.Transactions.Mutate(ctx, func(executor backup.SQLExecutor) error {
		loaded, err := loadPreRestoreDeleteIntent(ctx, executor, intent.OperationKey)
		if err != nil || loaded.Version != intent.Version || loaded.Status != 1 ||
			!samePreRestoreDeleteIntent(loaded, intent) {
			return ErrPreRestoreExportJobConflict
		}
		archive, err := loadPreRestoreArchiveRecord(ctx, executor, loaded.WorkspaceID, loaded.ArchiveID)
		if err != nil || !validPreRestoreDeleteArchiveBinding(loaded, archive) {
			return ErrPreRestoreExportJobConflict
		}
		result, err := executor.ExecContext(ctx, `UPDATE pre_restore_archives_v1 SET version=version+1,state=?,deleted_at=?
			WHERE archive_id=? AND workspace_id=? AND version=? AND state=?`,
			int32(tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED), formatPreRestoreTime(now),
			archive.ArchiveID, archive.WorkspaceID, archive.Version,
			int32(tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING))
		if err != nil || exactlyOnePreRestore(result) != nil {
			return ErrPreRestoreExportJobConflict
		}
		archive, err = loadPreRestoreArchiveRecord(ctx, executor, loaded.WorkspaceID, loaded.ArchiveID)
		if err != nil {
			return err
		}
		response = &tammyv1.DeletePreRestoreArchiveResponse{Archive: projectPreRestoreArchive(archive)}
		resultBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(response)
		if err != nil {
			return ErrPreRestoreArchiveCommand
		}
		if err := service.config.Audit.AppendPreRestoreArchiveCommand(ctx, executor, loaded.AuditEvent); err != nil {
			return err
		}
		update, err := executor.ExecContext(ctx, `UPDATE pre_restore_archive_commands_v1 SET status=2,version=version+1,
			result_proto=?,updated_at=? WHERE operation_key=? AND version=? AND status=1`, resultBytes,
			formatPreRestoreTime(now), loaded.OperationKey, loaded.Version)
		if err != nil || exactlyOnePreRestore(update) != nil {
			return ErrPreRestoreExportJobConflict
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func loadPreRestoreDeleteIntent(ctx context.Context, executor backup.SQLExecutor, operationKey string) (preRestoreDeleteIntent, error) {
	rows, err := executor.QueryContext(ctx, `SELECT operation_key,workspace_id,archive_id,expected_archive_version,
		deletion_reason,input_hash,audit_event_proto,status,version,result_proto,created_at,updated_at FROM pre_restore_archive_commands_v1
		WHERE operation_key=? AND command_type='DELETE'`, operationKey)
	if err != nil {
		return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
		}
		return preRestoreDeleteIntent{}, errPreRestoreDeleteIntentNotFound
	}
	var intent preRestoreDeleteIntent
	var eventBytes, resultBytes []byte
	var created, updated string
	if err := rows.Scan(&intent.OperationKey, &intent.WorkspaceID, &intent.ArchiveID, &intent.ExpectedArchiveVersion,
		&intent.DeletionReason, &intent.InputHash, &eventBytes, &intent.Status, &intent.Version, &resultBytes, &created, &updated); err != nil ||
		rows.Next() || rows.Err() != nil {
		return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
	}
	intent.AuditEvent = &tammyv1.AuditEvent{}
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(eventBytes, intent.AuditEvent) != nil ||
		len(intent.AuditEvent.ProtoReflect().GetUnknown()) != 0 {
		return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
	}
	canonicalEvent, _ := proto.MarshalOptions{Deterministic: true}.Marshal(intent.AuditEvent)
	intent.CreatedAt, err = time.Parse(preRestoreTimestampLayout, created)
	if err == nil {
		intent.UpdatedAt, err = time.Parse(preRestoreTimestampLayout, updated)
	}
	if !bytes.Equal(canonicalEvent, eventBytes) || !ids.IsCanonicalV7(intent.OperationKey) ||
		!ids.IsCanonicalV7(intent.WorkspaceID) || !ids.IsCanonicalV7(intent.ArchiveID) ||
		intent.ExpectedArchiveVersion == 0 || len(intent.InputHash) != sha256.Size || intent.Version == 0 ||
		!validPreRestoreDeletionReason(intent.DeletionReason) ||
		(intent.Status != 1 && intent.Status != 2) || err != nil || !validPreRestoreDeleteIntentBinding(intent) {
		return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
	}
	if intent.Status == 1 {
		if len(resultBytes) != 0 {
			return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
		}
		return intent, nil
	}
	intent.Result = &tammyv1.DeletePreRestoreArchiveResponse{}
	if (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(resultBytes, intent.Result) != nil ||
		len(intent.Result.ProtoReflect().GetUnknown()) != 0 || protovalidate.Validate(intent.Result) != nil {
		return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
	}
	canonicalResult, _ := proto.MarshalOptions{Deterministic: true}.Marshal(intent.Result)
	if !bytes.Equal(canonicalResult, resultBytes) {
		return preRestoreDeleteIntent{}, ErrPreRestoreArchiveCommand
	}
	return intent, nil
}

func validPreRestoreDeleteIntentBinding(intent preRestoreDeleteIntent) bool {
	expectedInputHash := preRestoreDeleteInputHash(intent.WorkspaceID, intent.ArchiveID,
		intent.ExpectedArchiveVersion, intent.DeletionReason)
	reasonHash := sha256.Sum256([]byte(intent.DeletionReason))
	event := intent.AuditEvent
	if event == nil || event.WorkspaceId != intent.WorkspaceID ||
		event.Type != tammyv1.AuditEventType_AUDIT_EVENT_TYPE_PRE_RESTORE_ARCHIVE_CHANGED ||
		event.CommandType != "DELETE_PRE_RESTORE_ARCHIVE" || event.IdempotencyKey == nil ||
		*event.IdempotencyKey != intent.OperationKey || event.OccurredAt == nil || !event.OccurredAt.IsValid() ||
		!event.OccurredAt.AsTime().UTC().Equal(intent.CreatedAt) || event.Actor == nil ||
		!ids.IsCanonicalV7(event.Actor.ActorUserId) || !ids.IsCanonicalV7(event.Actor.SessionId) ||
		len(event.BeforeSemanticHash) != sha256.Size || len(event.AfterSemanticHash) != sha256.Size ||
		subtle.ConstantTimeCompare(intent.InputHash, expectedInputHash[:]) != 1 ||
		subtle.ConstantTimeCompare(event.AfterSemanticHash, reasonHash[:]) != 1 ||
		(intent.Status == 1 && (!intent.UpdatedAt.Equal(intent.CreatedAt) || intent.Version != 1)) ||
		(intent.Status == 2 && (intent.UpdatedAt.Before(intent.CreatedAt) || intent.Version != 2)) {
		return false
	}
	payload := event.Payload.GetPreRestoreArchiveChanged()
	return payload != nil && payload.ArchiveId == intent.ArchiveID && payload.FromState != nil &&
		*payload.FromState == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE &&
		payload.ToState == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED &&
		len(payload.ContentHash) == sha256.Size && bytes.Equal(payload.ContentHash, event.BeforeSemanticHash)
}

func validPreRestoreDeleteArchiveBinding(intent preRestoreDeleteIntent, archive PreRestoreArchiveRecord) bool {
	payload := intent.AuditEvent.Payload.GetPreRestoreArchiveChanged()
	reasonHash := sha256.Sum256([]byte(intent.DeletionReason))
	if payload == nil || archive.WorkspaceID != intent.WorkspaceID || archive.ArchiveID != intent.ArchiveID ||
		!bytes.Equal(archive.ContentHash, payload.ContentHash) || !bytes.Equal(archive.ContentHash, intent.AuditEvent.BeforeSemanticHash) ||
		subtle.ConstantTimeCompare(archive.DeletionReasonHash, reasonHash[:]) != 1 {
		return false
	}
	if intent.Status == 1 {
		return archive.Version == intent.ExpectedArchiveVersion+1 &&
			archive.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING && archive.DeletedAt == nil
	}
	return archive.Version == intent.ExpectedArchiveVersion+2 &&
		archive.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED && archive.DeletedAt != nil &&
		archive.DeletedAt.Equal(intent.UpdatedAt)
}

func validPreRestoreDeleteResultBinding(intent preRestoreDeleteIntent, archive PreRestoreArchiveRecord) bool {
	if intent.Status != 2 || intent.Result == nil || !validPreRestoreDeleteArchiveBinding(intent, archive) {
		return false
	}
	expected := &tammyv1.DeletePreRestoreArchiveResponse{Archive: projectPreRestoreArchive(archive)}
	return proto.Equal(intent.Result, expected)
}

func samePreRestoreDeleteIntent(left, right preRestoreDeleteIntent) bool {
	return left.OperationKey == right.OperationKey && left.WorkspaceID == right.WorkspaceID &&
		left.ArchiveID == right.ArchiveID && left.ExpectedArchiveVersion == right.ExpectedArchiveVersion &&
		left.DeletionReason == right.DeletionReason && subtle.ConstantTimeCompare(left.InputHash, right.InputHash) == 1 &&
		left.Status == right.Status && left.Version == right.Version && left.CreatedAt.Equal(right.CreatedAt) &&
		left.UpdatedAt.Equal(right.UpdatedAt) && proto.Equal(left.AuditEvent, right.AuditEvent)
}

func validPreRestoreDeletionReason(reason string) bool {
	return reason != "" && strings.TrimSpace(reason) == reason && utf8.ValidString(reason) && utf8.RuneCountInString(reason) <= 512
}

func countActivePreRestoreExports(ctx context.Context, executor backup.SQLExecutor, archiveID string) (uint64, error) {
	rows, err := executor.QueryContext(ctx, `SELECT COUNT(*) FROM pre_restore_archive_export_jobs_v1
		WHERE archive_id=? AND state IN (1,2,3)`, archiveID)
	if err != nil {
		return 0, ErrPreRestoreArchiveCommand
	}
	defer rows.Close()
	var count uint64
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		return 0, ErrPreRestoreArchiveCommand
	}
	return count, nil
}

func preRestoreDeleteInputHash(workspaceID, archiveID string, expectedVersion uint64, reason string) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.pre-restore.archive.delete.semantic.v1\x00"))
	for _, value := range [][]byte{[]byte(workspaceID), []byte(archiveID),
		[]byte(strconv.FormatUint(expectedVersion, 10)), []byte(reason)} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(value)
	}
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}
