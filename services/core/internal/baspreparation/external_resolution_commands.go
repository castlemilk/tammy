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

const basResolveDisputePurpose = "bas_resolve_external_dispute"

func (service *Service) ResolveExternalBasDispute(ctx context.Context,
	request *tammyv1.ResolveExternalBasDisputeRequest) (*tammyv1.ResolveExternalBasDisputeResponse, error) {
	if service == nil || request == nil || request.Resolution == nil ||
		!validBASCommandContext(request.CommandContext) || !ids.IsCanonicalV7(request.OrganisationId) ||
		request.ExpectedVersion == 0 || request.ExpectedVersion >= math.MaxInt64 || request.Chain == nil {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.ResolveExternalBasDisputeResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationResolveExternalBasDispute,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.ResolveExternalBasDisputeResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.ResolveExternalBasDisputeRequest)
			if !ok || repos.ExternalHistory == nil || repos.Resolutions == nil || repos.FamilyStates == nil ||
				repos.BranchIdentities == nil || repos.BranchStates == nil || repos.Preparations == nil ||
				repos.ConsumeFreshFactor == nil {
				return ErrCommandUnavailable
			}
			if err := authorisation.ValidateFreshFactor(r.CommandContext.FreshFactor, basResolveDisputePurpose, service.now()); err != nil {
				return err
			}
			id, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(id) {
				return ErrInvalidCommand
			}
			eventID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(eventID) {
				return ErrInvalidCommand
			}
			// The request does not carry a family ID separately; the exact family is
			// pinned by the required Family selector in the chain expectation.
			familyRef := r.Chain.Family
			if familyRef == nil || familyRef.Type != "bas_family" || familyRef.Revision != 1 {
				return ErrCommandConflict
			}
			history, err := repos.ExternalHistory.Read(ctx, r.OrganisationId, familyRef)
			if err != nil {
				return err
			}
			if err := requireExternalExpectedChain(history.Chain, r.ExpectedVersion, r.Chain); err != nil {
				return err
			}
			plan, err := PlanResolveDispute(DisputeResolutionInput{Chain: history.Chain,
				ExpectedChain: r.Chain, History: history,
				ConfirmedDependencies: history.ConfirmedDependencies,
				Resolution:            r.Resolution})
			if err != nil {
				return errors.Join(ErrCommandConflict, err)
			}
			now := service.now().UTC()
			value := &tammyv1.BasExternalDisputeResolution{ResolutionId: id,
				Family:      cloneBASRef(familyRef),
				Resolution:  proto.Clone(r.Resolution).(*tammyv1.BasExternalDisputeInput),
				ActorUserId: r.CommandContext.Authentication.ActorUserId, ResolvedAt: timestamppb.New(now)}
			ref, ordinal, err := repos.Resolutions.AppendWithOrdinal(ctx, r.OrganisationId, familyRef,
				r.CommandContext.IdempotencyKey, eventID, value)
			if err != nil {
				return err
			}
			value.ContentHash = bytes.Clone(ref.ContentHash)
			preparation, err := appendExternalDisputeResolutionTransition(ctx, repos, history, plan,
				ref, ordinal, timestamppb.New(now), service.newID)
			if err != nil {
				return err
			}
			if err := repos.ConsumeFreshFactor(ctx, r.CommandContext.Authentication,
				r.CommandContext.FreshFactor, basResolveDisputePurpose); err != nil {
				return err
			}
			response = &tammyv1.ResolveExternalBasDisputeResponse{Resolution: value, Preparation: preparation}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_DISPUTE_RESOLVED,
				Source: cloneBASRef(ref),
				// The exact immutable resolution reference hashes the full bounded
				// subject set. Keep only the resulting preparation linked here.
				AffectedResources: []*tammyv1.SourceRef{preparationReference(preparation)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Resolution == nil || response.Preparation == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationResolveExternalBasDispute,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Resolution.ResolutionId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.ResolveExternalBasDisputeResponse)
	if !ok || value.Resolution == nil || value.Preparation == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

// appendExternalDisputeResolutionTransition persists the event watermark and
// the planned corrective branch in one transaction. In the no-existing-branch
// case, version 1 is necessarily ACTIVE (the repository's first-version rule),
// then version 2 pins the resolution and becomes CORRECTIVE before FamilyState
// adopts it.
func appendExternalDisputeResolutionTransition(ctx context.Context, repos CommandRepositories,
	history ExternalHistoryInput, plan *DisputeResolutionPlan, resolution *tammyv1.SourceRef,
	eventOrdinal uint64, at *timestamppb.Timestamp, newID func() (string, error)) (*tammyv1.BasPreparation, error) {
	chain := history.Chain
	if plan == nil || !validBASSourceRef(resolution, "bas_external_dispute_resolution", "") ||
		eventOrdinal == 0 || at == nil || !at.IsValid() || chain.Family == nil ||
		repos.FamilyStates == nil || repos.BranchStates == nil || repos.BranchIdentities == nil {
		return nil, ErrCommandUnavailable
	}
	org := chain.Family.Scope.OrganisationID
	family, err := repos.FamilyStates.GetCurrent(ctx, org, chain.Family.Reference)
	if err != nil || family == nil || family.Version != chain.Family.Version ||
		!sameBASSourceRef(family.RootPreparation, preparationReference(chain.RootPreparation)) ||
		!sameOptionalRef(family.EffectiveOriginal, preparationReference(chain.EffectiveOriginal)) ||
		!sameOptionalRef(family.LatestHead.GetRetained(), preparationReference(chain.LatestHead)) ||
		family.DisputeOverlay != chain.DisputeOverlay || eventOrdinal <= family.LastFamilyEventOrdinal {
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
	var responsePreparation *tammyv1.BasPreparation
	var branchRef *tammyv1.SourceRef
	var branchState *tammyv1.BasBranchState
	if plan.NextOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		if !plan.ReserveCorrectiveBranch {
			return nil, ErrChainSnapshot
		}
		if chain.Branch == nil {
			branchID, err := newID()
			if err != nil || !ids.IsCanonicalV7(branchID) {
				return nil, ErrInvalidCommand
			}
			identity := &tammyv1.BasBranchIdentity{BranchId: branchID, OrganisationId: org,
				Family: cloneBASRef(chain.Family.Reference), OriginalPurpose: tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION,
				FamilyRootPreparation: preparationReference(chain.RootPreparation),
				RootHead:              expectedPreparation(chain.LatestHead), EffectiveOriginal: preparationReference(chain.EffectiveOriginal),
				CreatedAt: proto.Clone(at).(*timestamppb.Timestamp)}
			branchRef, err = repos.BranchIdentities.Append(ctx, identity)
			if err != nil {
				return nil, err
			}
			first := &tammyv1.BasBranchState{BranchIdentity: cloneBASRef(branchRef), Version: 1,
				WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
				ActiveAttempt: expectedPreparation(nil), UpdatedAt: proto.Clone(at).(*timestamppb.Timestamp)}
			firstRef, err := repos.BranchStates.Append(ctx, org, first, nil)
			if err != nil {
				return nil, err
			}
			first.ContentHash = bytes.Clone(firstRef.ContentHash)
			branchState = proto.Clone(first).(*tammyv1.BasBranchState)
			branchState.Version++
			branchState.ContentHash = nil
			branchState.WorkflowState = tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE
			branchState.Resolution = cloneBASRef(resolution)
			branchState.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
			firstRefForState := cloneBASRef(firstRef)
			appendedRef, err := repos.BranchStates.Append(ctx, org, branchState, firstRefForState)
			if err != nil {
				return nil, err
			}
			branchState.ContentHash = bytes.Clone(appendedRef.ContentHash)
		} else {
			branchRef = cloneBASRef(chain.Branch.Reference)
			branchState, err = repos.BranchStates.GetCurrent(ctx, org, branchRef)
			if err != nil || branchState == nil || branchState.Version != chain.Branch.Version {
				return nil, errors.Join(ErrCommandConflict, err)
			}
			if branchState.Version >= math.MaxInt64 {
				return nil, ErrCommandConflict
			}
			branchCAS := branchStateReference(branchState)
			nextBranch := proto.Clone(branchState).(*tammyv1.BasBranchState)
			nextBranch.Version++
			nextBranch.ContentHash = nil
			nextBranch.WorkflowState = tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE
			nextBranch.Resolution = cloneBASRef(resolution)
			nextBranch.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
			if plan.ResetMutableAttemptToCollecting {
				if chain.Branch.ActiveAttempt == nil || repos.Preparations == nil {
					return nil, ErrChainSnapshot
				}
				currentRef := preparationReference(chain.Branch.ActiveAttempt)
				if !sameBASSourceRef(branchState.ActiveAttempt.GetRetained(), currentRef) {
					return nil, ErrCommandConflict
				}
				current, err := repos.Preparations.GetExact(ctx, org, currentRef)
				if err != nil || current == nil || current.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING &&
					current.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY {
					return nil, errors.Join(ErrCommandConflict, err)
				}
				updated := proto.Clone(current.Preparation).(*tammyv1.BasPreparation)
				if updated.Version >= math.MaxInt64 {
					return nil, ErrCommandConflict
				}
				updated.Version++
				updated.ContentHash = nil
				updated.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
				updated.CurrentCalculation, updated.CurrentValidation = nil, nil
				updated.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
				retained := &PreparationRetainedSnapshot{Preparation: updated,
					SavedPaygInputs: cloneBASRef(current.SavedPaygInputs), BranchID: cloneBASString(current.BranchID), AttemptID: current.AttemptID}
				updatedRef, err := repos.Preparations.Append(ctx, retained, currentRef)
				if err != nil {
					return nil, err
				}
				updated.ContentHash = bytes.Clone(updatedRef.ContentHash)
				nextBranch.ActiveAttempt = expectedPreparation(updated)
				responsePreparation = updated
			} else if plan.PreserveFrozenAttempt {
				if chain.Branch.ActiveAttempt == nil || plan.FrozenBarrier == nil ||
					!sameBASSourceRef(chain.Branch.ActiveAttempt.FrozenBas, plan.FrozenBarrier) {
					return nil, ErrChainSnapshot
				}
				if !sameBASSourceRef(branchState.ActiveAttempt.GetRetained(), preparationReference(chain.Branch.ActiveAttempt)) {
					return nil, ErrCommandConflict
				}
				nextBranch.BarrierPreparation = preparationReference(chain.Branch.ActiveAttempt)
				nextBranch.FrozenBarrier = cloneBASRef(plan.FrozenBarrier)
				responsePreparation = proto.Clone(chain.Branch.ActiveAttempt).(*tammyv1.BasPreparation)
			} else if chain.Branch.ActiveAttempt != nil {
				responsePreparation = proto.Clone(chain.Branch.ActiveAttempt).(*tammyv1.BasPreparation)
			}
			nextRef, err := repos.BranchStates.Append(ctx, org, nextBranch, branchCAS)
			if err != nil {
				return nil, err
			}
			nextBranch.ContentHash = bytes.Clone(nextRef.ContentHash)
			branchState = nextBranch
		}
		if branchState == nil {
			return nil, ErrSnapshotCorrupt
		}
		// `branchState` is the newly persisted corrective version in both paths.
		// The family CAS expectation below must remain the untouched loaded value.
	} else if plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD {
		return nil, ErrChainSnapshot
	} else if chain.Branch != nil && chain.Branch.ActiveAttempt != nil {
		responsePreparation = proto.Clone(chain.Branch.ActiveAttempt).(*tammyv1.BasPreparation)
	}
	if responsePreparation == nil {
		responsePreparation = chain.LatestHead
		if responsePreparation == nil {
			responsePreparation = chain.RootPreparation
		}
	}
	if responsePreparation == nil || family.Version >= math.MaxInt64 {
		return nil, ErrChainSnapshot
	}
	familyCAS := familyStateReference(family)
	nextFamily := proto.Clone(family).(*tammyv1.BasFamilyState)
	nextFamily.Version++
	nextFamily.ContentHash = nil
	nextFamily.LastFamilyEventOrdinal = eventOrdinal
	nextFamily.DisputeOverlay = plan.NextOverlay
	nextFamily.UpdatedAt = proto.Clone(at).(*timestamppb.Timestamp)
	if branchState != nil && plan.NextOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		nextFamily.ActiveBranch = basExpectedBranchIdentity(branchRef, branchState.Version)
	}
	if _, err := repos.FamilyStates.Append(ctx, org, nextFamily, familyCAS); err != nil {
		return nil, err
	}
	return responsePreparation, nil
}
