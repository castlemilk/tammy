package baspreparation

import (
	"context"
	"errors"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (service *Service) regenerateCorrectiveGenesis(
	ctx context.Context,
	repos CommandRepositories,
	request *tammyv1.RegenerateBasPreparationRequest,
) (*RegenerationResult, error) {
	if service == nil || request == nil || request.CommandContext == nil || request.ExpectedVersion != 0 ||
		repos.ExternalHistory == nil || repos.FamilyStates == nil || repos.BranchIdentities == nil ||
		repos.BranchStates == nil || repos.Preparations == nil || repos.Periods == nil || repos.Profiles == nil ||
		repos.CreateExternalAttempt == nil || repos.Calculations == nil || repos.Resolutions == nil || request.Period == nil || request.Profile == nil {
		return nil, ErrCommandUnavailable
	}
	familyRef := request.FamilyExpectation.GetRetained()
	if !validBASSourceRef(familyRef, "bas_family", "") || familyRef.Revision != 1 ||
		!basExpectedIdentityMatches(request.FamilyExpectation, familyRef) ||
		!basExpectedIdentityMatches(request.PreparationExpectation, nil) {
		return nil, ErrCommandConflict
	}
	history, err := repos.ExternalHistory.Read(ctx, request.OrganisationId, familyRef)
	if err != nil {
		return nil, err
	}
	chain := history.Chain
	if chain.Family == nil || !sameBASSourceRef(chain.Family.Reference, familyRef) || chain.Branch == nil ||
		chain.LatestHead == nil || chain.Branch.ActiveAttempt != nil || chain.Branch.Resolution == nil ||
		chain.Branch.FrozenBarrier != nil ||
		chain.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		return nil, ErrCommandConflict
	}
	predecessorRef := preparationReference(chain.LatestHead)
	if predecessorRef == nil || !samePreparationAggregate(chain.Branch.RootHead, chain.LatestHead) {
		return nil, ErrCommandConflict
	}
	familyBefore, err := repos.FamilyStates.GetCurrent(ctx, request.OrganisationId, familyRef)
	if err != nil || familyBefore == nil || familyBefore.Version != chain.Family.Version ||
		!sameBASSourceRef(familyBefore.ActiveBranch.GetRetained(), chain.Branch.Reference) ||
		familyBefore.ActiveBranch.GetExpectedVersion() != chain.Branch.Version ||
		!sameBASSourceRef(familyBefore.RootPreparation, preparationReference(chain.RootPreparation)) ||
		!sameOptionalRef(familyBefore.EffectiveOriginal, preparationReference(chain.EffectiveOriginal)) ||
		!sameBASSourceRef(familyBefore.LatestHead.GetRetained(), predecessorRef) ||
		familyBefore.LastFamilyEventOrdinal != history.LastOrdinal ||
		familyBefore.DisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	branchIdentity, err := repos.BranchIdentities.GetExact(ctx, request.OrganisationId, chain.Branch.Reference)
	if err != nil || branchIdentity == nil || branchIdentity.OriginalPurpose != tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION ||
		!sameBASSourceRef(branchIdentity.Family, familyRef) ||
		!sameBASSourceRef(branchIdentity.FamilyRootPreparation, preparationReference(chain.RootPreparation)) ||
		!sameOptionalRef(branchIdentity.RootHead.GetRetained(), predecessorRef) ||
		!sameOptionalRef(branchIdentity.EffectiveOriginal, preparationReference(chain.EffectiveOriginal)) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	branchBefore, err := repos.BranchStates.GetCurrent(ctx, request.OrganisationId, chain.Branch.Reference)
	if err != nil || branchBefore == nil || branchBefore.Version != chain.Branch.Version ||
		!sameBASSourceRef(branchBefore.BranchIdentity, chain.Branch.Reference) ||
		branchBefore.WorkflowState != tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE ||
		!sameBASSourceRef(branchBefore.Resolution, chain.Branch.Resolution) ||
		!basExpectedIdentityMatches(branchBefore.ActiveAttempt, nil) ||
		branchBefore.FrozenBarrier != nil || branchBefore.BarrierPreparation != nil {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	predecessor, err := repos.Preparations.GetExact(ctx, request.OrganisationId, predecessorRef)
	if err != nil || predecessor == nil || predecessor.Preparation == nil ||
		predecessor.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED ||
		!sameBASSourceRef(predecessor.Preparation.Family, familyRef) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	period, err := repos.Periods.GetExact(ctx, request.OrganisationId, request.Period)
	if err != nil || period == nil || !sameBASSourceRef(confirmedPeriodReference(period), request.Period) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	currentPeriod, currentPeriodRef, err := repos.Periods.CurrentForScope(ctx, request.OrganisationId, period.Confirmation)
	if err != nil || currentPeriod == nil || !sameBASSourceRef(currentPeriodRef, request.Period) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	scope, err := FamilyScopeFromPeriod(currentPeriod)
	if err != nil || !SameFamilyScope(chain.Family.Scope, scope) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	profile, effectiveProfileRef, err := repos.Profiles.GetEffectiveForPeriod(ctx, request.OrganisationId, period.Confirmation.Period)
	if err != nil || profile == nil || !sameBASSourceRef(effectiveProfileRef, request.Profile) ||
		!sameBASSourceRef(period.Confirmation.Profile, request.Profile) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	profile, err = repos.Profiles.GetExact(ctx, request.OrganisationId, request.Profile)
	if err != nil || profile == nil || !sameBASSourceRef(profileReference(profile), request.Profile) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	resolution, err := repos.Resolutions.GetExact(ctx, request.OrganisationId, chain.Branch.Resolution)
	if err != nil || resolution == nil || resolution.Resolution == nil || !validReason(resolution.Resolution.Reason) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	build, err := repos.CreateExternalAttempt(ctx, BASExternalAttemptBuild{
		Kind: BASExternalAttemptRevision, CorrectiveGenesis: true, CreationReason: resolution.Resolution.Reason,
		OrganisationID: request.OrganisationId, Family: cloneBASRef(familyRef),
		BeforeFamily: familyBefore, BeforeBranch: branchBefore,
		BranchIdentity: branchIdentity, ExistingBranchRef: cloneBASRef(chain.Branch.Reference),
		Predecessor: predecessor, PredecessorReference: cloneBASRef(predecessorRef),
		Period: currentPeriod, Profile: profile,
		Authentication: proto.Clone(request.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
		ActorUserID:    request.CommandContext.Authentication.ActorUserId,
		OperationKey:   request.CommandContext.IdempotencyKey,
		At:             timestamppb.New(service.now().UTC()),
	})
	if err != nil {
		return nil, err
	}
	actual, _, err := verifyExternalAttemptBuild(ctx, repos, chain, familyBefore, branchBefore,
		chain.Branch.Reference, branchIdentity, request.Period, request.Profile,
		predecessorRef, build, BASExternalAttemptRevision)
	if err != nil || build.Predecessor != nil || build.Preparation == nil || build.Calculation == nil ||
		!proto.Equal(actual, build.Preparation) || !sameBASSourceRef(actual.Predecessor, predecessorRef) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	return &RegenerationResult{
		Response: &tammyv1.RegenerateBasPreparationResponse{
			Preparation: actual, Calculation: build.Calculation,
		},
		Source: preparationReference(actual), Previous: cloneBASRef(predecessorRef),
		Affected: []*tammyv1.SourceRef{calculationReference(build.Calculation), cloneBASRef(build.Calculation.CentWorkpaper)},
	}, nil
}
