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

var ErrExternalObservationRepository = errors.New("baspreparation: external observation repository operation failed")

// ExternalObservationRepository stores the Core event and its typed retained
// observation atomically in a caller-owned transaction. The command applies
// transition policy, family/preparation CAS, authority, audit and replay.
type ExternalObservationRepository struct {
	lifecycleRepository
	frozen *FrozenRepository
}

func NewExternalObservationRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*ExternalObservationRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrExternalObservationRepository
	}
	frozen, err := NewFrozenRepository(executor, cursors)
	if err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	return &ExternalObservationRepository{lifecycleRepository: lifecycleRepository{executor, cursors}, frozen: frozen}, nil
}

func (repository *ExternalObservationRepository) ready(ctx context.Context) bool {
	return repository != nil && repository.frozen != nil && repository.lifecycleRepository.ready(ctx)
}

func (repository *ExternalObservationRepository) Append(ctx context.Context, org, operationKey, eventID string, value *tammyv1.BasExternalObservation) (*tammyv1.SourceRef, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !ids.IsCanonicalV7(eventID) || !validBASOperationKey(operationKey) || value == nil || value.OrganisationId != org {
		return nil, ErrExternalObservationRepository
	}
	wire, hash, err := sealObservation(value)
	if err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	frozen, err := repository.frozen.GetExact(ctx, org, value.FrozenBas)
	if err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	family := frozen.Family
	recordedAt, ok := basTimestampSQLText(value.RecordedAt)
	if !ok {
		return nil, ErrExternalObservationRepository
	}
	kind, claimedAt := "OBSERVATION", recordedAt
	var subjectKind string
	if input := value.GetObservation(); input != nil {
		claimedAt, ok = basTimestampSQLText(input.ClaimedEventAt)
		if !ok {
			return nil, ErrExternalObservationRepository
		}
	} else {
		kind = "CORRECTION"
		subject, err := repository.GetExact(ctx, org, value.CorrectsObservation)
		if err != nil || subject == nil || !sameBASSourceRef(subject.FrozenBas, value.FrozenBas) {
			return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
		}
		subjectKind = "OBSERVATION"
		if subject.GetCorrection() != nil {
			subjectKind = "CORRECTION"
		}
	}
	if err := repository.checkFamilyEventCapacity(ctx, org, family); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	const savepoint = "bas_external_observation_append"
	if _, err := repository.executor.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	failed := func(err error) (*tammyv1.SourceRef, error) {
		return nil, errors.Join(ErrExternalObservationRepository, err, rollbackRepositorySavepoint(ctx, repository.executor, savepoint))
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_family_events_v1(
		event_id,organisation_id,family_id,event_kind,operation_key,claimed_at,event_proto,body_hash,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, eventID, org, family.Id, kind, operationKey, claimedAt, wire, hash, recordedAt); err != nil {
		return failed(err)
	}
	if input := value.GetObservation(); input != nil {
		_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_external_observations_v1(
			observation_id,event_id,organisation_id,family_id,frozen_id,frozen_hash,event_kind,channel,claimed_event_at,
			external_reference,reason,trust,actor_user_id,recorded_at,content_hash,observation_proto)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.ObservationId, eventID, org, family.Id, value.FrozenBas.Id, value.FrozenBas.ContentHash,
			enumSQL(input.EventKind.String(), "BAS_EXTERNAL_EVENT_KIND_"), enumSQL(input.Channel.String(), "BAS_EXTERNAL_CHANNEL_"), claimedAt,
			input.GetExternalReference(), input.Reason, enumSQL(value.Trust.String(), "BAS_EXTERNAL_TRUST_"), value.ActorUserId, recordedAt, hash, wire)
	} else {
		input := value.GetCorrection()
		var observationID, observationHash, correctionID, correctionHash, correctedKind, correctedAt any
		if subjectKind == "OBSERVATION" {
			observationID, observationHash = value.CorrectsObservation.Id, value.CorrectsObservation.ContentHash
		} else {
			correctionID, correctionHash = value.CorrectsObservation.Id, value.CorrectsObservation.ContentHash
		}
		if input.CorrectedEventKind != nil {
			correctedKind = enumSQL(input.GetCorrectedEventKind().String(), "BAS_EXTERNAL_EVENT_KIND_")
		}
		if input.CorrectedClaimedEventAt != nil {
			text, ok := basTimestampSQLText(input.CorrectedClaimedEventAt)
			if !ok {
				return failed(ErrSnapshotCorrupt)
			}
			correctedAt = text
		}
		_, err = repository.executor.ExecContext(ctx, `INSERT INTO bas_external_observation_corrections_v1(
			correction_id,event_id,organisation_id,family_id,frozen_id,frozen_hash,disputed_observation_id,disputed_observation_hash,
			disputed_correction_id,disputed_correction_hash,correction_kind,corrected_event_kind,corrected_claimed_event_at,
			reason,trust,actor_user_id,recorded_at,content_hash,correction_proto) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			value.ObservationId, eventID, org, family.Id, value.FrozenBas.Id, value.FrozenBas.ContentHash, observationID, observationHash, correctionID, correctionHash,
			enumSQL(input.CorrectionKind.String(), "BAS_CORRECTION_KIND_"), correctedKind, correctedAt, input.Reason,
			enumSQL(value.Trust.String(), "BAS_EXTERNAL_TRUST_"), value.ActorUserId, recordedAt, hash, wire)
	}
	if err != nil {
		return failed(err)
	}
	if _, err := repository.executor.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		return failed(err)
	}
	return &tammyv1.SourceRef{Type: observationSnapshotSpec.resource, Id: value.ObservationId, Revision: 1, ContentHash: bytes.Clone(hash)}, nil
}

func (repository *lifecycleRepository) checkFamilyEventCapacity(ctx context.Context, org string, family *tammyv1.SourceRef) error {
	rows, err := repository.executor.QueryContext(ctx, "SELECT COUNT(*) FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=?", org, family.Id)
	if err != nil {
		return err
	}
	var count int64
	if !rows.Next() {
		return errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&count); err != nil {
		return errors.Join(err, rows.Close())
	}
	if rows.Next() {
		return errors.Join(ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if count < 0 || count >= maxExternalHistoryRecords {
		return ErrSnapshotBound
	}
	return nil
}

func (repository *ExternalObservationRepository) GetExact(ctx context.Context, org string, exact *tammyv1.SourceRef) (*tammyv1.BasExternalObservation, error) {
	if !repository.ready(ctx) || !ids.IsCanonicalV7(org) || !validBASSourceRef(exact, observationSnapshotSpec.resource, "") || exact.Revision != 1 {
		return nil, ErrExternalObservationRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT e.family_id,i.identity_hash,e.event_ordinal,e.event_kind
		FROM bas_family_events_v1 e JOIN bas_family_identities_v1 i ON i.family_id=e.family_id AND i.organisation_id=e.organisation_id
		WHERE e.organisation_id=? AND (
		EXISTS(SELECT 1 FROM bas_external_observations_v1 o WHERE o.event_id=e.event_id AND o.organisation_id=e.organisation_id AND o.family_id=e.family_id AND o.observation_id=?)
		OR EXISTS(SELECT 1 FROM bas_external_observation_corrections_v1 c WHERE c.event_id=e.event_id AND c.organisation_id=e.organisation_id AND c.family_id=e.family_id AND c.correction_id=?))`, org, exact.Id, exact.Id)
	if err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	var familyID, kind string
	var hash []byte
	var ordinal int64
	if !rows.Next() {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	if err := rows.Scan(&familyID, &hash, &ordinal, &kind); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err, rows.Close())
	}
	if rows.Next() {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, rows.Close())
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, err)
	}
	family := &tammyv1.SourceRef{Type: "bas_family", Id: familyID, Revision: 1, ContentHash: bytes.Clone(hash)}
	if _, err := repository.family(ctx, org, family); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
	}
	value, retainedKind, retainedOrdinal, err := repository.readObservationProof(ctx, org, family, exact)
	if err != nil || ordinal <= 0 || ordinal != retainedOrdinal || kind != retainedKind {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
	}
	if err := repository.verifyFamilyEvent(ctx, org, family, uint64(ordinal)); err != nil {
		return nil, errors.Join(ErrExternalObservationRepository, ErrSnapshotCorrupt, err)
	}
	return value, nil
}
