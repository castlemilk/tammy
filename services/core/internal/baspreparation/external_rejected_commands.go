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

func (service *Service) ReplaceRejectedBasAttempt(ctx context.Context,
	request *tammyv1.ReplaceRejectedBasAttemptRequest) (*tammyv1.ReplaceRejectedBasAttemptResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || request.ExpectedVersion == 0 || request.ExpectedVersion >= math.MaxInt64 ||
		request.Chain == nil || !ids.IsCanonicalV7(request.RejectedPreparationId) || len(request.RejectedPreparationHash) != 32 ||
		!validBASSourceRef(request.ResolvedRejection, "bas_external_observation", "") {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.ReplaceRejectedBasAttemptResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:    app.OrdinaryOperationReplaceRejectedBasAttempt,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication,
		Request: request, NewResult: func() proto.Message { return &tammyv1.ReplaceRejectedBasAttemptResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.ReplaceRejectedBasAttemptRequest)
			if !ok || repos.ExternalHistory == nil || repos.Preparations == nil || repos.Frozen == nil ||
				repos.FamilyStates == nil || repos.BranchIdentities == nil || repos.BranchStates == nil ||
				repos.Profiles == nil || repos.Periods == nil || repos.CreateExternalAttempt == nil || repos.ConsumeFreshFactor == nil {
				return ErrCommandUnavailable
			}
			if err := authorisation.ValidateFreshFactor(r.CommandContext.FreshFactor,
				authorisation.PurposeBASReplaceRejectedAttempt, service.now()); err != nil {
				return err
			}
			familyRef := r.Chain.Family
			if !validBASSourceRef(familyRef, "bas_family", "") || familyRef.Revision != 1 {
				return ErrCommandConflict
			}
			history, err := repos.ExternalHistory.Read(ctx, r.OrganisationId, familyRef)
			if err != nil {
				return err
			}
			if err := checkBASExpectedChain(history.Chain, r.Chain, r.ExpectedVersion); err != nil {
				return err
			}
			rejectedRef := &tammyv1.SourceRef{Type: "bas_preparation", Id: r.RejectedPreparationId,
				Revision: 0, ContentHash: bytes.Clone(r.RejectedPreparationHash)}
			var attempt AttemptSnapshot
			found := false
			for _, candidate := range history.Attempts {
				if candidate.Preparation != nil && candidate.Preparation.PreparationId == r.RejectedPreparationId &&
					bytes.Equal(candidate.Preparation.ContentHash, r.RejectedPreparationHash) {
					attempt, found = candidate, true
					break
				}
			}
			if !found || attempt.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF ||
				attempt.Preparation.FrozenBas == nil || !sameBASSourceRef(preparationReference(attempt.Preparation),
				&tammyv1.SourceRef{Type: "bas_preparation", Id: rejectedRef.Id, Revision: attempt.Preparation.Version,
					ContentHash: rejectedRef.ContentHash}) {
				return ErrCommandConflict
			}
			current, err := repos.Preparations.GetCurrent(ctx, r.OrganisationId, r.RejectedPreparationId)
			if err != nil || current == nil || !sameBASSourceRef(preparationReference(current.Preparation), preparationReference(attempt.Preparation)) ||
				!proto.Equal(current.Preparation, attempt.Preparation) {
				return errors.Join(ErrCommandConflict, err)
			}
			plan, err := PlanRejectedRecovery(RejectedRecoveryInput{Chain: history.Chain, ExpectedChain: r.Chain,
				Attempt: attempt, History: history, ResolvedRejection: r.ResolvedRejection, Reason: r.Reason})
			if err != nil || plan == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			familyBefore, err := repos.FamilyStates.GetCurrent(ctx, r.OrganisationId, familyRef)
			if err != nil || familyBefore == nil || familyBefore.Version != r.ExpectedVersion {
				return errors.Join(ErrCommandConflict, err)
			}
			var beforeBranch *tammyv1.BasBranchState
			var branchIdentity *tammyv1.BasBranchIdentity
			var branchRef *tammyv1.SourceRef
			if history.Chain.Branch != nil {
				branchRef = cloneBASRef(history.Chain.Branch.Reference)
				branchIdentity, err = repos.BranchIdentities.GetExact(ctx, r.OrganisationId, branchRef)
				if err != nil || branchIdentity == nil || !sameBASSourceRef(branchIdentity.Family, familyRef) {
					return errors.Join(ErrCommandConflict, err)
				}
				beforeBranch, err = repos.BranchStates.GetCurrent(ctx, r.OrganisationId, branchRef)
				if err != nil || beforeBranch == nil || beforeBranch.Version != history.Chain.Branch.Version ||
					!sameBASSourceRef(beforeBranch.ActiveAttempt.GetRetained(), preparationReference(attempt.Preparation)) {
					return errors.Join(ErrCommandConflict, err)
				}
			} else {
				id, err := service.newID()
				if err != nil || !ids.IsCanonicalV7(id) {
					return errors.Join(ErrInvalidCommand, err)
				}
				branchIdentity = newExternalBranchIdentity(id, r.OrganisationId, history.Chain,
					tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REPLACEMENT, timestamppb.New(service.now().UTC()))
				if branchIdentity == nil {
					return ErrInvalidCommand
				}
			}
			predecessor := current
			profile, err := repos.Profiles.GetExact(ctx, r.OrganisationId, predecessor.Preparation.Profile)
			if err != nil || profile == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			period, err := repos.Periods.GetExact(ctx, r.OrganisationId, predecessor.Preparation.Period)
			if err != nil || period == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			at := timestamppb.New(service.now().UTC())
			build, err := repos.CreateExternalAttempt(ctx, BASExternalAttemptBuild{
				Kind: BASExternalAttemptRejectedRecovery, CreationReason: r.Reason, OrganisationID: r.OrganisationId,
				Family: cloneBASRef(familyRef), BeforeFamily: familyBefore, BeforeBranch: beforeBranch,
				BranchIdentity: branchIdentity, ExistingBranchRef: branchRef,
				Predecessor: predecessor, PredecessorReference: cloneBASRef(plan.PredecessorPreparation),
				PredecessorFrozen: cloneBASRef(plan.PredecessorFrozen), ResolvedRejection: cloneBASRef(plan.ResolvedRejection),
				ClearFrozenBarrier: plan.ClearFrozenBarrier,
				Period:             period, Profile: profile, ActorUserID: r.CommandContext.Authentication.ActorUserId,
				Authentication: proto.Clone(r.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
				OperationKey:   r.CommandContext.IdempotencyKey, At: at,
			})
			if err != nil {
				return err
			}
			if build.SuccessorPredecessor == nil || build.Predecessor == nil || build.Preparation == nil ||
				build.SuccessorPredecessor.PreparationId != attempt.Preparation.PreparationId ||
				build.Predecessor.PreparationId != attempt.Preparation.PreparationId ||
				!sameTerminalPredecessorState(attempt.Preparation, build.SuccessorPredecessor,
					tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION, false) ||
				!sameTerminalPredecessorState(attempt.Preparation, build.Predecessor,
					tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION, true) ||
				!sameBASSourceRef(build.Predecessor.SupersededBy, preparationReference(build.Preparation)) ||
				!sameBASSourceRef(build.Predecessor.FrozenBas, plan.PredecessorFrozen) ||
				!sameBASSourceRef(build.Predecessor.CurrentCalculation, attempt.Preparation.CurrentCalculation) ||
				!sameBASSourceRef(build.Predecessor.CurrentValidation, attempt.Preparation.CurrentValidation) {
				return ErrCommandConflict
			}
			directRef := preparationReference(build.SuccessorPredecessor)
			direct, err := repos.Preparations.GetExact(ctx, r.OrganisationId, directRef)
			if err != nil || direct == nil || !proto.Equal(direct.Preparation, build.SuccessorPredecessor) ||
				!sameBASSourceRef(build.Preparation.Predecessor, directRef) {
				return errors.Join(ErrCommandConflict, err)
			}
			actual, _, err := verifyExternalAttemptBuild(ctx, repos, history.Chain, familyBefore, beforeBranch,
				branchRef, branchIdentity, build.Preparation.Period, build.Preparation.Profile,
				directRef, build, BASExternalAttemptRejectedRecovery)
			if err != nil {
				return err
			}
			if !proto.Equal(actual, build.Preparation) {
				return ErrCommandConflict
			}
			if err := repos.ConsumeFreshFactor(ctx, r.CommandContext.Authentication,
				r.CommandContext.FreshFactor, authorisation.PurposeBASReplaceRejectedAttempt); err != nil {
				return err
			}
			response = &tammyv1.ReplaceRejectedBasAttemptResponse{
				RejectedAttempt: build.Predecessor, ReplacementAttempt: actual,
			}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_REJECTED_ATTEMPT_REPLACED,
				Source: preparationReference(actual), Previous: preparationReference(attempt.Preparation),
				AffectedResources: []*tammyv1.SourceRef{cloneBASRef(plan.ResolvedRejection), cloneBASRef(build.BranchReference),
					calculationReferenceForPreparation(actual)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.RejectedAttempt == nil || response.ReplacementAttempt == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationReplaceRejectedBasAttempt,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.ReplacementAttempt.PreparationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.ReplaceRejectedBasAttemptResponse)
	if !ok || value.RejectedAttempt == nil || value.ReplacementAttempt == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func sameTerminalPredecessorState(before, after *tammyv1.BasPreparation,
	state tammyv1.BasPreparationState, linked bool) bool {
	if before == nil || after == nil || before.PreparationId != after.PreparationId ||
		after.Version <= before.Version || after.State != state || (after.SupersededBy != nil) != linked ||
		len(after.ContentHash) != 32 || after.UpdatedAt == nil || !after.UpdatedAt.IsValid() {
		return false
	}
	want := proto.Clone(before).(*tammyv1.BasPreparation)
	want.Version, want.State = after.Version, state
	want.SupersededBy = cloneBASRef(after.SupersededBy)
	want.ContentHash = bytes.Clone(after.ContentHash)
	want.UpdatedAt = proto.Clone(after.UpdatedAt).(*timestamppb.Timestamp)
	return proto.Equal(want, after)
}
