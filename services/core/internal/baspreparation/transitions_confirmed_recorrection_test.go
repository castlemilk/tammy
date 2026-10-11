package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestPlanCorrectionAfterUpheldRetractionReopensConfirmedDispute(t *testing.T) {
	for _, test := range []struct {
		name           string
		correctionKind tammyv1.BasCorrectionKind
		correctedEvent *tammyv1.BasExternalEventKind
	}{
		{
			name:           "retract stale original",
			correctionKind: tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT,
		},
		{
			name:           "reclassify stale original as pending",
			correctionKind: tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
			correctedEvent: eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE).Attempt
			attempt.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
			chain := initialChain(attempt.Preparation)
			chain.RootPreparation = clonePreparation(attempt.Preparation)
			chain.EffectiveOriginal = clonePreparation(attempt.Preparation)
			chain.LatestHead = clonePreparation(attempt.Preparation)

			confirmed := retainedObservation(attempt, 9820, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
			history := historyWith(chain, []AttemptSnapshot{attempt}, confirmed)
			retraction := retainedCorrection(attempt, 9821, observationReference(confirmed),
				tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
			firstPlan, err := PlanCorrection(CorrectionInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
				History: history, Correction: retraction.GetCorrection(),
			})
			if err != nil || !firstPlan.OpensDispute || firstPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
				t.Fatalf("retracting a confirmed qualifying event should open its first dispute: plan=%+v err=%v", firstPlan, err)
			}

			chain.DisputeOverlay = firstPlan.NextOverlay
			history.Chain = cloneChainSnapshot(chain)
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 2, Observation: retraction})
			history.LastOrdinal = 2
			firstResolutionInput := &tammyv1.BasExternalDisputeInput{
				Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
				DisputedObservations: []*tammyv1.SourceRef{
					observationReference(confirmed), observationReference(retraction),
				},
				AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationReference(attempt.Preparation)},
				DependentChainManifestHash: dependencyManifestHash([]*tammyv1.SourceRef{preparationReference(attempt.Preparation)}),
				Reason:                     "Uphold the exact retained confirmed assertion after review",
			}
			firstResolutionPlan, err := PlanResolveDispute(DisputeResolutionInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: cloneHistory(history),
				ConfirmedDependencies: history.ConfirmedDependencies, Resolution: firstResolutionInput,
			})
			if err != nil || firstResolutionPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD {
				t.Fatalf("the first confirmed dispute should be upholdable: plan=%+v err=%v", firstResolutionPlan, err)
			}

			firstResolution := &tammyv1.BasExternalDisputeResolution{
				ResolutionId: testUUID(9822), Family: cloneRef(chain.Family.Reference), Resolution: firstResolutionInput,
				ActorUserId: testUUID(9823), ResolvedAt: confirmed.RecordedAt, ContentHash: testHash(122),
			}
			chain.DisputeOverlay = firstResolutionPlan.NextOverlay
			history.Chain = cloneChainSnapshot(chain)
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 3, Resolution: firstResolution})
			history.LastOrdinal = 3

			secondCorrection := retainedCorrection(attempt, 9824, observationReference(confirmed), test.correctionKind,
				test.correctedEvent, nil)
			secondPlan, err := PlanCorrection(CorrectionInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
				History: history, Correction: secondCorrection.GetCorrection(),
			})
			if err != nil || !secondPlan.OpensDispute || secondPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED ||
				!sameRefSlices(secondPlan.AffectedConfirmedChain, []*tammyv1.SourceRef{preparationReference(attempt.Preparation)}) {
				t.Fatalf("a new correction against the upheld stale original must reopen the exact confirmed dependency dispute: plan=%+v err=%v", secondPlan, err)
			}

			chain.DisputeOverlay = secondPlan.NextOverlay
			history.Chain = cloneChainSnapshot(chain)
			history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 4, Observation: secondCorrection})
			history.LastOrdinal = 4
			secondResolution := &tammyv1.BasExternalDisputeInput{
				Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
				DisputedObservations: []*tammyv1.SourceRef{
					observationReference(confirmed), observationReference(secondCorrection),
				},
				AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationReference(attempt.Preparation)},
				DependentChainManifestHash: dependencyManifestHash([]*tammyv1.SourceRef{preparationReference(attempt.Preparation)}),
				Reason:                     "Resolve the later stale-target correction after reviewing its exact subjects",
			}
			if _, err := PlanResolveDispute(DisputeResolutionInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
				ConfirmedDependencies: history.ConfirmedDependencies, Resolution: secondResolution,
			}); err != nil {
				t.Fatalf("the reopened dispute must resolve against its exact new subjects: %v", err)
			}
		})
	}
}
