package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/backup"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPreRestoreArchiveRepository = errors.New("restore: invalid pre-restore archive repository operation")

const preRestoreTimestampLayout = "2006-01-02T15:04:05.000000000Z"

type PreRestoreArchiveRecord struct {
	WorkspaceID         string
	OperationID         string
	ArchiveID           string
	Version             uint64
	State               tammyv1.PreRestoreArchiveState
	CreatedAt           time.Time
	DeletionEligibleAt  time.Time
	ContentHash         []byte
	SourceGeneration    uint64
	EncryptedByteLength uint64
	DeletionReasonHash  []byte
	DeletedAt           *time.Time
}

type PreRestoreArchiveList struct {
	WorkspaceID string
	State       tammyv1.PreRestoreArchiveState
	PageSize    uint32
	Cursor      *string
}

type PreRestoreArchivePage struct {
	Archives []*tammyv1.PreRestoreArchive
	Page     *tammyv1.PageInfo
}

type PreRestoreArchiveRepository struct {
	executor backup.SQLExecutor
	cursors  *paging.Codec
}

func NewPreRestoreArchiveRepository(executor backup.SQLExecutor, cursors *paging.Codec) (*PreRestoreArchiveRepository, error) {
	if nilInterface(executor) || cursors == nil {
		return nil, ErrPreRestoreArchiveRepository
	}
	return &PreRestoreArchiveRepository{executor: executor, cursors: cursors}, nil
}

