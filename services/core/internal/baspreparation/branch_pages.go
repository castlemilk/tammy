package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strconv"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

type BranchHistoryRequest struct {
	OrganisationID string
	Branch         *tammyv1.SourceRef
	PageSize       uint32
	Cursor         *string
}

type BranchHistoryPage struct {
	States []*tammyv1.BasBranchState
	Page   *tammyv1.PageInfo
}

type branchHistoryPageRecord struct {
	ordinal int64
	version int64
	exact   *tammyv1.SourceRef
}

// List returns every retained workflow version in durable append order. The
// signed cursor binds the exact branch identity and fixes an append watermark,
// so later workflow changes cannot shift an existing history page.
func (repository *BranchStateRepository) List(ctx context.Context, request BranchHistoryRequest) (*BranchHistoryPage, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) ||
		!validBASSourceRef(request.Branch, "bas_branch", "") || request.Branch.Revision != 1 {
		return nil, ErrBranchStateRepository
	}
	pageSize, validSize := basNormalizedPageSize(request.PageSize)
	if !validSize {
		return nil, ErrBranchStateRepository
	}
	identity, err := (&BranchIdentityRepository{repository.lifecycleRepository}).GetExact(ctx, request.OrganisationID, request.Branch)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	if identity == nil || identity.OrganisationId != request.OrganisationID || identity.BranchId != request.Branch.Id {
		return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
	}

	queryHash := basPageQueryHash("bas_branch_history_v1", request.OrganisationID, refKey(request.Branch))
	snapshot, position := basPageSnapshot{}, int64(0)
	lastVersion := int64(0)
	if request.Cursor == nil {
		endpoints, err := repository.loadBranchHistoryPageRecords(ctx, request.OrganisationID, request.Branch.Id,
			`ORDER BY append_ordinal DESC LIMIT 1`)
		if err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		if len(endpoints) == 0 {
			return &BranchHistoryPage{States: []*tammyv1.BasBranchState{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if len(endpoints) != 1 {
			return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
		}
		if _, err := repository.verifyBranchHistoryPageRecord(ctx, request.OrganisationID, request.Branch, endpoints[0]); err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoints[0].ordinal, hash: bytes.Clone(endpoints[0].exact.ContentHash)}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrBranchStateRepository
		}
		var valid bool
		snapshot, valid = basParsePageSnapshot(cursor.Snapshot)
		if !valid {
			return nil, ErrBranchStateRepository
		}
		position, valid = basParsePositiveOrdinal(cursor.Position)
		if !valid || position >= snapshot.ordinal {
			return nil, ErrBranchStateRepository
		}
		endpoint, err := repository.loadBranchHistoryPageRecords(ctx, request.OrganisationID, request.Branch.Id,
			`AND append_ordinal=? LIMIT 1`, snapshot.ordinal)
		if err != nil || len(endpoint) != 1 || endpoint[0].ordinal != snapshot.ordinal ||
			!bytes.Equal(endpoint[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyBranchHistoryPageRecord(ctx, request.OrganisationID, request.Branch, endpoint[0]); err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		anchor, err := repository.loadBranchHistoryPageRecords(ctx, request.OrganisationID, request.Branch.Id,
			`AND append_ordinal=? AND append_ordinal<=? LIMIT 1`, position, snapshot.ordinal)
		if err != nil || len(anchor) != 1 || anchor[0].ordinal != position {
			return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyBranchHistoryPageRecord(ctx, request.OrganisationID, request.Branch, anchor[0]); err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		if endpoint[0].version <= anchor[0].version {
			return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
		}
		lastVersion = anchor[0].version
	}

	records, err := repository.loadBranchHistoryPageRecords(ctx, request.OrganisationID, request.Branch.Id,
		`AND append_ordinal>? AND append_ordinal<=? ORDER BY append_ordinal LIMIT ?`, position, snapshot.ordinal, pageSize+1)
	if err != nil {
		return nil, errors.Join(ErrBranchStateRepository, err)
	}
	if len(records) > pageSize+1 {
		return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotBound)
	}
	if len(records) == 0 {
		return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
	}
	values := make([]*tammyv1.BasBranchState, 0, len(records))
	lastOrdinal := position
	for _, record := range records {
		if record.ordinal <= lastOrdinal || record.ordinal > snapshot.ordinal || lastVersion == math.MaxInt64 || record.version != lastVersion+1 {
			return nil, errors.Join(ErrBranchStateRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyBranchHistoryPageRecord(ctx, request.OrganisationID, request.Branch, record)
		if err != nil {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		values = append(values, value)
		lastOrdinal, lastVersion = record.ordinal, record.version
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records, values = records[:pageSize], values[:pageSize]
	}
	page := &BranchHistoryPage{States: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot),
			Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrBranchStateRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

func (repository *BranchStateRepository) loadBranchHistoryPageRecords(ctx context.Context, organisationID, branchID, clause string,
	extraArguments ...any) ([]branchHistoryPageRecord, error) {
	arguments := append([]any{organisationID, branchID}, extraArguments...)
	rows, err := repository.executor.QueryContext(ctx, `SELECT append_ordinal,version,content_hash
		FROM bas_branch_versions_v1 WHERE organisation_id=? AND branch_id=? `+clause, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]branchHistoryPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			return nil, ErrSnapshotBound
		}
		var record branchHistoryPageRecord
		var hash []byte
		if err := rows.Scan(&record.ordinal, &record.version, &hash); err != nil {
			return nil, err
		}
		if record.ordinal <= 0 || record.version <= 0 {
			return nil, ErrSnapshotCorrupt
		}
		record.exact = &tammyv1.SourceRef{Type: "bas_branch_state", Id: branchID,
			Revision: uint64(record.version), ContentHash: bytes.Clone(hash)}
		if !validBASSourceRef(record.exact, "bas_branch_state", branchID) {
			return nil, ErrSnapshotCorrupt
		}
		records = append(records, record)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return records, nil
}

func (repository *BranchStateRepository) verifyBranchHistoryPageRecord(ctx context.Context, organisationID string,
	branch *tammyv1.SourceRef, record branchHistoryPageRecord) (*tammyv1.BasBranchState, error) {
	value, err := repository.GetExact(ctx, organisationID, record.exact)
	if err != nil {
		return nil, err
	}
	if value == nil || record.ordinal <= 0 || record.version <= 0 || value.Version != uint64(record.version) ||
		!sameBASSourceRef(branchStateReference(value), record.exact) || !sameBASSourceRef(value.BranchIdentity, branch) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
