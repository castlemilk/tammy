package baspreparation

import (
	"bytes"
	"context"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var (
	ErrSavedPaygRepository = errors.New("baspreparation: saved PAYG repository operation failed")
	ErrSavedPaygConflict   = errors.New("baspreparation: saved PAYG inputs conflict with the current preparation")
)

// SavedPaygRepository retains pairs pinned to an exact input-bearing owner,
// before a successor adds the envelope pointer in the same transaction. Actor,
// authorization, audit, replay and transaction completion remain command-owned.
type SavedPaygRepository struct {
	executor app.CommandSQLExecutor
	owners   *PreparationReader
}

func NewSavedPaygRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*SavedPaygRepository, error) {
	owners, err := NewPreparationReader(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	return &SavedPaygRepository{executor: executor, owners: owners}, nil
}

func (repository *SavedPaygRepository) Append(ctx context.Context, value *tammyv1.BasSavedPaygInputs) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || value == nil {
		return nil, ErrSavedPaygRepository
	}
	wire, hash, err := sealSavedPaygInputs(value)
	if err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	owner, err := repository.owners.GetCurrent(ctx, value.OrganisationId, value.PreparationId)
	if err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	prep := owner.Preparation
	if !sameBASSourceRef(value.Preparation, preparationReference(prep)) || value.InputRevision != prep.InputRevision ||
		!bytes.Equal(value.InputHash, prep.InputHash) ||
		(prep.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING &&
			prep.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED &&
			prep.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY) {
		return nil, ErrSavedPaygConflict
	}
	createdAt, ok := basTimestampSQLText(value.RecordedAt)
	if !ok {
		return nil, ErrSavedPaygRepository
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_payg_input_versions_v1(
		input_snapshot_id,preparation_id,organisation_id,family_id,preparation_version,preparation_hash,
		input_revision,input_hash,content_hash,input_proto,actor_user_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		value.InputSnapshotId, value.PreparationId, value.OrganisationId, prep.Family.Id, int64(value.Preparation.Revision), value.Preparation.ContentHash,
		int64(value.InputRevision), value.InputHash, hash, wire, value.ActorUserId, createdAt); err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	return &tammyv1.SourceRef{Type: savedPaygSnapshotSpec.resource, Id: value.InputSnapshotId,
		Revision: value.InputRevision, ContentHash: bytes.Clone(hash)}, nil
}

// GetExact verifies the complete saved pair and its exact historical owner. A
// newer current preparation never redirects a retained input/evidence record.
func (repository *SavedPaygRepository) GetExact(ctx context.Context, organisationID string,
	exact *tammyv1.SourceRef) (*tammyv1.BasSavedPaygInputs, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(organisationID) || !validBASSourceRef(exact, savedPaygSnapshotSpec.resource, "") {
		return nil, ErrSavedPaygRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT input_snapshot_id,preparation_id,organisation_id,family_id,
		preparation_version,preparation_hash,input_revision,input_hash,content_hash,
		CASE WHEN length(input_proto)<=? THEN input_proto ELSE NULL END,length(input_proto),actor_user_id,created_at
		FROM bas_payg_input_versions_v1 WHERE organisation_id=? AND input_snapshot_id=?`,
		savedPaygSnapshotSpec.maxBytes, organisationID, exact.Id)
	if err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrSavedPaygRepository, rows.Err(), rows.Close())
	}
	var record savedPaygReadRecord
	if err := rows.Scan(&record.id, &record.preparationID, &record.org, &record.familyID,
		&record.preparationVersion, &record.preparationHash, &record.inputRevision, &record.inputHash,
		&record.hash, &record.wire, &record.wireLength, &record.actor, &record.createdAt); err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	if rows.Next() {
		return nil, errors.Join(ErrSavedPaygRepository, ErrSnapshotCorrupt)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	value, err := openSavedPaygReadRecord(record, exact)
	if err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	owner, err := repository.owners.GetExact(ctx, organisationID, value.Preparation)
	if err != nil {
		return nil, errors.Join(ErrSavedPaygRepository, err)
	}
	if owner.Preparation.Family.Id != record.familyID || value.InputRevision != owner.Preparation.InputRevision ||
		!bytes.Equal(value.InputHash, owner.Preparation.InputHash) {
		return nil, errors.Join(ErrSavedPaygRepository, ErrSnapshotCorrupt)
	}
	return value, nil
}

func (repository *SavedPaygRepository) ready(ctx context.Context) bool {
	return repository != nil && !nilBASRepositoryExecutor(repository.executor) && repository.owners != nil &&
		ctx != nil && ctx.Err() == nil
}

type savedPaygReadRecord struct {
	id, preparationID, org, familyID, actor, createdAt string
	preparationVersion, inputRevision, wireLength      int64
	preparationHash, inputHash, hash, wire             []byte
}

func openSavedPaygReadRecord(record savedPaygReadRecord, exact *tammyv1.SourceRef) (*tammyv1.BasSavedPaygInputs, error) {
	if record.wireLength > int64(savedPaygSnapshotSpec.maxBytes) {
		return nil, ErrSnapshotBound
	}
	if record.wireLength <= 0 || int64(len(record.wire)) != record.wireLength || record.preparationVersion <= 0 || record.inputRevision <= 0 ||
		!sameBASSourceRef(exact, &tammyv1.SourceRef{Type: savedPaygSnapshotSpec.resource, Id: record.id,
			Revision: uint64(record.inputRevision), ContentHash: record.hash}) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openSavedPaygInputs(record.wire, record.org, exact)
	if err != nil {
		return nil, err
	}
	createdAt, ok := basTimestampSQLText(value.RecordedAt)
	if !ok || createdAt != record.createdAt || value.ActorUserId != record.actor || value.PreparationId != record.preparationID ||
		!bytes.Equal(value.InputHash, record.inputHash) ||
		!sameBASSourceRef(value.Preparation, &tammyv1.SourceRef{Type: preparationSnapshotSpec.resource, Id: record.preparationID,
			Revision: uint64(record.preparationVersion), ContentHash: record.preparationHash}) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
