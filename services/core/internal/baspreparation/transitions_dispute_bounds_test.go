package baspreparation

import (
	"strings"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestPlanResolveDisputeAcceptsExactly200Subjects(t *testing.T) {
	chain, attempt, history, observations := confirmedDisputeBoundsFixture(t, 100, 100)
	resolution := disputeBoundsResolution(attempt, observations)

	plan, err := PlanResolveDispute(DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: history.ConfirmedDependencies, Resolution: resolution,
	})
	if err != nil {
		t.Fatalf("a dispute with exactly 200 subjects must remain resolvable: %v", err)
	}
	if !sameRefSlices(plan.DisputedObservations, resolution.DisputedObservations) || len(plan.DisputedObservations) != 200 {
		t.Fatalf("resolution must retain all 200 exact subjects: got %d", len(plan.DisputedObservations))
	}
}

func TestPlanCorrectionRejectsCandidateThatWouldExceedDisputeSubjectBound(t *testing.T) {
	chain, attempt, history, observations := confirmedDisputeBoundsFixture(t, 100, 100)
	target := observationReference(observations[0])
	correction := &tammyv1.BasExternalObservationCorrectionInput{
		DisputedObservation: target,
		CorrectionKind:      tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT,
		Reason:              "Retract the retained assertion after reviewing its source",
	}

	if _, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history, Correction: correction,
	}); err == nil {
		t.Fatal("a correction that would increase the unresolved dispute from 200 to 201 subjects must be rejected")
	}
}

