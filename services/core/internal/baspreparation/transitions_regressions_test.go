package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestUpholdDisputePreservesExistingBranchAndReview(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF,
	} {
		t.Run(state.String(), func(t *testing.T) {
			input := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN)
			active := attemptVariant(input.History.Attempts[0], 5201, state)
			active.Preparation.Predecessor = preparationReference(input.Chain.LatestHead)
			input.Chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(5202), 1, 52), Version: 3,
				RootHead: clonePreparation(input.Chain.LatestHead), ActiveAttempt: clonePreparation(active.Preparation)}
			input.History.Chain = cloneChainSnapshot(input.Chain)
			if state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF {
				input.History.Attempts = append(input.History.Attempts, active)
			}
			input.ExpectedChain = mustChainExpectation(t, input.Chain)
			before := cloneBranch(input.Chain.Branch)
			plan, err := PlanResolveDispute(input)
			if err != nil {
				t.Fatalf("upholding the chain must preserve a compatible existing branch: %v", err)
			}
			if plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD ||
				plan.ReserveCorrectiveBranch || plan.AdoptedExistingBranch || plan.ResetMutableAttemptToCollecting ||
				plan.ClearCurrentReviewPointers || plan.FrozenBarrier != nil || plan.PreserveFrozenAttempt {
				t.Fatalf("UPHOLD clears the dispute overlay without changing branch workflow or review: %+v", plan)
			}
			if !proto.Equal(before.ActiveAttempt, input.Chain.Branch.ActiveAttempt) || before.Version != input.Chain.Branch.Version {
				t.Fatal("upholding mutated the loaded branch")
			}
		})
	}
}

func TestUpholdLateUnusedDisputeBeforeFirstHeadPreservesReplacement(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
	} {
		t.Run(state.String(), func(t *testing.T) {
			base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
			unused := attemptVariant(base.Attempt, 5211, state)
			active := attemptVariant(base.Attempt, 5212, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
			active.Preparation.Predecessor = preparationReference(unused.Preparation)
			chain := initialChain(unused.Preparation)
			chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(5213), 1, 53), Version: 2,
				ActiveAttempt: clonePreparation(active.Preparation)}
			late := retainedObservation(unused, 5214, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
			var observations []*tammyv1.BasExternalObservation
			if state == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION {
				observations = append(observations, retainedObservation(unused, 5215, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION))
			}
			observations = append(observations, late)
			chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
			history := historyWith(chain, []AttemptSnapshot{unused, active}, observations...)
			var subjects []*tammyv1.SourceRef
			for _, observation := range observations {
				subjects = append(subjects, observationReference(observation))
			}
			resolution := &tammyv1.BasExternalDisputeInput{
				Outcome:              tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
				DisputedObservations: subjects, DependentChainManifestHash: dependencyManifestHash(nil),
				Reason: "Uphold the retained unused/rejected predecessor and active replacement before any confirmation"}
			input := DisputeResolutionInput{Chain: chain, History: history, ExpectedChain: mustChainExpectation(t, chain), Resolution: resolution}
			plan, err := PlanResolveDispute(input)
			if err != nil {
				t.Fatalf("a pre-head dispute must have an explicit uphold path: %v", err)
			}
			if !plan.ExpectedChain.LatestHead.GetAbsent() || len(plan.AffectedConfirmedChain) != 0 ||
				plan.AdoptedExistingBranch || plan.ResetMutableAttemptToCollecting || plan.FrozenBarrier != nil {
				t.Fatalf("pre-head uphold cannot invent a confirmed ancestor or change the replacement: %+v", plan)
			}
			corrective := proto.Clone(resolution).(*tammyv1.BasExternalDisputeInput)
			corrective.Outcome = tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION
			input.Resolution = corrective
			if _, err := PlanResolveDispute(input); err == nil {
				t.Fatal("a corrective revision needs a retained confirmed head")
			}
			retained := &tammyv1.BasExternalDisputeResolution{ResolutionId: testUUID(5216), Family: cloneRef(chain.Family.Reference),
				Resolution: resolution, ActorUserId: late.ActorUserId, ResolvedAt: late.RecordedAt, ContentHash: testHash(56)}
			history.LastOrdinal++
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: history.LastOrdinal, Resolution: retained})
			chain.DisputeOverlay = plan.NextOverlay
			history.Chain = cloneChainSnapshot(chain)
			confirmation, err := PlanObservation(ObservationInput{Chain: chain, History: history, Attempt: active, Observation: base.Observation})
			if err != nil || !confirmation.AdvancesHead || confirmation.OpensDispute {
				t.Fatalf("the preserved active replacement may receive its own first confirmation after uphold: plan=%+v err=%v", confirmation, err)
			}
			// Reading the old resolution after a later confirmation must still validate
			// its empty historical dependency manifest at its own append ordinal.
			active.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
			chain.EffectiveOriginal = clonePreparation(active.Preparation)
			chain.LatestHead = clonePreparation(active.Preparation)
			chain.Branch = nil
			history.Chain = cloneChainSnapshot(chain)
			history.Attempts[1] = cloneAttemptSnapshot(active)
			history.ConfirmedDependencies = []AttemptSnapshot{cloneAttemptSnapshot(active)}
			history.LastOrdinal++
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: history.LastOrdinal,
				Observation: retainedObservation(active, 5217, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)})
			if err := validateExternalHistory(history); err != nil {
				t.Fatalf("later first confirmation rewrote a pre-head resolution: %v", err)
			}
		})
	}
}

