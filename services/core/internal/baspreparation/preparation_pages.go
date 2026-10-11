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
)

type PreparationListRequest struct {
	OrganisationID string
	Family         *tammyv1.SourceRef
	PageSize       uint32
	Cursor         *string
}

type PreparationPage struct {
	Preparations []*tammyv1.BasPreparationSummary
	Page         *tammyv1.PageInfo
}

type preparationPageRecord struct {
	ordinal int64
	exact   *tammyv1.SourceRef
}

// List returns each attempt once, at its latest retained version within a
// signed append watermark. Subsequent workflow changes never redirect unseen
// rows or repeat attempts already returned by the same cursor.
func (repository *PreparationRepository) List(ctx context.Context, request PreparationListRequest) (*PreparationPage, error) {
	if repository == nil || repository.reader == nil || !repository.reader.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) {
		return nil, ErrPreparationRepository
	}
	pageSize, validSize := basNormalizedPageSize(request.PageSize)
	if !validSize {
		return nil, ErrPreparationRepository
	}
	filter := `v.organisation_id=?`
	arguments := []any{request.OrganisationID}
	if request.Family != nil {
		if !validBASSourceRef(request.Family, "bas_family", "") || request.Family.Revision != 1 {
			return nil, ErrPreparationRepository
		}
		families, err := NewFamilyIdentityRepository(repository.executor)
		if err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		if _, err := families.GetExact(ctx, request.OrganisationID, request.Family); err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		filter += ` AND v.family_id=?`
		arguments = append(arguments, request.Family.Id)
	}
	queryHash := basPageQueryHash("bas_preparation_attempts_v1", request.OrganisationID, refKey(request.Family))
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		endpoints, err := repository.loadPreparationPageRecords(ctx, filter+` ORDER BY v.append_ordinal DESC LIMIT 1`, arguments)
		if err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		if len(endpoints) == 0 {
			return &PreparationPage{Preparations: []*tammyv1.BasPreparationSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if _, err := repository.verifyPreparationPageRecord(ctx, request.OrganisationID, endpoints[0]); err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoints[0].ordinal, hash: bytes.Clone(endpoints[0].exact.ContentHash)}
	} else {
		cursor, err := repository.reader.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		var valid bool
		snapshot, valid = basParsePageSnapshot(cursor.Snapshot)
		if !valid {
			return nil, ErrPreparationRepository
		}
		position, valid = basParsePositiveOrdinal(cursor.Position)
		if !valid || position > snapshot.ordinal {
			return nil, ErrPreparationRepository
		}
		endpointArgs := append(append([]any{}, arguments...), snapshot.ordinal)
		endpoints, err := repository.loadPreparationPageRecords(ctx, filter+` AND v.append_ordinal=? LIMIT 1`, endpointArgs)
		if err != nil || len(endpoints) != 1 || !bytes.Equal(endpoints[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrPreparationRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyPreparationPageRecord(ctx, request.OrganisationID, endpoints[0]); err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		anchorArgs := append(append([]any{}, arguments...), position, snapshot.ordinal)
		anchors, err := repository.loadPreparationPageRecords(ctx, filter+` AND v.append_ordinal=? AND NOT EXISTS(
			SELECT 1 FROM bas_preparation_versions_v1 later WHERE later.preparation_id=v.preparation_id AND later.append_ordinal<=? AND later.version>v.version) LIMIT 1`, anchorArgs)
		if err != nil || len(anchors) != 1 {
			return nil, errors.Join(ErrPreparationRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyPreparationPageRecord(ctx, request.OrganisationID, anchors[0]); err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
	}
	pageArguments := append(append([]any{}, arguments...), position, snapshot.ordinal, snapshot.ordinal, pageSize+1)
	records, err := repository.loadPreparationPageRecords(ctx, filter+` AND v.append_ordinal>? AND v.append_ordinal<=? AND NOT EXISTS(
		SELECT 1 FROM bas_preparation_versions_v1 later WHERE later.preparation_id=v.preparation_id AND later.append_ordinal<=? AND later.version>v.version)
		ORDER BY v.append_ordinal LIMIT ?`, pageArguments)
	if err != nil || len(records) > pageSize+1 {
		return nil, errors.Join(ErrPreparationRepository, err)
	}
	values := make([]*tammyv1.BasPreparationSummary, 0, len(records))
	lastOrdinal := position
	for _, record := range records {
		if record.ordinal <= lastOrdinal || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrPreparationRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyPreparationPageRecord(ctx, request.OrganisationID, record)
		if err != nil {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		values = append(values, preparationSummary(value.Preparation))
		lastOrdinal = record.ordinal
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records, values = records[:pageSize], values[:pageSize]
	}
	page := &PreparationPage{Preparations: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.reader.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot),
			Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrPreparationRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

func (repository *PreparationRepository) loadPreparationPageRecords(ctx context.Context, clause string, arguments []any) ([]preparationPageRecord, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT v.append_ordinal,v.preparation_id,v.version,v.content_hash FROM bas_preparation_versions_v1 v WHERE `+clause, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]preparationPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			return nil, ErrSnapshotBound
		}
		var ordinal, version int64
		var id string
		var hash []byte
		if err := rows.Scan(&ordinal, &id, &version, &hash); err != nil {
			return nil, err
		}
		if ordinal <= 0 || version <= 0 {
			return nil, ErrSnapshotCorrupt
		}
		exact := &tammyv1.SourceRef{Type: "bas_preparation", Id: id, Revision: uint64(version), ContentHash: bytes.Clone(hash)}
		if !validBASSourceRef(exact, "bas_preparation", "") {
			return nil, ErrSnapshotCorrupt
		}
		records = append(records, preparationPageRecord{ordinal: ordinal, exact: exact})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return records, nil
}

func (repository *PreparationRepository) verifyPreparationPageRecord(ctx context.Context, organisationID string,
	record preparationPageRecord) (*PreparationRetainedSnapshot, error) {
	value, err := repository.GetExact(ctx, organisationID, record.exact)
	if err != nil {
		return nil, err
	}
	if err := repository.verifyPreparationDependencies(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func preparationSummary(value *tammyv1.BasPreparation) *tammyv1.BasPreparationSummary {
	value = proto.Clone(value).(*tammyv1.BasPreparation)
	return &tammyv1.BasPreparationSummary{Preparation: preparationReference(value), Family: value.Family, Period: value.Period,
		Profile: value.Profile, State: value.State, CurrentCalculation: value.CurrentCalculation, FrozenBas: value.FrozenBas,
		Predecessor: value.Predecessor, DisputeOverlay: value.DisputeOverlay, UpdatedAt: value.UpdatedAt,
		InputRevision: value.InputRevision, InputHash: value.InputHash}
}