func TestPlanObservationRejectsFirstConflictBeyondDisputeSubjectBound(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	observations := make([]*tammyv1.BasExternalObservation, 0, 200)
	for index := 0; index < 100; index++ {
		observations = append(observations, retainedObservation(input.Attempt, uint64(7500+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT))
	}
	for index := 0; index < 100; index++ {
		observations = append(observations, retainedObservation(input.Attempt, uint64(7600+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION))
	}
	input.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	input.History = historyWith(input.Chain, []AttemptSnapshot{input.Attempt}, observations...)

	if _, err := PlanObservation(input); err == nil {
		t.Fatal("a first-conflict observation that would produce 201 unresolved subjects must be rejected")
	}
}

func TestPlanObservationAllowsResolvedHistoryOver200Records(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT)
	attempt := attemptVariant(base.Attempt, 7701,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	chain := initialChain(attempt.Preparation)
	chain.LatestHead = clonePreparation(attempt.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	qualifications := make([]*tammyv1.BasExternalObservation, 0, 100)
	rejections := make([]*tammyv1.BasExternalObservation, 0, 100)
	for index := 0; index < 100; index++ {
		qualifications = append(qualifications, retainedObservation(attempt, uint64(7800+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT))
	}
	for index := 0; index < 100; index++ {
		rejections = append(rejections, retainedObservation(attempt, uint64(7900+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION))
	}
	observations := append(qualifications, rejections...)
	history := historyWith(chain, []AttemptSnapshot{attempt}, observations...)
	resolution := disputeBoundsResolution(attempt, observations)
	resolutionPlan, err := PlanResolveDispute(DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: history.ConfirmedDependencies, Resolution: resolution,
	})
	if err != nil {
		t.Fatalf("the 200-subject dispute must resolve before testing retained-history growth: %v", err)
	}

	retainedResolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(8001), Family: cloneRef(chain.Family.Reference), Resolution: resolution,
		ActorUserId: testUUID(200), ResolvedAt: observations[len(observations)-1].RecordedAt, ContentHash: testHash(81),
	}
	history.LastOrdinal++
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: history.LastOrdinal, Resolution: retainedResolution})
	chain.DisputeOverlay = resolutionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	base.Chain = chain
	base.Attempt = attempt
	base.History = history

	plan, err := PlanObservation(base)
	if err != nil {
		t.Fatalf("a resolved 201-record history must still accept an observation when no unresolved subjects exceed the bound: %v", err)
	}
	if plan.OpensDispute || plan.AdvancesHead {
		t.Fatalf("a non-qualifying event after the resolved history must not reopen or advance the chain: %+v", plan)
	}
}

func TestPlanCorrectionRejectsAppendBeyondHistoryRecordBound(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	target := retainedObservation(base.Attempt, 8200, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	observations := []*tammyv1.BasExternalObservation{target}
	for index := 0; index < maxExternalHistoryRecords-1; index++ {
		observations = append(observations, retainedObservation(base.Attempt, uint64(8201+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT))
	}
	base.History = historyWith(base.Chain, []AttemptSnapshot{base.Attempt}, observations...)
	correction := &tammyv1.BasExternalObservationCorrectionInput{
		DisputedObservation: observationReference(target),
		CorrectionKind:      tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT,
		Reason:              "Retract the retained assertion after reviewing its source",
	}

	if err := validateExternalHistory(base.History); err != nil {
		t.Fatalf("a complete retained history at its bound must remain reducible: %v", err)
	}
	if _, err := PlanCorrection(CorrectionInput{
		Chain: base.Chain, ExpectedChain: mustChainExpectation(t, base.Chain), Attempt: base.Attempt,
		History: base.History, Correction: correction,
	}); err == nil {
		t.Fatal("a correction that would append record 1001 must be rejected")
	}
}

func TestPlanResolveDisputeRejectsAppendBeyondHistoryRecordBound(t *testing.T) {
	chain, attempt, history, observations := confirmedDisputeBoundsFixture(t, 100, 100)
	appendPaymentRecordsToHistory(t, &history, attempt, maxExternalHistoryRecords, 8300)
	resolution := disputeBoundsResolution(attempt, observations)

	if err := validateExternalHistory(history); err != nil {
		t.Fatalf("the unresolved dispute must remain valid at the complete-history bound: %v", err)
	}
	if _, err := PlanResolveDispute(DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: history.ConfirmedDependencies, Resolution: resolution,
	}); err == nil {
		t.Fatal("a resolution that would append record 1001 must be rejected")
	}
}

func TestPendingDisputeMutationsReserveResolutionRecord(t *testing.T) {
	for _, test := range []struct {
		name          string
		recordCount   int
		wantRejection bool
	}{
		{name: "observation leaves resolution slot", recordCount: maxExternalHistoryRecords - 2},
		{name: "observation cannot consume resolution slot", recordCount: maxExternalHistoryRecords - 1, wantRejection: true},
		{name: "correction leaves resolution slot", recordCount: maxExternalHistoryRecords - 2},
		{name: "correction cannot consume resolution slot", recordCount: maxExternalHistoryRecords - 1, wantRejection: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, qualification := pendingFirstConflictBoundsFixture(t, test.recordCount)
			var err error
			if strings.HasPrefix(test.name, "observation") {
				_, err = PlanObservation(input)
			} else {
				correction := &tammyv1.BasExternalObservationCorrectionInput{
					DisputedObservation: observationReference(qualification),
					CorrectionKind:      tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT,
					Reason:              "Retract the retained assertion after reviewing its source",
				}
				_, err = PlanCorrection(CorrectionInput{
					Chain: input.Chain, ExpectedChain: mustChainExpectation(t, input.Chain), Attempt: input.Attempt,
					History: input.History, Correction: correction,
				})
			}
			if test.wantRejection && err == nil {
				t.Fatal("pending dispute mutation must leave a retained-history slot for its resolution")
			}
			if !test.wantRejection && err != nil {
				t.Fatalf("pending dispute may grow when one resolution record will still fit: %v", err)
			}
		})
	}
}

func confirmedDisputeBoundsFixture(t *testing.T, qualificationCount, rejectionCount int) (ChainSnapshot, AttemptSnapshot, ExternalHistoryInput, []*tammyv1.BasExternalObservation) {
	t.Helper()
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	attempt := attemptVariant(base.Attempt, 7101,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	chain := initialChain(attempt.Preparation)
	chain.LatestHead = clonePreparation(attempt.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	observations := make([]*tammyv1.BasExternalObservation, 0, qualificationCount+rejectionCount)
	for index := 0; index < qualificationCount; index++ {
		observations = append(observations, retainedObservation(attempt, uint64(7200+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT))
	}
	for index := 0; index < rejectionCount; index++ {
		observations = append(observations, retainedObservation(attempt, uint64(7300+index),
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION))
	}
	return chain, attempt, historyWith(chain, []AttemptSnapshot{attempt}, observations...), observations
}

func disputeBoundsResolution(attempt AttemptSnapshot, observations []*tammyv1.BasExternalObservation) *tammyv1.BasExternalDisputeInput {
	affected := []*tammyv1.SourceRef{preparationReference(attempt.Preparation)}
	subjects := make([]*tammyv1.SourceRef, 0, len(observations))
	for _, observation := range observations {
		subjects = append(subjects, observationReference(observation))
	}
	return &tammyv1.BasExternalDisputeInput{
		Outcome:              tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
		DisputedObservations: subjects, AffectedConfirmedChain: affected,
		DependentChainManifestHash: dependencyManifestHash(affected),
		Reason:                     "Resolve the exact retained conflict after reviewing its assertions",
	}
}

func pendingFirstConflictBoundsFixture(t *testing.T, recordCount int) (ObservationInput, *tammyv1.BasExternalObservation) {
	t.Helper()
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	input.Chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	qualification := retainedObservation(input.Attempt, 8400, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	rejection := retainedObservation(input.Attempt, 8401, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	input.History = historyWith(input.Chain, []AttemptSnapshot{input.Attempt}, qualification, rejection)
	appendPaymentRecordsToHistory(t, &input.History, input.Attempt, recordCount, 8402)
	return input, qualification
}

func appendPaymentRecordsToHistory(t *testing.T, history *ExternalHistoryInput, attempt AttemptSnapshot, recordCount int, firstID uint64) {
	t.Helper()
	if recordCount < len(history.Records) || recordCount > maxExternalHistoryRecords {
		t.Fatalf("requested retained history size %d from current size %d is outside the test bound", recordCount, len(history.Records))
	}
	for len(history.Records) < recordCount {
		history.LastOrdinal++
		observation := retainedObservation(attempt, firstID, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT)
		firstID++
		history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: history.LastOrdinal, Observation: observation})
	}
}