func TestUpholdConflictingFirstConfirmationRequiresFreshActiveConfirmation(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	chain := base.Chain
	rejection := retainedObservation(base.Attempt, 5221, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := historyWith(chain, []AttemptSnapshot{base.Attempt}, rejection)
	observation, err := PlanObservation(ObservationInput{Chain: chain, History: history, Attempt: base.Attempt, Observation: base.Observation})
	if err != nil || !observation.OpensDispute || observation.AdvancesHead {
		t.Fatalf("contradictory first use must preserve an absent head and open a dispute: plan=%+v err=%v", observation, err)
	}
	qualification := retainedObservation(base.Attempt, 5222, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	chain.DisputeOverlay = observation.NextOverlay
	history = historyWith(chain, []AttemptSnapshot{base.Attempt}, rejection, qualification)
	resolution := &tammyv1.BasExternalDisputeInput{
		Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
		DisputedObservations:       []*tammyv1.SourceRef{observationReference(rejection), observationReference(qualification)},
		DependentChainManifestHash: dependencyManifestHash(nil), Reason: "Resolve competing external assertions before any effective confirmation"}
	plan, err := PlanResolveDispute(DisputeResolutionInput{Chain: chain, History: history,
		ExpectedChain: mustChainExpectation(t, chain), Resolution: resolution})
	if err != nil {
		t.Fatalf("competing first-use assertions need an explicit pre-head uphold path: %v", err)
	}
	if !plan.ExpectedChain.LatestHead.GetAbsent() || plan.ReserveCorrectiveBranch {
		t.Fatalf("upholding observations cannot turn either assertion into effective confirmation: %+v", plan)
	}
	retained := &tammyv1.BasExternalDisputeResolution{ResolutionId: testUUID(5223), Family: cloneRef(chain.Family.Reference),
		Resolution: resolution, ActorUserId: qualification.ActorUserId, ResolvedAt: qualification.RecordedAt, ContentHash: testHash(63)}
	history.LastOrdinal++
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: history.LastOrdinal, Resolution: retained})
	chain.DisputeOverlay = plan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	fresh, err := PlanObservation(ObservationInput{Chain: chain, History: history, Attempt: base.Attempt, Observation: base.Observation})
	if err != nil || !fresh.AdvancesHead || fresh.OpensDispute {
		t.Fatalf("only a fresh active-attempt confirmation after uphold may create the first head: plan=%+v err=%v", fresh, err)
	}
}

