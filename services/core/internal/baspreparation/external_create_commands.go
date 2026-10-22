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

const basUnusedReplacementPurpose = "bas_replace_unused_attempt"

type BASExternalAttemptKind uint8

const (
	BASExternalAttemptReplacement BASExternalAttemptKind = iota + 1
	BASExternalAttemptRevision
	BASExternalAttemptRejectedRecovery
)

// BASExternalAttemptBuild is a typed, transaction-local request to reuse the
// accepted source capture/calculation pipeline for an external branch attempt.
// For a replacement, Predecessor is the current frozen attempt. The builder
// first appends a superseded version with no successor link; that exact version
// becomes the new preparation's Predecessor. After creating the successor, the
// builder appends a second old-preparation version with the exact SupersededBy
// link. For a revision, Predecessor is the confirmed head; it remains untouched
// until actual external confirmation.
//
// If BranchIdentity is already retained (unused replacement within an existing
// branch), the builder must preserve its immutable purpose/root pins. If it is
// new, it appends that identity before the new preparation. In both modes it
// appends branch workflow and family CAS only after the new preparation is the
// exact current active attempt. PAYG values are copied from the predecessor's
// retained calculation but rebound to a new immutable saved-input envelope for
// the new preparation ID. Every cloned retained version clears ContentHash
// before typed Append; capture each expected-current SourceRef from the
// unmodified loaded body before mutating its successor.
type BASExternalAttemptBuild struct {
	Kind                 BASExternalAttemptKind
	Authentication       *tammyv1.AuthenticationContext
	ClearFrozenBarrier   bool
	CorrectiveGenesis    bool
	CreationReason       string
	ResolvedRejection    *tammyv1.SourceRef // rejected recovery only
	OrganisationID       string
	Family               *tammyv1.SourceRef
	BeforeFamily         *tammyv1.BasFamilyState
	BeforeBranch         *tammyv1.BasBranchState // nil when no branch is active
	BranchIdentity       *tammyv1.BasBranchIdentity
	ExistingBranchRef    *tammyv1.SourceRef // same as identity's ref; nil for a new identity
	Predecessor          *PreparationRetainedSnapshot
	PredecessorReference *tammyv1.SourceRef
	PredecessorFrozen    *tammyv1.SourceRef // replacement only
	Period               *tammyv1.BasConfirmedPeriod
	Profile              *tammyv1.BasObligationsProfile
	ActorUserID          string
	OperationKey         string
	At                   *timestamppb.Timestamp
}

type BASExternalAttemptBuildResult struct {
	Calculation          *tammyv1.BasCalculation
	BranchReference      *tammyv1.SourceRef
	SuccessorPredecessor *tammyv1.BasPreparation // terminal unlinked version directly pinned by successor
	Predecessor          *tammyv1.BasPreparation // terminal linked current version for replacement; nil for revision
	Preparation          *tammyv1.BasPreparation // final exact current version after full source capture
}

type BASExternalAttemptCreator func(context.Context, BASExternalAttemptBuild) (BASExternalAttemptBuildResult, error)

func newExternalBranchIdentity(id, org string, chain ChainSnapshot, purpose tammyv1.BasBranchPurpose,
	at *timestamppb.Timestamp) *tammyv1.BasBranchIdentity {
	if !ids.IsCanonicalV7(id) || !ids.IsCanonicalV7(org) || chain.Family == nil || chain.RootPreparation == nil || at == nil || !at.IsValid() {
		return nil
	}
	return &tammyv1.BasBranchIdentity{
		BranchId: id, OrganisationId: org, Family: cloneBASRef(chain.Family.Reference),
		OriginalPurpose: purpose, FamilyRootPreparation: preparationReference(chain.RootPreparation),
		RootHead:          expectedPreparation(chain.LatestHead),
		EffectiveOriginal: preparationReference(chain.EffectiveOriginal),
		CreatedAt:         proto.Clone(at).(*timestamppb.Timestamp),
	}
}

