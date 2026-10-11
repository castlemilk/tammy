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

type FrozenListRequest struct {
	OrganisationID string
	Family         *tammyv1.SourceRef
	PageSize       uint32
	Cursor         *string
}

type FrozenPage struct {
	Frozen []*tammyv1.BasFrozenBasSummary
	Page   *tammyv1.PageInfo
}

type frozenPageRecord struct {
	ordinal int64
	exact   *tammyv1.SourceRef
}

// List pins a durable append watermark and the complete optional family filter.
// It verifies complete frozen proofs before returning even shallow summaries.
func (repository *FrozenRepository) List(ctx context.Context, request FrozenListRequest) (*FrozenPage, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) {
		return nil, ErrFrozenRepository
	}
	pageSize, ok := basNormalizedPageSize(request.PageSize)
	if !ok {
		return nil, ErrFrozenRepository
	}
	filter := ""
	if request.Family != nil {
		if !validBASSourceRef(request.Family, "bas_family", "") || request.Family.Revision != 1 {
			return nil, ErrFrozenRepository
		}
		identities, err := NewFamilyIdentityRepository(repository.executor)
		if err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		if _, err := identities.GetExact(ctx, request.OrganisationID, request.Family); err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		filter = refKey(request.Family)
	}
	queryHash := basPageQueryHash("bas_frozen_history_v1", request.OrganisationID, filter)
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		endpoint, err := repository.loadFrozenPageRecords(ctx, request, "ORDER BY append_ordinal DESC LIMIT 1")
		if err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		if len(endpoint) == 0 {
			return &FrozenPage{Frozen: []*tammyv1.BasFrozenBasSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if len(endpoint) != 1 {
			return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt)
		}
		if _, err := repository.verifyFrozenPageRecord(ctx, request, endpoint[0]); err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoint[0].ordinal, hash: bytes.Clone(endpoint[0].exact.ContentHash)}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrFrozenRepository
		}
		snapshot, ok = basParsePageSnapshot(cursor.Snapshot)
		if !ok {
			return nil, ErrFrozenRepository
		}
		position, ok = basParsePositiveOrdinal(cursor.Position)
		if !ok || position >= snapshot.ordinal {
			return nil, ErrFrozenRepository
		}
		endpoint, err := repository.loadFrozenPageRecords(ctx, request, "AND append_ordinal=? LIMIT 1", snapshot.ordinal)
		if err != nil || len(endpoint) != 1 || !bytes.Equal(endpoint[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyFrozenPageRecord(ctx, request, endpoint[0]); err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		anchor, err := repository.loadFrozenPageRecords(ctx, request, "AND append_ordinal=? LIMIT 1", position)
		if err != nil || len(anchor) != 1 {
			return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyFrozenPageRecord(ctx, request, anchor[0]); err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
	}
	records, err := repository.loadFrozenPageRecords(ctx, request,
		"AND append_ordinal>? AND append_ordinal<=? ORDER BY append_ordinal LIMIT ?", position, snapshot.ordinal, pageSize+1)
	if err != nil {
		return nil, errors.Join(ErrFrozenRepository, err)
	}
	if len(records) == 0 || len(records) > pageSize+1 {
		return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt)
	}
	values := make([]*tammyv1.BasFrozenBasSummary, 0, len(records))
	last := position
	for _, record := range records {
		if record.ordinal <= last || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrFrozenRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyFrozenPageRecord(ctx, request, record)
		if err != nil {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		values = append(values, &tammyv1.BasFrozenBasSummary{FrozenBas: cloneRef(record.exact),
			Preparation: cloneRef(value.Preparation), Family: cloneRef(value.Family), Period: cloneRef(value.Period),
			Calculation: cloneRef(value.Calculation), ApprovedAt: proto.Clone(value.ApprovedAt).(*timestamppb.Timestamp)})
		last = record.ordinal
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records, values = records[:pageSize], values[:pageSize]
	}
	page := &FrozenPage{Frozen: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot),
			Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrFrozenRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

func (repository *FrozenRepository) loadFrozenPageRecords(ctx context.Context, request FrozenListRequest,
	clause string, extra ...any) ([]frozenPageRecord, error) {
	filter, arguments := "organisation_id=?", []any{request.OrganisationID}
	if request.Family != nil {
		filter += " AND family_id=?"
		arguments = append(arguments, request.Family.Id)
	}
	arguments = append(arguments, extra...)
	rows, err := repository.executor.QueryContext(ctx, "SELECT append_ordinal,frozen_id,content_hash FROM bas_frozen_bas_v1 WHERE "+filter+" "+clause, arguments...)
	if err != nil {
		return nil, err
	}
	records := make([]frozenPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			return nil, errors.Join(ErrSnapshotBound, rows.Close())
		}
		var record frozenPageRecord
		var id string
		var hash []byte
		if err := rows.Scan(&record.ordinal, &id, &hash); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		record.exact = &tammyv1.SourceRef{Type: frozenSnapshotSpec.resource, Id: id, Revision: 1, ContentHash: bytes.Clone(hash)}
		if record.ordinal <= 0 || !validBASSourceRef(record.exact, frozenSnapshotSpec.resource, "") {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		records = append(records, record)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return records, nil
}

func (repository *FrozenRepository) verifyFrozenPageRecord(ctx context.Context, request FrozenListRequest,
	record frozenPageRecord) (*tammyv1.BasFrozenBas, error) {
	value, err := repository.GetExact(ctx, request.OrganisationID, record.exact)
	if err != nil {
		return nil, err
	}
	if value == nil || value.FrozenId != record.exact.Id || !bytes.Equal(value.ContentHash, record.exact.ContentHash) ||
		(request.Family != nil && !sameBASSourceRef(request.Family, value.Family)) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