func TestLateConfirmationOfUnusedPredecessorDisputesReplacementAndCanResolve(t *testing.T) {
	for _, predecessorState := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
	} {
		for _, outcome := range []tammyv1.BasDisputeResolutionOutcome{
			tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
		} {
			t.Run(predecessorState.String()+"/"+outcome.String(), func(t *testing.T) {
				base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
				unused := attemptVariant(base.Attempt, 5101, predecessorState)
				head := attemptVariant(base.Attempt, 5102, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
				head.Preparation.Predecessor = preparationReference(unused.Preparation)
				chain := initialChain(unused.Preparation)
				chain.EffectiveOriginal = clonePreparation(head.Preparation)
				chain.LatestHead = clonePreparation(head.Preparation)
				headConfirmation := retainedObservation(head, 5103, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
				var observations []*tammyv1.BasExternalObservation
				if predecessorState == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION {
					observations = append(observations, retainedObservation(unused, 5105, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION))
				}
				observations = append(observations, headConfirmation)
				history := historyWith(chain, []AttemptSnapshot{unused, head}, observations...)
				affected := []*tammyv1.SourceRef{preparationReference(head.Preparation)}
				plan, err := PlanObservation(ObservationInput{Chain: chain, History: history, Attempt: unused, Observation: base.Observation})
				if err != nil || !plan.OpensDispute || plan.AdvancesHead || !sameRefSlices(plan.AffectedConfirmedChain, affected) {
					t.Fatalf("late use of an unused predecessor must pin its confirmed replacement: plan=%+v err=%v", plan, err)
				}
				lateConfirmation := retainedObservation(unused, 5104, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
				chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
				observations = append(observations, lateConfirmation)
				history = historyWith(chain, []AttemptSnapshot{unused, head}, observations...)
				var subjects []*tammyv1.SourceRef
				for _, observation := range observations {
					subjects = append(subjects, observationReference(observation))
				}
				resolution := &tammyv1.BasExternalDisputeInput{Outcome: outcome,
					DisputedObservations:   subjects,
					AffectedConfirmedChain: affected, DependentChainManifestHash: dependencyManifestHash(affected), Reason: "Review late conflicting external use of the replaced unused attempt"}
				if _, err := PlanResolveDispute(DisputeResolutionInput{Chain: chain, History: history, ExpectedChain: mustChainExpectation(t, chain),
					ConfirmedDependencies: history.ConfirmedDependencies, Resolution: resolution}); err != nil {
					t.Fatalf("both resolution outcomes must retain and resolve the exact competing use: %v", err)
				}
			})
		}
	}
}

func TestPlanObservationRetainsConfirmedRevisionAfterUnusedRetry(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	ancestor := attemptVariant(base.Attempt, 3801, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	unused := attemptVariant(base.Attempt, 3802, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION)
	unused.Preparation.Predecessor = preparationReference(ancestor.Preparation)
	head := attemptVariant(base.Attempt, 3803, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	head.Preparation.Predecessor = preparationReference(unused.Preparation)
	chain := initialChain(ancestor.Preparation)
	chain.LatestHead = clonePreparation(head.Preparation)
	history := historyWith(chain, []AttemptSnapshot{ancestor, unused, head},
		retainedObservation(ancestor, 3804, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE),
		retainedObservation(head, 3805, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE))
	input := ObservationInput{Chain: chain, History: history, Attempt: head, Observation: base.Observation}
	plan, err := PlanObservation(input)
	if err != nil || plan.OpensDispute || plan.AdvancesHead {
		t.Fatalf("confirmed history must traverse the retained unused attempt: plan=%+v err=%v", plan, err)
	}
	input.History.Attempts = []AttemptSnapshot{ancestor, head}
	if _, err := PlanObservation(input); err == nil {
		t.Fatal("the exact intermediate predecessor cannot be omitted from retained history")
	}
}

func TestPlanObservationRejectsEvidenceOverflowWithoutRejectingExactBound(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	for i := 0; i < 20; i++ {
		input.Observation.Evidence = append(input.Observation.Evidence, sourceRef("document", testUUID(uint64(3900+i)), 1, byte(i)))
	}
	if _, err := PlanObservation(input); err != nil {
		t.Fatalf("twenty valid evidence references must remain supported: %v", err)
	}
	input.Observation.Evidence = append(input.Observation.Evidence, sourceRef("document", testUUID(3921), 1, 21))
	if _, err := PlanObservation(input); err == nil {
		t.Fatal("the pure command policy must enforce the generated twenty-evidence bound")
	}
}

func TestPlanObservationRejectsCompleteHistoryOverflow(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	input.History.Records = nil
	for i := 0; i < 1000; i++ {
		input.History.Records = append(input.History.Records, ExternalHistoryRecord{Ordinal: uint64(i + 1),
			Observation: retainedObservation(input.Attempt, uint64(4000+i), tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT)})
	}
	input.History.LastOrdinal = 1000
	if err := validateExternalHistory(input.History); err != nil {
		t.Fatalf("a complete retained history at its bound must remain reducible: %v", err)
	}
	if _, err := PlanObservation(input); err == nil {
		t.Fatal("an observation that would append record 1001 must be rejected")
	}
	input.History.Records = append(input.History.Records, ExternalHistoryRecord{Ordinal: 1001,
		Observation: retainedObservation(input.Attempt, 5001, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT)})
	input.History.LastOrdinal = 1001
	if _, err := PlanObservation(input); err == nil {
		t.Fatal("overflow must fail instead of reducing an unbounded or truncated history")
	}
}

func TestPlanResolveDisputeIncludesQualifyingRetractionAndEveryAffectedDescendant(t *testing.T) {
	for _, outcome := range []tammyv1.BasDisputeResolutionOutcome{
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
	} {
		t.Run(outcome.String(), func(t *testing.T) {
			base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE).Attempt
			ancestor := attemptVariant(base, 3701, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
			head := attemptVariant(base, 3702, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
			head.Preparation.Predecessor = preparationReference(ancestor.Preparation)
			chain := initialChain(ancestor.Preparation)
			chain.LatestHead = clonePreparation(head.Preparation)
			chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
			confirmedAncestor := retainedObservation(ancestor, 3703, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
			confirmedHead := retainedObservation(head, 3704, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
			retraction := retainedCorrection(ancestor, 3705, observationReference(confirmedAncestor), tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
			history := historyWith(chain, []AttemptSnapshot{ancestor, head}, confirmedAncestor, confirmedHead, retraction)
			affected := []*tammyv1.SourceRef{preparationReference(ancestor.Preparation), preparationReference(head.Preparation)}
			resolution := &tammyv1.BasExternalDisputeInput{Outcome: outcome,
				DisputedObservations:   []*tammyv1.SourceRef{observationReference(confirmedAncestor), observationReference(retraction)},
				AffectedConfirmedChain: affected, DependentChainManifestHash: dependencyManifestHash(affected), Reason: "Review the retraction and its dependent confirmed revision"}
			plan, err := PlanResolveDispute(DisputeResolutionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
				ConfirmedDependencies: history.ConfirmedDependencies, Resolution: resolution})
			if err != nil || !sameRefSlices(plan.AffectedConfirmedChain, affected) {
				t.Fatalf("a qualifying retraction must resolve with the retained affected chain: %v", err)
			}
		})
	}
}

func TestPlanResolveDisputeManifestBindsAffectedSuffixRatherThanUnaffectedAncestor(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE).Attempt
	ancestor := attemptVariant(base, 3711, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	head := attemptVariant(base, 3712, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	head.Preparation.Predecessor = preparationReference(ancestor.Preparation)
	chain := initialChain(ancestor.Preparation)
	chain.LatestHead = clonePreparation(head.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	ancestorConfirmation := retainedObservation(ancestor, 3713, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	headConfirmation := retainedObservation(head, 3714, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	headRejection := retainedObservation(head, 3715, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := historyWith(chain, []AttemptSnapshot{ancestor, head}, ancestorConfirmation, headConfirmation, headRejection)
	affected := []*tammyv1.SourceRef{preparationReference(head.Preparation)}
	resolution := &tammyv1.BasExternalDisputeInput{Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
		DisputedObservations:   []*tammyv1.SourceRef{observationReference(headConfirmation), observationReference(headRejection)},
		AffectedConfirmedChain: affected, DependentChainManifestHash: dependencyManifestHash(affected), Reason: "Review only the contradicted descendant"}
	input := DisputeResolutionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: history.ConfirmedDependencies, Resolution: resolution}
	if _, err := PlanResolveDispute(input); err != nil {
		t.Fatalf("affected suffix must resolve: %v", err)
	}
	resolution.DependentChainManifestHash = dependencyManifestHash(confirmedDependencyRefs(history.ConfirmedDependencies))
	if _, err := PlanResolveDispute(input); err == nil {
		t.Fatal("unaffected ancestor must not change the affected-chain manifest")
	}
}

func TestPlanCorrectionQualifierReplacementKeepsConfirmedChainUndisputed(t *testing.T) {
	input := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN)
	input.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE
	input.History.Chain = cloneChainSnapshot(input.Chain)
	input.History.Records = input.History.Records[:1]
	input.History.LastOrdinal = 1
	correction := &tammyv1.BasExternalObservationCorrectionInput{DisputedObservation: input.Resolution.DisputedObservations[0],
		CorrectionKind:     tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		CorrectedEventKind: eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE), Reason: "Correct the kind while retaining qualifying external use"}
	plan, err := PlanCorrection(CorrectionInput{Chain: input.Chain, ExpectedChain: mustChainExpectation(t, input.Chain), History: input.History,
		Attempt: input.History.Attempts[0], Correction: correction})
	if err != nil || plan.OpensDispute || plan.AdvancesHead || plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE {
		t.Fatalf("qualifier-to-qualifier correction must append without a synthetic dispute: plan=%+v err=%v", plan, err)
	}
}

func TestPlanCorrectionStaleSameTargetReopensDisputeForConflictingQualifiers(t *testing.T) {
	attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE).Attempt
	attempt.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	chain := initialChain(attempt.Preparation)
	chain.RootPreparation = clonePreparation(attempt.Preparation)
	chain.EffectiveOriginal = clonePreparation(attempt.Preparation)
	chain.LatestHead = clonePreparation(attempt.Preparation)
	acceptance := retainedObservation(attempt, 374, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	history := historyWith(chain, []AttemptSnapshot{attempt}, acceptance)

	completedCorrection := retainedCorrection(attempt, 375, observationReference(acceptance),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT), nil)
	firstPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history, Attempt: attempt,
		Correction: completedCorrection.GetCorrection(),
	})
	if err != nil || firstPlan.OpensDispute || firstPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE {
		t.Fatalf("a first qualifier-preserving correction should not create a dispute: plan=%+v err=%v", firstPlan, err)
	}
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 2, Observation: completedCorrection})
	history.LastOrdinal = 2

	staleAcceptanceCorrection := retainedCorrection(attempt, 376, observationReference(acceptance),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE), nil)
	secondPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history, Attempt: attempt,
		Correction: staleAcceptanceCorrection.GetCorrection(),
	})
	if err != nil || !secondPlan.OpensDispute ||
		secondPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
		t.Fatalf("a later correction against the superseded original must surface its conflict: plan=%+v err=%v", secondPlan, err)
	}

	chain.DisputeOverlay = secondPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 3, Observation: staleAcceptanceCorrection})
	history.LastOrdinal = 3
	preparationRef := preparationReference(attempt.Preparation)
	resolutionInput := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(attempt)},
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations:       []*tammyv1.SourceRef{observationReference(acceptance), observationReference(completedCorrection), observationReference(staleAcceptanceCorrection)},
			AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationRef},
			DependentChainManifestHash: dependencyRefsManifest([]*tammyv1.SourceRef{preparationRef}),
			Reason:                     "Review the conflicting same-target qualifying corrections",
		},
	}
	if _, err := PlanResolveDispute(resolutionInput); err != nil {
		t.Fatalf("the stale-target conflict should remain resolvable from its exact retained history: %v", err)
	}
}

