package baspreparation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
	"google.golang.org/protobuf/proto"
)

const maxExternalConfirmedDependencies = 200

// BuildBASExternalDisputeSummary retains the reducer's complete subject order
// and dependency framing for an exact resolution request.
func BuildBASExternalDisputeSummary(history ExternalHistoryInput) (*tammyv1.BasExternalDisputeSummary, error) {
	reduced, err := deriveExternalHistory(history)
	if err != nil {
		return nil, err
	}
	dispute := reduced.disputeSummary()
	if len(dispute.observations) > maxExternalDisputeSubjects {
		return nil, ErrSnapshotBound
	}
	return &tammyv1.BasExternalDisputeSummary{DisputedObservations: cloneRefs(dispute.observations),
		AffectedConfirmedChain: cloneRefs(dispute.affected), DependentChainManifestHash: dependencyRefsManifest(dispute.affected)}, nil
}

// BASExternalHistoryReader loads a complete family-scoped proof inside the
// caller's transaction. Attempt/event headers are first censused with limit+1;
// no body is copied until both independent 1000-row bounds have passed.
type BASExternalHistoryReader struct {
	executor         app.CommandSQLExecutor
	cursors          *paging.Codec
	families         *FamilyIdentityRepository
	familyStates     *FamilyStateRepository
	branchIdentities *BranchIdentityRepository
	branchStates     *BranchStateRepository
	preparations     *PreparationRepository
	frozen           *FrozenRepository
	observations     *ExternalObservationRepository
	resolutions      *DisputeResolutionRepository
}

func NewBASExternalHistoryReader(executor app.CommandSQLExecutor, cursors *paging.Codec) (*BASExternalHistoryReader, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrExternalHistory
	}
	families, err := NewFamilyIdentityRepository(executor)
	if err != nil {
		return nil, err
	}
	familyStates, err := NewFamilyStateRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	branchIdentities, err := NewBranchIdentityRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	branchStates, err := NewBranchStateRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	preparations, err := NewPreparationRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	frozen, err := NewFrozenRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	observations, err := NewExternalObservationRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	resolutions, err := NewDisputeResolutionRepository(executor, cursors)
	if err != nil {
		return nil, err
	}
	return &BASExternalHistoryReader{executor: executor, cursors: cursors, families: families,
		familyStates: familyStates, branchIdentities: branchIdentities, branchStates: branchStates,
		preparations: preparations, frozen: frozen, observations: observations, resolutions: resolutions}, nil
}