func checkBASExpectedChain(chain ChainSnapshot, expected *tammyv1.BasChainExpectation, familyVersion uint64) error {
	if chain.Family == nil || chain.Family.Version != familyVersion {
		return ErrCommandConflict
	}
	actual, err := BuildChainExpectation(chain)
	if err != nil || expected == nil || !proto.Equal(actual, expected) {
		return errors.Join(ErrCommandConflict, err)
	}
	return nil
}

func currentPrepForRef(ctx context.Context, repos CommandRepositories, org string, exact *tammyv1.SourceRef) (*PreparationRetainedSnapshot, error) {
	if repos.Preparations == nil || exact == nil {
		return nil, ErrCommandUnavailable
	}
	retained, err := repos.Preparations.GetCurrent(ctx, org, exact.Id)
	if err != nil || retained == nil || !sameBASSourceRef(preparationReference(retained.Preparation), exact) {
		return nil, errors.Join(ErrCommandConflict, err)
	}
	return retained, nil
}

// verifyExternalAttemptBuild re-reads all exact outputs through typed readers
// after the creator callback. It rejects an incomplete callback result or an
// accidental update to the family's immutable root/confirmed head/effective
// original/event watermark.
func verifyExternalAttemptBuild(ctx context.Context, repos CommandRepositories,
	before ChainSnapshot, beforeFamily *tammyv1.BasFamilyState, beforeBranch *tammyv1.BasBranchState,
	identityRef *tammyv1.SourceRef, expectedIdentity *tammyv1.BasBranchIdentity,
	periodRef, profileRef, predecessorRef *tammyv1.SourceRef, output BASExternalAttemptBuildResult,
	kind BASExternalAttemptKind) (*tammyv1.BasPreparation, *tammyv1.BasFamilyState, error) {
	if repos.FamilyStates == nil || repos.BranchIdentities == nil || repos.BranchStates == nil ||
		repos.Preparations == nil || repos.Calculations == nil || output.Preparation == nil || output.BranchReference == nil {
		return nil, nil, ErrCommandUnavailable
	}
	branchRef := output.BranchReference
	if !validBASSourceRef(branchRef, "bas_branch", "") || branchRef.Revision != 1 || !ids.IsCanonicalV7(branchRef.Id) || len(branchRef.ContentHash) != 32 {
		return nil, nil, ErrCommandConflict
	}
	if identityRef != nil && !sameBASSourceRef(identityRef, branchRef) {
		return nil, nil, ErrCommandConflict
	}
	identity, err := repos.BranchIdentities.GetExact(ctx, before.Family.Scope.OrganisationID, branchRef)
	if err != nil || identity == nil || !sameBASSourceRef(identity.Family, before.Family.Reference) ||
		!sameBASSourceRef(identity.FamilyRootPreparation, preparationReference(before.RootPreparation)) ||
		!sameOptionalRef(identity.EffectiveOriginal, preparationReference(before.EffectiveOriginal)) {
		return nil, nil, errors.Join(ErrCommandConflict, err)
	}
	if expectedIdentity != nil && (identity.BranchId != expectedIdentity.BranchId ||
		identity.OriginalPurpose != expectedIdentity.OriginalPurpose || !sameBASSourceRef(identity.Family, expectedIdentity.Family) ||
		!sameBASSourceRef(identity.FamilyRootPreparation, expectedIdentity.FamilyRootPreparation) ||
		!proto.Equal(identity.RootHead, expectedIdentity.RootHead) ||
		!sameOptionalRef(identity.EffectiveOriginal, expectedIdentity.EffectiveOriginal)) {
		return nil, nil, ErrCommandConflict
	}
	if kind == BASExternalAttemptRevision && identity.OriginalPurpose != tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION ||
		(kind == BASExternalAttemptReplacement || kind == BASExternalAttemptRejectedRecovery) && identityRef == nil &&
			identity.OriginalPurpose != tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REPLACEMENT {
		return nil, nil, ErrCommandConflict
	}

	family, err := repos.FamilyStates.GetCurrent(ctx, before.Family.Scope.OrganisationID, before.Family.Reference)
	if err != nil || family == nil || family.Version != beforeFamily.Version+2 ||
		!sameBASSourceRef(family.RootPreparation, beforeFamily.RootPreparation) ||
		!sameOptionalRef(family.LatestHead.GetRetained(), beforeFamily.LatestHead.GetRetained()) ||
		!sameOptionalRef(family.EffectiveOriginal, beforeFamily.EffectiveOriginal) ||
		family.DisputeOverlay != beforeFamily.DisputeOverlay ||
		family.LastFamilyEventOrdinal != beforeFamily.LastFamilyEventOrdinal ||
		!sameBASSourceRef(family.ActiveBranch.GetRetained(), branchRef) {
		return nil, nil, errors.Join(ErrCommandConflict, err)
	}
	branch, err := repos.BranchStates.GetCurrent(ctx, before.Family.Scope.OrganisationID, branchRef)
	if err != nil || branch == nil || !sameBASSourceRef(branch.BranchIdentity, branchRef) ||
		!sameBASSourceRef(branch.ActiveAttempt.GetRetained(), preparationReference(output.Preparation)) ||
		branch.BarrierPreparation != nil || branch.FrozenBarrier != nil {
		return nil, nil, errors.Join(ErrCommandConflict, err)
	}
	if beforeBranch == nil {
		if branch.Version != 2 || branch.WorkflowState != tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE || branch.Resolution != nil {
			return nil, nil, ErrCommandConflict
		}
	} else if branch.Version != beforeBranch.Version+2 || branch.WorkflowState != beforeBranch.WorkflowState ||
		!sameOptionalRef(branch.Resolution, beforeBranch.Resolution) {
		return nil, nil, ErrCommandConflict
	}
	retained, err := currentPrepForRef(ctx, repos, before.Family.Scope.OrganisationID, preparationReference(output.Preparation))
	if err != nil || retained == nil || retained.BranchID == nil || *retained.BranchID != branchRef.Id ||
		!sameBASSourceRef(retained.Preparation.Family, before.Family.Reference) ||
		!sameBASSourceRef(retained.Preparation.Predecessor, predecessorRef) ||
		!sameBASSourceRef(retained.Preparation.Period, periodRef) || !sameBASSourceRef(retained.Preparation.Profile, profileRef) ||
		retained.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING ||
		retained.SavedPaygInputs == nil || retained.Preparation.CurrentCalculation == nil {
		return nil, nil, errors.Join(ErrCommandConflict, err)
	}
	calculation, err := repos.Calculations.GetExact(ctx, before.Family.Scope.OrganisationID, retained.Preparation.CurrentCalculation)
	if err != nil || calculation == nil || calculation.Preparation == nil ||
		calculation.Preparation.Id != retained.Preparation.PreparationId ||
		calculation.Preparation.Revision >= retained.Preparation.Version ||
		!sameBASSourceRef(calculation.Family, before.Family.Reference) ||
		!sameBASSourceRef(calculation.Period, periodRef) || !sameBASSourceRef(calculation.Profile, profileRef) ||
		calculation.InputRevision != retained.Preparation.InputRevision || !bytes.Equal(calculation.InputHash, retained.Preparation.InputHash) {
		return nil, nil, errors.Join(ErrCommandConflict, err)
	}
	return retained.Preparation, family, nil
}

