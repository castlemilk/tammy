package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"

	"github.com/tammyapp/tammy/services/core/internal/app"
	"github.com/tammyapp/tammy/services/core/internal/authorisation"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const basRecordConfirmationPurpose = "bas_record_external_confirmation"

func validExternalCommand(request *tammyv1.CommandContext, org string, expected uint64,
	chain *tammyv1.BasChainExpectation, frozenID string, frozenHash []byte) bool {
	return validBASCommandContext(request) && ids.IsCanonicalV7(org) && expected > 0 && expected < math.MaxInt64 &&
		chain != nil && ids.IsCanonicalV7(frozenID) && len(frozenHash) == 32
}

func externalObservationRef(value *tammyv1.BasExternalObservation) *tammyv1.SourceRef {
	if value == nil || value.ObservationId == "" || len(value.ContentHash) != 32 {
		return nil
	}
	return &tammyv1.SourceRef{Type: "bas_external_observation", Id: value.ObservationId,
		Revision: 1, ContentHash: bytes.Clone(value.ContentHash)}
}

func findExternalAttempt(history ExternalHistoryInput, frozen *tammyv1.SourceRef) (AttemptSnapshot, bool) {
	for _, attempt := range history.Attempts {
		if sameBASSourceRef(attempt.FrozenReference, frozen) {
			return attempt, true
		}
	}
	for _, attempt := range history.ConfirmedDependencies {
		if sameBASSourceRef(attempt.FrozenReference, frozen) {
			return attempt, true
		}
	}
	return AttemptSnapshot{}, false
}

func requireExternalExpectedChain(chain ChainSnapshot, familyVersion uint64,
	request *tammyv1.BasChainExpectation) error {
	if chain.Family == nil || chain.Family.Version != familyVersion {
		return ErrCommandConflict
	}
	expected, err := BuildChainExpectation(chain)
	if err != nil || request == nil || !proto.Equal(expected, request) {
		return errors.Join(ErrCommandConflict, err)
	}
	return nil
}

