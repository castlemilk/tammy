package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ExportEventListRequest struct {
	OrganisationID string
	Target         *tammyv1.BasExportTarget
	PageSize       uint32
	Cursor         *string
}

type ExportEventPage struct {
	Events []*tammyv1.BasExportEventSummary
	Page   *tammyv1.PageInfo
}

type exportPageRecord struct {
	ordinal int64
	version int64
	exact   *tammyv1.SourceRef
}

// List scans a bounded family batch under one authenticated durable watermark,
// then filters verified bodies by the exact target. Empty result pages can have
// a next cursor; callers continue until it is absent. Every endpoint, anchor,
// scanned event and lookahead has its full proof read.
func (repository *ExportEventRepository) List(ctx context.Context, request ExportEventListRequest) (*ExportEventPage, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) {
		return nil, ErrExportEventRepository
	}
	pageSize, ok := basNormalizedPageSize(request.PageSize)
	kind, target, targetOK := exportTargetRef(request.Target)
	if !ok || !targetOK {
		return nil, ErrExportEventRepository
	}
	family, err := repository.verifyTarget(ctx, request.OrganisationID, request.Target)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	queryHash := basPageQueryHash("bas_export_history_family_scan_v1", request.OrganisationID, refKey(family)+":"+kind+":"+refKey(target))
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		endpoint, err := repository.loadExportFamilyPageRecords(ctx, request.OrganisationID, family, "ORDER BY append_ordinal DESC LIMIT 1")
		if err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
		if len(endpoint) == 0 {
			return &ExportEventPage{Events: []*tammyv1.BasExportEventSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if len(endpoint) != 1 {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
		}
		if _, err := repository.verifyExportPageRecord(ctx, request, endpoint[0]); err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoint[0].ordinal, hash: bytes.Clone(endpoint[0].exact.ContentHash)}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrExportEventRepository
		}
		snapshot, ok = basParsePageSnapshot(cursor.Snapshot)
		if !ok {
			return nil, ErrExportEventRepository
		}
		position, ok = basParsePositiveOrdinal(cursor.Position)
		if !ok || position >= snapshot.ordinal {
			return nil, ErrExportEventRepository
		}
		endpoint, err := repository.loadExportFamilyPageRecords(ctx, request.OrganisationID, family, "AND append_ordinal=? LIMIT 1", snapshot.ordinal)
		if err != nil || len(endpoint) != 1 || !bytes.Equal(endpoint[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyExportPageRecord(ctx, request, endpoint[0]); err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
		anchor, err := repository.loadExportFamilyPageRecords(ctx, request.OrganisationID, family, "AND append_ordinal=? LIMIT 1", position)
		if err != nil || len(anchor) != 1 {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyExportPageRecord(ctx, request, anchor[0]); err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
	}
	records, err := repository.loadExportFamilyPageRecords(ctx, request.OrganisationID, family, "AND append_ordinal>? AND append_ordinal<=? ORDER BY append_ordinal LIMIT ?", position, snapshot.ordinal, pageSize+1)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	if len(records) == 0 || len(records) > pageSize+1 {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
	}
	values := make([]*tammyv1.BasExportEventSummary, 0, len(records))
	last := position
	for index, record := range records {
		if record.ordinal <= last || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyExportPageRecord(ctx, request, record)
		if err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
		if index < pageSize && proto.Equal(value.Event.Target, request.Target) {
			summary := &tammyv1.BasExportEventSummary{ExportEvent: cloneRef(record.exact), Target: proto.Clone(value.Event.Target).(*tammyv1.BasExportTarget),
				Format: value.Event.Format, Phase: value.Event.Phase, RecordedAt: proto.Clone(value.RecordedAt).(*timestamppb.Timestamp)}
			if value.Event.Outcome != nil {
				summary.Outcome = value.Event.GetOutcome().Enum()
			}
			values = append(values, summary)
		}
		last = record.ordinal
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records = records[:pageSize]
	}
	page := &ExportEventPage{Events: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot), Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

func (repository *ExportEventRepository) loadExportPageRecords(ctx context.Context, request ExportEventListRequest, clause string, extra ...any) ([]exportPageRecord, error) {
	filter, args, ok := exportTargetSQLFilter(request.OrganisationID, request.Target)
	if !ok {
		return nil, ErrExportEventRepository
	}
	return repository.loadExportRecords(ctx, filter, args, clause, extra...)
}

func (repository *ExportEventRepository) loadExportFamilyPageRecords(ctx context.Context, org string, family *tammyv1.SourceRef, clause string, extra ...any) ([]exportPageRecord, error) {
	return repository.loadExportRecords(ctx, "organisation_id=? AND family_id=?", []any{org, family.Id}, clause, extra...)
}

func (repository *ExportEventRepository) loadExportRecords(ctx context.Context, filter string, args []any, clause string, extra ...any) ([]exportPageRecord, error) {
	args = append(args, extra...)
	rows, err := repository.executor.QueryContext(ctx, "SELECT append_ordinal,event_version,export_event_id,content_hash FROM bas_export_events_v1 WHERE "+filter+" "+clause, args...)
	if err != nil {
		return nil, err
	}
	records := make([]exportPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			return nil, errors.Join(ErrSnapshotBound, rows.Close())
		}
		var record exportPageRecord
		var id string
		var hash []byte
		if err := rows.Scan(&record.ordinal, &record.version, &id, &hash); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		record.exact = &tammyv1.SourceRef{Type: exportSnapshotSpec.resource, Id: id, Revision: 1, ContentHash: bytes.Clone(hash)}
		if record.ordinal <= 0 || record.version <= 0 || !validBASSourceRef(record.exact, exportSnapshotSpec.resource, "") {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		records = append(records, record)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return records, nil
}

func (repository *ExportEventRepository) verifyExportPageRecord(ctx context.Context, request ExportEventListRequest, record exportPageRecord) (*tammyv1.BasExportEvent, error) {
	value, err := repository.GetExact(ctx, request.OrganisationID, record.exact)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ExportEventId != record.exact.Id || !bytes.Equal(value.ContentHash, record.exact.ContentHash) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
