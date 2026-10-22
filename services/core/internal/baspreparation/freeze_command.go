package baspreparation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"hash"
	"math"
	"sort"
	"strings"
)

const basFreezeManualHandoffPurpose = "bas_freeze_manual_handoff"

// FreezeChainReader builds the small exact chain needed by PlanFreeze. It does
// not load external-observation history; that is not part of the freeze policy.
// The current mutable attempt is kept separate from immutable root/head refs.
type FreezeChainReader struct {
	familyIdentities *FamilyIdentityRepository
	familyStates     *FamilyStateRepository
	branchIdentities *BranchIdentityRepository
	branchStates     *BranchStateRepository
	preparations     *PreparationRepository
}

func NewFreezeChainReader(families *FamilyIdentityRepository, familyStates *FamilyStateRepository,
	branchIdentities *BranchIdentityRepository, branchStates *BranchStateRepository,
	preparations *PreparationRepository) (*FreezeChainReader, error) {
	if families == nil || familyStates == nil || branchIdentities == nil || branchStates == nil || preparations == nil {
		return nil, ErrCommandUnavailable
	}
	return &FreezeChainReader{familyIdentities: families, familyStates: familyStates,
		branchIdentities: branchIdentities, branchStates: branchStates, preparations: preparations}, nil
}

func (reader *FreezeChainReader) LoadForPreparation(ctx context.Context,
	current *PreparationRetainedSnapshot) (ChainSnapshot, error) {
	fail := func(err error) (ChainSnapshot, error) { return ChainSnapshot{}, errors.Join(ErrChainSnapshot, err) }
	if reader == nil || ctx == nil || ctx.Err() != nil || current == nil || current.Preparation == nil {
		return fail(nil)
	}
	prep := current.Preparation
	familyRef := prep.Family
	identity, err := reader.familyIdentities.GetExact(ctx, prep.OrganisationId, familyRef)
	if err != nil || identity == nil || identity.OrganisationId != prep.OrganisationId ||
		!sameBASSourceRef(basFamilyIdentityReference(identity), familyRef) {
		return fail(err)
	}
	familyState, err := reader.familyStates.GetCurrent(ctx, prep.OrganisationId, familyRef)
	if err != nil || familyState == nil || !sameBASSourceRef(familyState.Family, familyRef) {
		return fail(err)
	}

	loadPreparation := func(ref *tammyv1.SourceRef) (*tammyv1.BasPreparation, error) {
		if ref == nil {
			return nil, nil
		}
		if !validBASSourceRef(ref, "bas_preparation", "") {
			return nil, ErrChainSnapshot
		}
		retained, err := reader.preparations.GetExact(ctx, prep.OrganisationId, ref)
		if err != nil || retained == nil || retained.Preparation == nil ||
			!sameBASSourceRef(preparationReference(retained.Preparation), ref) {
			return nil, errors.Join(ErrChainSnapshot, err)
		}
		return retained.Preparation, nil
	}
	root, err := loadPreparation(familyState.RootPreparation)
	if err != nil || root == nil {
		return fail(err)
	}
	latest, err := loadPreparation(familyState.LatestHead.GetRetained())
	if err != nil {
		return fail(err)
	}
	effectiveOriginal, err := loadPreparation(familyState.EffectiveOriginal)
	if err != nil {
		return fail(err)
	}

	scope := FamilyScope{OrganisationID: identity.OrganisationId,
		Start: cloneCalendarDate(identity.PeriodScope.GetStartDate()),
		End:   cloneCalendarDate(identity.PeriodScope.GetEndDate()), Window: identity.Window}
	chain := ChainSnapshot{
		Family:          &Family{Reference: cloneBASRef(familyRef), Version: familyState.Version, Scope: scope},
		RootPreparation: root, EffectiveOriginal: effectiveOriginal, LatestHead: latest,
		DisputeOverlay: familyState.DisputeOverlay, PeriodReference: cloneBASRef(prep.Period),
	}

	branchRef := familyState.ActiveBranch.GetRetained()
	if branchRef == nil {
		if current.BranchID != nil {
			return fail(nil)
		}
	} else {
		if !validBASSourceRef(branchRef, "bas_branch", "") || branchRef.Revision != 1 ||
			familyState.ActiveBranch.ExpectedVersion == nil || familyState.ActiveBranch.GetExpectedVersion() == 0 ||
			current.BranchID == nil || *current.BranchID != branchRef.Id {
			return fail(nil)
		}
		branchIdentity, err := reader.branchIdentities.GetExact(ctx, prep.OrganisationId, branchRef)
		if err != nil || branchIdentity == nil || !sameBASSourceRef(branchIdentity.Family, familyRef) {
			return fail(err)
		}
		branchState, err := reader.branchStates.GetCurrent(ctx, prep.OrganisationId, branchRef)
		if err != nil || branchState == nil || branchState.Version != familyState.ActiveBranch.GetExpectedVersion() ||
			!sameBASSourceRef(branchState.BranchIdentity, branchRef) {
			return fail(err)
		}
		branchRootHead, err := loadPreparation(familyState.LatestHead.GetRetained())
		if err != nil {
			return fail(err)
		}
		activeAttempt, err := loadPreparation(branchState.ActiveAttempt.GetRetained())
		if err != nil {
			return fail(err)
		}
		chain.Branch = &Branch{Reference: cloneBASRef(branchRef), Version: branchState.Version,
			RootHead: branchRootHead, ActiveAttempt: activeAttempt,
			Resolution: cloneBASRef(branchState.Resolution), FrozenBarrier: cloneBASRef(branchState.FrozenBarrier)}
	}
	if err := validateChainSnapshot(chain); err != nil {
		return fail(err)
	}
	return chain, nil
}