// Read returns the full exact chain plus every retained event ordered by
// Core's family-event ordinal. It rejects an overflow, owner mismatch, missing
// exact body, stale family watermark, broken predecessor path, or >200
// unresolved dispute subjects instead of returning a partial prefix.
func (reader *BASExternalHistoryReader) Read(ctx context.Context, organisationID string,
	familyRef *tammyv1.SourceRef) (ExternalHistoryInput, error) {
	if reader == nil || nilBASRepositoryExecutor(reader.executor) || reader.cursors == nil ||
		ctx == nil || ctx.Err() != nil || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(familyRef, "bas_family", "") || familyRef.Revision != 1 {
		return ExternalHistoryInput{}, ErrExternalHistory
	}
	// Reserve the complete finite census before hydrating any retained body.
	for _, census := range []struct {
		query   string
		maximum int
	}{
		{`SELECT count(*) FROM (SELECT 1 FROM bas_preparation_identities_v1 WHERE organisation_id=? AND family_id=? LIMIT ?)`, maxExternalHistoryAttempts},
		{`SELECT count(*) FROM (SELECT 1 FROM bas_family_events_v1 WHERE organisation_id=? AND family_id=? LIMIT ?)`, maxExternalHistoryRecords},
	} {
		var count int64
		if err := (basQueryRow{executor: reader.executor, ctx: ctx, query: census.query,
			args: []any{organisationID, familyRef.Id, census.maximum + 1}}).Scan(&count); err != nil {
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err, ctx.Err())
		}
		if count > int64(census.maximum) {
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotBound)
		}
	}

	identity, err := reader.families.GetExact(ctx, organisationID, familyRef)
	if err != nil || identity == nil || identity.OrganisationId != organisationID {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt, err)
	}
	familyState, err := reader.familyStates.GetCurrent(ctx, organisationID, familyRef)
	if err != nil || familyState == nil || !sameBASSourceRef(familyState.Family, familyRef) {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt, err)
	}
	chain, err := reader.readChain(ctx, organisationID, identity, familyState)
	if err != nil {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
	}

	// Header-only complete censuses happen before any attempt/event bodies are
	// loaded. The queries are scoped to this exact organisation and family.
	attemptHeaders, err := reader.attemptHeaders(ctx, organisationID, familyRef.Id)
	if err != nil {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
	}
	eventHeaders, err := reader.eventHeaders(ctx, organisationID, familyRef.Id)
	if err != nil {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
	}
	if len(eventHeaders) == 0 {
		if familyState.LastFamilyEventOrdinal != 0 {
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt)
		}
	} else if uint64(eventHeaders[len(eventHeaders)-1].ordinal) != familyState.LastFamilyEventOrdinal {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt)
	}

	history := ExternalHistoryInput{Chain: chain}
	for _, header := range attemptHeaders {
		retained, err := reader.preparations.GetCurrent(ctx, organisationID, header.preparationID)
		if err != nil || retained == nil || retained.Preparation == nil ||
			retained.Preparation.OrganisationId != organisationID ||
			!sameBASSourceRef(retained.Preparation.Family, familyRef) ||
			retained.AttemptID != header.attemptID || !sameOptionalString(retained.BranchID, header.branchID) {
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt, err)
		}
		if retained.Preparation.FrozenBas == nil {
			// An unfrozen identity is relevant only as the current root/active
			// mutable attempt. All older attempts with external history must retain
			// their exact frozen snapshot and are loaded below.
			if !samePreparationIdentity(retained.Preparation, chain.RootPreparation) &&
				(chain.Branch == nil || !samePreparationIdentity(retained.Preparation, chain.Branch.ActiveAttempt)) {
				return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt)
			}
			continue
		}
		attempt, err := reader.attemptForExactPreparation(ctx, organisationID, preparationReference(retained.Preparation))
		if err != nil {
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
		}
		history.Attempts = append(history.Attempts, attempt)
	}

	confirmed, err := reader.confirmedDependencies(ctx, organisationID, chain)
	if err != nil {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
	}
	history.ConfirmedDependencies = confirmed
	history.Records = make([]ExternalHistoryRecord, 0, len(eventHeaders))
	for _, header := range eventHeaders {
		if err := ctx.Err(); err != nil {
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
		}
		switch header.kind {
		case "OBSERVATION", "CORRECTION":
			exact := &tammyv1.SourceRef{Type: observationSnapshotSpec.resource, Id: header.id,
				Revision: 1, ContentHash: bytes.Clone(header.hash)}
			value, err := reader.observations.GetExact(ctx, organisationID, exact)
			if err != nil || value == nil || value.ObservationId != header.id ||
				!bytes.Equal(value.ContentHash, header.hash) ||
				(header.kind == "OBSERVATION") != (value.GetObservation() != nil) ||
				(header.kind == "CORRECTION") != (value.GetCorrection() != nil) {
				return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt, err)
			}
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: uint64(header.ordinal), Observation: value})
		case "RESOLUTION":
			exact := &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: header.id,
				Revision: 1, ContentHash: bytes.Clone(header.hash)}
			value, err := reader.resolutions.GetExact(ctx, organisationID, exact)
			if err != nil || value == nil || value.ResolutionId != header.id ||
				!bytes.Equal(value.ContentHash, header.hash) || !sameBASSourceRef(value.Family, familyRef) {
				return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt, err)
			}
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: uint64(header.ordinal), Resolution: value})
		default:
			return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotCorrupt)
		}
	}
	if len(eventHeaders) != 0 {
		history.LastOrdinal = uint64(eventHeaders[len(eventHeaders)-1].ordinal)
	}

	reduced, err := deriveExternalHistory(history)
	if err != nil {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, err)
	}
	if len(reduced.disputeSummary().observations) > maxExternalDisputeSubjects {
		return ExternalHistoryInput{}, errors.Join(ErrExternalHistory, ErrSnapshotBound)
	}
	return history, nil
}

