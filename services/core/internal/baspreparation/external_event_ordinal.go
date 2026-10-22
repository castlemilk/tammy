package baspreparation

import (
	"context"
	"errors"
	"math"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

// AppendWithOrdinal keeps the old API stable. The ordinal is the actual global
// AUTOINCREMENT assigned by SQLite, read by the exact event/family/body tuple
// within the caller-owned transaction.
func (repository *ExternalObservationRepository) AppendWithOrdinal(ctx context.Context,
	org, operationKey, eventID string, value *tammyv1.BasExternalObservation,
) (*tammyv1.SourceRef, uint64, error) {
	ref, err := repository.Append(ctx, org, operationKey, eventID, value)
	if err != nil {
		return nil, 0, err
	}
	frozen, err := repository.frozen.GetExact(ctx, org, value.FrozenBas)
	if err != nil || frozen == nil {
		return nil, 0, errors.Join(ErrExternalObservationRepository, err)
	}
	kind := "OBSERVATION"
	if value.GetCorrection() != nil {
		kind = "CORRECTION"
	}
	ordinal, err := readBASFamilyEventOrdinal(ctx, repository.executor, eventID, org, frozen.Family.Id, kind, ref.ContentHash)
	if err != nil {
		return nil, 0, errors.Join(ErrExternalObservationRepository, err)
	}
	return ref, ordinal, nil
}

func (repository *DisputeResolutionRepository) AppendWithOrdinal(ctx context.Context,
	org string, family *tammyv1.SourceRef, operationKey, eventID string,
	value *tammyv1.BasExternalDisputeResolution,
) (*tammyv1.SourceRef, uint64, error) {
	ref, err := repository.Append(ctx, org, family, operationKey, eventID, value)
	if err != nil {
		return nil, 0, err
	}
	ordinal, err := readBASFamilyEventOrdinal(ctx, repository.executor, eventID, org, family.Id, "RESOLUTION", ref.ContentHash)
	if err != nil {
		return nil, 0, errors.Join(ErrDisputeResolutionRepository, err)
	}
	return ref, ordinal, nil
}

func readBASFamilyEventOrdinal(ctx context.Context, executor app.CommandSQLExecutor,
	eventID, organisationID, familyID, kind string, bodyHash []byte) (uint64, error) {
	if nilBASRepositoryExecutor(executor) || len(bodyHash) != 32 ||
		(kind != "OBSERVATION" && kind != "CORRECTION" && kind != "RESOLUTION") {
		return 0, ErrSnapshotCorrupt
	}
	rows, err := executor.QueryContext(ctx, `SELECT event_ordinal FROM bas_family_events_v1
		WHERE event_id=? AND organisation_id=? AND family_id=? AND event_kind=? AND body_hash=?`,
		eventID, organisationID, familyID, kind, bodyHash)
	if err != nil {
		return 0, err
	}
	var ordinal int64
	if !rows.Next() {
		return 0, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&ordinal); err != nil {
		return 0, errors.Join(ErrSnapshotCorrupt, err, rows.Close())
	}
	if rows.Next() {
		return 0, errors.Join(ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return 0, err
	}
	if ordinal <= 0 || ordinal > math.MaxInt64 {
		return 0, ErrSnapshotCorrupt
	}
	return uint64(ordinal), nil
}