func TestPlanObservationDuplicateAfterUpheldResolutionKeepsOverlayResolved(t *testing.T) {
	input := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN)
	resolution := &tammyv1.BasExternalDisputeResolution{ResolutionId: testUUID(3721), Family: cloneRef(input.Chain.Family.Reference),
		Resolution: input.Resolution, ActorUserId: testUUID(3722), ResolvedAt: input.History.Records[0].Observation.RecordedAt, ContentHash: testHash(91)}
	input.History.Records = append(input.History.Records, ExternalHistoryRecord{Ordinal: 3, Resolution: resolution})
	input.History.LastOrdinal = 3
	input.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD
	input.History.Chain = cloneChainSnapshot(input.Chain)
	observation := ObservationInput{Chain: input.Chain, History: input.History, Attempt: input.History.Attempts[0],
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT, ClaimedEventAt: input.History.Records[0].Observation.RecordedAt,
			Reason: "Retain a duplicate report after the exact conflict was upheld"}}
	plan, err := PlanObservation(observation)
	if err != nil || plan.OpensDispute || plan.AdvancesHead || plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD {
		t.Fatalf("a resolved old rejection must not reopen on duplicate confirmation: plan=%+v err=%v", plan, err)
	}
	incomplete := cloneHistory(observation.History)
	incomplete.Records[2].Resolution.Resolution.DisputedObservations = incomplete.Records[2].Resolution.Resolution.DisputedObservations[:1]
	badInput := observation
	badInput.History = incomplete
	if _, err := PlanObservation(badInput); err == nil {
		t.Fatal("a retained resolution that omitted the conflicting rejection cannot resolve historical subjects")
	}
	observation.Observation.EventKind = tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION
	newRejection, err := PlanObservation(observation)
	if err != nil || !newRejection.OpensDispute || newRejection.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
		t.Fatalf("a new rejection after resolution is a new retained dispute: plan=%+v err=%v", newRejection, err)
	}
}
