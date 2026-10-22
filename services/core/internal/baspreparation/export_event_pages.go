package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

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
	var boundaryRecords []exportPageRecord
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
		boundaryRecords = append(boundaryRecords, endpoint[0])
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
		boundaryRecords = append(boundaryRecords, endpoint[0])
		anchor, err := repository.loadExportFamilyPageRecords(ctx, request.OrganisationID, family, "AND append_ordinal=? LIMIT 1", position)
		if err != nil || len(anchor) != 1 {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt, err)
		}
		boundaryRecords = append(boundaryRecords, anchor[0])
	}
	records, err := repository.loadExportFamilyPageRecords(ctx, request.OrganisationID, family, "AND append_ordinal>? AND append_ordinal<=? ORDER BY append_ordinal LIMIT ?", position, snapshot.ordinal, pageSize+1)
	if err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	if len(records) == 0 || len(records) > pageSize+1 {
		return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
	}
	last := position
	for _, record := range records {
		if record.ordinal <= last || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrExportEventRepository, ErrSnapshotCorrupt)
		}
		last = record.ordinal
	}
	proofRecords := make([]exportPageRecord, 0, len(boundaryRecords)+len(records))
	proofRecords = append(proofRecords, boundaryRecords...)
	proofRecords = append(proofRecords, records...)
	if err := repository.verifyExportPageVersionPositions(ctx, request.OrganisationID, family.Id, snapshot.ordinal, proofRecords); err != nil {
		return nil, errors.Join(ErrExportEventRepository, err)
	}
	for _, record := range boundaryRecords {
		if _, err := repository.verifyExportPageRecord(ctx, request, record); err != nil {
			return nil, errors.Join(ErrExportEventRepository, err)
		}
	}
	values := make([]*tammyv1.BasExportEventSummary, 0, len(records))
	for index, record := range records {
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
	// List has already batch-proved the event's persisted target-local version
	// position at the same family watermark. Keep every body-level proof here.
	value, err := repository.getExact(ctx, request.OrganisationID, record.exact, false)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ExportEventId != record.exact.Id || !bytes.Equal(value.ContentHash, record.exact.ContentHash) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}