func writeApprovedHashField(h hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write(value)
}

func writeApprovedHashRef(h hash.Hash, ref *tammyv1.SourceRef) error {
	if ref == nil {
		writeApprovedHashField(h, nil)
		return nil
	}
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(ref)
	if err != nil {
		return err
	}
	writeApprovedHashField(h, wire)
	return nil
}

// approvedBASSnapshotHash identifies the exact material reviewed and approved;
// it is independent of the frozen body's own content hash. Acks are sorted by
// identity so their caller/repository iteration order cannot change the digest.
func approvedBASSnapshotHash(preparation, profile, period, calculation, validation *tammyv1.SourceRef,
	snapshot *tammyv1.BasCalculation, acknowledgements []*tammyv1.BasWarningAcknowledgement) ([]byte, error) {
	if snapshot == nil || len(snapshot.SourceSnapshotHash) != sha256.Size {
		return nil, ErrReviewInput
	}
	h := sha256.New()
	writeApprovedHashField(h, []byte("tammy.bas.approved-snapshot.v1"))
	for _, ref := range []*tammyv1.SourceRef{preparation, profile, period, calculation, validation} {
		if err := writeApprovedHashRef(h, ref); err != nil {
			return nil, errors.Join(ErrInvalidCommand, err)
		}
	}
	writeApprovedHashField(h, snapshot.SourceSnapshotHash)
	for _, fingerprint := range []struct {
		version string
		hash    []byte
	}{
		{snapshot.RuleVersion, snapshot.RuleHash}, {snapshot.CalculatorVersion, snapshot.CalculatorHash},
		{snapshot.FormVersion, snapshot.FormHash}, {snapshot.SchemaVersion, snapshot.SchemaHash},
		{snapshot.DescriptorVersion, snapshot.DescriptorHash},
	} {
		writeApprovedHashField(h, []byte(fingerprint.version))
		writeApprovedHashField(h, fingerprint.hash)
	}
	ordered := append([]*tammyv1.BasWarningAcknowledgement(nil), acknowledgements...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i] == nil {
			return false
		}
		if ordered[j] == nil {
			return true
		}
		return ordered[i].AcknowledgementId < ordered[j].AcknowledgementId
	})
	for _, acknowledgement := range ordered {
		if acknowledgement == nil {
			return nil, ErrReviewInput
		}
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(acknowledgement)
		if err != nil {
			return nil, errors.Join(ErrInvalidCommand, err)
		}
		writeApprovedHashField(h, wire)
	}
	return h.Sum(nil), nil
}

