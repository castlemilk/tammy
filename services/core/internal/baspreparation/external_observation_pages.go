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

type ExternalObservationListRequest struct {
	OrganisationID string
	Frozen         *tammyv1.SourceRef
	PageSize       uint32
	Cursor         *string
}

type ExternalObservationPage struct {
	Observations []*tammyv1.BasExternalObservationSummary
	Page         *tammyv1.PageInfo
}

type observationPageRecord struct {
	ordinal int64
	exact   *tammyv1.SourceRef
}

// List includes ordinary assertions and every retained correction for the exact
// frozen approval, using Core append order rather than claimed event time.
func (repository *ExternalObservationRepository) List(ctx context.Context, request ExternalObservationListRequest) (*ExternalObservationPage, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(request.OrganisationID) || !validBASSourceRef(request.Frozen, frozenSnapshotSpec.resource, "") || request.Frozen.Revision != 1 {
		return nil, ErrExternalObservationRepository
	}
	pageSize, ok := basNormalizedPageSize(request.PageSize)
	if !ok {
		return nil, ErrExternalObservationRepository
	}
	frozen, err := repository.frozen.GetExact(ctx, request.OrganisationID, request.Frozen)
	if err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	if err := repository.verifyObservationRouting(ctx, request.OrganisationID, frozen.Family); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
	}
	queryHash := basPageQueryHash("bas_external_observation_history_v1", request.OrganisationID, refKey(request.Frozen))
	snapshot, position := basPageSnapshot{}, int64(0)
	if request.Cursor == nil {
		endpoint, err := repository.loadObservationPageRecords(ctx, request, "ORDER BY e.event_ordinal DESC LIMIT 1")
		if err != nil {
			return nil, errors.Join(ErrExternalObservationRepository, err)
		}
		if len(endpoint) == 0 {
			return &ExternalObservationPage{Observations: []*tammyv1.BasExternalObservationSummary{}, Page: &tammyv1.PageInfo{}}, nil
		}
		if len(endpoint) != 1 {
			return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt)
		}
		if _, err := repository.verifyObservationPageRecord(ctx, request, endpoint[0]); err != nil {
			return nil, errors.Join(ErrExternalObservationRepository, err)
		}
		snapshot = basPageSnapshot{ordinal: endpoint[0].ordinal, hash: bytes.Clone(endpoint[0].exact.ContentHash)}
	} else {
		cursor, err := repository.cursors.Decode(*request.Cursor, queryHash)
		if err != nil {
			return nil, ErrExternalObservationRepository
		}
		snapshot, ok = basParsePageSnapshot(cursor.Snapshot)
		if !ok {
			return nil, ErrExternalObservationRepository
		}
		position, ok = basParsePositiveOrdinal(cursor.Position)
		if !ok || position >= snapshot.ordinal {
			return nil, ErrExternalObservationRepository
		}
		endpoint, err := repository.loadObservationPageRecords(ctx, request, "AND e.event_ordinal=? LIMIT 1", snapshot.ordinal)
		if err != nil || len(endpoint) != 1 || !bytes.Equal(endpoint[0].exact.ContentHash, snapshot.hash) {
			return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyObservationPageRecord(ctx, request, endpoint[0]); err != nil {
			return nil, errors.Join(ErrExternalObservationRepository, err)
		}
		anchor, err := repository.loadObservationPageRecords(ctx, request, "AND e.event_ordinal=? LIMIT 1", position)
		if err != nil || len(anchor) != 1 {
			return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
		}
		if _, err := repository.verifyObservationPageRecord(ctx, request, anchor[0]); err != nil {
			return nil, errors.Join(ErrExternalObservationRepository, err)
		}
	}
	records, err := repository.loadObservationPageRecords(ctx, request, "AND e.event_ordinal>? AND e.event_ordinal<=? ORDER BY e.event_ordinal LIMIT ?", position, snapshot.ordinal, pageSize+1)
	if err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	if len(records) == 0 || len(records) > pageSize+1 {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt)
	}
	values := make([]*tammyv1.BasExternalObservationSummary, 0, len(records))
	last := position
	for _, record := range records {
		if record.ordinal <= last || record.ordinal > snapshot.ordinal {
			return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt)
		}
		value, err := repository.verifyObservationPageRecord(ctx, request, record)
		if err != nil {
			return nil, errors.Join(ErrExternalObservationRepository, err)
		}
		summary := &tammyv1.BasExternalObservationSummary{Observation: cloneRef(record.exact), FrozenBas: cloneRef(value.FrozenBas), Trust: value.Trust,
			RecordedAt: proto.Clone(value.RecordedAt).(*timestamppb.Timestamp), CorrectsObservation: cloneRef(value.CorrectsObservation)}
		if input := value.GetObservation(); input != nil {
			summary.EventKind = input.EventKind.Enum()
		} else {
			summary.CorrectionKind = value.GetCorrection().CorrectionKind.Enum()
		}
		values = append(values, summary)
		last = record.ordinal
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records, values = records[:pageSize], values[:pageSize]
	}
	page := &ExternalObservationPage{Observations: values, Page: &tammyv1.PageInfo{ReturnedCount: uint32(len(values))}}
	if hasMore {
		token, err := repository.cursors.Encode(paging.Cursor{Snapshot: formatBASPageSnapshot(snapshot), Position: strconv.FormatInt(records[len(records)-1].ordinal, 10), QueryHash: queryHash})
		if err != nil || token == "" {
			return nil, errors.Join(ErrExternalObservationRepository, err)
		}
		page.Page.NextCursor = &token
	}
	return page, nil
}

