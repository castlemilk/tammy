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

type CalculationListRequest struct {
	OrganisationID string
	Preparation    *tammyv1.SourceRef
	PageSize       uint32
	Cursor         *string
}

type CalculationPage struct {
	Calculations []*tammyv1.BasCalculationSummary
	Page         *tammyv1.PageInfo
}

type calculationPageRecord struct {
	ordinal int64
	exact   *tammyv1.SourceRef
}

// List returns every retained calculation version for one preparation identity
// at or below a signed append-ordinal watermark. The full preparation
// reference is validated and bound into the cursor so a later owner revision
// cannot resume a cursor created for a different historical capture.
func (repository *CalculationRepository) List(ctx context.Context, request CalculationListRequest) (*CalculationPage, error) {
	if repository == nil || !repository.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) ||
		!validBASSourceRef(request.Preparation, preparationSnapshotSpec.resource, "") {
		return nil, ErrCalculationRepository
	}
	pageSize, validSize := basNormalizedPageSize(request.PageSize)
	if !validSize {
		return nil, ErrCalculationRepository
	}
	preparation, err := repository.preparations.GetExact(ctx, request.OrganisationID, request.Preparation)
	if err != nil {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	if preparation == nil || preparation.Preparation == nil ||
		preparation.Preparation.OrganisationId != request.OrganisationID ||
		preparation.Preparation.PreparationId != request.Preparation.Id ||
		!sameBASSourceRef(preparationReference(preparation.Preparation), request.Preparation) {
		return nil, errors.Join(ErrCalculationRepository, ErrSnapshotCorrupt)
	}

	queryHash := basPageQueryHash("bas_calculation_history_v1", request.OrganisationID, refKey(request.Preparation))
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		endpoints, err := repository.loadCalculationPageRecords(ctx,
			`c.organisation_id=? AND c.preparation_id=? ORDER BY c.append_ordinal DESC LIMIT 1`,
			request.OrganisationID, request.Preparation.Id)
		if err != nil {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
		if len(endpoints) == 0 {
			return &CalculationPage{Calculations: []*tammyv1.BasCalculationSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if len(endpoints) != 1 {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotCorrupt)
		}
		if _, err := repository.verifyCalculationPageRecord(ctx, request.OrganisationID, request.Preparation.Id, endpoints[0]); err != nil {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoints[0].ordinal, hash: bytes.Clone(endpoints[0].exact.ContentHash)}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrCalculationRepository
		}
		var valid bool
		snapshot, valid = basParsePageSnapshot(cursor.Snapshot)
		if !valid {
			return nil, ErrCalculationRepository
		}
		position, valid = basParsePositiveOrdinal(cursor.Position)
		if !valid || position > snapshot.ordinal {
			return nil, ErrCalculationRepository
		}
		endpoint, err := repository.loadCalculationPageRecords(ctx,
			`c.organisation_id=? AND c.preparation_id=? AND c.append_ordinal=? LIMIT 1`,
			request.OrganisationID, request.Preparation.Id, snapshot.ordinal)
		if err != nil || len(endpoint) != 1 || endpoint[0].ordinal != snapshot.ordinal ||
			!bytes.Equal(endpoint[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyCalculationPageRecord(ctx, request.OrganisationID, request.Preparation.Id, endpoint[0]); err != nil {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
		anchor, err := repository.loadCalculationPageRecords(ctx,
			`c.organisation_id=? AND c.preparation_id=? AND c.append_ordinal=? AND c.append_ordinal<=? LIMIT 1`,
			request.OrganisationID, request.Preparation.Id, position, snapshot.ordinal)
		if err != nil || len(anchor) != 1 || anchor[0].ordinal != position {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyCalculationPageRecord(ctx, request.OrganisationID, request.Preparation.Id, anchor[0]); err != nil {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
	}

	records, err := repository.loadCalculationPageRecords(ctx,
		`c.organisation_id=? AND c.preparation_id=? AND c.append_ordinal>? AND c.append_ordinal<=? ORDER BY c.append_ordinal LIMIT ?`,
		request.OrganisationID, request.Preparation.Id, position, snapshot.ordinal, pageSize+1)
	if err != nil || len(records) > pageSize+1 {
		return nil, errors.Join(ErrCalculationRepository, err)
	}
	values := make([]*tammyv1.BasCalculationSummary, 0, len(records))
	lastOrdinal := position
	for _, record := range records {
		if record.ordinal <= lastOrdinal || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrCalculationRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyCalculationPageRecord(ctx, request.OrganisationID, request.Preparation.Id, record)
		if err != nil {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
		values = append(values, calculationSummary(value))
		lastOrdinal = record.ordinal
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records, values = records[:pageSize], values[:pageSize]
	}
	page := &CalculationPage{Calculations: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot),
			Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrCalculationRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

func (repository *CalculationRepository) loadCalculationPageRecords(ctx context.Context, clause string, arguments ...any) ([]calculationPageRecord, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT c.append_ordinal,c.calculation_id,c.version,c.content_hash
		FROM bas_calculations_v1 c WHERE `+clause, arguments...)
	if err != nil {
		return nil, err
	}
	records := make([]calculationPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			_ = rows.Close()
			return nil, ErrSnapshotBound
		}
		var ordinal, version int64
		var calculationID string
		var contentHash []byte
		if err := rows.Scan(&ordinal, &calculationID, &version, &contentHash); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if ordinal <= 0 || version <= 0 {
			_ = rows.Close()
			return nil, ErrSnapshotCorrupt
		}
		exact := &tammyv1.SourceRef{Type: calculationSnapshotSpec.resource, Id: calculationID,
			Revision: uint64(version), ContentHash: bytes.Clone(contentHash)}
		if !validBASSourceRef(exact, calculationSnapshotSpec.resource, "") {
			_ = rows.Close()
			return nil, ErrSnapshotCorrupt
		}
		if len(records) != 0 && ordinal <= records[len(records)-1].ordinal {
			_ = rows.Close()
			return nil, ErrSnapshotCorrupt
		}
		records = append(records, calculationPageRecord{ordinal: ordinal, exact: exact})
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil || closeErr != nil {
		return nil, errors.Join(rowsErr, closeErr)
	}
	return records, nil
}

func (repository *CalculationRepository) verifyCalculationPageRecord(ctx context.Context, organisationID, preparationID string,
	record calculationPageRecord) (*tammyv1.BasCalculation, error) {
	value, err := repository.GetExact(ctx, organisationID, record.exact)
	if err != nil {
		return nil, err
	}
	if value == nil || record.ordinal <= 0 || value.OrganisationId != organisationID ||
		value.CalculationId != record.exact.Id || value.Version != record.exact.Revision ||
		value.Preparation == nil || value.Preparation.Id != preparationID {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}

func calculationSummary(value *tammyv1.BasCalculation) *tammyv1.BasCalculationSummary {
	return &tammyv1.BasCalculationSummary{Calculation: calculationReference(value), Preparation: cloneRef(value.Preparation),
		Family: cloneRef(value.Family), Period: cloneRef(value.Period), Outcome: value.Result.Outcome,
		CreatedAt: proto.Clone(value.CreatedAt).(*timestamppb.Timestamp)}
}