func (service *Service) FreezeBasForManualHandoff(ctx context.Context,
	request *tammyv1.FreezeBasForManualHandoffRequest) (*tammyv1.FreezeBasForManualHandoffResponse, error) {
	if service == nil || request == nil || !validBASCommandContext(request.CommandContext) ||
		!ids.IsCanonicalV7(request.OrganisationId) || !ids.IsCanonicalV7(request.PreparationId) ||
		request.ExpectedVersion == 0 || request.ExpectedVersion >= math.MaxInt64 || len(request.PreparationHash) != sha256.Size ||
		!ids.IsCanonicalV7(request.CalculationId) || len(request.CalculationHash) != sha256.Size ||
		!ids.IsCanonicalV7(request.ValidationId) || len(request.ValidationHash) != sha256.Size || request.Chain == nil ||
		strings.TrimSpace(request.ApprovalReason) == "" || len(request.ApprovalReason) > 512 {
		return nil, ErrInvalidCommand
	}
	factor := request.CommandContext.FreshFactor
	if factor == nil || factor.Purpose != basFreezeManualHandoffPurpose ||
		!ids.IsCanonicalV7(factor.AssertionId) || factor.AssertedAt == nil || !factor.AssertedAt.IsValid() {
		return nil, ErrInvalidCommand
	}

	var response *tammyv1.FreezeBasForManualHandoffResponse
	var auditPayload *tammyv1.BasPreparationChangedEvent
	completed, err := service.commands.Execute(ctx, app.OrdinaryCommand[CommandRepositories]{
		Operation:      app.OrdinaryOperationFreezeBasForManualHandoff,
		OperationKey:   request.CommandContext.IdempotencyKey,
		Authentication: request.CommandContext.Authentication,
		Request:        request,
		NewResult:      func() proto.Message { return &tammyv1.FreezeBasForManualHandoffResponse{} },
		SaveSource: func(ctx context.Context, repos CommandRepositories, owned proto.Message) error {
			r, ok := owned.(*tammyv1.FreezeBasForManualHandoffRequest)
			if !ok || repos.Preparations == nil || repos.Profiles == nil || repos.Periods == nil ||
				repos.Calculations == nil || repos.Validations == nil || repos.Acknowledgements == nil ||
				repos.Frozen == nil || repos.FreezeChains == nil || repos.FamilyStates == nil || repos.BranchStates == nil ||
				repos.ConsumeFreshFactor == nil {
				return ErrCommandUnavailable
			}
			factor := r.CommandContext.FreshFactor
			if factor == nil || factor.Purpose != basFreezeManualHandoffPurpose || !ids.IsCanonicalV7(factor.AssertionId) {
				return ErrInvalidCommand
			}
			current, err := repos.Preparations.GetCurrent(ctx, r.OrganisationId, r.PreparationId)
			if err != nil || current == nil || current.Preparation == nil {
				return errors.Join(ErrCommandConflict, err)
			}
			prep := current.Preparation
			prepRef := preparationReference(prep)
			if prep.Version != r.ExpectedVersion || !bytes.Equal(prep.ContentHash, r.PreparationHash) ||
				prep.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || prep.FrozenBas != nil ||
				prep.CurrentCalculation == nil || prep.CurrentValidation == nil || current.SavedPaygInputs == nil ||
				prep.CurrentCalculation.Id != r.CalculationId || !bytes.Equal(prep.CurrentCalculation.ContentHash, r.CalculationHash) ||
				prep.CurrentValidation.Id != r.ValidationId || !bytes.Equal(prep.CurrentValidation.ContentHash, r.ValidationHash) {
				return ErrCommandConflict
			}

			calculation, err := repos.Calculations.GetExact(ctx, r.OrganisationId, prep.CurrentCalculation)
			if err != nil || calculation == nil || !sameBASSourceRef(calculationReference(calculation), prep.CurrentCalculation) {
				return errors.Join(ErrCommandConflict, err)
			}
			profile, err := repos.Profiles.GetExact(ctx, r.OrganisationId, prep.Profile)
			if err != nil || profile == nil || !proto.Equal(profile, calculation.ProfileSnapshot) {
				return errors.Join(ErrCommandConflict, err)
			}
			period, err := repos.Periods.GetExact(ctx, r.OrganisationId, prep.Period)
			if err != nil || period == nil || !proto.Equal(period, calculation.PeriodSnapshot) {
				return errors.Join(ErrCommandConflict, err)
			}
			retainedValidation, err := repos.Validations.GetRetained(ctx, r.OrganisationId, prep.CurrentValidation)
			if err != nil || retainedValidation == nil || retainedValidation.Validation == nil ||
				!warningAcknowledgementCurrentCaptureMatches(current, retainedValidation) ||
				!sameBASSourceRef(retainedValidation.Calculation, prep.CurrentCalculation) ||
				!sameBASSourceRef(validationReference(retainedValidation.Validation), prep.CurrentValidation) {
				return errors.Join(ErrCommandConflict, err)
			}

			currentCapture, err := captureBASReviewCurrentness(ctx, repos, current, calculation)
			if err != nil {
				return err
			}
			acknowledgements, err := repos.Acknowledgements.ListForValidation(ctx, r.OrganisationId, prep.CurrentValidation)
			if err != nil {
				return err
			}
			review := ReviewInput{Preparation: prep, Calculation: calculation,
				Validation: retainedValidation.Validation, Acknowledgements: acknowledgements, CurrentCapture: currentCapture}
			assessment, err := AssessReview(review)
			if err != nil || assessment.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
				!assessment.CanFreeze || assessment.SourceChanged {
				return errors.Join(ErrFreezeNotReady, err)
			}
			chain, err := repos.FreezeChains.LoadForPreparation(ctx, current)
			if err != nil {
				return err
			}
			plan, err := PlanFreeze(FreezeInput{Review: review, Chain: chain})
			if err != nil {
				return errors.Join(ErrFreezeNotReady, err)
			}
			if !proto.Equal(r.Chain, plan.ExpectedChain) || plan.ExpectedPreparationVersion != r.ExpectedVersion ||
				!bytes.Equal(plan.ExpectedPreparationHash, r.PreparationHash) {
				return ErrCommandConflict
			}
			if prep.Version >= math.MaxInt64 {
				return ErrCommandConflict
			}

			frozenID, err := service.newID()
			if err != nil || !ids.IsCanonicalV7(frozenID) {
				return ErrInvalidCommand
			}
			approvedHash, err := approvedBASSnapshotHash(prepRef, prep.Profile, prep.Period,
				prep.CurrentCalculation, prep.CurrentValidation, plan.Calculation, plan.Acknowledgements)
			if err != nil || len(approvedHash) != sha256.Size {
				return errors.Join(ErrInvalidCommand, err)
			}
			approvedAt := timestamppb.New(service.now().UTC())
			frozen := &tammyv1.BasFrozenBas{
				FrozenId: frozenID, OrganisationId: r.OrganisationId,
				Preparation: cloneBASRef(prepRef), Family: cloneBASRef(prep.Family),
				Period: cloneBASRef(prep.Period), Profile: cloneBASRef(prep.Profile),
				Calculation: cloneBASRef(prep.CurrentCalculation), Validation: cloneBASRef(prep.CurrentValidation),
				CalculationSnapshot:    proto.Clone(plan.Calculation).(*tammyv1.BasCalculation),
				Acknowledgements:       cloneAcknowledgements(plan.Acknowledgements),
				ApprovedSnapshotHash:   approvedHash,
				ApprovalActorUserId:    r.CommandContext.Authentication.ActorUserId,
				FreshFactorAssertionId: factor.AssertionId, ApprovedAt: approvedAt,
				ApprovalReason: r.ApprovalReason,
			}
			frozenRef, err := repos.Frozen.Append(ctx, r.OrganisationId, frozen, prepRef)
			if err != nil {
				return err
			}
			frozen.ContentHash = bytes.Clone(frozenRef.ContentHash)

			next := proto.Clone(prep).(*tammyv1.BasPreparation)
			next.Version++
			next.ContentHash = nil
			next.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF
			next.FrozenBas = cloneBASRef(frozenRef)
			next.UpdatedAt = proto.Clone(approvedAt).(*timestamppb.Timestamp)
			retained := &PreparationRetainedSnapshot{Preparation: next,
				SavedPaygInputs: cloneBASRef(current.SavedPaygInputs),
				BranchID:        cloneBASString(current.BranchID), AttemptID: current.AttemptID}
			nextRef, err := repos.Preparations.Append(ctx, retained, prepRef)
			if err != nil {
				return err
			}
			next.ContentHash = bytes.Clone(nextRef.ContentHash)

			// A branch's current attempt is a separate workflow CAS. Updating it and
			// the family active-branch pointer leaves the immutable root and latest
			// confirmed head untouched. Root attempts have no branch CAS to advance.
			if current.BranchID != nil {
				if err := advanceBASActiveAttempt(ctx, r.OrganisationId, prep.Family, prepRef, nextRef,
					approvedAt, repos.Preparations, repos.FamilyStates, repos.BranchStates); err != nil {
					return err
				}
			}

			response = &tammyv1.FreezeBasForManualHandoffResponse{Preparation: next, FrozenBas: frozen}
			auditPayload = &tammyv1.BasPreparationChangedEvent{
				Action: tammyv1.BasPreparationAuditAction_BAS_PREPARATION_AUDIT_ACTION_FROZEN_FOR_MANUAL_HANDOFF,
				Source: cloneBASRef(nextRef), Previous: cloneBASRef(prepRef),
				AffectedResources: []*tammyv1.SourceRef{
					cloneBASRef(frozenRef),
					cloneBASRef(prep.Profile), cloneBASRef(prep.Period),
					cloneBASRef(prep.CurrentCalculation), cloneBASRef(prep.CurrentValidation),
				},
			}
			// Must remain the last SaveSource effect. If it rejects this assertion,
			// the coordinator rolls back frozen row, prep CAS, and branch/family CAS.
			return repos.ConsumeFreshFactor(ctx, r.CommandContext.Authentication, factor, basFreezeManualHandoffPurpose)
		},
		BuildResult: func(ctx context.Context, _ CommandRepositories, _ proto.Message) (app.CommandResult, error) {
			if response == nil || response.Preparation == nil || response.FrozenBas == nil || auditPayload == nil {
				return app.CommandResult{}, ErrInvalidCommand
			}
			return service.audit.Build(ctx, app.OrdinaryOperationFreezeBasForManualHandoff,
				request.CommandContext.Authentication, request.CommandContext.IdempotencyKey,
				response.Preparation.PreparationId, response, auditPayload)
		},
	})
	if err != nil {
		return nil, err
	}
	value, ok := completed.(*tammyv1.FreezeBasForManualHandoffResponse)
	if !ok || value.Preparation == nil || value.FrozenBas == nil {
		return nil, ErrInvalidCommand
	}
	return value, nil
}
