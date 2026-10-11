package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
)

// These bounded exact reads establish structural evidence bindings for workflow
// pointers. They do not grant approval authority or replay transition policy.
func (repository *lifecycleRepository) verifyFamilyEvent(ctx context.Context, org string, family *tammyv1.SourceRef, ordinal uint64) error {
	if ordinal == 0 || ordinal > math.MaxInt64 {
		return ErrSnapshotCorrupt
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT event_id,event_kind,operation_key,claimed_at,
		CASE WHEN length(event_proto)<=? THEN event_proto ELSE NULL END,length(event_proto),body_hash,created_at
		FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=? AND event_ordinal=?`, 128*1024, org, family.Id, int64(ordinal))
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var id, kind, operation, claimedAt, createdAt string
	var wire, hash []byte
	var length int64
	if err := rows.Scan(&id, &kind, &operation, &claimedAt, &wire, &length, &hash, &createdAt); err != nil {
		return err
	}
	if rows.Next() {
		return ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if err := repositorySnapshotBytes(wire, length, 128*1024); err != nil {
		return err
	}
	if !ids.IsCanonicalV7(id) || !validBASOperationKey(operation) || !canonicalRepositoryTime(claimedAt) {
		return ErrSnapshotCorrupt
	}
	switch kind {
	case "OBSERVATION", "CORRECTION":
		candidate := &tammyv1.BasExternalObservation{}
		if err := proto.Unmarshal(wire, candidate); err != nil {
			return errors.Join(ErrSnapshotCorrupt, err)
		}
		exact := &tammyv1.SourceRef{Type: "bas_external_observation", Id: candidate.ObservationId, Revision: 1, ContentHash: hash}
		value, err := openObservation(wire, org, exact)
		if err != nil {
			return err
		}
		if (kind == "OBSERVATION") != (value.GetObservation() != nil) {
			return ErrSnapshotCorrupt
		}
		recordedAt, validTime := basTimestampSQLText(value.RecordedAt)
		if !validTime || recordedAt != createdAt {
			return ErrSnapshotCorrupt
		}
		if value.GetObservation() != nil {
			claimed, validClaim := basTimestampSQLText(value.GetObservation().ClaimedEventAt)
			if !validClaim || claimed != claimedAt {
				return ErrSnapshotCorrupt
			}
		}
		retained, retainedKind, retainedOrdinal, err := repository.readObservationProof(ctx, org, family, exact)
		if err != nil {
			return err
		}
		if retainedKind != kind || retainedOrdinal != int64(ordinal) || !proto.Equal(retained, value) {
			return ErrSnapshotCorrupt
		}
		return repository.verifyObservationProof(ctx, org, family, exact, kind, 0)
	case "RESOLUTION":
		candidate := &tammyv1.BasExternalDisputeResolution{}
		if err := proto.Unmarshal(wire, candidate); err != nil {
			return errors.Join(ErrSnapshotCorrupt, err)
		}
		_, err := repository.readResolutionProofForEvent(ctx, org, family, &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: candidate.ResolutionId, Revision: 1, ContentHash: hash}, int64(ordinal))
		return err
	default:
		return ErrSnapshotCorrupt
	}
}

func canonicalRepositoryTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == value
}

func (repository *lifecycleRepository) readResolutionProof(ctx context.Context, org string, family, exact *tammyv1.SourceRef) (*tammyv1.BasExternalDisputeResolution, error) {
	return repository.readResolutionProofForEvent(ctx, org, family, exact, 0)
}

func (repository *lifecycleRepository) readResolutionProofForEvent(ctx context.Context, org string, family, exact *tammyv1.SourceRef, expectedOrdinal int64) (*tammyv1.BasExternalDisputeResolution, error) {
	if !validBASSourceRef(exact, "bas_external_dispute_resolution", "") || exact.Revision != 1 {
		return nil, ErrSnapshotCorrupt
	}
	identity, err := repository.family(ctx, org, family)
	if err != nil {
		return nil, err
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT r.outcome,r.dependent_chain_manifest_hash,r.reason,r.actor_user_id,r.resolved_at,r.content_hash,
		CASE WHEN length(r.resolution_proto)<=? THEN r.resolution_proto ELSE NULL END,length(r.resolution_proto),
		e.event_kind,e.body_hash,CASE WHEN length(e.event_proto)<=? THEN e.event_proto ELSE NULL END,length(e.event_proto),e.created_at,e.event_ordinal,e.event_id,e.operation_key,e.claimed_at
		FROM bas_dispute_resolutions_v1 r JOIN bas_family_events_v1 e ON e.event_id=r.event_id AND e.organisation_id=r.organisation_id AND e.family_id=r.family_id
		WHERE r.resolution_id=? AND r.organisation_id=? AND r.family_id=?`, 128*1024, 128*1024, exact.Id, org, family.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var outcome, reason, actor, resolvedAt, eventKind, createdAt, eventID, operationKey, claimedAt string
	var manifest, hash, wire, eventHash, eventWire []byte
	var length, eventLength, ordinal int64
	if err := rows.Scan(&outcome, &manifest, &reason, &actor, &resolvedAt, &hash, &wire, &length, &eventKind, &eventHash, &eventWire, &eventLength, &createdAt, &ordinal, &eventID, &operationKey, &claimedAt); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	if err := repositorySnapshotBytes(wire, length, 128*1024); err != nil {
		return nil, err
	}
	if err := repositorySnapshotBytes(eventWire, eventLength, 128*1024); err != nil {
		return nil, err
	}
	if !bytes.Equal(hash, exact.ContentHash) || !bytes.Equal(hash, eventHash) || !bytes.Equal(wire, eventWire) || eventKind != "RESOLUTION" {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openResolution(wire, org, exact, identity)
	if err != nil {
		return nil, err
	}
	timeText, validTime := basTimestampSQLText(value.ResolvedAt)
	if !validTime || timeText != resolvedAt || resolvedAt != createdAt || resolvedAt != claimedAt || !ids.IsCanonicalV7(eventID) || !validBASOperationKey(operationKey) || value.ActorUserId != actor || value.Resolution.Reason != reason ||
		!bytes.Equal(value.Resolution.DependentChainManifestHash, manifest) || enumSQL(value.Resolution.Outcome.String(), "BAS_DISPUTE_RESOLUTION_OUTCOME_") != outcome {
		return nil, ErrSnapshotCorrupt
	}
	if ordinal <= 0 || (expectedOrdinal != 0 && ordinal != expectedOrdinal) || !bytes.Equal(dependencyRefsManifest(value.Resolution.AffectedConfirmedChain), manifest) {
		return nil, ErrSnapshotCorrupt
	}
	if err := repository.verifyResolutionLinks(ctx, org, value, ordinal); err != nil {
		return nil, err
	}
	return value, nil
}

func (repository *lifecycleRepository) verifyResolutionLinks(ctx context.Context, org string, value *tammyv1.BasExternalDisputeResolution, ordinal int64) error {
	expected := make(map[string]*tammyv1.SourceRef, len(value.Resolution.DisputedObservations))
	for _, ref := range value.Resolution.DisputedObservations {
		if !validBASSourceRef(ref, "bas_external_observation", "") || ref.Revision != 1 || expected[ref.Id] != nil {
			return ErrSnapshotCorrupt
		}
		expected[ref.Id] = ref
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT subject_kind,subject_id,subject_hash,observation_id,observation_hash,correction_id,correction_hash
		FROM bas_dispute_resolution_observations_v1 WHERE resolution_id=? AND organisation_id=? AND family_id=? LIMIT 201`, value.ResolutionId, org, value.Family.Id)
	if err != nil {
		return err
	}
	type subject struct {
		kind string
		ref  *tammyv1.SourceRef
	}
	var subjects []subject
	for rows.Next() {
		if len(subjects) == 200 {
			rows.Close()
			return ErrSnapshotBound
		}
		var kind, id string
		var hash, observationHash, correctionHash []byte
		var observationID, correctionID sql.NullString
		if err := rows.Scan(&kind, &id, &hash, &observationID, &observationHash, &correctionID, &correctionHash); err != nil {
			rows.Close()
			return err
		}
		ref := expected[id]
		if ref == nil || !bytes.Equal(ref.ContentHash, hash) ||
			(kind == "OBSERVATION" && (!observationID.Valid || observationID.String != id || !bytes.Equal(hash, observationHash) || correctionID.Valid || correctionHash != nil)) ||
			(kind == "CORRECTION" && (!correctionID.Valid || correctionID.String != id || !bytes.Equal(hash, correctionHash) || observationID.Valid || observationHash != nil)) || (kind != "OBSERVATION" && kind != "CORRECTION") {
			rows.Close()
			return ErrSnapshotCorrupt
		}
		subjects = append(subjects, subject{kind, ref})
		delete(expected, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(expected) != 0 {
		return ErrSnapshotCorrupt
	}
	for _, subject := range subjects {
		if err := repository.verifyObservationProof(ctx, org, value.Family, subject.ref, subject.kind, ordinal); err != nil {
			return err
		}
	}
	expectedChain := make(map[string]*tammyv1.SourceRef, len(value.Resolution.AffectedConfirmedChain))
	for _, ref := range value.Resolution.AffectedConfirmedChain {
		if !validBASSourceRef(ref, "bas_preparation", "") || expectedChain[ref.Id] != nil {
			return ErrSnapshotCorrupt
		}
		expectedChain[ref.Id] = ref
	}
	rows, err = repository.executor.QueryContext(ctx, `SELECT preparation_id,preparation_version,preparation_hash FROM bas_dispute_resolution_preparations_v1
		WHERE resolution_id=? AND organisation_id=? AND family_id=? LIMIT 201`, value.ResolutionId, org, value.Family.Id)
	if err != nil {
		return err
	}
	var chain []*tammyv1.SourceRef
	for rows.Next() {
		if len(chain) == 200 {
			rows.Close()
			return ErrSnapshotBound
		}
		var id string
		var revision int64
		var hash []byte
		if err := rows.Scan(&id, &revision, &hash); err != nil {
			rows.Close()
			return err
		}
		ref := expectedChain[id]
		if revision <= 0 || ref == nil || ref.Revision != uint64(revision) || !bytes.Equal(ref.ContentHash, hash) {
			rows.Close()
			return ErrSnapshotCorrupt
		}
		chain = append(chain, ref)
		delete(expectedChain, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(expectedChain) != 0 {
		return ErrSnapshotCorrupt
	}
	for _, ref := range chain {
		retained, err := repository.preparation(ctx, org, value.Family, ref)
		if err != nil {
			return err
		}
		if retained.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED || retained.Preparation.FrozenBas == nil {
			return ErrSnapshotCorrupt
		}
		if err := repository.verifyFrozenBarrier(ctx, org, value.Family, retained.Preparation, retained.Preparation.FrozenBas); err != nil {
			return err
		}
	}
	return nil
}

// Follow a correction's exact earlier subject without falling back to mutable
// history. The bound prevents malicious chains from expanding an exact read.
func (repository *lifecycleRepository) verifyObservationProof(ctx context.Context, org string, family, ref *tammyv1.SourceRef, expectedKind string, beforeOrdinal int64) error {
	var frozen *tammyv1.SourceRef
	for depth := 0; depth < 1000; depth++ {
		value, kind, ordinal, err := repository.readObservationProof(ctx, org, family, ref)
		if err != nil {
			return err
		}
		if (expectedKind != "" && kind != expectedKind) || ordinal <= 0 || (beforeOrdinal != 0 && ordinal >= beforeOrdinal) {
			return ErrSnapshotCorrupt
		}
		if frozen == nil {
			frozen = value.FrozenBas
		} else if !sameBASSourceRef(frozen, value.FrozenBas) {
			return ErrSnapshotCorrupt
		}
		if value.GetCorrection() == nil {
			_, err := repository.readFrozenProof(ctx, org, family, frozen)
			return err
		}
		ref, beforeOrdinal, expectedKind = value.CorrectsObservation, ordinal, ""
	}
	return ErrSnapshotBound
}

func (repository *lifecycleRepository) readObservationProof(ctx context.Context, org string, family, exact *tammyv1.SourceRef) (*tammyv1.BasExternalObservation, string, int64, error) {
	if !validBASSourceRef(exact, "bas_external_observation", "") || exact.Revision != 1 {
		return nil, "", 0, ErrSnapshotCorrupt
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT e.event_kind,e.event_ordinal,e.body_hash,
		CASE WHEN length(e.event_proto)<=? THEN e.event_proto ELSE NULL END,length(e.event_proto),e.created_at,e.claimed_at,
		COALESCE(o.content_hash,c.content_hash),CASE WHEN length(COALESCE(o.observation_proto,c.correction_proto))<=? THEN COALESCE(o.observation_proto,c.correction_proto) ELSE NULL END,
		length(COALESCE(o.observation_proto,c.correction_proto)),COALESCE(o.actor_user_id,c.actor_user_id),COALESCE(o.recorded_at,c.recorded_at),COALESCE(o.trust,c.trust),COALESCE(o.reason,c.reason),
		o.frozen_id,o.frozen_hash,o.event_kind,o.channel,o.claimed_event_at,o.external_reference,
		c.disputed_observation_id,c.disputed_observation_hash,c.disputed_correction_id,c.disputed_correction_hash,c.correction_kind,c.corrected_event_kind,c.corrected_claimed_event_at,c.frozen_id,c.frozen_hash,e.event_id,e.operation_key
		FROM bas_family_events_v1 e LEFT JOIN bas_external_observations_v1 o ON o.event_id=e.event_id AND o.organisation_id=e.organisation_id AND o.family_id=e.family_id
		LEFT JOIN bas_external_observation_corrections_v1 c ON c.event_id=e.event_id AND c.organisation_id=e.organisation_id AND c.family_id=e.family_id
		WHERE e.organisation_id=? AND e.family_id=? AND (o.observation_id=? OR c.correction_id=?)`, 128*1024, 128*1024, org, family.Id, exact.Id, exact.Id)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, "", 0, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var kind, createdAt, claimedAt, actor, recordedAt, trust, reason, eventID, operationKey string
	var eventHash, eventWire, hash, wire, frozenHash, observationHash, correctionHash, correctionFrozenHash []byte
	var ordinal, eventLength, length int64
	var frozenID, eventKind, channel, claimedEventAt, reference, observationID, correctionID, correctionKind, correctedKind, correctedAt, correctionFrozenID sql.NullString
	if err := rows.Scan(&kind, &ordinal, &eventHash, &eventWire, &eventLength, &createdAt, &claimedAt, &hash, &wire, &length, &actor, &recordedAt, &trust, &reason,
		&frozenID, &frozenHash, &eventKind, &channel, &claimedEventAt, &reference, &observationID, &observationHash, &correctionID, &correctionHash, &correctionKind, &correctedKind, &correctedAt, &correctionFrozenID, &correctionFrozenHash, &eventID, &operationKey); err != nil {
		return nil, "", 0, err
	}
	if rows.Next() {
		return nil, "", 0, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, "", 0, err
	}
	for _, pair := range []struct {
		wire   []byte
		length int64
	}{{wire, length}, {eventWire, eventLength}} {
		if err := repositorySnapshotBytes(pair.wire, pair.length, 128*1024); err != nil {
			return nil, "", 0, err
		}
	}
	if !bytes.Equal(hash, exact.ContentHash) || !bytes.Equal(hash, eventHash) || !bytes.Equal(wire, eventWire) || !ids.IsCanonicalV7(eventID) || !validBASOperationKey(operationKey) {
		return nil, "", 0, ErrSnapshotCorrupt
	}
	value, err := openObservation(wire, org, exact)
	if err != nil {
		return nil, "", 0, err
	}
	timeText, validTime := basTimestampSQLText(value.RecordedAt)
	if !validTime || recordedAt != timeText || createdAt != timeText || actor != value.ActorUserId || trust != enumSQL(value.Trust.String(), "BAS_EXTERNAL_TRUST_") {
		return nil, "", 0, ErrSnapshotCorrupt
	}
	if input := value.GetObservation(); input != nil {
		claim, validClaim := basTimestampSQLText(input.ClaimedEventAt)
		if kind != "OBSERVATION" || !frozenID.Valid || !singleSnapshotSQLMatches(frozenID, frozenHash, value.FrozenBas, "bas_frozen") ||
			!eventKind.Valid || eventKind.String != enumSQL(input.EventKind.String(), "BAS_EXTERNAL_EVENT_KIND_") || !channel.Valid || channel.String != enumSQL(input.Channel.String(), "BAS_EXTERNAL_CHANNEL_") ||
			!validClaim || !claimedEventAt.Valid || claim != claimedEventAt.String || claim != claimedAt || !reference.Valid || reference.String != input.GetExternalReference() || reason != input.Reason || correctionID.Valid || observationID.Valid || correctionKind.Valid || correctionFrozenID.Valid || correctionFrozenHash != nil {
			return nil, "", 0, ErrSnapshotCorrupt
		}
	} else {
		input := value.GetCorrection()
		if kind != "CORRECTION" || input == nil || frozenID.Valid || eventKind.Valid || !correctionKind.Valid || correctionKind.String != enumSQL(input.CorrectionKind.String(), "BAS_CORRECTION_KIND_") || reason != input.Reason || claimedAt != timeText ||
			!singleSnapshotSQLMatches(correctionFrozenID, correctionFrozenHash, value.FrozenBas, "bas_frozen") {
			return nil, "", 0, ErrSnapshotCorrupt
		}
		if observationID.Valid == correctionID.Valid {
			return nil, "", 0, ErrSnapshotCorrupt
		}
		targetID, targetHash := observationID, observationHash
		if correctionID.Valid {
			targetID, targetHash = correctionID, correctionHash
		}
		if !singleSnapshotSQLMatches(targetID, targetHash, value.CorrectsObservation, "bas_external_observation") {
			return nil, "", 0, ErrSnapshotCorrupt
		}
		if (input.CorrectedEventKind != nil) != correctedKind.Valid || (input.CorrectedEventKind != nil && correctedKind.String != enumSQL(input.GetCorrectedEventKind().String(), "BAS_EXTERNAL_EVENT_KIND_")) || (input.CorrectedClaimedEventAt != nil) != correctedAt.Valid {
			return nil, "", 0, ErrSnapshotCorrupt
		}
		if input.CorrectedClaimedEventAt != nil {
			text, valid := basTimestampSQLText(input.CorrectedClaimedEventAt)
			if !valid || text != correctedAt.String {
				return nil, "", 0, ErrSnapshotCorrupt
			}
		}
	}
	return value, kind, ordinal, nil
}

func (repository *lifecycleRepository) verifyCorrectiveResolution(ctx context.Context, org string, family, exact *tammyv1.SourceRef) error {
	value, err := repository.readResolutionProof(ctx, org, family, exact)
	if err != nil {
		return err
	}
	if value.Resolution.Outcome != tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION {
		return ErrSnapshotCorrupt
	}
	return nil
}

func (repository *lifecycleRepository) verifyFrozenBarrier(ctx context.Context, org string, family *tammyv1.SourceRef, barrier *tammyv1.BasPreparation, exact *tammyv1.SourceRef) error {
	value, err := repository.readFrozenProof(ctx, org, family, exact)
	if err != nil {
		return err
	}
	if value.Preparation.Id != barrier.PreparationId || value.Preparation.Revision >= barrier.Version ||
		!sameBASSourceRef(value.Profile, barrier.Profile) || !sameBASSourceRef(value.Period, barrier.Period) ||
		!sameBASSourceRef(value.Calculation, barrier.CurrentCalculation) || !sameBASSourceRef(value.Validation, barrier.CurrentValidation) {
		return ErrSnapshotCorrupt
	}
	return nil
}

func (repository *lifecycleRepository) readFrozenProof(ctx context.Context, org string, family, exact *tammyv1.SourceRef) (*tammyv1.BasFrozenBas, error) {
	frozen, err := NewFrozenRepository(repository.executor, repository.cursors)
	if err != nil {
		return nil, err
	}
	value, err := frozen.GetExact(ctx, org, exact)
	if err != nil {
		return nil, err
	}
	if !sameBASSourceRef(value.Family, family) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}

// readFrozenSnapshot verifies the retained frame and scalar columns. The public
// repository adds complete calculation, validation and acknowledgement proofs.
func (repository *lifecycleRepository) readFrozenSnapshot(ctx context.Context, org string, family, exact *tammyv1.SourceRef) (*tammyv1.BasFrozenBas, error) {
	if !validBASSourceRef(exact, "bas_frozen", "") || exact.Revision != 1 {
		return nil, ErrSnapshotCorrupt
	}
	rows, err := repository.executor.QueryContext(ctx, `SELECT preparation_id,preparation_version,preparation_hash,
		profile_id,profile_version,profile_hash,period_id,period_version,period_hash,calculation_id,calculation_version,calculation_hash,
		validation_id,validation_revision,validation_hash,source_snapshot_hash,approved_snapshot_hash,content_hash,
		CASE WHEN length(calculation_snapshot_proto)<=? THEN calculation_snapshot_proto ELSE NULL END,length(calculation_snapshot_proto),
		CASE WHEN length(frozen_proto)<=? THEN frozen_proto ELSE NULL END,length(frozen_proto),approval_actor_user_id,fresh_factor_assertion_id,approved_at,approval_reason
		FROM bas_frozen_bas_v1 WHERE frozen_id=? AND organisation_id=? AND family_id=?`, frozenSnapshotLimit, frozenSnapshotLimit, exact.Id, org, family.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errors.Join(ErrSnapshotCorrupt, rows.Err(), rows.Close())
	}
	var preparation, profile, period, calculation, validation preparationReadSource
	var sourceHash, approvedHash, hash, calculationWire, wire []byte
	var calculationLength, length int64
	var actor, factor, approvedAt, reason string
	if err := rows.Scan(&preparation.id, &preparation.version, &preparation.hash, &profile.id, &profile.version, &profile.hash,
		&period.id, &period.version, &period.hash, &calculation.id, &calculation.version, &calculation.hash,
		&validation.id, &validation.version, &validation.hash, &sourceHash, &approvedHash, &hash,
		&calculationWire, &calculationLength, &wire, &length, &actor, &factor, &approvedAt, &reason); err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, ErrSnapshotCorrupt
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	if err := repositorySnapshotBytes(wire, length, frozenSnapshotLimit); err != nil {
		return nil, err
	}
	if err := repositorySnapshotBytes(calculationWire, calculationLength, frozenSnapshotLimit); err != nil {
		return nil, err
	}
	if !bytes.Equal(hash, exact.ContentHash) {
		return nil, ErrSnapshotCorrupt
	}
	value, err := openFrozen(wire, org, exact)
	if err != nil {
		return nil, err
	}
	timeText, validTime := basTimestampSQLText(value.ApprovedAt)
	if !sameBASSourceRef(value.Family, family) || !preparation.matches(value.Preparation, "bas_preparation") ||
		!profile.matches(value.Profile, profileSnapshotSpec.resource) || !period.matches(value.Period, periodSnapshotSpec.resource) ||
		!calculation.matches(value.Calculation, "bas_calculation") || !validation.matches(value.Validation, "bas_validation") ||
		!bytes.Equal(sourceHash, value.CalculationSnapshot.SourceSnapshotHash) || !bytes.Equal(approvedHash, value.ApprovedSnapshotHash) ||
		actor != value.ApprovalActorUserId || factor != value.FreshFactorAssertionId || !validTime || approvedAt != timeText || reason != value.ApprovalReason {
		return nil, ErrSnapshotCorrupt
	}
	retainedCalculation, err := openCalculation(calculationWire, org, value.Calculation)
	if err != nil {
		return nil, err
	}
	if !proto.Equal(retainedCalculation, value.CalculationSnapshot) {
		return nil, ErrSnapshotCorrupt
	}
	owner, err := repository.preparation(ctx, org, family, value.Preparation)
	if err != nil {
		return nil, err
	}
	if owner.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		!sameBASSourceRef(owner.Preparation.Profile, value.Profile) || !sameBASSourceRef(owner.Preparation.Period, value.Period) ||
		!sameBASSourceRef(owner.Preparation.CurrentCalculation, value.Calculation) || !sameBASSourceRef(owner.Preparation.CurrentValidation, value.Validation) {
		return nil, ErrSnapshotCorrupt
	}
	return value, nil
}
