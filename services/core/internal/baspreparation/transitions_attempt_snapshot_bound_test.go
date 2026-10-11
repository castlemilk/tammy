package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestAttemptCreatingPlansRejectSnapshotBoundWithRepeatedPreparationID(t *testing.T) {
	t.Run("replacement", func(t *testing.T) {
		chain, attempt, history := attemptCapacityHistory(t, maxExternalHistoryAttempts-1,
			tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
		appendSecondRetainedRootVersion(&history)
		assertAttemptSnapshotCount(t, history, maxExternalHistoryAttempts-1, maxExternalHistoryAttempts)

		plan, err := PlanCreateReplacement(ReplacementInput{
			Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history,
			Reason: "Replace the retained unused attempt",
		})
		if err == nil || plan != nil {
			t.Fatalf("replacement must reject 1000 retained preparation snapshots before creating snapshot 1001: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("rejected recovery", func(t *testing.T) {
		chain, attempt, history := attemptCapacityHistory(t, maxExternalHistoryAttempts-1,
			tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
		appendSecondRetainedRootVersion(&history)
		assertAttemptSnapshotCount(t, history, maxExternalHistoryAttempts-1, maxExternalHistoryAttempts)
		rejection := retainedObservation(attempt, 991001,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
		history.Records = []ExternalHistoryRecord{{Ordinal: 1, Observation: rejection}}
		history.LastOrdinal = 1

		plan, err := PlanRejectedRecovery(RejectedRecoveryInput{
			Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history,
			ResolvedRejection: observationReference(rejection), Reason: "Retry after the retained rejection",
		})
		if err == nil || plan != nil {
			t.Fatalf("rejected recovery must reject 1000 retained preparation snapshots before creating snapshot 1001: plan=%+v err=%v", plan, err)
		}
	})

	t.Run("revision", func(t *testing.T) {
		input := attemptCapacityRevisionInput(t, maxExternalHistoryAttempts-1)
		appendSecondRetainedRootVersion(&input.History)
		assertAttemptSnapshotCount(t, input.History, maxExternalHistoryAttempts-1, maxExternalHistoryAttempts)

		plan, err := PlanCreateRevision(input)
		if err == nil || plan != nil {
			t.Fatalf("revision must reject 1000 retained preparation snapshots before creating snapshot 1001: plan=%+v err=%v", plan, err)
		}
	})
}

func appendSecondRetainedRootVersion(history *ExternalHistoryInput) {
	rootVersion := cloneAttemptSnapshot(history.Attempts[0])
	rootVersion.Preparation.Version++
	rootVersion.Preparation.ContentHash = testHash(194)
	history.Attempts = append(history.Attempts, rootVersion)
}

func assertAttemptSnapshotCount(t *testing.T, history ExternalHistoryInput, wantUnique, wantSnapshots int) {
	t.Helper()
	unique := make(map[string]struct{}, len(history.Attempts))
	for _, attempt := range history.Attempts {
		if attempt.Preparation == nil {
			t.Fatal("attempt fixture unexpectedly has no preparation")
		}
		unique[attempt.Preparation.PreparationId] = struct{}{}
	}
	if len(unique) != wantUnique || len(history.Attempts) != wantSnapshots {
		t.Fatalf("attempt fixture has %d unique preparation IDs across %d snapshots; want %d and %d", len(unique), len(history.Attempts), wantUnique, wantSnapshots)
	}
}