func (service *Service) RecordExternalBasObservation(ctx context.Context,
	request *tammyv1.RecordExternalBasObservationRequest) (*tammyv1.RecordExternalBasObservationResponse, error) {
	if service == nil || request == nil || request.Observation == nil ||
		!validExternalCommand(request.CommandContext, request.OrganisationId, request.ExpectedVersion,
			request.Chain, request.FrozenId, request.FrozenHash) {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.RecordExternalBasObservationResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationRecordExternalBasObservation,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.RecordExternalBasObservationResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.RecordExternalBasObservationRequest)
			if !ok || repos.ExternalHistory == nil || repos.Frozen == nil || repos.ExternalObservations == nil ||
				repos.Preparations == nil || repos.FamilyStates == nil || repos.BranchStates == nil {
				return ErrCommandUnavailable
			}
			confirmation := qualifiesExternalConfirmation(r.Observation.GetEventKind())
			if confirmation {
				if err := authorisation.ValidateFreshFactor(r.CommandContext.FreshFactor, basRecordConfirmationPurpose, service.now()); err != nil {
					return err
				}
				if repos.ConsumeFreshFactor == nil {
					return ErrCommandUnavailable
				}
			}
			frozenRef := &tammyv1.SourceRef{Type: "bas_frozen", Id: r.FrozenId, Revision: 1, ContentHash: bytes.Clone(r.FrozenHash)}
			frozen, err := repos.Frozen.GetExact(ctx, r.OrganisationId, frozenRef)
			if err != nil || frozen == nil || frozen.OrganisationId != r.OrganisationId {
				return errors.Join(ErrCommandConflict, err)
			}
			history, err := repos.ExternalHistory.Read(ctx, r.OrganisationId, frozen.Family)
			if err != nil {
				return err
			}
			if err := requireExternalExpectedChain(history.Chain, r.ExpectedVersion, r.Chain); err != nil {
				return err
			}
			attempt, found := findExternalAttempt(history, frozenRef)
			if !found {
				return ErrCommandConflict
			}
			plan, err := PlanObservation(ObservationInput{Chain: history.Chain, Attempt: attempt,
				Observation: r.Observation, History: history})
			if err != nil {
				return errors.Join(ErrCommandConflict, err)
			}
			id, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(id) {
				return ErrInvalidCommand
			}
			eventID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(eventID) {
				return ErrInvalidCommand
			}
			now := service.now().UTC()
			value := &tammyv1.BasExternalObservation{ObservationId: id, OrganisationId: r.OrganisationId,
				FrozenBas: cloneBASRef(frozenRef), Payload: &tammyv1.BasExternalObservation_Observation{
					Observation: proto.Clone(r.Observation).(*tammyv1.BasExternalObservationInput)},
				Trust:       tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
				ActorUserId: r.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
			ref, ordinal, err := repos.ExternalObservations.AppendWithOrdinal(ctx, r.OrganisationId,
				r.CommandContext.IdempotencyKey, eventID, value)
			if err != nil {
				return err
			}
			value.ContentHash = bytes.Clone(ref.ContentHash)
			updatedPrep, err := appendExternalObservationTransition(ctx, repos, history, attempt,
				plan.AdvancesHead, plan.NextOverlay, plan.CompletedResolution, ordinal, timestamppb.New(now))
			if err != nil {
				return err
			}
			if updatedPrep == nil {
				updatedPrep = proto.Clone(attempt.Preparation).(*tammyv1.BasPreparation)
			}
			if confirmation {
				if err := repos.ConsumeFreshFactor(ctx, r.CommandContext.Authentication,
					r.CommandContext.FreshFactor, basRecordConfirmationPurpose); err != nil {
					return err
				}
			}
			response = &tammyv1.RecordExternalBasObservationResponse{Observation: value, Preparation: updatedPrep}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action:            tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_OBSERVATION_RECORDED,
				Source:            cloneBASRef(ref),
				AffectedResources: []*tammyv1.SourceRef{cloneBASRef(frozenRef), preparationReference(updatedPrep)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Observation == nil || response.Preparation == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationRecordExternalBasObservation,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Observation.ObservationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.RecordExternalBasObservationResponse)
	if !ok || value.Observation == nil || value.Preparation == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func (service *Service) CorrectExternalBasObservation(ctx context.Context,
	request *tammyv1.CorrectExternalBasObservationRequest) (*tammyv1.CorrectExternalBasObservationResponse, error) {
	if service == nil || request == nil || request.Correction == nil ||
		!validExternalCommand(request.CommandContext, request.OrganisationId, request.ExpectedVersion,
			request.Chain, request.FrozenId, request.FrozenHash) {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.CorrectExternalBasObservationResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationCorrectExternalBasObservation,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.CorrectExternalBasObservationResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.CorrectExternalBasObservationRequest)
			if !ok || repos.ExternalHistory == nil || repos.Frozen == nil || repos.ExternalObservations == nil ||
				repos.Preparations == nil || repos.FamilyStates == nil || repos.BranchStates == nil {
				return ErrCommandUnavailable
			}
			frozenRef := &tammyv1.SourceRef{Type: "bas_frozen", Id: r.FrozenId, Revision: 1, ContentHash: bytes.Clone(r.FrozenHash)}
			frozen, err := repos.Frozen.GetExact(ctx, r.OrganisationId, frozenRef)
			if err != nil || frozen == nil || frozen.OrganisationId != r.OrganisationId {
				return errors.Join(ErrCommandConflict, err)
			}
			history, err := repos.ExternalHistory.Read(ctx, r.OrganisationId, frozen.Family)
			if err != nil {
				return err
			}
			if err := requireExternalExpectedChain(history.Chain, r.ExpectedVersion, r.Chain); err != nil {
				return err
			}
			attempt, found := findExternalAttempt(history, frozenRef)
			if !found {
				return ErrCommandConflict
			}
			plan, err := PlanCorrection(CorrectionInput{Chain: history.Chain, ExpectedChain: r.Chain,
				Attempt: attempt, History: history, Correction: r.Correction})
			if err != nil {
				return errors.Join(ErrCommandConflict, err)
			}
			id, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(id) {
				return ErrInvalidCommand
			}
			eventID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(eventID) {
				return ErrInvalidCommand
			}
			now := service.now().UTC()
			value := &tammyv1.BasExternalObservation{ObservationId: id, OrganisationId: r.OrganisationId,
				FrozenBas: cloneBASRef(frozenRef), CorrectsObservation: cloneBASRef(r.Correction.DisputedObservation),
				Payload:     &tammyv1.BasExternalObservation_Correction{Correction: proto.Clone(r.Correction).(*tammyv1.BasExternalObservationCorrectionInput)},
				Trust:       tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
				ActorUserId: r.CommandContext.Authentication.ActorUserId, RecordedAt: timestamppb.New(now)}
			ref, ordinal, err := repos.ExternalObservations.AppendWithOrdinal(ctx, r.OrganisationId,
				r.CommandContext.IdempotencyKey, eventID, value)
			if err != nil {
				return err
			}
			value.ContentHash = bytes.Clone(ref.ContentHash)
			updatedPrep, err := appendExternalObservationTransition(ctx, repos, history, attempt,
				false, plan.NextOverlay, nil, ordinal, timestamppb.New(now))
			if err != nil {
				return err
			}
			if updatedPrep == nil {
				updatedPrep = proto.Clone(attempt.Preparation).(*tammyv1.BasPreparation)
			}
			response = &tammyv1.CorrectExternalBasObservationResponse{Correction: value, Preparation: updatedPrep}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_OBSERVATION_CORRECTED,
				Source: cloneBASRef(ref), Previous: cloneBASRef(r.Correction.DisputedObservation),
				AffectedResources: []*tammyv1.SourceRef{cloneBASRef(frozenRef), preparationReference(updatedPrep)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Correction == nil || response.Preparation == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationCorrectExternalBasObservation,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Correction.ObservationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.CorrectExternalBasObservationResponse)
	if !ok || value.Correction == nil || value.Preparation == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func qualifiesExternalConfirmation(kind tammyv1.BasExternalEventKind) bool {
	return kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT ||
		kind == tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE
}

// Append the event ordinal and family-state watermark atomically. A successful
// confirmation appends an exact preparation version, advances LatestHead, and
// closes its completed branch. The stored frozen identity/labels never change.
func appendExternalObservationTransition(ctx context.Context, repos CommandRepositories,
	history ExternalHistoryInput, attempt AttemptSnapshot, advances bool,
	overlay tammyv1.BasDisputeOverlayState, completedResolution *tammyv1.SourceRef,
	eventOrdinal uint64, at *timestamppb.Timestamp) (*tammyv1.BasPreparation, error) {
	chain := history.Chain
	if repos.Preparations == nil || repos.FamilyStates == nil || repos.BranchStates == nil ||
		chain.Family == nil || eventOrdinal == 0 || at == nil || !at.IsValid() {
		return nil, ErrCommandUnavailable
	}
	family, err := repos.FamilyStates.GetCurrent(ctx, chain.Family.Scope.OrganisationID, chain.Family.Reference)
	if err != nil || family == nil || family.Version != chain.Family.Version ||
		!sameBASSourceRef(family.RootPreparation, preparationReference(chain.RootPreparation)) ||
		!sameOptionalRef(family.EffectiveOriginal, preparationReference(chain.EffectiveOriginal)) ||
		!sameOptionalRef(family.LatestHead.GetRetained(), preparationReference(chain.LatestHead)) ||
		family.DisputeOverlay != chain.DisputeOverlay {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	if chain.Branch == nil {
		if family.ActiveBranch.GetRetained() != nil {
			return nil, ErrCommandConflict
		}
	} else if !sameBASSourceRef(family.ActiveBranch.GetRetained(), chain.Branch.Reference) ||
		family.ActiveBranch.GetExpectedVersion() != chain.Branch.Version {
		return nil, ErrCommandConflict
	}
	var updated *tammyv1.BasPreparation
	var updatedRef *tammyv1.SourceRef
	var branchIdentity *tammyv1.BasBranchIdentity
	if chain.Branch != nil {
		if repos.BranchIdentities == nil {
			return nil, ErrCommandUnavailable
		}
		branchIdentity, err = repos.BranchIdentities.GetExact(ctx, chain.Family.Scope.OrganisationID, chain.Branch.Reference)
		if err != nil || branchIdentity == nil || !sameBASSourceRef(branchIdentity.Family, chain.Family.Reference) {
			return nil, errors.Join(ErrSnapshotCorrupt, err)
		}
	}
	if advances {
		if attempt.Preparation == nil || attempt.Frozen == nil || eventOrdinal <= family.LastFamilyEventOrdinal ||
			attempt.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF {
			return nil, ErrCommandConflict
		}
		retained, err := repos.Preparations.GetExact(ctx, chain.Family.Scope.OrganisationID, preparationReference(attempt.Preparation))
		if err != nil || retained == nil || !sameBASSourceRef(retained.Preparation.FrozenBas, attempt.FrozenReference) {
			return nil, errors.Join(ErrCommandConflict, err)
		}
		next := proto.Clone(retained.Preparation).(*tammyv1.BasPreparation)
		if next.Version >= math.MaxInt64 {
			return nil, ErrCommandConflict
		}
		next.Version++
		next.ContentHash = nil
		next.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
		next.DisputeOverlay = overlay
		next.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
		nextRetained := &PreparationRetainedSnapshot{Preparation: next,
			SavedPaygInputs: cloneBASRef(retained.SavedPaygInputs), BranchID: cloneBASString(retained.BranchID), AttemptID: retained.AttemptID}
		updatedRef, err = repos.Preparations.Append(ctx, nextRetained, preparationReference(retained.Preparation))
		if err != nil {
			return nil, err
		}
		next.ContentHash = bytes.Clone(updatedRef.ContentHash)
		updated = next
		if branchIdentity != nil && branchIdentity.OriginalPurpose == tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION {
			if chain.LatestHead == nil {
				return nil, ErrChainSnapshot
			}
			priorRef := preparationReference(chain.LatestHead)
			prior, err := repos.Preparations.GetExact(ctx, chain.Family.Scope.OrganisationID, priorRef)
			if err != nil || prior == nil || prior.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
				prior.Preparation.SupersededBy != nil {
				return nil, errors.Join(ErrCommandConflict, err)
			}
			superseded := proto.Clone(prior.Preparation).(*tammyv1.BasPreparation)
			if superseded.Version >= math.MaxInt64 {
				return nil, ErrCommandConflict
			}
			superseded.Version++
			superseded.ContentHash = nil
			superseded.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION
			superseded.SupersededBy = cloneBASRef(updatedRef)
			superseded.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
			priorSuccessor := &PreparationRetainedSnapshot{Preparation: superseded,
				SavedPaygInputs: cloneBASRef(prior.SavedPaygInputs), BranchID: cloneBASString(prior.BranchID), AttemptID: prior.AttemptID}
			priorSuccessorRef, err := repos.Preparations.Append(ctx, priorSuccessor, priorRef)
			if err != nil {
				return nil, err
			}
			superseded.ContentHash = bytes.Clone(priorSuccessorRef.ContentHash)
		}
		if chain.Branch != nil {
			branch, err := repos.BranchStates.GetCurrent(ctx, chain.Family.Scope.OrganisationID, chain.Branch.Reference)
			if err != nil || branch == nil || branch.Version != chain.Branch.Version ||
				!sameBASSourceRef(branch.ActiveAttempt.GetRetained(), preparationReference(attempt.Preparation)) {
				return nil, errors.Join(ErrCommandConflict, err)
			}
			if branch.Version >= math.MaxInt64 {
				return nil, ErrCommandConflict
			}
			nextBranch := proto.Clone(branch).(*tammyv1.BasBranchState)
			nextBranch.Version++
			nextBranch.ContentHash = nil
			nextBranch.ActiveAttempt = expectedPreparation(updated)
			// Any branch attempt that becomes the confirmed family head is closed.
			// Corrective branches retain the exact resolution proof which opened them.
			nextBranch.WorkflowState = tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_RESOLVED
			if completedResolution != nil {
				if branch.Resolution == nil || !sameBASSourceRef(branch.Resolution, completedResolution) {
					return nil, ErrCommandConflict
				}
				nextBranch.Resolution = cloneBASRef(completedResolution)
			}
			nextBranch.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
			if _, err := repos.BranchStates.Append(ctx, chain.Family.Scope.OrganisationID, nextBranch, branchStateReference(branch)); err != nil {
				return nil, err
			}
		}
	}
	if eventOrdinal <= family.LastFamilyEventOrdinal || family.Version >= math.MaxInt64 {
		return nil, ErrCommandConflict
	}
	familyCAS := familyStateReference(family)
	nextFamily := proto.Clone(family).(*tammyv1.BasFamilyState)
	nextFamily.Version++
	nextFamily.ContentHash = nil
	nextFamily.LastFamilyEventOrdinal = eventOrdinal
	nextFamily.DisputeOverlay = overlay
	nextFamily.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
	if advances {
		nextFamily.LatestHead = expectedPreparation(updated)
		if nextFamily.EffectiveOriginal == nil {
			nextFamily.EffectiveOriginal = preparationReference(updated)
		}
		if chain.Branch != nil {
			nextFamily.ActiveBranch = basExpectedBranchIdentity(nil, 0)
		}
	}
	if _, err := repos.FamilyStates.Append(ctx, chain.Family.Scope.OrganisationID, nextFamily, familyCAS); err != nil {
		return nil, err
	}
	return updated, nil
}
