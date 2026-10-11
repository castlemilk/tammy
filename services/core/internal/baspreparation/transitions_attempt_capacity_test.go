package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestPlanCreateReplacementReservesAttemptHistoryCapacity(t *testing.T) {
	for _, test := range []struct {
		name        string
		attempts    int
		wantSuccess bool
	}{
		{name: "999 attempts leave one successor slot", attempts: maxExternalHistoryAttempts - 1, wantSuccess: true},
		{name: "1000 attempts cannot create attempt 1001", attempts: maxExternalHistoryAttempts},
	} {
		t.Run(test.name, func(t *testing.T) {
			chain, attempt, history := attemptCapacityHistory(t, test.attempts, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
			plan, err := PlanCreateReplacement(ReplacementInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history,
				Reason: "Replace the retained unused attempt",
			})
			if test.wantSuccess {
				if err != nil || plan == nil {
					t.Fatalf("a complete same-family history with one remaining attempt slot should permit replacement: plan=%+v err=%v", plan, err)
				}
				return
			}
			if err == nil {
				t.Fatal("replacement must reject a history already at the 1000-attempt bound instead of authorizing attempt 1001")
			}
		})
	}
}

func TestPlanRejectedRecoveryReservesAttemptHistoryCapacity(t *testing.T) {
	for _, test := range []struct {
		name        string
		attempts    int
		wantSuccess bool
	}{
		{name: "999 attempts leave one successor slot", attempts: maxExternalHistoryAttempts - 1, wantSuccess: true},
		{name: "1000 attempts cannot create attempt 1001", attempts: maxExternalHistoryAttempts},
	} {
		t.Run(test.name, func(t *testing.T) {
			chain, attempt, history := attemptCapacityHistory(t, test.attempts, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
			rejection := retainedObservation(attempt, uint64(900000+test.attempts), tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
			history.Records = []ExternalHistoryRecord{{Ordinal: 1, Observation: rejection}}
			history.LastOrdinal = 1

			plan, err := PlanRejectedRecovery(RejectedRecoveryInput{
				Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history,
				ResolvedRejection: observationReference(rejection), Reason: "Retry after the exact retained rejection",
			})
			if test.wantSuccess {
				if err != nil || plan == nil {
					t.Fatalf("a complete same-family history with one remaining attempt slot should permit rejected recovery: plan=%+v err=%v", plan, err)
				}
				return
			}
			if err == nil {
				t.Fatal("rejected recovery must reject a history already at the 1000-attempt bound instead of authorizing attempt 1001")
			}
		})
	}
}

func TestPlanCreateRevisionRequiresBoundedCompleteAttemptHistory(t *testing.T) {
	for _, test := range []struct {
		name        string
		attempts    int
		wantSuccess bool
	}{
		{name: "999 attempts leave one successor slot", attempts: maxExternalHistoryAttempts - 1, wantSuccess: true},
		{name: "1000 attempts cannot create attempt 1001", attempts: maxExternalHistoryAttempts},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := attemptCapacityRevisionInput(t, test.attempts)
			plan, err := PlanCreateRevision(input)
			if test.wantSuccess {
				if err != nil || plan == nil {
					t.Fatalf("a complete same-family history with one remaining attempt slot should permit revision: plan=%+v err=%v", plan, err)
				}
				return
			}
			if err == nil {
				t.Fatal("revision must reject a history already at the 1000-attempt bound instead of authorizing attempt 1001")
			}
		})
	}
}

func TestPlanCreateRevisionRejectsMissingOrMismatchedAttemptHistory(t *testing.T) {
	input := attemptCapacityRevisionInput(t, 2)
	input.History = ExternalHistoryInput{}
	if _, err := PlanCreateRevision(input); err == nil {
		t.Fatal("revision planning must require a complete retained history rather than create an attempt with no history bound")
	}

	input = attemptCapacityRevisionInput(t, 2)
	input.History.Chain.Family.Version++
	if _, err := PlanCreateRevision(input); err == nil {
		t.Fatal("revision planning must reject a complete history loaded for a different family snapshot")
	}
}

func attemptCapacityHistory(t *testing.T, count int, activeState tammyv1.BasPreparationState) (ChainSnapshot, AttemptSnapshot, ExternalHistoryInput) {
	t.Helper()
	if count < 1 || count > maxExternalHistoryAttempts {
		t.Fatalf("test attempt count %d is outside the retained history bound", count)
	}
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	attempts := make([]AttemptSnapshot, 0, count)
	for index := 0; index < count; index++ {
		state := tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
		if index == count-1 {
			state = activeState
		}
		attempt := attemptVariant(base, uint64(60000+index*10), state)
		if index > 0 {
			attempt.Preparation.Predecessor = preparationReference(attempts[index-1].Preparation)
		}
		attempts = append(attempts, attempt)
	}

	chain := initialChain(attempts[0].Preparation)
	if count > 1 {
		chain.Branch = &Branch{
			Reference: sourceRef("bas_branch", testUUID(uint64(75000+count)), 1, 120),
			Version:   1, ActiveAttempt: clonePreparation(attempts[count-1].Preparation),
		}
	}
	history := ExternalHistoryInput{Chain: cloneChainSnapshot(chain), Attempts: cloneAttemptSnapshots(attempts)}
	return chain, cloneAttemptSnapshot(attempts[count-1]), history
}

func attemptCapacityRevisionInput(t *testing.T, count int) RevisionInput {
	t.Helper()
	if count < 1 || count > maxExternalHistoryAttempts {
		t.Fatalf("test attempt count %d is outside the retained history bound", count)
	}
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	attempts := make([]AttemptSnapshot, 0, count)
	for index := 0; index < count; index++ {
		state := tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
		if index == count-1 {
			state = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
		}
		attempt := attemptVariant(base, uint64(100000+index*10), state)
		if index > 0 {
			attempt.Preparation.Predecessor = preparationReference(attempts[index-1].Preparation)
		}
		attempts = append(attempts, attempt)
	}

	confirmedHead := attempts[count-1]
	chain := initialChain(attempts[0].Preparation)
	chain.EffectiveOriginal = clonePreparation(confirmedHead.Preparation)
	chain.LatestHead = clonePreparation(confirmedHead.Preparation)
	chain.PeriodReference = cloneRef(confirmedHead.Preparation.Period)
	history := ExternalHistoryInput{
		Chain: cloneChainSnapshot(chain), Attempts: cloneAttemptSnapshots(attempts),
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(confirmedHead)},
	}
	period := confirmedPeriod(testUUID(120000), confirmedHead.Preparation.OrganisationId, 2, 121)
	return RevisionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), PeriodSnapshot: period,
		ProfileSnapshot: obligationsProfile(confirmedHead.Preparation.OrganisationId), History: history,
		Reason: "Correct a confirmed statement",
	}
}
