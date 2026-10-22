package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (handler *basRegenerationHandler) CreateExternalAttempt(
	ctx context.Context,
	in BASExternalAttemptBuild,
) (BASExternalAttemptBuildResult, error) {
	fail := func(err error) (BASExternalAttemptBuildResult, error) {
		if ctx != nil && ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		return BASExternalAttemptBuildResult{}, errors.Join(ErrCommandConflict, err)
	}
	if handler == nil || ctx == nil || in.Authentication == nil || !validReason(in.CreationReason) ||
		!ids.IsCanonicalV7(in.OrganisationID) || !ids.IsCanonicalV7(in.ActorUserID) ||
		in.Authentication.ActorUserId != in.ActorUserID || !ids.IsCanonicalV7(in.OperationKey) ||
		!validBASSourceRef(in.Family, "bas_family", "") || in.Family.Revision != 1 ||
		in.BeforeFamily == nil || !sameBASSourceRef(in.BeforeFamily.Family, in.Family) ||
		in.Predecessor == nil || in.Predecessor.Preparation == nil ||
		!sameBASSourceRef(preparationReference(in.Predecessor.Preparation), in.PredecessorReference) ||
		in.Period == nil || in.Profile == nil ||
		!sameBASSourceRef(confirmedPeriodReference(in.Period), in.Predecessor.Preparation.Period) && in.Kind != BASExternalAttemptRevision ||
		!ids.IsCanonicalV7(in.Predecessor.Preparation.PreparationId) ||
		in.Predecessor.Preparation.OrganisationId != in.OrganisationID ||
		!sameBASSourceRef(in.Predecessor.Preparation.Family, in.Family) ||
		!sameBASSourceRef(confirmedPeriodReference(in.Period), in.PeriodRef()) ||
		!sameBASSourceRef(profileReference(in.Profile), in.ProfileRef()) ||
		in.At == nil || !in.At.IsValid() || handler.preparations == nil || handler.savedPayg == nil ||
		handler.calculations == nil || handler.familyIdentities == nil || handler.familyStates == nil ||
		handler.branchIdentities == nil || handler.branchStates == nil || handler.newID == nil {
		return fail(nil)
	}
	if in.CorrectiveGenesis && (in.ExistingBranchRef == nil || in.BeforeBranch == nil ||
		in.BranchIdentity == nil || in.ClearFrozenBarrier || in.Kind != BASExternalAttemptRevision) {
		return fail(nil)
	}
	if in.Kind != BASExternalAttemptReplacement && in.Kind != BASExternalAttemptRevision &&
		in.Kind != BASExternalAttemptRejectedRecovery {
		return fail(nil)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	familyCurrent, err := handler.familyStates.GetCurrent(ctx, in.OrganisationID, in.Family)
	if err != nil || familyCurrent == nil || !proto.Equal(familyCurrent, in.BeforeFamily) {
		return fail(errors.Join(ErrCommandConflict, err))
	}
	familyCAS := familyStateReference(familyCurrent) // capture before any clone is changed
	if familyCurrent.Version >= math.MaxInt64-2 {
		return fail(ErrCommandConflict)
	}

	// Resolve the branch identity/state against this exact family aggregate.
	var branchRef *tammyv1.SourceRef
	var identity *tammyv1.BasBranchIdentity
	var branchBefore *tammyv1.BasBranchState
	var branchCAS *tammyv1.SourceRef
	var branchID string
	if in.ExistingBranchRef != nil {
		if in.BeforeBranch == nil || in.BranchIdentity == nil ||
			!sameBASSourceRef(familyCurrent.ActiveBranch.GetRetained(), in.ExistingBranchRef) ||
			familyCurrent.ActiveBranch.GetExpectedVersion() != in.BeforeBranch.Version {
			return fail(nil)
		}
		branchRef = cloneBASRef(in.ExistingBranchRef)
		identity, err = handler.branchIdentities.GetExact(ctx, in.OrganisationID, branchRef)
		if err != nil || identity == nil || !proto.Equal(identity, in.BranchIdentity) ||
			!sameBASSourceRef(identity.Family, in.Family) {
			return fail(errors.Join(ErrCommandConflict, err))
		}
		branchBefore, err = handler.branchStates.GetCurrent(ctx, in.OrganisationID, branchRef)
		if err != nil || branchBefore == nil {
			return fail(errors.Join(ErrCommandConflict, err))
		}
		activeAttemptMatches := sameBASSourceRef(branchBefore.ActiveAttempt.GetRetained(), in.PredecessorReference)
		if in.CorrectiveGenesis {
			activeAttemptMatches = branchBefore.ActiveAttempt != nil && basExpectedIdentityMatches(branchBefore.ActiveAttempt, nil) &&
				familyCurrent.DisputeOverlay == tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED &&
				branchBefore.WorkflowState == tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE &&
				branchBefore.Resolution != nil && branchBefore.FrozenBarrier == nil && branchBefore.BarrierPreparation == nil &&
				identity.OriginalPurpose == tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION &&
				sameBASSourceRef(familyCurrent.LatestHead.GetRetained(), in.PredecessorReference) &&
				sameBASSourceRef(identity.FamilyRootPreparation, familyCurrent.RootPreparation) &&
				sameOptionalRef(identity.RootHead.GetRetained(), familyCurrent.LatestHead.GetRetained()) &&
				sameOptionalRef(identity.EffectiveOriginal, familyCurrent.EffectiveOriginal)
		}
		if !proto.Equal(branchBefore, in.BeforeBranch) || !activeAttemptMatches ||
			((branchBefore.FrozenBarrier != nil || branchBefore.BarrierPreparation != nil) && !in.ClearFrozenBarrier) ||
			(branchBefore.FrozenBarrier != nil && !sameBASSourceRef(branchBefore.FrozenBarrier, in.PredecessorFrozen)) ||
			(branchBefore.BarrierPreparation != nil && !sameBASSourceRef(branchBefore.BarrierPreparation, in.PredecessorReference)) ||
			(branchBefore.WorkflowState != tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE &&
				branchBefore.WorkflowState != tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE) ||
			(branchBefore.WorkflowState == tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_CORRECTIVE && branchBefore.Resolution == nil) {
			return fail(errors.Join(ErrCommandConflict, err))
		}
		branchCAS = branchStateReference(branchBefore) // capture before mutation
		branchID = branchRef.Id
	} else {
		if in.BeforeBranch != nil || in.BranchIdentity == nil || familyCurrent.ActiveBranch.GetRetained() != nil ||
			!sameBASSourceRef(in.BranchIdentity.Family, in.Family) ||
			!sameBASSourceRef(in.BranchIdentity.FamilyRootPreparation, familyCurrent.RootPreparation) ||
			!sameOptionalRef(in.BranchIdentity.RootHead.GetRetained(), familyCurrent.LatestHead.GetRetained()) ||
			!sameOptionalRef(in.BranchIdentity.EffectiveOriginal, familyCurrent.EffectiveOriginal) ||
			!ids.IsCanonicalV7(in.BranchIdentity.BranchId) {
			return fail(nil)
		}
		if in.Kind == BASExternalAttemptRevision && in.BranchIdentity.OriginalPurpose != tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION ||
			in.Kind != BASExternalAttemptRevision && in.BranchIdentity.OriginalPurpose != tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REPLACEMENT {
			return fail(nil)
		}
		branchID = in.BranchIdentity.BranchId
		identity = proto.Clone(in.BranchIdentity).(*tammyv1.BasBranchIdentity)
		identity.ContentHash = nil
		branchRef, err = handler.branchIdentities.Append(ctx, identity)
		if err != nil {
			return fail(err)
		}
		identity, err = handler.branchIdentities.GetExact(ctx, in.OrganisationID, branchRef)
		if err != nil || identity == nil {
			return fail(errors.Join(ErrSnapshotCorrupt, err))
		}
	}
	branchIDPtr := cloneBASString(&branchID)

	// Read the exact calculation and PAYG envelope that produced the frozen or
	// confirmed predecessor. The new attempt gets a fresh envelope scoped to its
	// own preparation and starts at input revision 1; the pair hash is unchanged.
	predecessor := in.Predecessor
	predecessorBody := predecessor.Preparation
	if predecessorBody.CurrentCalculation == nil || predecessorBody.CurrentValidation == nil ||
		len(predecessorBody.InputHash) != 32 || len(predecessorBody.SourceSnapshotHash) != 32 {
		return fail(nil)
	}
	calculation, err := handler.calculations.GetExact(ctx, in.OrganisationID, predecessorBody.CurrentCalculation)
	if err != nil || calculation == nil || calculation.Preparation == nil ||
		calculation.Preparation.Id != predecessorBody.PreparationId ||
		calculation.Preparation.Revision >= predecessorBody.Version ||
		!sameBASSourceRef(calculation.Family, predecessorBody.Family) ||
		!sameBASSourceRef(calculation.Profile, predecessorBody.Profile) ||
		!sameBASSourceRef(calculation.Period, predecessorBody.Period) ||
		calculation.InputRevision != predecessorBody.InputRevision ||
		!bytes.Equal(calculation.InputHash, predecessorBody.InputHash) ||
		!bytes.Equal(calculation.SourceSnapshotHash, predecessorBody.SourceSnapshotHash) ||
		calculation.PaygwInputs == nil || calculation.PaygiInputs == nil {
		return fail(errors.Join(ErrSnapshotCorrupt, err))
	}
	if predecessor.SavedPaygInputs == nil {
		return fail(ErrSnapshotCorrupt)
	}
	saved, err := handler.savedPayg.GetExact(ctx, in.OrganisationID, predecessor.SavedPaygInputs)
	if err != nil || saved == nil || saved.PreparationId != predecessorBody.PreparationId ||
		saved.InputRevision != predecessorBody.InputRevision || !bytes.Equal(saved.InputHash, predecessorBody.InputHash) ||
		!proto.Equal(saved.PaygwInputs, calculation.PaygwInputs) || !proto.Equal(saved.PaygiInputs, calculation.PaygiInputs) {
		return fail(errors.Join(ErrSnapshotCorrupt, err))
	}
	paygw := proto.Clone(calculation.PaygwInputs).(*tammyv1.BasPaygwInputs)
	paygi := proto.Clone(calculation.PaygiInputs).(*tammyv1.BasPaygiInputs)
	inputHash, err := hashPaygInputs(paygw, paygi)
	if err != nil || !bytes.Equal(inputHash, calculation.InputHash) || !bytes.Equal(inputHash, predecessorBody.InputHash) {
		return fail(errors.Join(ErrSnapshotCorrupt, err))
	}

	// Replacement/recovery must first retain an unlinked terminal predecessor;
	// that exact terminal version is then the immutable Predecessor link of the
	// new attempt. The final old-preparation version gets SupersededBy only after
	// the successor has been fully regenerated.
	var successorPredecessor *tammyv1.BasPreparation
	var successorPredecessorRef *tammyv1.SourceRef
	terminalState := tammyv1.BasPreparationState_BAS_PREPARATION_STATE_UNSPECIFIED
	if in.Kind == BASExternalAttemptReplacement || in.Kind == BASExternalAttemptRejectedRecovery {
		if in.Kind == BASExternalAttemptReplacement {
			terminalState = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
		} else {
			terminalState = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION
		}
		if predecessorBody.Version >= math.MaxInt64-2 || predecessorBody.FrozenBas == nil ||
			!sameBASSourceRef(predecessorBody.FrozenBas, in.PredecessorFrozen) {
			return fail(nil)
		}
		currentOld, err := handler.preparations.GetCurrent(ctx, in.OrganisationID, predecessorBody.PreparationId)
		if err != nil || currentOld == nil ||
			!sameBASSourceRef(preparationReference(currentOld.Preparation), in.PredecessorReference) ||
			!proto.Equal(currentOld.Preparation, predecessorBody) {
			return fail(errors.Join(ErrCommandConflict, err))
		}
		terminal := proto.Clone(predecessorBody).(*tammyv1.BasPreparation)
		terminal.Version++
		terminal.State = terminalState
		terminal.SupersededBy = nil
		terminal.ContentHash = nil
		terminal.UpdatedAt = proto.Clone(in.At).(*timestamppb.Timestamp)
		terminalRef, err := handler.preparations.Append(ctx, &PreparationRetainedSnapshot{
			Preparation: terminal, SavedPaygInputs: cloneBASRef(currentOld.SavedPaygInputs),
			BranchID: cloneBASString(currentOld.BranchID), AttemptID: currentOld.AttemptID,
		}, cloneBASRef(in.PredecessorReference))
		if err != nil {
			return fail(err)
		}
		terminal.ContentHash = bytes.Clone(terminalRef.ContentHash)
		successorPredecessor, successorPredecessorRef = terminal, terminalRef
	} else {
		successorPredecessor = proto.Clone(predecessorBody).(*tammyv1.BasPreparation)
		successorPredecessorRef = cloneBASRef(in.PredecessorReference)
	}

	preparationID, err := basRegenerationNewID(handler.newID)
	if err != nil {
		return fail(err)
	}
	attemptID, err := basRegenerationNewID(handler.newID)
	if err != nil {
		return fail(err)
	}
	inputSnapshotID, err := basRegenerationNewID(handler.newID)
	if err != nil {
		return fail(err)
	}
	periodRef, profileRef := confirmedPeriodReference(in.Period), profileReference(in.Profile)
	if periodRef == nil || profileRef == nil {
		return fail(nil)
	}
	initial := &tammyv1.BasPreparation{
		PreparationId: preparationID, OrganisationId: in.OrganisationID, Version: 1, CreationReason: in.CreationReason,
		Family: cloneBASRef(in.Family), Period: cloneBASRef(periodRef), Profile: cloneBASRef(profileRef),
		State:       tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		Predecessor: cloneBASRef(successorPredecessorRef), DisputeOverlay: familyCurrent.DisputeOverlay,
		SourceSnapshotHash: bytes.Clone(predecessorBody.SourceSnapshotHash),
		CreatedAt:          proto.Clone(in.At).(*timestamppb.Timestamp), UpdatedAt: proto.Clone(in.At).(*timestamppb.Timestamp),
		InputRevision: 1, InputHash: bytes.Clone(inputHash),
	}
	initialRetained := &PreparationRetainedSnapshot{Preparation: initial, BranchID: branchIDPtr, AttemptID: attemptID}
	initialRef, err := handler.preparations.Append(ctx, initialRetained, nil)
	if err != nil {
		return fail(err)
	}
	initial.ContentHash = bytes.Clone(initialRef.ContentHash)

	paygEnvelope := &tammyv1.BasSavedPaygInputs{
		InputSnapshotId: inputSnapshotID, OrganisationId: in.OrganisationID,
		PreparationId: preparationID, Preparation: cloneBASRef(initialRef), InputRevision: 1,
		InputHash: bytes.Clone(inputHash), PaygwInputs: paygw, PaygiInputs: paygi,
		ActorUserId: in.ActorUserID, RecordedAt: proto.Clone(in.At).(*timestamppb.Timestamp),
	}
	savedRef, err := handler.savedPayg.Append(ctx, paygEnvelope)
	if err != nil {
		return fail(err)
	}
	staged := proto.Clone(initial).(*tammyv1.BasPreparation)
	staged.Version = 2
	staged.ContentHash = nil
	staged.UpdatedAt = proto.Clone(in.At).(*timestamppb.Timestamp)
	stagedRetained := &PreparationRetainedSnapshot{Preparation: staged, SavedPaygInputs: cloneBASRef(savedRef),
		BranchID: cloneBASString(branchIDPtr), AttemptID: attemptID}
	stagedRef, err := handler.preparations.Append(ctx, stagedRetained, initialRef)
	if err != nil {
		return fail(err)
	}
	staged.ContentHash = bytes.Clone(stagedRef.ContentHash)

	// CAS the workflow to the staged collecting attempt before Regenerate. Its
	// own successful append advances the same branch/family pointers one more
	// version after it captures all bounded current sources and calculates.
	if branchBefore == nil {
		branch := &tammyv1.BasBranchState{
			BranchIdentity: cloneBASRef(branchRef), Version: 1,
			WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE,
			ActiveAttempt: expectedPreparation(staged), UpdatedAt: proto.Clone(in.At).(*timestamppb.Timestamp),
		}
		branchRefState, err := handler.branchStates.Append(ctx, in.OrganisationID, branch, nil)
		if err != nil {
			return fail(err)
		}
		branch.ContentHash = bytes.Clone(branchRefState.ContentHash)
		branchBefore = branch
		branchCAS = branchRefState
	} else {
		branch := proto.Clone(branchBefore).(*tammyv1.BasBranchState)
		branch.Version++
		branch.ContentHash = nil
		branch.ActiveAttempt = expectedPreparation(staged)
		branch.BarrierPreparation, branch.FrozenBarrier = nil, nil
		branch.UpdatedAt = proto.Clone(in.At).(*timestamppb.Timestamp)
		branchRefState, err := handler.branchStates.Append(ctx, in.OrganisationID, branch, branchCAS)
		if err != nil {
			return fail(err)
		}
		branch.ContentHash = bytes.Clone(branchRefState.ContentHash)
		branchBefore = branch
		branchCAS = branchRefState
	}
	familyNext := proto.Clone(familyCurrent).(*tammyv1.BasFamilyState)
	familyNext.Version++
	familyNext.ContentHash = nil
	familyNext.ActiveBranch = basExpectedBranchIdentity(branchRef, branchBefore.Version)
	familyNext.UpdatedAt = proto.Clone(in.At).(*timestamppb.Timestamp)
	familyRefNext, err := handler.familyStates.Append(ctx, in.OrganisationID, familyNext, familyCAS)
	if err != nil {
		return fail(err)
	}
	familyNext.ContentHash = bytes.Clone(familyRefNext.ContentHash)

	regenerated, err := handler.Regenerate(ctx, &tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{IdempotencyKey: in.OperationKey,
			Authentication: proto.Clone(in.Authentication).(*tammyv1.AuthenticationContext)},
		OrganisationId: in.OrganisationID, ExpectedVersion: staged.Version,
		FamilyExpectation: basExpectedBranchIdentity(in.Family, in.Family.Revision),
		Period:            cloneBASRef(periodRef), Profile: cloneBASRef(profileRef),
		PreparationExpectation: expectedPreparation(staged),
	}, in.ActorUserID, proto.Clone(in.At).(*timestamppb.Timestamp))
	if err != nil || regenerated == nil || regenerated.Response == nil || regenerated.Response.Preparation == nil {
		return fail(errors.Join(ErrCommandConflict, err))
	}
	final := regenerated.Response.Preparation
	finalRef := preparationReference(final)
	if !validBASSourceRef(finalRef, "bas_preparation", preparationID) || final.Version <= staged.Version ||
		!sameBASSourceRef(final.Predecessor, successorPredecessorRef) ||
		!sameBASSourceRef(final.Period, periodRef) || !sameBASSourceRef(final.Profile, profileRef) ||
		!sameBASSourceRef(final.Family, in.Family) || final.CurrentCalculation == nil ||
		!sameBASSourceRef(final.CurrentCalculation, calculationReference(regenerated.Response.Calculation)) {
		return fail(nil)
	}

	var linkedPredecessor *tammyv1.BasPreparation
	if terminalState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_UNSPECIFIED {
		linked := proto.Clone(successorPredecessor).(*tammyv1.BasPreparation)
		linked.Version++
		linked.ContentHash = nil
		linked.SupersededBy = cloneBASRef(finalRef)
		linked.UpdatedAt = proto.Clone(in.At).(*timestamppb.Timestamp)
		linkedRef, err := handler.preparations.Append(ctx, &PreparationRetainedSnapshot{
			Preparation: linked, SavedPaygInputs: cloneBASRef(predecessor.SavedPaygInputs),
			BranchID: cloneBASString(predecessor.BranchID), AttemptID: predecessor.AttemptID,
		}, successorPredecessorRef)
		if err != nil {
			return fail(err)
		}
		linked.ContentHash = bytes.Clone(linkedRef.ContentHash)
		linkedRetained, err := handler.preparations.GetExact(ctx, in.OrganisationID, linkedRef)
		if err != nil || linkedRetained == nil || !proto.Equal(linkedRetained.Preparation, linked) {
			return fail(errors.Join(ErrSnapshotCorrupt, err))
		}
		linkedPredecessor = linkedRetained.Preparation
	}
	return BASExternalAttemptBuildResult{
		BranchReference: cloneBASRef(branchRef), SuccessorPredecessor: successorPredecessor,
		Predecessor: linkedPredecessor, Preparation: final, Calculation: regenerated.Response.Calculation,
	}, nil
}

// Create this request only from the exact immutable retained scope. The helper
// indirection keeps the build contract explicit when the revision period/profile
// differ from the predecessor's pinned versions.
func (in BASExternalAttemptBuild) PeriodRef() *tammyv1.SourceRef {
	if in.Period == nil {
		return nil
	}
	return confirmedPeriodReference(in.Period)
}
func (in BASExternalAttemptBuild) ProfileRef() *tammyv1.SourceRef {
	if in.Profile == nil {
		return nil
	}
	return profileReference(in.Profile)
}
