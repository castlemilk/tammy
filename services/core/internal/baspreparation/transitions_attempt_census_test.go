package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestPlanCreateRevisionCountsConfirmedHeadOutsideAttemptCensus(t *testing.T) {
	input := attemptCapacityRevisionInput(t, maxExternalHistoryAttempts)
	// The confirmed head is also supplied separately as a dependency. Removing
	// it from the current aggregate census must not hide one retained attempt.
	input.History.Attempts = input.History.Attempts[:len(input.History.Attempts)-1]

	if _, err := PlanCreateRevision(input); err == nil {
		t.Fatal("revision planning must reject a 1000-attempt history when its confirmed head is omitted from the attempt census")
	}
}

func TestReplacementPlansRequireActiveAttemptInCensus(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		name := "unused replacement"
		if recovery {
			name = "rejected recovery"
		}
		t.Run(name, func(t *testing.T) {
			chain, active, history := attemptCapacityHistory(t, maxExternalHistoryAttempts,
				tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
			// The active attempt remains in Chain.Branch and in the command input,
			// but is absent from the 999 listed predecessor attempts.
			history.Attempts = history.Attempts[:len(history.Attempts)-1]
			if recovery {
				rejection := retainedObservation(active, 990001,
					tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
				history.Records = []ExternalHistoryRecord{{Ordinal: 1, Observation: rejection}}
				history.LastOrdinal = 1
				plan, err := PlanRejectedRecovery(RejectedRecoveryInput{
					Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: active, History: history,
					ResolvedRejection: observationReference(rejection), Reason: "Retry after the retained rejection",
				})
				if err == nil || plan != nil {
					t.Fatalf("rejected recovery must not authorize attempt 1001 from an incomplete census: plan=%+v err=%v", plan, err)
				}
				return
			}
			plan, err := PlanCreateReplacement(ReplacementInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: active, History: history,
				Reason: "Replace the retained unused attempt",
			})
			if err == nil || plan != nil {
				t.Fatalf("replacement must not authorize attempt 1001 from an incomplete census: plan=%+v err=%v", plan, err)
			}
		})
	}
}

func TestPlanCreateRevisionRejectsMissingFirstConfirmedAttemptAncestry(t *testing.T) {
	input := attemptCapacityRevisionInput(t, 5)
	// The only confirmed dependency is the first effective head, whose retained
	// preparation points through these superseded attempts. Omit an intermediate
	// ancestor while keeping the exact confirmed head and chain snapshots intact.
	input.History.Attempts = append(input.History.Attempts[:2], input.History.Attempts[3:]...)

	if _, err := PlanCreateRevision(input); err == nil {
		t.Fatal("revision planning must reject an incomplete predecessor census before the first confirmed head")
	}
}