// Filtering on a damaged normalized frozen tuple must not silently omit the
// body-pinned original history. The accepted family bound permits a complete
// metadata census before filtering; unrelated parents are read only when used.
func (repository *ExternalObservationRepository) verifyObservationRouting(ctx context.Context, org string, family *tammyv1.SourceRef) error {
	const limit = 128 * 1024
	rows, err := repository.executor.QueryContext(ctx, `SELECT e.event_kind,e.body_hash,
		CASE WHEN length(e.event_proto)<=? THEN e.event_proto ELSE NULL END,length(e.event_proto),
		r.id,r.content_hash,CASE WHEN length(r.wire)<=? THEN r.wire ELSE NULL END,length(r.wire),r.frozen_id,r.frozen_hash
		FROM bas_family_events_v1 e LEFT JOIN (
		SELECT observation_id AS id,event_id,organisation_id,family_id,frozen_id,frozen_hash,content_hash,observation_proto AS wire FROM bas_external_observations_v1
		UNION ALL SELECT correction_id AS id,event_id,organisation_id,family_id,frozen_id,frozen_hash,content_hash,correction_proto AS wire FROM bas_external_observation_corrections_v1
		) r ON r.event_id=e.event_id AND r.organisation_id=e.organisation_id AND r.family_id=e.family_id
		WHERE e.organisation_id=? AND e.family_id=? AND e.event_kind IN ('OBSERVATION','CORRECTION')
		ORDER BY e.event_ordinal LIMIT ?`, limit, limit, org, family.Id, maxExternalHistoryRecords+1)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		count++
		if count > maxExternalHistoryRecords {
			return errors.Join(ErrSnapshotBound, rows.Close())
		}
		var kind, id, frozenID string
		var eventHash, eventWire, hash, wire, frozenHash []byte
		var eventLength, length int64
		if err := rows.Scan(&kind, &eventHash, &eventWire, &eventLength, &id, &hash, &wire, &length, &frozenID, &frozenHash); err != nil {
			return errors.Join(err, rows.Close())
		}
		if err := errors.Join(repositorySnapshotBytes(eventWire, eventLength, limit), repositorySnapshotBytes(wire, length, limit)); err != nil {
			return errors.Join(err, rows.Close())
		}
		exact := &tammyv1.SourceRef{Type: observationSnapshotSpec.resource, Id: id, Revision: 1, ContentHash: hash}
		value, err := openObservation(wire, org, exact)
		if err != nil || !bytes.Equal(hash, eventHash) || !bytes.Equal(wire, eventWire) ||
			value == nil || !validBASSourceRef(value.FrozenBas, frozenSnapshotSpec.resource, "") || value.FrozenBas.Revision != 1 ||
			value.FrozenBas.Id != frozenID || !bytes.Equal(value.FrozenBas.ContentHash, frozenHash) ||
			(kind == "OBSERVATION") != (value.GetObservation() != nil) {
			return errors.Join(ErrSnapshotCorrupt, err, rows.Close())
		}
	}
	return errors.Join(rows.Err(), rows.Close())
}

func (repository *ExternalObservationRepository) loadObservationPageRecords(ctx context.Context, request ExternalObservationListRequest, clause string, extra ...any) ([]observationPageRecord, error) {
	args := []any{request.OrganisationID, request.Frozen.Id, request.Frozen.ContentHash}
	args = append(args, extra...)
	rows, err := repository.executor.QueryContext(ctx, `SELECT e.event_ordinal,r.id,r.content_hash FROM bas_family_events_v1 e JOIN (
		SELECT observation_id AS id,event_id,organisation_id,family_id,frozen_id,frozen_hash,content_hash FROM bas_external_observations_v1
		UNION ALL SELECT correction_id AS id,event_id,organisation_id,family_id,frozen_id,frozen_hash,content_hash FROM bas_external_observation_corrections_v1
		) r ON r.event_id=e.event_id AND r.organisation_id=e.organisation_id AND r.family_id=e.family_id
		WHERE e.organisation_id=? AND r.frozen_id=? AND r.frozen_hash=? `+clause, args...)
	if err != nil {
		return nil, err
	}
	records := make([]observationPageRecord, 0)
	for rows.Next() {
		if len(records) >= basRepositoryPageSizeMaximum+1 {
			return nil, errors.Join(ErrSnapshotBound, rows.Close())
		}
		var record observationPageRecord
		var id string
		var hash []byte
		if err := rows.Scan(&record.ordinal, &id, &hash); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		record.exact = &tammyv1.SourceRef{Type: observationSnapshotSpec.resource, Id: id, Revision: 1, ContentHash: bytes.Clone(hash)}
		if record.ordinal <= 0 || !validBASSourceRef(record.exact, observationSnapshotSpec.resource, "") {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		records = append(records, record)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return records, nil
}

func (repository *ExternalObservationRepository) verifyObservationPageRecord(ctx context.Context, request ExternalObservationListRequest, record observationPageRecord) (*tammyv1.BasExternalObservation, error) {
	value, err := repository.GetExact(ctx, request.OrganisationID, record.exact)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ObservationId != record.exact.Id || !bytes.Equal(value.ContentHash, record.exact.ContentHash) || !sameBASSourceRef(value.FrozenBas, request.Frozen) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