// verifyExportPageVersionPositions replaces one prefix aggregate per event
// with one window pass for the bounded List batch. Public GetExact continues to
// use verifyExportVersionPrefix; this shortcut is confined to List after exact
// SQL row positions have been checked here.
func (repository *ExportEventRepository) verifyExportPageVersionPositions(ctx context.Context, org, familyID string, watermark int64, candidates []exportPageRecord) error {
	if !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(familyID) || watermark <= 0 || len(candidates) == 0 || len(candidates) > basRepositoryPageSizeMaximum+3 {
		return ErrSnapshotCorrupt
	}
	wanted := make(map[string]exportPageRecord, len(candidates))
	for _, candidate := range candidates {
		if candidate.ordinal <= 0 || candidate.ordinal > watermark || candidate.version <= 0 ||
			!validBASSourceRef(candidate.exact, exportSnapshotSpec.resource, "") || candidate.exact.Revision != 1 {
			return ErrSnapshotCorrupt
		}
		if previous, ok := wanted[candidate.exact.Id]; ok {
			if previous.ordinal != candidate.ordinal || previous.version != candidate.version || !bytes.Equal(previous.exact.ContentHash, candidate.exact.ContentHash) {
				return ErrSnapshotCorrupt
			}
			continue
		}
		wanted[candidate.exact.Id] = candidate
	}
	if len(wanted) == 0 || len(wanted) > basRepositoryPageSizeMaximum+3 {
		return ErrSnapshotCorrupt
	}

	eventIDs := make([]string, 0, len(wanted))
	args := make([]any, 0, 3+len(wanted))
	args = append(args, org, familyID, watermark)
	for id := range wanted {
		eventIDs = append(eventIDs, id)
	}
	// Bound the dynamic SQL to placeholder punctuation only; all identifiers are
	// values. Sorting keeps statement arguments deterministic for diagnostics.
	sort.Strings(eventIDs)
	for _, id := range eventIDs {
		args = append(args, id)
	}
	places := strings.TrimSuffix(strings.Repeat("?,", len(eventIDs)), ",")
	// Only targets represented by this bounded batch need prefix validation.
	// The two fixed branches let SQLite use the existing target-key indexes
	// rather than ranking unrelated event histories in the family. The final
	// candidate-ID filter uses a second bounded placeholder set.
	args = append(args, org, familyID, watermark, org, familyID, watermark)
	for _, id := range eventIDs {
		args = append(args, id)
	}
	query := "WITH candidate_targets AS MATERIALIZED (" +
		" SELECT DISTINCT target_kind,calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash" +
		" FROM bas_export_events_v1 WHERE organisation_id=? AND family_id=? AND append_ordinal<=?" +
		" AND export_event_id IN (" + places + ")" +
		"), target_history AS MATERIALIZED (" +
		" SELECT e.append_ordinal,e.event_version,e.export_event_id,e.content_hash,e.target_kind," +
		" e.calculation_id,e.calculation_version,e.calculation_hash,e.frozen_id,e.frozen_hash" +
		" FROM candidate_targets AS c CROSS JOIN bas_export_events_v1 AS e INDEXED BY bas_export_events_calculation_page_idx" +
		" WHERE c.target_kind='CALCULATION' AND e.target_kind='CALCULATION'" +
		" AND e.organisation_id=? AND e.family_id=? AND e.append_ordinal<=?" +
		" AND e.calculation_id=c.calculation_id AND e.calculation_version=c.calculation_version AND e.calculation_hash=c.calculation_hash" +
		" UNION ALL" +
		" SELECT e.append_ordinal,e.event_version,e.export_event_id,e.content_hash,e.target_kind," +
		" e.calculation_id,e.calculation_version,e.calculation_hash,e.frozen_id,e.frozen_hash" +
		" FROM candidate_targets AS c CROSS JOIN bas_export_events_v1 AS e INDEXED BY bas_export_events_frozen_page_idx" +
		" WHERE c.target_kind='FROZEN' AND e.target_kind='FROZEN'" +
		" AND e.organisation_id=? AND e.family_id=? AND e.append_ordinal<=?" +
		" AND e.frozen_id=c.frozen_id AND e.frozen_hash=c.frozen_hash" +
		"), ranked AS (" +
		" SELECT append_ordinal,event_version,export_event_id,content_hash,target_kind," +
		" calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash," +
		" ROW_NUMBER() OVER (" +
		" PARTITION BY target_kind,calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash" +
		" ORDER BY append_ordinal) AS expected_version FROM target_history" +
		"), checked AS (" +
		" SELECT append_ordinal,event_version,export_event_id,content_hash,expected_version," +
		" MAX(CASE WHEN event_version != expected_version THEN 1 ELSE 0 END) OVER (" +
		" PARTITION BY target_kind,calculation_id,calculation_version,calculation_hash,frozen_id,frozen_hash" +
		" ORDER BY append_ordinal ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS prefix_invalid" +
		" FROM ranked" +
		") SELECT append_ordinal,event_version,export_event_id,content_hash,expected_version,prefix_invalid" +
		" FROM checked WHERE export_event_id IN (" + places + ")"
	rows, err := repository.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ordinal, version, expectedVersion, prefixInvalid int64
		var eventID string
		var contentHash []byte
		if err := rows.Scan(&ordinal, &version, &eventID, &contentHash, &expectedVersion, &prefixInvalid); err != nil {
			return errors.Join(err, rows.Close())
		}
		candidate, ok := wanted[eventID]
		if !ok || candidate.ordinal != ordinal || candidate.version != version || expectedVersion != version || prefixInvalid != 0 ||
			!bytes.Equal(candidate.exact.ContentHash, contentHash) {
			return errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		delete(wanted, eventID)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(wanted) != 0 {
		return ErrSnapshotCorrupt
	}
	return nil
}
