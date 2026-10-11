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

var ErrDisputeResolutionRepository = errors.New("baspreparation: dispute resolution repository operation failed")

// DisputeResolutionRepository atomically retains a Core event, resolution and
// its exact subject/dependency links. The caller owns live policy, authority,
// fresh-factor consumption, workflow CAS, audit and the outer transaction.
type DisputeResolutionRepository struct{ lifecycleRepository }

func NewDisputeResolutionRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*DisputeResolutionRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrDisputeResolutionRepository
	}
	return &DisputeResolutionRepository{lifecycleRepository{executor, cursors}}, nil
}

func (repository *DisputeResolutionRepository) ready(ctx context.Context) bool {
	return repository != nil && repository.lifecycleRepository.ready(ctx)
}

func (repository *DisputeResolutionRepository) Append(ctx context.Context, org string, family *tammyv1.SourceRef,
	operationKey, eventID string, value *tammyv1.BasExternalDisputeResolution) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(eventID) || !validBASOperationKey(operationKey) ||
		!validBASSourceRef(family, "bas_family", "") || family.Revision != 1 || value == nil || !sameBASSourceRef(value.Family, family) {
		return nil, ErrDisputeResolutionRepository
	}
	identity, err := repository.family(ctx, org, family)
	if err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err)
	}
	wire, hash, err := sealResolution(value, identity)
	if err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err)
	}
	resolvedAt, ok := basTimestampSQLText(value.ResolvedAt)
	if !ok || !bytes.Equal(dependencyRefsManifest(value.Resolution.AffectedConfirmedChain), value.Resolution.DependentChainManifestHash) {
		return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt)
	}
	subjectKinds := make(map[string]string, len(value.Resolution.DisputedObservations))
	for _, ref := range value.Resolution.DisputedObservations {
		if !validBASSourceRef(ref, observationSnapshotSpec.resource, "") || ref.Revision != 1 || subjectKinds[ref.Id] != "" {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt)
		}
		_, kind, ordinal, err := repository.readObservationProof(ctx, org, family, ref)
		if err != nil || ordinal <= 0 {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		if err := repository.verifyFamilyEvent(ctx, org, family, uint64(ordinal)); err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		subjectKinds[ref.Id] = kind
	}
	dependencies := make(map[string]bool, len(value.Resolution.AffectedConfirmedChain))
	for _, ref := range value.Resolution.AffectedConfirmedChain {
		if !validBASSourceRef(ref, "bas_preparation", "") || dependencies[ref.Id] {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt)
		}
		retained, err := repository.preparation(ctx, org, family, ref)
		if err != nil || retained == nil || retained.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED || retained.Preparation.FrozenBas == nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		if err := repository.verifyFrozenBarrier(ctx, org, family, retained.Preparation, retained.Preparation.FrozenBas); err != nil {
			return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
		}
		dependencies[ref.Id] = true
	}
	if err := repository.checkFamilyEventCapacity(ctx, org, family); err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err)
	}
	const savepoint = "bas_dispute_resolution_append"
	if _, err := repository.executor.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err)
	}
	failed := func(err error) (*tammyv1.SourceRef, error) {
		return nil, errors.Join(ErrDisputeResolutionRepository, err, rollbackRepositorySavepoint(ctx, repository.executor, savepoint))
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_family_events_v1(
		event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at)
		VALUES(?,?,?,'RESOLUTION',?,?,?,?,?)`, eventID, org, family.Id, operationKey, resolvedAt, wire, hash, resolvedAt); err != nil {
		return failed(err)
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_dispute_resolutions_v1(
		resolution_id,event_id,organisation_id,family_id,outcome,dependent_chain_manifest_hash,reason,actor_user_id,resolved_at,content_hash,resolution_proto)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, value.ResolutionId, eventID, org, family.Id,
		enumSQL(value.Resolution.Outcome.String(), "BAS_DISPUTE_RESOLUTION_OUTCOME_"), value.Resolution.DependentChainManifestHash,
		value.Resolution.Reason, value.ActorUserId, resolvedAt, hash, wire); err != nil {
		return failed(err)
	}
	for _, ref := range value.Resolution.DisputedObservations {
		var observationID, observationHash, correctionID, correctionHash any
		if subjectKinds[ref.Id] == "OBSERVATION" {
			observationID, observationHash = ref.Id, ref.ContentHash
		} else {
			correctionID, correctionHash = ref.Id, ref.ContentHash
		}
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_dispute_resolution_observations_v1(
			resolution_id,organisation_id,family_id,subject_kind,subject_id,subject_hash,observation_id,observation_hash,correction_id,correction_hash)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, value.ResolutionId, org, family.Id, subjectKinds[ref.Id], ref.Id, ref.ContentHash,
			observationID, observationHash, correctionID, correctionHash); err != nil {
			return failed(err)
		}
	}
	for _, ref := range value.Resolution.AffectedConfirmedChain {
		if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_dispute_resolution_preparations_v1(
			resolution_id,organisation_id,family_id,preparation_id,preparation_version,preparation_hash) VALUES(?,?,?,?,?,?)`,
			value.ResolutionId, org, family.Id, ref.Id, int64(ref.Revision), ref.ContentHash); err != nil {
			return failed(err)
		}
	}
	exact := &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: value.ResolutionId, Revision: 1, ContentHash: bytes.Clone(hash)}
	if _, err := repository.GetExact(ctx, org, exact); err != nil {
		return failed(err)
	}
	if _, err := repository.executor.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return failed(err)
	}
	return exact, nil
}

func (repository *DisputeResolutionRepository) GetExact(ctx context.Context, org string, exact *tammyv1.SourceRef) (*tammyv1.BasExternalDisputeResolution, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, "bas_external_dispute_resolution", "") || exact.Revision != 1 {
		return nil, ErrDisputeResolutionRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT r.family_id,i.identity_hash,e.event_ordinal
		FROM bas_dispute_resolutions_v1 r JOIN bas_family_identities_v1 i ON i.family_id=r.family_id AND i.organisation_id=r.organisation_id
		JOIN bas_family_events_v1 e ON e.event_id=r.event_id AND e.organisation_id=r.organisation_id AND e.family_id=r.family_id
		WHERE r.organisation_id=? AND r.resolution_id=?`, org, exact.Id)
	if err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err)
	}
	var familyID string
	var familyHash []byte
	var ordinal int64
	if !rows.Next() {
		return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&familyID, &familyHash, &ordinal); err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, err)
	}
	family := &tammyv1.SourceRef{Type: "bas_family", Id: familyID, Revision: 1, ContentHash: bytes.Clone(familyHash)}
	if ordinal <= 0 {
		return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt)
	}
	value, err := repository.readResolutionProofForEvent(ctx, org, family, exact, ordinal)
	if err != nil {
		return nil, errors.Join(ErrDisputeResolutionRepository, ErrSnapshotCorrupt, err)
	}
	return value, nil
}
