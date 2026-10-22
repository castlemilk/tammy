/*
Task 9 addition to package baspreparation. Add these typed exact lookups to the
existing repository package. The ID+hash check distinguishes an absent exact
identity (Found=false) from a present-but-corrupt retained proof (error).
All SQL uses fixed module-owned tables and remains inside the caller transaction.
*/
package baspreparation

import (
	"bytes"
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

func basReadUniqueSnapshotHash(ctx context.Context, executor app.CommandSQLExecutor,
	query string, args ...any) ([]byte, bool, error) {
	if ctx == nil || executor == nil {
		return nil, false, ErrSnapshotCorrupt
	}
	rows, err := executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	if !rows.Next() {
		return nil, false, errors.Join(rows.Err(), rows.Close())
	}
	var hash []byte
	if err := rows.Scan(&hash); err != nil {
		return nil, false, errors.Join(err, rows.Close())
	}
	hash = bytes.Clone(hash)
	if rows.Next() {
		return nil, false, errors.Join(ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, false, err
	}
	if len(hash) != 32 {
		return nil, false, ErrSnapshotCorrupt
	}
	return hash, true, nil
}

func basExactSnapshotHashMatches(ctx context.Context, executor app.CommandSQLExecutor,
	query string, exact *tammyv1.SourceRef, args ...any) (bool, error) {
	hash, found, err := basReadUniqueSnapshotHash(ctx, executor, query, args...)
	if err != nil || !found {
		return false, err
	}
	return bytes.Equal(hash, exact.ContentHash), nil
}

func (repository *FrozenRepository) GetExactByHash(ctx context.Context, org string,
	exact *tammyv1.SourceRef) (*tammyv1.BasFrozenBas, bool, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, frozenSnapshotSpec.resource, "") || exact.Revision != 1 {
		return nil, false, ErrFrozenRepository
	}
	found, err := basExactSnapshotHashMatches(ctx, repository.executor,
		"SELECT CASE WHEN typeof(content_hash)='blob' AND length(content_hash)=32 THEN content_hash END FROM bas_frozen_bas_v1 WHERE organisation_id=? AND frozen_id=?", exact, org, exact.Id)
	if err != nil {
		return nil, false, errors.Join(ErrFrozenRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	value, err := repository.GetExact(ctx, org, exact)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (repository *ExternalObservationRepository) GetExactByHash(ctx context.Context, org string,
	exact *tammyv1.SourceRef) (*tammyv1.BasExternalObservation, bool, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, observationSnapshotSpec.resource, "") || exact.Revision != 1 {
		return nil, false, ErrExternalObservationRepository
	}
	found, err := basExactSnapshotHashMatches(ctx, repository.executor,
		"SELECT CASE WHEN typeof(content_hash)='blob' AND length(content_hash)=32 THEN content_hash END FROM bas_external_observations_v1 WHERE organisation_id=? AND observation_id=? "+
			"UNION ALL SELECT CASE WHEN typeof(content_hash)='blob' AND length(content_hash)=32 THEN content_hash END FROM bas_external_observation_corrections_v1 WHERE organisation_id=? AND correction_id=?",
		exact, org, exact.Id, org, exact.Id)
	if err != nil {
		return nil, false, errors.Join(ErrExternalObservationRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	value, err := repository.GetExact(ctx, org, exact)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

func (repository *DisputeResolutionRepository) GetExactByHash(ctx context.Context, org string,
	exact *tammyv1.SourceRef) (*tammyv1.BasExternalDisputeResolution, bool, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, "bas_external_dispute_resolution", "") || exact.Revision != 1 {
		return nil, false, ErrDisputeResolutionRepository
	}
	found, err := basExactSnapshotHashMatches(ctx, repository.executor,
		"SELECT CASE WHEN typeof(content_hash)='blob' AND length(content_hash)=32 THEN content_hash END FROM bas_dispute_resolutions_v1 WHERE organisation_id=? AND resolution_id=?", exact, org, exact.Id)
	if err != nil {
		return nil, false, errors.Join(ErrDisputeResolutionRepository, err)
	}
	if !found {
		return nil, false, nil
	}
	value, err := repository.GetExact(ctx, org, exact)
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}