func (service *Service) CreateBasReplacement(ctx context.Context,
	request *tammyv1.CreateBasReplacementRequest) (*tammyv1.CreateBasReplacementResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || request.ExpectedVersion == 0 || request.ExpectedVersion >= math.MaxInt64 ||
		request.Chain == nil || !ids.IsCanonicalV7(request.PredecessorFrozenId) || len(request.PredecessorFrozenHash) != 32 {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.CreateBasReplacementResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:    app.OrdinaryOperationCreateBasReplacement,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication,
		Request: request, NewResult: func() proto.Message { return &tammyv1.CreateBasReplacementResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.CreateBasReplacementRequest)
			if !ok || repos.ExternalHistory == nil || repos.Frozen == nil || repos.ExternalObservations == nil ||
				repos.Resolutions == nil || repos.Preparations == nil || repos.FamilyStates == nil ||
				repos.BranchIdentities == nil || repos.BranchStates == nil || repos.CreateExternalAttempt == nil ||
				repos.ConsumeFreshFactor == nil {
				return ErrCommandUnavailable
			}
			if err := authorisation.ValidateFreshFactor(r.CommandContext.FreshFactor, basUnusedReplacementPurpose, service.now()); err != nil {
				return err
			}
			familyRef := r.Chain.Family
			if !validBASSourceRef(familyRef, "bas_family", "") || familyRef.Revision != 1 {
				return ErrCommandConflict
			}
			frozenRef := &tammyv1.SourceRef{Type: "bas_frozen", Id: r.PredecessorFrozenId, Revision: 1, ContentHash: bytes.Clone(r.PredecessorFrozenHash)}
			frozen, err := repos.Frozen.GetExact(ctx, r.OrganisationId, frozenRef)
			if err != nil || frozen == nil || frozen.OrganisationId != r.OrganisationId || !sameBASSourceRef(frozen.Family, familyRef) {
				return errors.Join(ErrCommandConflict, err)
			}
			history, err := repos.ExternalHistory.Read(ctx, r.OrganisationId, familyRef)
			if err != nil {
				return err
			}
			if err := checkBASExpectedChain(history.Chain, r.Chain, r.ExpectedVersion); err != nil {
				return err
			}
			attempt, ok := findExternalAttempt(history, frozenRef)
			if !ok {
				return ErrCommandConflict
			}
			plan, err := PlanCreateReplacement(ReplacementInput{Chain: history.Chain, ExpectedChain: r.Chain,
				Attempt: attempt, History: history, Reason: r.Reason})
			if err != nil {
				return errors.Join(ErrCommandConflict, err)
			}
			familyBefore, err := repos.FamilyStates.GetCurrent(ctx, r.OrganisationId, familyRef)
			if err != nil || familyBefore == nil || familyBefore.Version != r.ExpectedVersion {
				return errors.Join(ErrCommandConflict, err)
			}
			var existingBranchRef *tammyv1.SourceRef
			var beforeBranch *tammyv1.BasBranchState
			var identity *tammyv1.BasBranchIdentity
			var identityRef *tammyv1.SourceRef
			if history.Chain.Branch != nil {
				identityRef = cloneBASRef(history.Chain.Branch.Reference)
				identity, err = repos.BranchIdentities.GetExact(ctx, r.OrganisationId, identityRef)
				if err != nil || identity == nil || !sameBASSourceRef(identity.Family, familyRef) {
					return errors.Join(ErrCommandConflict, err)
				}
				beforeBranch, err = repos.BranchStates.GetCurrent(ctx, r.OrganisationId, identityRef)
				if err != nil || beforeBranch == nil || beforeBranch.Version != history.Chain.Branch.Version ||
					!sameBASSourceRef(beforeBranch.ActiveAttempt.GetRetained(), plan.PredecessorPreparation) {
					return errors.Join(ErrCommandConflict, err)
				}
				existingBranchRef = cloneBASRef(identityRef)
			} else {
				branchID, idErr := service.newID()
				if idErr != nil || !ids.IsCanonicalV7(branchID) {
					return errors.Join(ErrInvalidCommand, idErr)
				}
				identity = newExternalBranchIdentity(branchID, r.OrganisationId, history.Chain,
					tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REPLACEMENT, timestamppb.New(service.now().UTC()))
				if identity == nil {
					return ErrInvalidCommand
				}
			}
			predecessor, err := currentPrepForRef(ctx, repos, r.OrganisationId, plan.PredecessorPreparation)
			if err != nil || predecessor == nil || !sameBASSourceRef(predecessor.Preparation.FrozenBas, frozenRef) {
				return errors.Join(ErrCommandConflict, err)
			}
			profile, err := repos.Profiles.GetExact(ctx, r.OrganisationId, predecessor.Preparation.Profile)
			if err != nil || profile == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			period, err := repos.Periods.GetExact(ctx, r.OrganisationId, predecessor.Preparation.Period)
			if err != nil || period == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			build, err := repos.CreateExternalAttempt(ctx, BASExternalAttemptBuild{
				Kind: BASExternalAttemptReplacement, CreationReason: r.Reason, OrganisationID: r.OrganisationId, Family: cloneBASRef(familyRef),
				BeforeFamily: familyBefore, BeforeBranch: beforeBranch, BranchIdentity: identity, ExistingBranchRef: existingBranchRef,
				Predecessor: predecessor, PredecessorReference: cloneBASRef(plan.PredecessorPreparation), PredecessorFrozen: cloneBASRef(frozenRef),
				ClearFrozenBarrier: plan.ClearFrozenBarrier, Authentication: proto.Clone(r.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
				Period: period, Profile: profile, ActorUserID: r.CommandContext.Authentication.ActorUserId,
				OperationKey: r.CommandContext.IdempotencyKey, At: timestamppb.New(service.now().UTC()),
			})
			if err != nil {
				return err
			}
			expectedPurpose := identity.OriginalPurpose
			if build.SuccessorPredecessor == nil || build.Predecessor == nil || build.Preparation == nil ||
				build.SuccessorPredecessor.PreparationId != attempt.Preparation.PreparationId ||
				build.Predecessor.PreparationId != attempt.Preparation.PreparationId ||
				!sameTerminalPredecessor(attempt.Preparation, build.SuccessorPredecessor, false) ||
				!sameTerminalPredecessor(attempt.Preparation, build.Predecessor, true) ||
				!sameBASSourceRef(build.Predecessor.SupersededBy, preparationReference(build.Preparation)) ||
				!sameBASSourceRef(build.Predecessor.FrozenBas, frozenRef) ||
				!sameBASSourceRef(build.Predecessor.CurrentCalculation, attempt.Preparation.CurrentCalculation) ||
				!sameBASSourceRef(build.Predecessor.CurrentValidation, attempt.Preparation.CurrentValidation) {
				return ErrCommandConflict
			}
			successorPredecessor := preparationReference(build.SuccessorPredecessor)
			direct, err := repos.Preparations.GetExact(ctx, r.OrganisationId, successorPredecessor)
			if err != nil || direct == nil || !proto.Equal(direct.Preparation, build.SuccessorPredecessor) {
				return errors.Join(ErrCommandConflict, err)
			}
			actual, _, err := verifyExternalAttemptBuild(ctx, repos, history.Chain, familyBefore, beforeBranch,
				identityRef, identity, predecessor.Preparation.Period, predecessor.Preparation.Profile,
				successorPredecessor, build, BASExternalAttemptReplacement)
			if err != nil {
				return err
			}
			if !proto.Equal(actual, build.Preparation) || expectedPurpose == tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_UNSPECIFIED {
				return ErrCommandConflict
			}
			linkedCurrent, err := currentPrepForRef(ctx, repos, r.OrganisationId, preparationReference(build.Predecessor))
			if err != nil || linkedCurrent == nil || !proto.Equal(linkedCurrent.Preparation, build.Predecessor) {
				return errors.Join(ErrCommandConflict, err)
			}
			if err := repos.ConsumeFreshFactor(ctx, r.CommandContext.Authentication, r.CommandContext.FreshFactor, basUnusedReplacementPurpose); err != nil {
				return err
			}
			response = &tammyv1.CreateBasReplacementResponse{Predecessor: build.Predecessor, Replacement: actual}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_EXTERNAL_REPLACEMENT_CREATED,
				Source: preparationReference(actual), Previous: cloneBASRef(plan.PredecessorPreparation),
				AffectedResources: []*tammyv1.SourceRef{cloneBASRef(frozenRef), cloneBASRef(build.BranchReference), calculationReferenceForPreparation(actual)},
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Predecessor == nil || response.Replacement == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationCreateBasReplacement, request.CommandContext.Authentication,
				request.CommandContext.IdempotencyKey, response.Replacement.PreparationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.CreateBasReplacementResponse)
	if !ok || value.Predecessor == nil || value.Replacement == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func (service *Service) CreateBasRevision(ctx context.Context,
	request *tammyv1.CreateBasRevisionRequest) (*tammyv1.CreateBasRevisionResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || request.ExpectedVersion == 0 || request.ExpectedVersion >= math.MaxInt64 ||
		request.Chain == nil || !ids.IsCanonicalV7(request.EffectiveOriginalPreparationId) || len(request.EffectiveOriginalPreparationHash) != 32 ||
		!ids.IsCanonicalV7(request.LatestConfirmedHeadId) || len(request.LatestConfirmedHeadHash) != 32 ||
		request.Period == nil || request.Profile == nil {
		return nil, ErrInvalidCommand
	}
	var response *tammyv1.CreateBasRevisionResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:    app.OrdinaryOperationCreateBasRevision,
		OperationKey: request.CommandContext.IdempotencyKey, Authentication: request.CommandContext.Authentication,
		Request: request, NewResult: func() proto.Message { return &tammyv1.CreateBasRevisionResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.CreateBasRevisionRequest)
			if !ok || repos.ExternalHistory == nil || repos.Periods == nil || repos.Profiles == nil ||
				repos.FamilyStates == nil || repos.BranchIdentities == nil || repos.BranchStates == nil ||
				repos.Preparations == nil || repos.CreateExternalAttempt == nil {
				return ErrCommandUnavailable
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
			requestedOriginal := preparationReference(history.Chain.EffectiveOriginal)
			requestedHead := preparationReference(history.Chain.LatestHead)
			if requestedOriginal == nil || requestedHead == nil ||
				requestedOriginal.Id != r.EffectiveOriginalPreparationId || !bytes.Equal(requestedOriginal.ContentHash, r.EffectiveOriginalPreparationHash) ||
				requestedHead.Id != r.LatestConfirmedHeadId || !bytes.Equal(requestedHead.ContentHash, r.LatestConfirmedHeadHash) {
				return ErrCommandConflict
			}
			period, err := repos.Periods.GetExact(ctx, r.OrganisationId, r.Period)
			if err != nil || period == nil || !sameBASSourceRef(confirmedPeriodReference(period), r.Period) {
				return errors.Join(ErrCommandConflict, err)
			}
			currentPeriod, currentPeriodRef, err := repos.Periods.CurrentForScope(ctx, r.OrganisationId, period.Confirmation)
			if err != nil || currentPeriod == nil || !sameBASSourceRef(currentPeriodRef, r.Period) {
				return errors.Join(ErrCommandConflict, err)
			}
			profile, effectiveProfileRef, err := repos.Profiles.GetEffectiveForPeriod(ctx, r.OrganisationId, period.Confirmation.Period)
			if err != nil || profile == nil || !sameBASSourceRef(effectiveProfileRef, r.Profile) ||
				!sameBASSourceRef(period.Confirmation.Profile, r.Profile) {
				return errors.Join(ErrCommandConflict, err)
			}
			profileExact, err := repos.Profiles.GetExact(ctx, r.OrganisationId, r.Profile)
			if err != nil || profileExact == nil || !sameBASSourceRef(profileReference(profileExact), r.Profile) {
				return errors.Join(ErrCommandConflict, err)
			}
			familyBefore, err := repos.FamilyStates.GetCurrent(ctx, r.OrganisationId, familyRef)
			if err != nil || familyBefore == nil || familyBefore.Version != r.ExpectedVersion || history.Chain.Branch != nil ||
				!sameBASSourceRef(familyBefore.LatestHead.GetRetained(), requestedHead) ||
				!sameBASSourceRef(familyBefore.EffectiveOriginal, requestedOriginal) {
				return errors.Join(ErrCommandConflict, err)
			}
			plan, err := PlanCreateRevision(RevisionInput{Chain: history.Chain, ExpectedChain: r.Chain,
				History: history, PeriodSnapshot: period, ProfileSnapshot: profileExact, Reason: r.Reason})
			if err != nil {
				return errors.Join(ErrCommandConflict, err)
			}
			predecessorRef := plan.LatestHead
			predecessor, err := repos.Preparations.GetExact(ctx, r.OrganisationId, predecessorRef)
			if err != nil || predecessor == nil || !sameBASSourceRef(predecessor.Preparation.Family, familyRef) {
				return errors.Join(ErrCommandConflict, err)
			}
			branchID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(branchID) {
				return errors.Join(ErrInvalidCommand, err)
			}
			identity := newExternalBranchIdentity(branchID, r.OrganisationId, history.Chain,
				tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION, timestamppb.New(service.now().UTC()))
			if identity == nil {
				return ErrInvalidCommand
			}
			build, err := repos.CreateExternalAttempt(ctx, BASExternalAttemptBuild{
				Kind: BASExternalAttemptRevision, CreationReason: r.Reason, OrganisationID: r.OrganisationId, Family: cloneBASRef(familyRef),
				BeforeFamily: familyBefore, BranchIdentity: identity, Predecessor: predecessor,
				PredecessorReference: cloneBASRef(predecessorRef), Period: period, Profile: profileExact,
				ActorUserID:    r.CommandContext.Authentication.ActorUserId,
				Authentication: proto.Clone(r.CommandContext.Authentication).(*tammyv1.AuthenticationContext),
				OperationKey:   r.CommandContext.IdempotencyKey,
				At:             timestamppb.New(service.now().UTC()),
			})
			if err != nil {
				return err
			}
			actual, _, err := verifyExternalAttemptBuild(ctx, repos, history.Chain, familyBefore, nil,
				nil, identity, r.Period, r.Profile, predecessorRef, build, BASExternalAttemptRevision)
			if err != nil {
				return err
			}
			if build.Predecessor != nil || actual.PreparationId == predecessor.Preparation.PreparationId ||
				!sameBASSourceRef(actual.Predecessor, predecessorRef) {
				return ErrCommandConflict
			}
			original, err := repos.Preparations.GetExact(ctx, r.OrganisationId, requestedOriginal)
			if err != nil || original == nil || !sameBASSourceRef(preparationReference(original.Preparation), requestedOriginal) {
				return errors.Join(ErrCommandConflict, err)
			}
			response = &tammyv1.CreateBasRevisionResponse{Original: proto.Clone(original.Preparation).(*tammyv1.BasPreparation), Revision: actual}
			affected := []*tammyv1.SourceRef{cloneBASRef(build.BranchReference), calculationReferenceForPreparation(actual)}
			if !sameBASSourceRef(requestedOriginal, requestedHead) {
				affected = append(affected, cloneBASRef(requestedOriginal))
			}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_REVISION_CREATED,
				Source: preparationReference(actual), Previous: cloneBASRef(requestedHead),
				AffectedResources: affected,
			}
			return nil
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Original == nil || response.Revision == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationCreateBasRevision, request.CommandContext.Authentication,
				request.CommandContext.IdempotencyKey, response.Revision.PreparationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.CreateBasRevisionResponse)
	if !ok || value.Original == nil || value.Revision == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}

func sameTerminalPredecessor(before, after *tammyv1.BasPreparation, linked bool) bool {
	if before == nil || after == nil || before.PreparationId != after.PreparationId ||
		after.Version <= before.Version || after.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
		(after.SupersededBy != nil) != linked || after.ContentHash == nil || len(after.ContentHash) != 32 ||
		after.UpdatedAt == nil || !after.UpdatedAt.IsValid() {
		return false
	}
	want := proto.Clone(before).(*tammyv1.BasPreparation)
	want.Version, want.State = after.Version, after.State
	want.SupersededBy = cloneBASRef(after.SupersededBy)
	want.ContentHash = bytes.Clone(after.ContentHash)
	want.UpdatedAt = proto.Clone(after.UpdatedAt).(*timestamppb.Timestamp)
	return proto.Equal(want, after)
}

func calculationReferenceForPreparation(preparation *tammyv1.BasPreparation) *tammyv1.SourceRef {
	if preparation == nil {
		return nil
	}
	return cloneBASRef(preparation.CurrentCalculation)
}