func sameOptionalString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func (reader *BASExternalHistoryReader) readChain(ctx context.Context, org string,
	identity *tammyv1.BasFamilyIdentity, state *tammyv1.BasFamilyState) (ChainSnapshot, error) {
	familyRef := basFamilyIdentityReference(identity)
	root, err := reader.preparations.GetExact(ctx, org, state.RootPreparation)
	if err != nil || root == nil {
		return ChainSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
	}
	chain := ChainSnapshot{Family: &Family{Reference: familyRef, Version: state.Version,
		Scope: FamilyScope{OrganisationID: identity.OrganisationId,
			Start: proto.Clone(identity.PeriodScope.StartDate).(*tammyv1.CivilDate),
			End:   proto.Clone(identity.PeriodScope.EndDate).(*tammyv1.CivilDate), Window: identity.Window}},
		RootPreparation: root.Preparation, DisputeOverlay: state.DisputeOverlay,
		PeriodReference: cloneRef(root.Preparation.Period)}
	if !sameBASSourceRef(root.Preparation.Family, familyRef) || root.BranchID != nil {
		return ChainSnapshot{}, ErrSnapshotCorrupt
	}
	if originalRef := state.EffectiveOriginal; originalRef != nil {
		original, err := reader.preparations.GetExact(ctx, org, originalRef)
		if err != nil || original == nil || !sameBASSourceRef(original.Preparation.Family, familyRef) {
			return ChainSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
		}
		chain.EffectiveOriginal = original.Preparation
	}
	if latestRef := state.LatestHead.GetRetained(); latestRef != nil {
		latest, err := reader.preparations.GetExact(ctx, org, latestRef)
		if err != nil || latest == nil || !sameBASSourceRef(latest.Preparation.Family, familyRef) {
			return ChainSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
		}
		chain.LatestHead = latest.Preparation
	}

	branchRef := state.ActiveBranch.GetRetained()
	if branchRef == nil {
		return chain, nil
	}
	branchIdentity, err := reader.branchIdentities.GetExact(ctx, org, branchRef)
	if err != nil || branchIdentity == nil || !sameBASSourceRef(branchIdentity.Family, familyRef) ||
		!sameBASSourceRef(branchIdentity.FamilyRootPreparation, state.RootPreparation) ||
		!sameOptionalRef(branchIdentity.EffectiveOriginal, state.EffectiveOriginal) {
		return ChainSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
	}
	branchState, err := reader.branchStates.getVersion(ctx, org, branchRef, state.ActiveBranch.GetExpectedVersion())
	if err != nil || branchState == nil || !sameBASSourceRef(branchState.BranchIdentity, branchRef) {
		return ChainSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
	}
	branch := &Branch{Reference: cloneRef(branchRef), Version: branchState.Version,
		RootHead: chain.LatestHead, Resolution: cloneRef(branchState.Resolution), FrozenBarrier: cloneRef(branchState.FrozenBarrier)}
	if activeRef := branchState.ActiveAttempt.GetRetained(); activeRef != nil {
		active, err := reader.preparations.GetCurrent(ctx, org, activeRef.Id)
		if err != nil || active == nil || !sameBASSourceRef(preparationReference(active.Preparation), activeRef) ||
			active.BranchID == nil || *active.BranchID != branchRef.Id {
			return ChainSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
		}
		branch.ActiveAttempt = active.Preparation
	}
	chain.Branch = branch
	return chain, nil
}

type basExternalAttemptHeader struct {
	preparationID string
	branchID      *string
	attemptID     string
}

