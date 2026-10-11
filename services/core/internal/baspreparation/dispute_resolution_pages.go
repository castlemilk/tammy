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

type DisputeResolutionListRequest struct {
	OrganisationID string
	Family         *tammyv1.SourceRef
	PageSize       uint32
	Cursor         *string
}

type DisputeResolutionPage struct {
	Resolutions []*tammyv1.BasExternalDisputeResolutionSummary
	Page        *tammyv1.PageInfo
}

type resolutionPageRecord struct {
	ordinal int64
	exact   *tammyv1.SourceRef
}

// List verifies bounded organisation-wide resolution batches before filtering
// their exact family. Empty pages may carry a continuation cursor. Append order,
// the durable watermark and full filters are bound into the authenticated cursor.
func (repository *DisputeResolutionRepository) List(ctx context.Context, request DisputeResolutionListRequest) (*DisputeResolutionPage, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) {
		return nil, ErrDisputeResolutionRepository
	}
	pageSize, ok := basNormalizedPageSize(request.PageSize)
	if !ok {
		return nil, ErrDisputeResolutionRepository
	}
	familyFilter := ""
	if request.Family != nil {
		if !validBASSourceRef(request.Family, "bas_family", "") || request.Family.Revision != 1 {
			return nil, ErrDisputeResolutionRepository
		}
		if _, err := repository.family(ctx, request.OrganisationID, request.Family); err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, err)
		}
		familyFilter = refKey(request.Family)
	}
	queryHash := basPageQueryHash("bas_dispute_resolution_org_scan_v1", request.OrganisationID, familyFilter)
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		endpoint, err := repository.loadResolutionPageRecords(ctx, request.OrganisationID, "ORDER BY e.event_ordinal DESC LIMIT 1")
		if err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		if len(endpoint) == 0 {
			return &DisputeResolutionPage{Resolutions: []*tammyv1.BasExternalDisputeResolutionSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if len(endpoint) != 1 {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt)
		}
		if _, err := repository.verifyResolutionPageRecord(ctx, request.OrganisationID, endpoint[0]); err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoint[0].ordinal, hash: bytes.Clone(endpoint[0].exact.ContentHash)}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrDisputeResolutionRepository
		}
		snapshot, ok = basParsePageSnapshot(cursor.Snapshot)
		if !ok {
			return nil, ErrDisputeResolutionRepository
		}
		position, ok = basParsePositiveOrdinal(cursor.Position)
		if !ok || position >= snapshot.ordinal {
			return nil, ErrDisputeResolutionRepository
		}
		endpoint, err := repository.loadResolutionPageRecords(ctx, request.OrganisationID, "AND e.event_ordinal=? LIMIT 1", snapshot.ordinal)
		if err != nil || len(endpoint) != 1 || !bytes.Equal(endpoint[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyResolutionPageRecord(ctx, request.OrganisationID, endpoint[0]); err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, err)
		}
		anchor, err := repository.loadResolutionPageRecords(ctx, request.OrganisationID, "AND e.event_ordinal=? LIMIT 1", position)
		if err != nil || len(anchor) != 1 {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyResolutionPageRecord(ctx, request.OrganisationID, anchor[0]); err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, err)
		}
	}
	records, err := repository.loadResolutionPageRecords(ctx, request.OrganisationID,
		"AND e.event_ordinal>? AND e.event_ordinal<=? ORDER BY e.event_ordinal LIMIT ?", position, snapshot.ordinal, pageSize+1)
	if err != nil || len(records) == 0 || len(records) > pageSize+1 {
		return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
	}
	values := make([]*tammyv1.BasExternalDisputeResolutionSummary, 0, pageSize)
	last := position
	for index, record := range records {
		if record.ordinal <= last || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyResolutionPageRecord(ctx, request.OrganisationID, record)
		if err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, err)
		}
		if index < pageSize && (request.Family == nil || sameBASSourceRef(value.Family, request.Family)) {
			values = append(values, &tammyv1.BasExternalDisputeResolutionSummary{Resolution: cloneRef(record.exact), Family: cloneRef(value.Family),
				Outcome: value.Resolution.Outcome, ResolvedAt: proto.Clone(value.ResolvedAt).(*timestamppb.Timestamp)})
		}
		last = record.ordinal
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records = records[:pageSize]
	}
	page := &DisputeResolutionPage{Resolutions: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot), Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrDisputeResolutionRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

func (repository *DisputeResolutionRepository) loadResolutionPageRecords(ctx context.Context, org, clause string, extra ...any) ([]resolutionPageRecord, error) {
	args := append([]any{org}, extra...)
	rows, err := repository.executor.QueryContext(ctx, `SELECT e.event_ordinal,r.resolution_id,r.content_hash
		FROM bas_family_events_v1 e LEFT JOIN bas_dispute_resolutions_v1 r
		ON r.event_id=e.event_id AND r.organisation_id=e.organisation_id AND r.family_id=e.family_id
		WHERE e.organisation_id=? AND e.event_kind='RESOLUTION' `+clause, args...)
	if err != nil {
		return nil, err
	}
	records := make([]resolutionPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			return nil, errors.Join(ErrSnapshotBound, rows.Close())
		}
		var record resolutionPageRecord
		var id string
		var hash []byte
		if err := rows.Scan(&record.ordinal, &id, &hash); err != nil {
			return nil, errors.Join(ErrSnapshotCorrupt, err, rows.Close())
		}
		record.exact = &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: id, Revision: 1, ContentHash: bytes.Clone(hash)}
		if record.ordinal <= 0 || !validBASSourceRef(record.exact, "bas_external_dispute_resolution", "") {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		records = append(records, record)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return records, nil
}

func (repository *DisputeResolutionRepository) verifyResolutionPageRecord(ctx context.Context, org string, record resolutionPageRecord) (*tammyv1.BasExternalDisputeResolution, error) {
	value, err := repository.GetExact(ctx, org, record.exact)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ResolutionId != record.exact.Id || !bytes.Equal(value.ContentHash, record.exact.ContentHash) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