func PersistPreRestoreArchive(ctx context.Context, executor backup.SQLExecutor, record PreRestoreArchiveRecord) error {
	if ctx == nil || nilInterface(executor) || !validPreRestoreArchiveRecord(record) || ctx.Err() != nil {
		return ErrPreRestoreArchiveRepository
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO pre_restore_archives_v1(
		archive_id,workspace_id,operation_id,version,state,created_at,deletion_eligible_at,content_hash,
		source_generation,encrypted_byte_length) VALUES(?,?,?,?,?,?,?,?,?,?)`, record.ArchiveID, record.WorkspaceID,
		record.OperationID, record.Version, int32(record.State), formatPreRestoreTime(record.CreatedAt),
		formatPreRestoreTime(record.DeletionEligibleAt), append([]byte(nil), record.ContentHash...),
		record.SourceGeneration, record.EncryptedByteLength)
	if err == nil {
		return nil
	}
	existing, loadErr := loadPreRestoreArchiveRecord(ctx, executor, record.WorkspaceID, record.ArchiveID)
	if loadErr == nil && samePreRestoreArchiveRecord(existing, record) {
		return nil
	}
	return ErrPreRestoreArchiveRepository
}

func (repository *PreRestoreArchiveRepository) Get(ctx context.Context, workspaceID, archiveID string) (*tammyv1.PreRestoreArchive, error) {
	if repository == nil || ctx == nil || !ids.IsCanonicalV7(workspaceID) || !ids.IsCanonicalV7(archiveID) || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveRepository
	}
	record, err := loadPreRestoreArchiveRecord(ctx, repository.executor, workspaceID, archiveID)
	if err != nil {
		return nil, err
	}
	return projectPreRestoreArchive(record), nil
}

func (repository *PreRestoreArchiveRepository) List(ctx context.Context, request PreRestoreArchiveList) (*PreRestoreArchivePage, error) {
	if repository == nil || ctx == nil || !ids.IsCanonicalV7(request.WorkspaceID) ||
		request.State < tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_UNSPECIFIED ||
		request.State > tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING || ctx.Err() != nil {
		return nil, ErrPreRestoreArchiveRepository
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		return nil, ErrPreRestoreArchiveRepository
	}
	queryHash := preRestoreListQueryHash(request.WorkspaceID, request.State)
	snapshot, position := "", ""
	if request.Cursor != nil {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrPreRestoreArchiveRepository
		}
		snapshot, position = cursor.Snapshot, cursor.Position
	} else {
		var created, archiveID string
		rows, err := repository.executor.QueryContext(ctx, `SELECT created_at,archive_id FROM pre_restore_archives_v1
			WHERE workspace_id=? AND (?=0 OR state=?) ORDER BY created_at DESC,archive_id DESC LIMIT 1`,
			request.WorkspaceID, int32(request.State), int32(request.State))
		if err != nil {
			return nil, ErrPreRestoreArchiveRepository
		}
		if !rows.Next() {
			rowsErr := rows.Err()
			_ = rows.Close()
			if rowsErr != nil {
				return nil, ErrPreRestoreArchiveRepository
			}
			return &PreRestoreArchivePage{Page: &tammyv1.PageInfo{}}, nil
		}
		if rows.Scan(&created, &archiveID) != nil || rows.Next() || rows.Err() != nil || rows.Close() != nil {
			return nil, ErrPreRestoreArchiveRepository
		}
		snapshot = created + "|" + archiveID
		position = "!|!"
	}
	snapshotCreated, snapshotID, ok := splitPreRestoreCursorComponent(snapshot)
	if !ok {
		return nil, ErrPreRestoreArchiveRepository
	}
	positionCreated, positionID := "", ""
	if position != "!|!" {
		positionCreated, positionID, ok = splitPreRestoreCursorComponent(position)
		if !ok {
			return nil, ErrPreRestoreArchiveRepository
		}
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT archive_id,workspace_id,operation_id,version,state,
		created_at,deletion_eligible_at,content_hash,source_generation,encrypted_byte_length,deletion_reason_hash,deleted_at
		FROM pre_restore_archives_v1 WHERE workspace_id=? AND (?=0 OR state=?)
		AND (created_at < ? OR (created_at=? AND archive_id<=?))
		AND (?='' OR created_at > ? OR (created_at=? AND archive_id>?))
		ORDER BY created_at,archive_id LIMIT ?`, request.WorkspaceID, int32(request.State), int32(request.State),
		snapshotCreated, snapshotCreated, snapshotID, positionCreated, positionCreated, positionCreated, positionID, pageSize+1)
	if err != nil {
		return nil, ErrPreRestoreArchiveRepository
	}
	defer rows.Close()
	records := make([]PreRestoreArchiveRecord, 0, pageSize+1)
	for rows.Next() {
		record, err := scanPreRestoreArchiveRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if rows.Err() != nil || len(records) > int(pageSize)+1 {
		return nil, ErrPreRestoreArchiveRepository
	}
	hasMore := len(records) > int(pageSize)
	if hasMore {
		records = records[:pageSize]
	}
	page := &PreRestoreArchivePage{Archives: make([]*tammyv1.PreRestoreArchive, len(records)),
		Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(records))}}
	for index := range records {
		page.Archives[index] = projectPreRestoreArchive(records[index])
	}
	if hasMore && len(records) > 0 {
		last := records[len(records)-1]
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: snapshot,
			Position: formatPreRestoreTime(last.CreatedAt) + "|" + last.ArchiveID, QueryHash: queryHash})
		if err != nil {
			return nil, ErrPreRestoreArchiveRepository
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

type preRestoreRowScanner interface{ Scan(...any) error }

func loadPreRestoreArchiveRecord(ctx context.Context, executor backup.SQLExecutor, workspaceID, archiveID string) (PreRestoreArchiveRecord, error) {
	rows, err := executor.QueryContext(ctx, `SELECT archive_id,workspace_id,operation_id,version,state,created_at,
		deletion_eligible_at,content_hash,source_generation,encrypted_byte_length,deletion_reason_hash,deleted_at FROM pre_restore_archives_v1
		WHERE workspace_id=? AND archive_id=?`, workspaceID, archiveID)
	if err != nil {
		return PreRestoreArchiveRecord{}, ErrPreRestoreArchiveRepository
	}
	defer rows.Close()
	if !rows.Next() {
		return PreRestoreArchiveRecord{}, ErrPreRestoreArchiveRepository
	}
	record, err := scanPreRestoreArchiveRecord(rows)
	if err != nil || rows.Next() || rows.Err() != nil {
		return PreRestoreArchiveRecord{}, ErrPreRestoreArchiveRepository
	}
	return record, nil
}

func scanPreRestoreArchiveRecord(scanner preRestoreRowScanner) (PreRestoreArchiveRecord, error) {
	var record PreRestoreArchiveRecord
	var state int32
	var created, eligible string
	var deleted sql.NullString
	if err := scanner.Scan(&record.ArchiveID, &record.WorkspaceID, &record.OperationID, &record.Version, &state,
		&created, &eligible, &record.ContentHash, &record.SourceGeneration, &record.EncryptedByteLength,
		&record.DeletionReasonHash, &deleted); err != nil {
		return PreRestoreArchiveRecord{}, ErrPreRestoreArchiveRepository
	}
	record.State = tammyv1.PreRestoreArchiveState(state)
	var err error
	record.CreatedAt, err = time.Parse(preRestoreTimestampLayout, created)
	if err != nil {
		return PreRestoreArchiveRecord{}, ErrPreRestoreArchiveRepository
	}
	record.DeletionEligibleAt, err = time.Parse(preRestoreTimestampLayout, eligible)
	if err == nil && deleted.Valid {
		var deletedAt time.Time
		deletedAt, err = time.Parse(preRestoreTimestampLayout, deleted.String)
		if err == nil {
			record.DeletedAt = &deletedAt
		}
	}
	if err != nil || !validPreRestoreArchiveRecord(record) {
		return PreRestoreArchiveRecord{}, ErrPreRestoreArchiveRepository
	}
	return record, nil
}

func validPreRestoreArchiveRecord(record PreRestoreArchiveRecord) bool {
	return ids.IsCanonicalV7(record.WorkspaceID) && ids.IsCanonicalV7(record.OperationID) && ids.IsCanonicalV7(record.ArchiveID) &&
		record.Version > 0 && (record.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE ||
		record.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED ||
		record.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING) &&
		!record.CreatedAt.IsZero() && record.CreatedAt.Equal(record.CreatedAt.UTC()) &&
		!record.DeletionEligibleAt.Before(record.CreatedAt.AddDate(1, 0, 0)) && len(record.ContentHash) == sha256.Size &&
		record.SourceGeneration > 0 && record.EncryptedByteLength > 0 && record.EncryptedByteLength <= maximumPreRestoreArchiveFileBytes &&
		((record.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_AVAILABLE &&
			record.DeletedAt == nil && len(record.DeletionReasonHash) == 0) ||
			(record.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETE_PENDING &&
				record.DeletedAt == nil && len(record.DeletionReasonHash) == sha256.Size) ||
			(record.State == tammyv1.PreRestoreArchiveState_PRE_RESTORE_ARCHIVE_STATE_DELETED &&
				record.DeletedAt != nil && record.DeletedAt.Equal(record.DeletedAt.UTC()) &&
				!record.DeletedAt.Before(record.CreatedAt) && len(record.DeletionReasonHash) == sha256.Size))
}

func samePreRestoreArchiveRecord(left, right PreRestoreArchiveRecord) bool {
	return left.WorkspaceID == right.WorkspaceID && left.OperationID == right.OperationID && left.ArchiveID == right.ArchiveID &&
		left.Version == right.Version && left.State == right.State && left.CreatedAt.Equal(right.CreatedAt) &&
		left.DeletionEligibleAt.Equal(right.DeletionEligibleAt) && bytes.Equal(left.ContentHash, right.ContentHash) &&
		left.SourceGeneration == right.SourceGeneration && left.EncryptedByteLength == right.EncryptedByteLength &&
		bytes.Equal(left.DeletionReasonHash, right.DeletionReasonHash) && sameOptionalPreRestoreTime(left.DeletedAt, right.DeletedAt)
}

func projectPreRestoreArchive(record PreRestoreArchiveRecord) *tammyv1.PreRestoreArchive {
	archive := &tammyv1.PreRestoreArchive{Id: record.ArchiveID, Version: record.Version, State: record.State,
		CreatedAt: timestamppb.New(record.CreatedAt), DeletionEligibleAt: timestamppb.New(record.DeletionEligibleAt),
		ContentHash: append([]byte(nil), record.ContentHash...), SourceGeneration: record.SourceGeneration}
	if record.DeletedAt != nil {
		archive.DeletedAt = timestamppb.New(*record.DeletedAt)
	}
	return archive
}

func sameOptionalPreRestoreTime(left, right *time.Time) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && left.Equal(*right))
}

func preRestoreListQueryHash(workspaceID string, state tammyv1.PreRestoreArchiveState) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte("tammy.pre-restore.archive.list.v1\x00"))
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

func splitPreRestoreCursorComponent(value string) (string, string, bool) {
	created, identifier, ok := strings.Cut(value, "|")
	if !ok || strings.Contains(identifier, "|") || !ids.IsCanonicalV7(identifier) {
		return "", "", false
	}
	instant, err := time.Parse(preRestoreTimestampLayout, created)
	if err != nil || formatPreRestoreTime(instant) != created {
		return "", "", false
	}
	return created, identifier, true
}

func formatPreRestoreTime(value time.Time) string {
	return value.UTC().Format(preRestoreTimestampLayout)
}