func (reader *BASExternalHistoryReader) attemptHeaders(ctx context.Context, org, familyID string) ([]basExternalAttemptHeader, error) {
	rows, err := reader.executor.QueryContext(ctx, `SELECT
		CASE WHEN typeof(preparation_id)='text' AND length(preparation_id)<=128 THEN preparation_id END,
		COALESCE(length(preparation_id),0),
		CASE WHEN branch_id IS NULL THEN NULL WHEN typeof(branch_id)='text' AND length(branch_id)<=128 THEN branch_id END,
		COALESCE(typeof(branch_id),'null'),COALESCE(length(branch_id),0),
		CASE WHEN typeof(attempt_id)='text' AND length(attempt_id)<=128 THEN attempt_id END,
		COALESCE(length(attempt_id),0)
		FROM bas_preparation_identities_v1 WHERE organisation_id=? AND family_id=?
		ORDER BY attempt_id,preparation_id LIMIT ?`, org, familyID, maxExternalHistoryAttempts+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]basExternalAttemptHeader, 0)
	seenAttempts := make(map[string]struct{})
	for rows.Next() {
		if len(values) >= maxExternalHistoryAttempts {
			return nil, errors.Join(ErrSnapshotBound, rows.Close())
		}
		var prepID, attemptID sql.NullString
		var prepLength, branchLength, attemptLength int64
		var branchID sql.NullString
		var branchType string
		if err := rows.Scan(&prepID, &prepLength, &branchID, &branchType, &branchLength, &attemptID, &attemptLength); err != nil {
			return nil, errors.Join(ErrSnapshotCorrupt, err, rows.Close())
		}
		if !prepID.Valid || prepLength <= 0 || prepLength > 128 || !attemptID.Valid || attemptLength <= 0 || attemptLength > 128 ||
			(branchType == "null" && (branchID.Valid || branchLength != 0)) ||
			(branchType != "null" && (branchType != "text" || !branchID.Valid || branchLength <= 0 || branchLength > 128)) {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		if _, exists := seenAttempts[attemptID.String]; exists {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		seenAttempts[attemptID.String] = struct{}{}
		var branch *string
		if branchID.Valid {
			value := branchID.String
			branch = &value
		}
		values = append(values, basExternalAttemptHeader{preparationID: prepID.String, branchID: branch, attemptID: attemptID.String})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return values, nil
}

type basExternalEventHeader struct {
	ordinal int64
	kind    string
	id      string
	hash    []byte
}

func (reader *BASExternalHistoryReader) eventHeaders(ctx context.Context, org, familyID string) ([]basExternalEventHeader, error) {
	rows, err := reader.executor.QueryContext(ctx, `SELECT
		e.event_ordinal,
		CASE WHEN typeof(e.event_kind)='text' AND length(e.event_kind)<=32 THEN e.event_kind END,
		COALESCE(length(e.event_kind),0),
		CASE WHEN typeof(e.event_id)='text' AND length(e.event_id)<=128 THEN e.event_id END,
		COALESCE(length(e.event_id),0),
		CASE WHEN typeof(e.body_hash)='blob' AND length(e.body_hash)=32 THEN e.body_hash END,
		COALESCE(length(e.body_hash),0),
		CASE WHEN typeof(o.event_id)='text' AND length(o.event_id)<=128 THEN o.event_id END,
		CASE WHEN typeof(o.observation_id)='text' AND length(o.observation_id)<=128 THEN o.observation_id END,
		COALESCE(length(o.observation_id),0),
		CASE WHEN typeof(o.content_hash)='blob' AND length(o.content_hash)=32 THEN o.content_hash END,
		COALESCE(length(o.content_hash),0),
		CASE WHEN typeof(c.event_id)='text' AND length(c.event_id)<=128 THEN c.event_id END,
		CASE WHEN typeof(c.correction_id)='text' AND length(c.correction_id)<=128 THEN c.correction_id END,
		COALESCE(length(c.correction_id),0),
		CASE WHEN typeof(c.content_hash)='blob' AND length(c.content_hash)=32 THEN c.content_hash END,
		COALESCE(length(c.content_hash),0),
		CASE WHEN typeof(r.event_id)='text' AND length(r.event_id)<=128 THEN r.event_id END,
		CASE WHEN typeof(r.resolution_id)='text' AND length(r.resolution_id)<=128 THEN r.resolution_id END,
		COALESCE(length(r.resolution_id),0),
		CASE WHEN typeof(r.content_hash)='blob' AND length(r.content_hash)=32 THEN r.content_hash END,
		COALESCE(length(r.content_hash),0)
		FROM bas_family_events_v1 e
		LEFT JOIN bas_external_observations_v1 o ON o.event_id=e.event_id AND o.organisation_id=e.organisation_id AND o.family_id=e.family_id
		LEFT JOIN bas_external_observation_corrections_v1 c ON c.event_id=e.event_id AND c.organisation_id=e.organisation_id AND c.family_id=e.family_id
		LEFT JOIN bas_dispute_resolutions_v1 r ON r.event_id=e.event_id AND r.organisation_id=e.organisation_id AND r.family_id=e.family_id
		WHERE e.organisation_id=? AND e.family_id=? ORDER BY e.event_ordinal LIMIT ?`, org, familyID, maxExternalHistoryRecords+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]basExternalEventHeader, 0)
	var previous int64
	for rows.Next() {
		if len(values) >= maxExternalHistoryRecords {
			return nil, errors.Join(ErrSnapshotBound, rows.Close())
		}
		var ordinal int64
		var kind, eventID, observationEvent, observationID, correctionEvent, correctionID, resolutionEvent, resolutionID sql.NullString
		var kindLength, eventIDLength, bodyHashLength, observationIDLength, observationHashLength int64
		var correctionIDLength, correctionHashLength, resolutionIDLength, resolutionHashLength int64
		var bodyHash, observationHash, correctionHash, resolutionHash []byte
		if err := rows.Scan(&ordinal, &kind, &kindLength, &eventID, &eventIDLength, &bodyHash, &bodyHashLength,
			&observationEvent, &observationID, &observationIDLength, &observationHash, &observationHashLength,
			&correctionEvent, &correctionID, &correctionIDLength, &correctionHash, &correctionHashLength,
			&resolutionEvent, &resolutionID, &resolutionIDLength, &resolutionHash, &resolutionHashLength); err != nil {
			return nil, errors.Join(ErrSnapshotCorrupt, err, rows.Close())
		}
		if ordinal <= previous || !kind.Valid || kindLength <= 0 || kindLength > 32 || !eventID.Valid || eventIDLength <= 0 || eventIDLength > 128 ||
			bodyHashLength != 32 || len(bodyHash) != 32 {
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		var id string
		var hash []byte
		switch kind.String {
		case "OBSERVATION":
			if !observationEvent.Valid || observationEvent.String != eventID.String || !observationID.Valid || observationIDLength <= 0 || observationIDLength > 128 ||
				observationHashLength != 32 || len(observationHash) != 32 || !bytes.Equal(bodyHash, observationHash) ||
				correctionEvent.Valid || correctionID.Valid || correctionHashLength != 0 || resolutionEvent.Valid || resolutionID.Valid || resolutionHashLength != 0 {
				return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
			}
			id, hash = observationID.String, observationHash
		case "CORRECTION":
			if !correctionEvent.Valid || correctionEvent.String != eventID.String || !correctionID.Valid || correctionIDLength <= 0 || correctionIDLength > 128 ||
				correctionHashLength != 32 || len(correctionHash) != 32 || !bytes.Equal(bodyHash, correctionHash) ||
				observationEvent.Valid || observationID.Valid || observationHashLength != 0 || resolutionEvent.Valid || resolutionID.Valid || resolutionHashLength != 0 {
				return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
			}
			id, hash = correctionID.String, correctionHash
		case "RESOLUTION":
			if !resolutionEvent.Valid || resolutionEvent.String != eventID.String || !resolutionID.Valid || resolutionIDLength <= 0 || resolutionIDLength > 128 ||
				resolutionHashLength != 32 || len(resolutionHash) != 32 || !bytes.Equal(bodyHash, resolutionHash) ||
				observationEvent.Valid || observationID.Valid || observationHashLength != 0 || correctionEvent.Valid || correctionID.Valid || correctionHashLength != 0 {
				return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
			}
			id, hash = resolutionID.String, resolutionHash
		default:
			return nil, errors.Join(ErrSnapshotCorrupt, rows.Close())
		}
		values = append(values, basExternalEventHeader{ordinal: ordinal, kind: kind.String, id: id, hash: bytes.Clone(hash)})
		previous = ordinal
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return values, nil
}

func (reader *BASExternalHistoryReader) attemptForExactPreparation(ctx context.Context, org string,
	exact *tammyv1.SourceRef) (AttemptSnapshot, error) {
	retained, err := reader.preparations.GetExact(ctx, org, exact)
	if err != nil || retained == nil || retained.Preparation == nil || retained.Preparation.FrozenBas == nil {
		return AttemptSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
	}
	frozenRef := cloneRef(retained.Preparation.FrozenBas)
	frozen, err := reader.frozen.GetExact(ctx, org, frozenRef)
	if err != nil || frozen == nil || !sameBASSourceRef(frozen.Family, retained.Preparation.Family) ||
		frozen.Preparation == nil || frozen.Preparation.Id != retained.Preparation.PreparationId {
		return AttemptSnapshot{}, errors.Join(ErrSnapshotCorrupt, err)
	}
	return AttemptSnapshot{Preparation: retained.Preparation, FrozenReference: frozenRef, Frozen: frozen}, nil
}

func (reader *BASExternalHistoryReader) confirmedDependencies(ctx context.Context, org string,
	chain ChainSnapshot) ([]AttemptSnapshot, error) {
	if chain.LatestHead == nil {
		if chain.EffectiveOriginal != nil {
			return nil, ErrSnapshotCorrupt
		}
		return []AttemptSnapshot{}, nil
	}
	if chain.EffectiveOriginal == nil {
		return nil, ErrSnapshotCorrupt
	}
	current := chain.LatestHead
	reverse := make([]AttemptSnapshot, 0)
	visited := make(map[string]struct{})
	reachedEffectiveOriginal := false
	for steps := 0; steps < maxExternalHistoryAttempts; steps++ {
		currentRef := preparationReference(current)
		key := refKey(currentRef)
		if _, exists := visited[key]; exists {
			return nil, ErrSnapshotCorrupt
		}
		visited[key] = struct{}{}
		if current.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
			attempt, err := reader.attemptForExactPreparation(ctx, org, currentRef)
			if err != nil {
				return nil, err
			}
			reverse = append(reverse, attempt)
			if len(reverse) > maxExternalConfirmedDependencies {
				return nil, ErrSnapshotBound
			}
		} else if current.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION &&
			current.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION {
			return nil, ErrSnapshotCorrupt
		}
		if sameBASSourceRef(currentRef, preparationReference(chain.EffectiveOriginal)) {
			reachedEffectiveOriginal = true
			break
		}
		if !validBASSourceRef(current.Predecessor, preparationSnapshotSpec.resource, "") {
			return nil, ErrSnapshotCorrupt
		}
		prior, err := reader.preparations.GetExact(ctx, org, current.Predecessor)
		if err != nil || prior == nil || !sameBASSourceRef(prior.Preparation.Family, chain.Family.Reference) {
			return nil, errors.Join(ErrSnapshotCorrupt, err)
		}
		current = prior.Preparation
	}
	if !reachedEffectiveOriginal {
		return nil, ErrSnapshotBound
	}
	if len(reverse) == 0 || !sameBASSourceRef(preparationReference(reverse[len(reverse)-1].Preparation), preparationReference(chain.EffectiveOriginal)) {
		return nil, ErrSnapshotCorrupt
	}
	result := make([]AttemptSnapshot, len(reverse))
	for i := range reverse {
		result[len(reverse)-1-i] = reverse[i]
	}
	return result, nil
}
