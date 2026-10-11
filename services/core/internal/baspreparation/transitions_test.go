package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAssessReviewRequiresCompleteValidationAndExactWarningAcknowledgements(t *testing.T) {
	input := readyReviewInput()
	warning := finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, "WARNING_SOURCE")
	information := finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_INFORMATION, "INFO_SOURCE")
	input.Validation.Findings = []*tammyv1.BasValidationFinding{warning, information}
	input.Acknowledgements = []*tammyv1.BasWarningAcknowledgement{acknowledgement(input.Validation, warning)}

	decision, err := AssessReview(input)
	if err != nil {
		t.Fatalf("AssessReview returned error: %v", err)
	}
	if decision.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || !decision.CanFreeze || len(decision.UnacknowledgedWarnings) != 0 {
		t.Fatalf("an exact warning acknowledgement should allow review-ready freeze, while information needs no acknowledgement: %+v", decision)
	}

	stale := cloneReviewInput(input)
	stale.CurrentCapture.SourceSnapshotHash = testHash(45)
	staleDecision, err := AssessReview(stale)
	if err != nil {
		t.Fatalf("AssessReview returned error for stale capture: %v", err)
	}
	if staleDecision.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || staleDecision.CanFreeze || !staleDecision.SourceChanged {
		t.Fatalf("a live source change should invalidate review and return to collection: %+v", staleDecision)
	}

	frozen := cloneReviewInput(input)
	frozen.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF
	frozen.CurrentCapture.SourceSnapshotHash = testHash(45)
	frozenDecision, err := AssessReview(frozen)
	if err != nil {
		t.Fatalf("AssessReview returned error for a frozen preparation: %v", err)
	}
	if frozenDecision.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF || frozenDecision.CanFreeze || !frozenDecision.SourceChanged {
		t.Fatalf("live source change must mark a frozen snapshot without reopening or recalculating it: %+v", frozenDecision)
	}

	for _, severity := range []tammyv1.BasFindingSeverity{
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER,
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED,
	} {
		t.Run(severity.String(), func(t *testing.T) {
			blocked := readyReviewInput()
			blocked.Validation.Findings = []*tammyv1.BasValidationFinding{finding(severity, "BLOCKING_SOURCE")}
			result, err := AssessReview(blocked)
			if err != nil {
				t.Fatalf("AssessReview returned error: %v", err)
			}
			if result.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_BLOCKED || result.CanFreeze {
				t.Fatalf("%v cannot be waived for freeze: %+v", severity, result)
			}
		})
	}
	for _, severity := range []tammyv1.BasFindingSeverity{
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_BLOCKER,
		tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED,
	} {
		blockedAck := readyReviewInput()
		blockedAck.Validation.Findings = []*tammyv1.BasValidationFinding{finding(severity, "BLOCKING_SOURCE")}
		blockedAck.Acknowledgements = []*tammyv1.BasWarningAcknowledgement{acknowledgement(blockedAck.Validation, blockedAck.Validation.Findings[0])}
		if _, err := AssessReview(blockedAck); err == nil {
			t.Fatalf("a %v acknowledgement is extraneous and cannot waive readiness", severity)
		}
	}

	for _, outcome := range []tammyv1.BasCalculationOutcome{
		tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_INCOMPLETE,
		tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSUPPORTED,
		tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_UNSPECIFIED,
	} {
		t.Run(outcome.String(), func(t *testing.T) {
			incomplete := readyReviewInput()
			incomplete.Calculation.Result.Outcome = outcome
			result, err := AssessReview(incomplete)
			if err != nil {
				t.Fatalf("AssessReview returned error: %v", err)
			}
			if result.CanFreeze {
				t.Fatalf("calculation outcome %v cannot freeze", outcome)
			}
		})
	}
}

func TestAssessReviewRejectsMalformedUnsupportedFinding(t *testing.T) {
	input := readyReviewInput()
	unsupported := finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED, "UNSUPPORTED_SOURCE")
	unsupported.StableCode = string(bytes.Repeat([]byte{'X'}, 129))
	input.Validation.Findings = []*tammyv1.BasValidationFinding{unsupported}
	if _, err := AssessReview(input); err == nil {
		t.Fatal("a malformed UNSUPPORTED finding must fail as invalid retained validation, not become a normal blocker")
	}

	input = readyReviewInput()
	unsupported = finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_UNSUPPORTED, "UNSUPPORTED_SOURCE")
	unsupported.Sources[0].Type = "invalid source type"
	input.Validation.Findings = []*tammyv1.BasValidationFinding{unsupported}
	if _, err := AssessReview(input); err == nil {
		t.Fatal("an unsupported finding with malformed source identity must fail closed")
	}
}

func TestAssessReviewKeepsIncompleteCollectionInCollecting(t *testing.T) {
	for _, test := range []struct {
		name string
		make func() ReviewInput
	}{
		{name: "no calculation yet", make: func() ReviewInput {
			input := readyReviewInput()
			input.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
			input.Preparation.CurrentCalculation = nil
			input.Preparation.CurrentValidation = nil
			input.Calculation = nil
			input.Validation = nil
			return input
		}},
		{name: "calculation but no validation", make: func() ReviewInput {
			input := readyReviewInput()
			input.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING
			input.Preparation.CurrentValidation = nil
			input.Validation = nil
			return input
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision, err := AssessReview(test.make())
			if err != nil {
				t.Fatalf("valid incomplete collection should not be an error: %v", err)
			}
			if decision.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || decision.CanFreeze {
				t.Fatalf("incomplete collection must remain collecting and not freezeable: %+v", decision)
			}
		})
	}
}

func TestPolicyFixturesSatisfyGeneratedContracts(t *testing.T) {
	review := readyReviewInput()
	for _, message := range []proto.Message{
		review.Preparation,
		review.Calculation.PeriodSnapshot,
		review.Calculation.ProfileSnapshot,
		review.Calculation,
		review.Validation,
	} {
		if err := protovalidate.Validate(message); err != nil {
			t.Fatalf("positive retained fixture %T must satisfy its generated contract: %v", message, err)
		}
	}
	frozen := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt.Frozen
	if err := protovalidate.Validate(frozen); err != nil {
		t.Fatalf("positive frozen fixture must satisfy its generated contract: %v", err)
	}
}

func TestAssessReviewBindsStableInputsWithoutComparingHistoricalPreparationHashes(t *testing.T) {
	input := readyReviewInput()
	// Calculation and validation may pin different retained aggregate versions:
	// validation metadata is saved after the calculation pointer was written.
	input.Validation.PreparationHash = testHash(19)
	input.Validation.ContentHash = testHash(31)
	input.Preparation.CurrentValidation = sourceRef("bas_validation", input.Validation.ValidationId, input.Validation.Revision, 31)
	decision, err := AssessReview(input)
	if err != nil {
		t.Fatalf("historical aggregate hashes must not override stable input and current snapshot identity: %v", err)
	}
	if decision.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || !decision.CanFreeze {
		t.Fatalf("matching stable financial identity should remain freezeable: %+v", decision)
	}
}

func TestAssessReviewTurnsChangedLiveInputIdentityIntoStaleCollection(t *testing.T) {
	input := readyReviewInput()
	input.CurrentCapture.InputRevision++
	input.CurrentCapture.InputHash = testHash(91)
	decision, err := AssessReview(input)
	if err != nil {
		t.Fatalf("a newer live input capture is stale review state, not malformed retained history: %v", err)
	}
	if decision.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || !decision.SourceChanged || decision.CanFreeze {
		t.Fatalf("changed live financial identity must return to collection: %+v", decision)
	}
}

func TestAssessReviewPreservesEveryTerminalLabelOnLiveSourceChange(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BY_CONFIRMED_REVISION,
	} {
		t.Run(state.String(), func(t *testing.T) {
			input := readyReviewInput()
			input.Preparation.State = state
			input.CurrentCapture.SourceSnapshotHash = testHash(45)
			decision, err := AssessReview(input)
			if err != nil {
				t.Fatalf("live source indication must not reopen or revalidate a terminal attempt: %v", err)
			}
			if decision.State != state || decision.CanFreeze || !decision.SourceChanged {
				t.Fatalf("terminal state and labels stay frozen while live source change is surfaced: %+v", decision)
			}
		})
	}
}

func TestAssessReviewRejectsDuplicateExtraneousAndMismatchedAcknowledgements(t *testing.T) {
	base := readyReviewInput()
	warning := finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, "WARNING_SOURCE")
	base.Validation.Findings = []*tammyv1.BasValidationFinding{warning}
	validAck := acknowledgement(base.Validation, warning)

	for _, test := range []struct {
		name string
		acks []*tammyv1.BasWarningAcknowledgement
	}{
		{name: "duplicate", acks: []*tammyv1.BasWarningAcknowledgement{validAck, proto.Clone(validAck).(*tammyv1.BasWarningAcknowledgement)}},
		{name: "wrong validation", acks: []*tammyv1.BasWarningAcknowledgement{func() *tammyv1.BasWarningAcknowledgement {
			ack := proto.Clone(validAck).(*tammyv1.BasWarningAcknowledgement)
			ack.Validation = sourceRef("bas_validation", testUUID(91), 1, 91)
			return ack
		}()}},
		{name: "wrong finding", acks: []*tammyv1.BasWarningAcknowledgement{func() *tammyv1.BasWarningAcknowledgement {
			ack := proto.Clone(validAck).(*tammyv1.BasWarningAcknowledgement)
			ack.Finding = sourceRef("bas_validation_finding", testUUID(92), 1, 92)
			return ack
		}()}},
		{name: "same finding id but wrong finding hash", acks: []*tammyv1.BasWarningAcknowledgement{func() *tammyv1.BasWarningAcknowledgement {
			ack := proto.Clone(validAck).(*tammyv1.BasWarningAcknowledgement)
			ack.Finding.ContentHash = testHash(92)
			return ack
		}()}},
		{name: "wrong source snapshot", acks: []*tammyv1.BasWarningAcknowledgement{func() *tammyv1.BasWarningAcknowledgement {
			ack := proto.Clone(validAck).(*tammyv1.BasWarningAcknowledgement)
			ack.SourceSnapshotHash = testHash(92)
			return ack
		}()}},
		{name: "extraneous", acks: []*tammyv1.BasWarningAcknowledgement{func() *tammyv1.BasWarningAcknowledgement {
			ack := proto.Clone(validAck).(*tammyv1.BasWarningAcknowledgement)
			ack.Finding.Id = testUUID(93)
			return ack
		}()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := cloneReviewInput(base)
			input.Acknowledgements = test.acks
			if _, err := AssessReview(input); err == nil {
				t.Fatal("duplicate, extraneous or mismatched acknowledgement must fail closed")
			}
		})
	}

	missing := cloneReviewInput(base)
	if result, err := AssessReview(missing); err != nil || result.CanFreeze || len(result.UnacknowledgedWarnings) != 1 {
		t.Fatalf("missing acknowledgement should remain review-ready but not freezeable: decision=%+v err=%v", result, err)
	}
}

func TestPlanFreezeUsesStableInputsAndFreshCaptureRatherThanMutableAggregateHash(t *testing.T) {
	input := readyReviewInput()
	warning := finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, "WARNING_SOURCE")
	input.Validation.Findings = []*tammyv1.BasValidationFinding{warning}
	input.Acknowledgements = []*tammyv1.BasWarningAcknowledgement{acknowledgement(input.Validation, warning)}
	chain := initialChain(input.Preparation)
	freeze := readyFreezeInput(input, chain)

	// Saving validation and acknowledgement metadata may change the aggregate CAS hash.
	// Stable financial identity and a fresh source capture still match the reviewed snapshot.
	freeze.Review.Preparation.ContentHash = testHash(33)
	freeze.Review.Preparation.Version++
	plan, err := PlanFreeze(freeze)
	if err != nil {
		t.Fatalf("metadata-only aggregate hash changes must not stale validation: %v", err)
	}
	if !bytes.Equal(plan.ExpectedPreparationHash, freeze.Review.Preparation.ContentHash) {
		t.Fatal("freeze plan must return the current aggregate hash for transactional CAS")
	}
	if plan.ExpectedPreparationVersion != freeze.Review.Preparation.Version {
		t.Fatal("freeze plan must return the current aggregate version for transactional CAS")
	}

	staleSource := readyFreezeInput(input, chain)
	staleSource.Review.CurrentCapture.SourceSnapshotHash = testHash(44)
	if _, err := PlanFreeze(staleSource); err == nil {
		t.Fatal("changed fresh source snapshot must invalidate freeze")
	}

	changedVectorSameFingerprint := readyFreezeInput(input, chain)
	changedVectorSameFingerprint.Review.CurrentCapture.RevisionVector.LedgerRevision++
	if _, err := PlanFreeze(changedVectorSameFingerprint); err != nil {
		t.Fatalf("a provenance vector change with identical complete fingerprint must not falsely stale the review: %v", err)
	}

	staleInput := readyFreezeInput(input, chain)
	staleInput.Review.CurrentCapture.InputRevision++
	if _, err := PlanFreeze(staleInput); err == nil {
		t.Fatal("changed stable financial input identity must invalidate freeze")
	}

	staleValidation := readyFreezeInput(input, chain)
	staleValidation.Review.Preparation.CurrentCalculation = sourceRef("bas_calculation", testUUID(99), 1, 99)
	if _, err := PlanFreeze(staleValidation); err == nil {
		t.Fatal("freeze must use the preparation's current calculation and validation references")
	}
}

func TestPlanFreezeChecksActualConfirmedPeriodScopeFromCalculationSnapshot(t *testing.T) {
	input := readyReviewInput()
	input.Calculation.PeriodSnapshot.Confirmation.Period = &tammyv1.BasCivilDateRange{
		StartDate: testCivilDate(2026, 10, 1), EndDate: testCivilDate(2026, 12, 31),
	}
	input.Calculation.PeriodSnapshot.Confirmation.Window = tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC
	if _, err := PlanFreeze(readyFreezeInput(input, initialChain(input.Preparation))); err == nil {
		t.Fatal("a calculation captured for another actual civil period/window cannot freeze in this family")
	}
}

func TestPlanFreezeRejectsSameScopeCalculationWithDifferentCurrentPeriodOrProfileRefs(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ReviewInput)
	}{
		{name: "period reference", mutate: func(review *ReviewInput) {
			period := confirmedPeriod(testUUID(220), review.Preparation.OrganisationId, 2, 220)
			review.Calculation.Period = sourceRef("bas_period", period.PeriodId, period.Version, 220)
			review.Calculation.PeriodSnapshot = period
		}},
		{name: "profile reference", mutate: func(review *ReviewInput) {
			profile := obligationsProfile(review.Preparation.OrganisationId)
			profile.ProfileId = testUUID(221)
			profile.Revision = 2
			profile.ContentHash = testHash(221)
			profileRef := sourceRef("bas_obligations_profile", profile.ProfileId, profile.Revision, 221)
			review.Calculation.Profile = profileRef
			review.Calculation.ProfileSnapshot = profile
			review.Calculation.PeriodSnapshot.Confirmation.Profile = cloneRef(profileRef)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			review := readyReviewInput()
			test.mutate(&review)
			if _, err := PlanFreeze(readyFreezeInput(review, initialChain(review.Preparation))); err == nil {
				t.Fatal("same family scope does not permit freezing a calculation bound to different current preparation period/profile references")
			}
		})
	}
}

func TestPlanFreezeReturnsDefensiveSnapshotAndExactChainCAS(t *testing.T) {
	input := readyReviewInput()
	warning := finding(tammyv1.BasFindingSeverity_BAS_FINDING_SEVERITY_WARNING, "WARNING_SOURCE")
	input.Validation.Findings = []*tammyv1.BasValidationFinding{warning}
	input.Acknowledgements = []*tammyv1.BasWarningAcknowledgement{acknowledgement(input.Validation, warning)}
	chain := initialChain(input.Preparation)
	freezeInput := readyFreezeInput(input, chain)
	plan, err := PlanFreeze(freezeInput)
	if err != nil {
		t.Fatalf("PlanFreeze returned error: %v", err)
	}
	if plan.ExpectedChain == nil || plan.ExpectedChain.Family == nil || plan.ExpectedChain.ExpectedFamilyVersion != chain.Family.Version {
		t.Fatalf("freeze plan must pin exact family identity and version: %+v", plan.ExpectedChain)
	}
	if !plan.ExpectedChain.LatestHead.GetAbsent() || !plan.ExpectedChain.Branch.GetAbsent() || !plan.ExpectedChain.ActiveAttempt.GetAbsent() {
		t.Fatalf("missing head, branch and active attempt must be explicit absence: %+v", plan.ExpectedChain)
	}
	if plan.Calculation == nil || plan.Validation == nil || !reflect.DeepEqual(plan.Acknowledgements, input.Acknowledgements) {
		t.Fatal("freeze intent must retain the exact calculation, validation and acknowledgement snapshots")
	}

	freezeInput.Review.Preparation.ContentHash[0] ^= 0xff
	freezeInput.Review.Calculation.ContentHash[0] ^= 0xff
	freezeInput.Review.Validation.ContentHash[0] ^= 0xff
	freezeInput.Review.Acknowledgements[0].SourceSnapshotHash[0] ^= 0xff
	freezeInput.Chain.Family.Reference.ContentHash[0] ^= 0xff
	if plan.ExpectedPreparationHash[0] == freezeInput.Review.Preparation.ContentHash[0] || plan.Calculation.ContentHash[0] == freezeInput.Review.Calculation.ContentHash[0] || plan.Validation.ContentHash[0] == freezeInput.Review.Validation.ContentHash[0] || plan.Acknowledgements[0].SourceSnapshotHash[0] == freezeInput.Review.Acknowledgements[0].SourceSnapshotHash[0] || plan.ExpectedChain.Family.ContentHash[0] == freezeInput.Chain.Family.Reference.ContentHash[0] {
		t.Fatal("returned pure plan must not alias caller-owned mutable data")
	}
}

func TestPlanFreezeRejectsUnreservedNonrootOrEmptyBranchAttempt(t *testing.T) {
	original := readyReviewInput()
	nonroot := cloneReviewInput(original)
	nonroot.Preparation.PreparationId = testUUID(225)
	nonroot.Calculation.Preparation = sourceRef("bas_preparation", nonroot.Preparation.PreparationId, 1, 225)
	nonroot.Validation.PreparationId = nonroot.Preparation.PreparationId
	chain := initialChain(original.Preparation)
	if _, err := PlanFreeze(readyFreezeInput(nonroot, chain)); err == nil {
		t.Fatal("a nonroot attempt cannot freeze without a reserved branch and active-attempt pointer")
	}
	chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(226), 1, 226), Version: 1}
	if _, err := PlanFreeze(readyFreezeInput(nonroot, chain)); err == nil {
		t.Fatal("a branch reservation with an explicit absent attempt cannot freeze an unrelated preparation")
	}
}

func TestFamilyScopeUsesOrganisationActualPeriodAndObligationWindow(t *testing.T) {
	first, err := FamilyScopeFromPeriod(confirmedPeriod(testUUID(6), testUUID(1), 1, 60))
	if err != nil {
		t.Fatalf("derive family scope from retained period: %v", err)
	}
	samePeriod, err := FamilyScopeFromPeriod(confirmedPeriod(testUUID(16), testUUID(1), 2, 61))
	if err != nil {
		t.Fatalf("derive same civil scope from replacement period version: %v", err)
	}
	if !SameFamilyScope(first, samePeriod) {
		t.Fatal("profile/rule updates and changed retained period references must keep one family")
	}
	for _, changed := range []FamilyScope{
		{OrganisationID: testUUID(3), Start: samePeriod.Start, End: samePeriod.End, Window: samePeriod.Window},
		{OrganisationID: first.OrganisationID, Start: testCivilDate(2026, 10, 1), End: testCivilDate(2026, 12, 31), Window: samePeriod.Window},
		{OrganisationID: first.OrganisationID, Start: samePeriod.Start, End: samePeriod.End, Window: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC},
	} {
		if SameFamilyScope(first, changed) {
			t.Fatalf("changed organisation, actual period or obligation window must not share a family: %+v", changed)
		}
	}
}

func TestPlanObservationQualifiesOnlyCompletedLodgementOrAcceptance(t *testing.T) {
	for _, kind := range []tammyv1.BasExternalEventKind{
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
	} {
		t.Run(kind.String(), func(t *testing.T) {
			input := observationInputFor(kind)
			input.Observation.Evidence = []*tammyv1.SourceRef{sourceRef("bas_external_document", testUUID(250), 1, 250)}
			plan, err := PlanObservation(input)
			if err != nil {
				t.Fatalf("PlanObservation returned error: %v", err)
			}
			if !plan.Qualifies || !plan.AdvancesHead || plan.Trust != tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED {
				t.Fatalf("only a bound user-recorded completed event may advance the head: %+v", plan)
			}
			if plan.Observation == nil || plan.Observation.EventKind != kind || !sameRef(plan.Frozen, input.Attempt.FrozenReference) {
				t.Fatal("confirmation intent must bind the exact event and frozen BAS version")
			}
			if plan.Trust != tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED {
				t.Fatal("evidence references never upgrade user-recorded external event trust")
			}
		})
	}

	for _, kind := range []tammyv1.BasExternalEventKind{
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REVIEWER_HANDOFF,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REFUND,
	} {
		t.Run(kind.String(), func(t *testing.T) {
			plan, err := PlanObservation(observationInputFor(kind))
			if err != nil {
				t.Fatalf("nonqualifying observations remain appendable: %v", err)
			}
			if plan.Qualifies || plan.AdvancesHead {
				t.Fatalf("%v cannot advance the effective chain: %+v", kind, plan)
			}
		})
	}
}

func TestPlanObservationUsesRetainedFrozenPreparationAcrossMutableAggregateUpdates(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	input.Attempt.Preparation.Version++
	input.Attempt.Preparation.ContentHash = testHash(81)
	input.History.Chain = input.Chain
	input.History.Attempts[0] = input.Attempt
	if _, err := PlanObservation(input); err != nil {
		t.Fatalf("a metadata-updated preparation aggregate must still match its exact retained frozen snapshot: %v", err)
	}
}

func TestPlanObservationBindsStableSourceIdentityAndIgnoresCurrentPeriodProvenance(t *testing.T) {
	changedFinancialFacts := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	changedFinancialFacts.Attempt.Preparation.SourceSnapshotHash = testHash(227)
	changedFinancialFacts.History.Attempts[0] = cloneAttemptSnapshot(changedFinancialFacts.Attempt)
	if _, err := PlanObservation(changedFinancialFacts); err == nil {
		t.Fatal("a frozen attempt cannot be observed against a different stable source snapshot identity")
	}

	historicalAttempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	correctedCurrentPeriod := confirmedPeriod(testUUID(228), historicalAttempt.Attempt.Preparation.OrganisationId, 2, 228)
	historicalAttempt.Chain.PeriodReference = sourceRef("bas_period", correctedCurrentPeriod.PeriodId, correctedCurrentPeriod.Version, 228)
	historicalAttempt.History.Chain = cloneChainSnapshot(historicalAttempt.Chain)
	if _, err := PlanObservation(historicalAttempt); err != nil {
		t.Fatalf("a later current-period correction with the same actual family scope cannot rewrite or stale an older frozen artifact: %v", err)
	}
}

func TestPlanObservationReturnsDefensiveCopyOfExactIntentAndCAS(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	plan, err := PlanObservation(input)
	if err != nil {
		t.Fatalf("PlanObservation returned error: %v", err)
	}
	originalReason := plan.Observation.Reason
	originalFrozenHash := bytes.Clone(plan.Frozen.ContentHash)
	originalFamilyHash := bytes.Clone(plan.ExpectedChain.Family.ContentHash)
	input.Observation.Reason = "Caller mutation after policy decision"
	input.Attempt.FrozenReference.ContentHash[0] ^= 0xff
	input.Chain.Family.Reference.ContentHash[0] ^= 0xff
	if plan.Observation.Reason != originalReason || !bytes.Equal(plan.Frozen.ContentHash, originalFrozenHash) || !bytes.Equal(plan.ExpectedChain.Family.ContentHash, originalFamilyHash) {
		t.Fatal("returned observation intent and exact chain CAS must not alias caller-owned mutable data")
	}
}

func TestPlanObservationAcceptsCorrectedPeriodReferenceWithSameActualFamilyScope(t *testing.T) {
	original := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	rootSnapshot := clonePreparation(original.Preparation)
	originalFrozen := proto.Clone(original.Frozen).(*tammyv1.BasFrozenBas)
	original.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	original.Preparation.Version++
	original.Preparation.ContentHash = testHash(229)
	periodSnapshot := confirmedPeriod(testUUID(230), original.Preparation.OrganisationId, 2, 230)
	corrected := rebindAttemptToPeriod(attemptVariant(original, 231, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF), periodSnapshot, 231)
	corrected.Preparation.Predecessor = preparationReference(original.Preparation)
	chain := initialChain(original.Preparation)
	chain.RootPreparation = rootSnapshot
	chain.EffectiveOriginal = nil
	chain.PeriodReference = cloneRef(corrected.Preparation.Period)
	chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(232), 1, 232), Version: 2,
		ActiveAttempt: clonePreparation(corrected.Preparation)}
	input := ObservationInput{Chain: chain, Attempt: corrected, History: historyWith(chain, []AttemptSnapshot{original, corrected}),
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
			Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT, ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)), Reason: "Corrected-period replacement lodgement"}}
	if _, err := PlanObservation(input); err != nil {
		t.Fatalf("a different retained period reference with the same confirmed civil scope remains in the family: %v", err)
	}
	if !proto.Equal(original.Frozen, originalFrozen) {
		t.Fatal("creating the corrected linked attempt cannot rewrite the original frozen BAS artifact")
	}
}

func TestPlanObservationRejectsInconsistentFrozenProfileOrPeriodBinding(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*AttemptSnapshot)
	}{
		{name: "period reference", change: func(attempt *AttemptSnapshot) {
			attempt.Frozen.CalculationSnapshot.Period = sourceRef("bas_period", testUUID(16), 2, 61)
		}},
		{name: "profile reference", change: func(attempt *AttemptSnapshot) {
			attempt.Frozen.CalculationSnapshot.Profile = sourceRef("bas_obligations_profile", testUUID(17), 2, 62)
		}},
		{name: "period snapshot profile reference", change: func(attempt *AttemptSnapshot) {
			attempt.Frozen.CalculationSnapshot.PeriodSnapshot.Confirmation.Profile = sourceRef("bas_obligations_profile", testUUID(17), 2, 62)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
			test.change(&input.Attempt)
			input.History.Attempts[0] = input.Attempt
			if _, err := PlanObservation(input); err == nil {
				t.Fatal("the frozen artifact, current attempt and calculation snapshot must retain exact period/profile provenance")
			}
		})
	}
}

func TestPlanObservationRejectsHistoryLoadedAgainstAStaleEffectiveHead(t *testing.T) {
	input := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	head := attemptVariant(input.Attempt, 233, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	head.Preparation.Predecessor = preparationReference(input.Attempt.Preparation)
	input.Chain.LatestHead = clonePreparation(head.Preparation)
	input.Chain.EffectiveOriginal = clonePreparation(input.Attempt.Preparation)
	// History still pins the prior family pointer set, as happens when another
	// transaction advances the chain after a policy read.
	input.History = historyWith(initialChain(input.Attempt.Preparation), []AttemptSnapshot{input.Attempt})
	if _, err := PlanObservation(input); err == nil {
		t.Fatal("an observation cannot proceed from a stale family head snapshot")
	}
}

func TestPlanFreezePinsImmutableFamilyRootAndCurrentHeadAsSeparateCASValues(t *testing.T) {
	input := readyReviewInput()
	chain := initialChain(input.Preparation)
	chain.RootPreparation = preparationAt(10, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION)
	chain.RootPreparation.Family = proto.Clone(input.Preparation.Family).(*tammyv1.SourceRef)
	chain.LatestHead = preparationAt(11, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	chain.LatestHead.Family = proto.Clone(input.Preparation.Family).(*tammyv1.SourceRef)
	chain.EffectiveOriginal = clonePreparation(chain.LatestHead)
	chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(12), 1, 12), Version: 4,
		RootHead: proto.Clone(chain.LatestHead).(*tammyv1.BasPreparation), ActiveAttempt: proto.Clone(input.Preparation).(*tammyv1.BasPreparation)}
	plan, err := PlanFreeze(readyFreezeInput(input, chain))
	if err != nil {
		t.Fatalf("PlanFreeze returned error: %v", err)
	}
	got := plan.ExpectedChain
	if got.RootPreparation.Id != chain.RootPreparation.PreparationId || got.LatestHead.GetRetained().Id != chain.LatestHead.PreparationId {
		t.Fatalf("immutable family root and effective current head must remain distinct in CAS: %+v", got)
	}
	if got.LatestHead.GetExpectedVersion() != chain.LatestHead.Version || got.Branch.GetRetained().Id != chain.Branch.Reference.Id || got.Branch.GetExpectedVersion() != chain.Branch.Version || got.ActiveAttempt.GetRetained().Id != input.Preparation.PreparationId || got.ActiveAttempt.GetExpectedVersion() != input.Preparation.Version {
		t.Fatalf("freeze must pin latest-head, branch and active-attempt identities with positive versions: %+v", got)
	}
}

func TestBuildChainExpectationRejectsMalformedRetainedIdentityAndVersions(t *testing.T) {
	base := initialChain(preparationAt(254, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING))
	for _, test := range []struct {
		name   string
		mutate func(*ChainSnapshot)
	}{
		{name: "family version", mutate: func(chain *ChainSnapshot) { chain.Family.Version = 0 }},
		{name: "family ref type", mutate: func(chain *ChainSnapshot) { chain.Family.Reference.Type = "bas_preparation" }},
		{name: "family ref hash", mutate: func(chain *ChainSnapshot) { chain.Family.Reference.ContentHash = nil }},
		{name: "root family mismatch", mutate: func(chain *ChainSnapshot) {
			chain.RootPreparation.Family = sourceRef("bas_family", testUUID(255), 1, 255)
		}},
		{name: "period provenance", mutate: func(chain *ChainSnapshot) { chain.PeriodReference.Revision = 0 }},
		{name: "branch version", mutate: func(chain *ChainSnapshot) {
			chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(256), 1, 1)}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			chain := cloneChainSnapshot(base)
			test.mutate(&chain)
			if _, err := BuildChainExpectation(chain); err == nil {
				t.Fatal("malformed or unversioned retained identity must fail before producing a CAS expectation")
			}
		})
	}
}

func TestPlanCreateReplacementRequiresUnusedExactFrozenAttemptAndKeepsNonlodgementEvents(t *testing.T) {
	for _, kind := range []tammyv1.BasExternalEventKind{
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REVIEWER_HANDOFF,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REFUND,
	} {
		t.Run(kind.String(), func(t *testing.T) {
			attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
			chain := initialChain(attempt.Preparation)
			history := historyWith(chain, []AttemptSnapshot{attempt}, retainedObservation(attempt, 101, kind))
			input := ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history, Reason: "Correct source data before external use"}
			plan, err := PlanCreateReplacement(input)
			if err != nil {
				t.Fatalf("%v alone does not prove external lodging and cannot block an unused assertion: %v", kind, err)
			}
			if plan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING || !plan.ExpectedChain.ActiveAttempt.GetAbsent() ||
				!sameRef(plan.PredecessorPreparation, preparationReference(attempt.Preparation)) || !sameRef(plan.PredecessorFrozen, attempt.FrozenReference) {
				t.Fatalf("an initial original replacement keeps absent branch/attempt CAS and pins its predecessor separately: %+v", plan)
			}
		})
	}

	for _, kinds := range [][]tammyv1.BasExternalEventKind{
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT},
		{tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION},
	} {
		t.Run(kinds[0].String()+"-history", func(t *testing.T) {
			attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
			chain := initialChain(attempt.Preparation)
			observations := make([]*tammyv1.BasExternalObservation, 0, len(kinds))
			for index, kind := range kinds {
				observations = append(observations, retainedObservation(attempt, uint64(111+index), kind))
			}
			input := ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
				History: historyWith(chain, []AttemptSnapshot{attempt}, observations...), Reason: "Replace only after a verified unused assertion"}
			if _, err := PlanCreateReplacement(input); err == nil {
				t.Fatalf("pending, unknown, rejected, qualifying or contradictory external history must block unused replacement: %v", kinds)
			}
		})
	}
}

func TestPlanCreateReplacementReusesOneBranchAcrossMultipleUnusedAttempts(t *testing.T) {
	corrective, resolutionRef := correctiveObservationInput(t)
	first := cloneAttemptSnapshot(corrective.Attempt)
	chain := cloneChainSnapshot(corrective.Chain)
	history := cloneHistory(corrective.History)
	firstInput := ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: first,
		History: history, Reason: "The frozen corrective statement was not used"}
	firstPlan, err := PlanCreateReplacement(firstInput)
	if err != nil {
		t.Fatalf("first unused replacement should reserve the existing branch: %v", err)
	}
	if !sameRef(firstPlan.ExpectedChain.Branch.GetRetained(), chain.Branch.Reference) || firstPlan.ExpectedChain.Branch.GetExpectedVersion() != chain.Branch.Version {
		t.Fatalf("replacement must advance the existing branch with exact CAS: %+v", firstPlan.ExpectedChain)
	}
	if !proto.Equal(firstInput.Attempt.Preparation, first.Preparation) || firstPlan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatal("the predecessor remains immutable in policy input; the intent only requests supersession and a collecting successor")
	}

	first.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	first.Preparation.Version++
	first.Preparation.ContentHash = testHash(120)
	second := attemptVariant(first, 121, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	second.Preparation.Predecessor = preparationReference(first.Preparation)
	secondChain := cloneChainSnapshot(chain)
	secondChain.Branch.Version++
	secondChain.Branch.ActiveAttempt = clonePreparation(second.Preparation)
	secondInput := ReplacementInput{Chain: secondChain, ExpectedChain: mustChainExpectation(t, secondChain), Attempt: second,
		History: cloneHistory(history), Reason: "The second frozen statement was also unused"}
	secondInput.History.Chain = cloneChainSnapshot(secondChain)
	secondInput.History.Attempts[1] = cloneAttemptSnapshot(first)
	secondInput.History.Attempts = append(secondInput.History.Attempts, cloneAttemptSnapshot(second))
	secondPlan, err := PlanCreateReplacement(secondInput)
	if err != nil {
		t.Fatalf("a branch can retain multiple unused replacement attempts: %v", err)
	}
	if !sameRef(secondPlan.ExpectedChain.Branch.GetRetained(), chain.Branch.Reference) || secondPlan.ExpectedChain.Branch.GetExpectedVersion() != secondChain.Branch.Version {
		t.Fatal("the next attempt must reuse the one branch and pin its current version")
	}
	if secondPlan.ExpectedChain.LatestHead.GetRetained().Id != chain.LatestHead.PreparationId ||
		secondPlan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatal("unused corrective attempts keep the confirmed head and create only a new collecting successor")
	}

	stale := secondInput
	stale.ExpectedChain = mustChainExpectation(t, chain)
	if _, err := PlanCreateReplacement(stale); err == nil {
		t.Fatal("a stale branch version cannot be used to assert a second unused replacement")
	}
	if !sameRef(secondChain.Branch.Resolution, resolutionRef) {
		t.Fatal("repeated corrective replacements retain the exact resolution link")
	}
}

func TestPlanRejectedRecoveryCanReuseOneCorrectiveBranchAcrossRejectedAttempts(t *testing.T) {
	corrective, resolutionRef := correctiveObservationInput(t)
	root := cloneAttemptSnapshot(corrective.History.Attempts[0])
	first := attemptVariant(root, 123, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION)
	first.Preparation.Predecessor = preparationReference(root.Preparation)
	second := attemptVariant(root, 124, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	second.Preparation.Predecessor = preparationReference(first.Preparation)
	chain := cloneChainSnapshot(corrective.Chain)
	chain.Branch.Version++
	chain.Branch.ActiveAttempt = clonePreparation(second.Preparation)
	firstRejection := retainedObservation(first, 127, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	secondRejection := retainedObservation(second, 128, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := cloneHistory(corrective.History)
	history.Chain = cloneChainSnapshot(chain)
	history.Attempts[1] = cloneAttemptSnapshot(first)
	history.Attempts = append(history.Attempts, cloneAttemptSnapshot(second))
	history.Records = append(history.Records,
		ExternalHistoryRecord{Ordinal: 4, Observation: firstRejection},
		ExternalHistoryRecord{Ordinal: 5, Observation: secondRejection})
	history.LastOrdinal = 5
	input := RejectedRecoveryInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: second, History: history,
		ResolvedRejection: observationReference(secondRejection), Reason: "Retry only the exact retained rejection"}
	plan, err := PlanRejectedRecovery(input)
	if err != nil {
		t.Fatalf("a later rejected attempt can be recovered in the same corrective branch: %v", err)
	}
	if !sameRef(plan.ExpectedChain.Branch.GetRetained(), chain.Branch.Reference) ||
		plan.ExpectedChain.LatestHead.GetRetained().Id != root.Preparation.PreparationId ||
		!sameRef(plan.ResolvedRejection, observationReference(secondRejection)) {
		t.Fatalf("rejected recovery reuses the corrective reservation, exact head and exact rejection: %+v", plan)
	}
	stale := input
	staleChain := cloneChainSnapshot(chain)
	staleChain.Branch.Version--
	stale.ExpectedChain = mustChainExpectation(t, staleChain)
	if _, err := PlanRejectedRecovery(stale); err == nil {
		t.Fatal("a stale corrective branch version cannot authorize rejected recovery")
	}
	if !sameRef(chain.Branch.Resolution, resolutionRef) {
		t.Fatal("rejected retries retain the exact corrective resolution link")
	}
}

func TestPlanRejectedRecoveryRequiresExactUnambiguousRetainedRejection(t *testing.T) {
	attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(attempt.Preparation)
	rejection := retainedObservation(attempt, 130, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := historyWith(chain, []AttemptSnapshot{attempt}, rejection)
	rejectionRef := observationReference(rejection)
	input := RejectedRecoveryInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history,
		ResolvedRejection: rejectionRef, Reason: "The rejected submission requires a corrected retry"}
	plan, err := PlanRejectedRecovery(input)
	if err != nil {
		t.Fatalf("an explicit recovery command may use this exact retained unambiguous rejection: %v", err)
	}
	if !sameRef(plan.ResolvedRejection, rejectionRef) || plan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatalf("recovery must retain the exact rejection and start a collecting successor: %+v", plan)
	}
	if !proto.Equal(input.Attempt.Preparation, attempt.Preparation) {
		t.Fatal("rejected predecessor must remain retained and unchanged by the pure plan")
	}

	wrongRef := proto.Clone(rejectionRef).(*tammyv1.SourceRef)
	wrongRef.ContentHash = testHash(131)
	input.ResolvedRejection = wrongRef
	if _, err := PlanRejectedRecovery(input); err == nil {
		t.Fatal("recovery must reject a caller reference that does not exactly identify the retained rejection")
	}

	for _, extraKind := range []tammyv1.BasExternalEventKind{
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_UNKNOWN,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION,
	} {
		t.Run("conflict-"+extraKind.String(), func(t *testing.T) {
			conflicting := historyWith(chain, []AttemptSnapshot{attempt}, rejection,
				retainedObservation(attempt, uint64(140+extraKind), extraKind))
			candidate := RejectedRecoveryInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
				History: conflicting, ResolvedRejection: rejectionRef, Reason: "Resolve the exact rejected statement"}
			if _, err := PlanRejectedRecovery(candidate); err == nil {
				t.Fatalf("later %v or a second rejection makes the retained rejection ambiguous", extraKind)
			}
		})
	}
}

func TestPlanReplacementAndRejectedRecoveryExplicitlyClearFrozenBarrier(t *testing.T) {
	replacement, _ := correctiveObservationInput(t)
	replacement.Chain.Branch.FrozenBarrier = cloneRef(replacement.Attempt.FrozenReference)
	replacement.History.Chain = cloneChainSnapshot(replacement.Chain)
	replacementExpectation := mustChainExpectation(t, replacement.Chain)
	replacementPlan, err := PlanCreateReplacement(ReplacementInput{
		Chain: replacement.Chain, ExpectedChain: replacementExpectation, Attempt: replacement.Attempt,
		History: replacement.History, Reason: "Replace a frozen pre-resolution attempt before correction",
	})
	if err != nil {
		t.Fatalf("the frozen barrier attempt can be replaced through the explicit unused path: %v", err)
	}
	if !replacementPlan.ClearFrozenBarrier {
		t.Fatal("unused replacement intent must explicitly clear the exact old frozen barrier for its new successor")
	}

	recovery, _ := correctiveObservationInput(t)
	recovery.Chain.Branch.FrozenBarrier = cloneRef(recovery.Attempt.FrozenReference)
	recovery.History.Chain = cloneChainSnapshot(recovery.Chain)
	recoveryExpectation := mustChainExpectation(t, recovery.Chain)
	rejection := retainedObservation(recovery.Attempt, 253, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	recovery.History.Records = append(recovery.History.Records, ExternalHistoryRecord{Ordinal: 4, Observation: rejection})
	recovery.History.LastOrdinal = 4
	recoveryPlan, err := PlanRejectedRecovery(RejectedRecoveryInput{
		Chain: recovery.Chain, ExpectedChain: recoveryExpectation, Attempt: recovery.Attempt,
		History: recovery.History, ResolvedRejection: observationReference(rejection),
		Reason: "Retry after the exact pre-resolution attempt rejection",
	})
	if err != nil {
		t.Fatalf("the frozen barrier attempt can recover through its exact rejection: %v", err)
	}
	if !recoveryPlan.ClearFrozenBarrier {
		t.Fatal("rejected recovery intent must explicitly clear the exact old frozen barrier for its new successor")
	}
}

func TestPlanCreateRevisionSeparatesImmutableFamilyRootFromEffectiveOriginalAndHead(t *testing.T) {
	root := preparationAt(150, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION)
	confirmedOriginalAttempt := attemptVariant(observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt, 151,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	confirmedOriginal := confirmedOriginalAttempt.Preparation
	confirmedOriginal.Predecessor = preparationReference(root)
	chain := initialChain(root)
	chain.RootPreparation = clonePreparation(root)
	chain.EffectiveOriginal = clonePreparation(confirmedOriginal)
	chain.LatestHead = clonePreparation(confirmedOriginal)
	chain.PeriodReference = cloneRef(confirmedOriginal.Period)
	inputPeriod := confirmedPeriod(testUUID(152), confirmedOriginal.OrganisationId, 2, 152)
	history := historyWith(chain, []AttemptSnapshot{confirmedOriginalAttempt})
	history.ConfirmedDependencies = []AttemptSnapshot{cloneAttemptSnapshot(confirmedOriginalAttempt)}
	input := RevisionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), PeriodSnapshot: inputPeriod,
		ProfileSnapshot: obligationsProfile(confirmedOriginal.OrganisationId), History: history, Reason: "Correct a confirmed statement"}
	rootBefore := proto.Clone(chain.RootPreparation).(*tammyv1.BasPreparation)
	originalBefore := proto.Clone(chain.EffectiveOriginal).(*tammyv1.BasPreparation)
	plan, err := PlanCreateRevision(input)
	if err != nil {
		t.Fatalf("a confirmed original can create a same-scope linked revision: %v", err)
	}
	if plan.ExpectedChain.RootPreparation.Id != root.PreparationId || !sameRef(plan.EffectiveOriginal, preparationReference(confirmedOriginal)) ||
		!sameRef(plan.LatestHead, preparationReference(confirmedOriginal)) {
		t.Fatalf("revision must distinguish immutable family root from the effective original/current head: %+v", plan)
	}
	if !sameFamilyScopeMustMatch(t, inputPeriod, chain.Family.Scope) || !proto.Equal(chain.RootPreparation, rootBefore) || !proto.Equal(chain.EffectiveOriginal, originalBefore) {
		t.Fatal("same-period profile/rule corrections stay in the existing family and never supersede the original during planning")
	}
	if !plan.ExpectedChain.Branch.GetAbsent() || !plan.ExpectedChain.ActiveAttempt.GetAbsent() {
		t.Fatal("creating a revision reserves one branch with an explicit absent first attempt")
	}

	wrongPeriod := proto.Clone(inputPeriod).(*tammyv1.BasConfirmedPeriod)
	wrongPeriod.Confirmation.Period.StartDate = testCivilDate(2026, 10, 1)
	if _, err := PlanCreateRevision(RevisionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), PeriodSnapshot: wrongPeriod,
		ProfileSnapshot: obligationsProfile(confirmedOriginal.OrganisationId), History: history, Reason: "Different actual obligation period"}); err == nil {
		t.Fatal("a same-family profile revision cannot silently move the actual civil period or obligation window")
	}

	staleChain := cloneChainSnapshot(chain)
	staleHead := clonePreparation(confirmedOriginal)
	staleHead.PreparationId = testUUID(153)
	staleHead.Version = 1
	staleHead.ContentHash = testHash(153)
	staleHead.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	staleHead.Predecessor = preparationReference(confirmedOriginal)
	staleChain.LatestHead = staleHead
	staleInput := input
	staleInput.Chain = staleChain
	if _, err := PlanCreateRevision(staleInput); err == nil {
		t.Fatal("a revision cannot be planned using a stale family head expectation")
	}
}

func TestPlanObservationDuplicateConfirmationOnlyAppendsAndLateContradictionsDisputeWithoutRewind(t *testing.T) {
	confirmed := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	confirmed.Attempt.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	confirmed.Chain.RootPreparation = clonePreparation(confirmed.Attempt.Preparation)
	confirmed.Chain.LatestHead = clonePreparation(confirmed.Attempt.Preparation)
	confirmed.Chain.EffectiveOriginal = clonePreparation(confirmed.Attempt.Preparation)
	confirmed.History = historyWith(confirmed.Chain, []AttemptSnapshot{confirmed.Attempt},
		retainedObservation(confirmed.Attempt, 160, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT))
	confirmed.History.LastOrdinal = 1
	duplicate, err := PlanObservation(confirmed)
	if err != nil {
		t.Fatalf("a duplicate qualifying current-head report remains append-only history: %v", err)
	}
	if !duplicate.Qualifies || duplicate.AdvancesHead || duplicate.OpensDispute {
		t.Fatalf("duplicate qualification must not advance twice or invent a dispute: %+v", duplicate)
	}

	ancestor := attemptVariant(confirmed.Attempt, 161, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant := attemptVariant(confirmed.Attempt, 162, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant.Preparation.Predecessor = preparationReference(ancestor.Preparation)
	chain := cloneChainSnapshot(confirmed.Chain)
	chain.RootPreparation = clonePreparation(ancestor.Preparation)
	chain.EffectiveOriginal = clonePreparation(ancestor.Preparation)
	chain.LatestHead = clonePreparation(descendant.Preparation)
	history := historyWith(chain, []AttemptSnapshot{ancestor, descendant},
		retainedObservation(ancestor, 163, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT),
		retainedObservation(descendant, 164, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT))
	lateRejection := ObservationInput{Chain: chain, Attempt: ancestor, History: history,
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION,
			Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL, ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)), Reason: "Later evidence contradicts the recorded confirmation"}}
	late, err := PlanObservation(lateRejection)
	if err != nil {
		t.Fatalf("a late rejection of a confirmed ancestor must be retained as a dispute observation: %v", err)
	}
	if late.AdvancesHead || !late.OpensDispute || !sameRef(late.ExpectedChain.LatestHead.GetRetained(), preparationReference(descendant.Preparation)) ||
		!containsRef(late.AffectedConfirmedChain, preparationReference(ancestor.Preparation)) || !containsRef(late.AffectedConfirmedChain, preparationReference(descendant.Preparation)) {
		t.Fatalf("late rejection must dispute all affected confirmed descendants while preserving the exact effective head: %+v", late)
	}
}

func TestPlanObservationLateConfirmationOfUnusedSupersededAttemptDisputesWithoutRevival(t *testing.T) {
	old := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	old.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	newAttempt := attemptVariant(old, 170, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	newAttempt.Preparation.Predecessor = preparationReference(old.Preparation)
	chain := initialChain(old.Preparation)
	chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(171), 1, 171), Version: 2,
		ActiveAttempt: clonePreparation(newAttempt.Preparation)}
	history := historyWith(chain, []AttemptSnapshot{old, newAttempt})
	lateConfirmation := ObservationInput{Chain: chain, Attempt: old, History: history,
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT, ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)), Reason: "Late user report for the unused predecessor"}}
	plan, err := PlanObservation(lateConfirmation)
	if err != nil {
		t.Fatalf("late qualification of a superseded-unused attempt must be retained and disputed, not discarded: %v", err)
	}
	if plan.AdvancesHead || !plan.OpensDispute || !plan.Qualifies || plan.ExpectedChain.LatestHead.GetAbsent() == false ||
		!sameRef(plan.ExpectedChain.ActiveAttempt.GetRetained(), preparationReference(newAttempt.Preparation)) {
		t.Fatalf("a late qualifier disputes the unused assertion and leaves the active successor/head pointers unchanged: %+v", plan)
	}

	rootAttempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	rootAttempt.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	rootOnly := ObservationInput{Chain: initialChain(rootAttempt.Preparation), Attempt: rootAttempt,
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
			Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_OTHER, ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)), Reason: "Late report for superseded family root"}}
	rootOnly.Chain.RootPreparation = clonePreparation(rootAttempt.Preparation)
	rootOnly.Chain.EffectiveOriginal = nil
	rootOnly.History = historyWith(rootOnly.Chain, []AttemptSnapshot{rootAttempt})
	rootLate, err := PlanObservation(rootOnly)
	if err != nil {
		t.Fatalf("a superseded initial root with no branch remains reportable as late contradictory history: %v", err)
	}
	if rootLate.AdvancesHead || !rootLate.OpensDispute || rootLate.ExpectedChain.LatestHead.GetAbsent() == false {
		t.Fatalf("a superseded root must never revive or advance the effective head: %+v", rootLate)
	}
}

func TestPlanObservationDerivesTypedAppendOnlyCorrectionsAndRejectsDanglingSubjects(t *testing.T) {
	attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(attempt.Preparation)
	pending := retainedObservation(attempt, 180, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING)
	retraction := retainedCorrection(attempt, 181, observationReference(pending), tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
	history := historyWith(chain, []AttemptSnapshot{attempt}, pending, retraction)
	input := ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: history,
		Reason: "Correct a report that was not externally used"}
	plan, err := PlanCreateReplacement(input)
	if err != nil {
		t.Fatalf("a retained retraction removes the pending overlay without erasing history or turning it into UNKNOWN: %v", err)
	}
	if len(plan.RetainedHistory) != 2 || plan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatalf("correction must append and retain both the original report and correction: %+v", plan)
	}

	badCorrection := proto.Clone(retraction).(*tammyv1.BasExternalObservation)
	badCorrection.CorrectsObservation = sourceRef("bas_external_observation", testUUID(182), 1, 182)
	badHistory := historyWith(chain, []AttemptSnapshot{attempt}, pending, badCorrection)
	if _, err := PlanCreateReplacement(ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
		History: badHistory, Reason: "Must reject dangling correction"}); err == nil {
		t.Fatal("a correction whose oneof subject disagrees with the retained target must fail closed")
	}
	omittedTail := historyWith(chain, []AttemptSnapshot{attempt}, pending)
	omittedTail.LastOrdinal = 2
	if _, err := PlanCreateReplacement(ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
		History: omittedTail, Reason: "Must reject an incomplete history watermark"}); err == nil {
		t.Fatal("complete typed history must include the retained family watermark")
	}
	globalGap := historyWith(chain, []AttemptSnapshot{attempt}, retainedObservation(attempt, 194, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT))
	globalGap.Records[0].Ordinal = 4
	globalGap.LastOrdinal = 4
	if _, err := PlanCreateReplacement(ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
		History: globalGap, Reason: "A global append ordinal may have unrelated-family gaps"}); err != nil {
		t.Fatalf("global ordinal gaps from other families do not make this family history incomplete: %v", err)
	}

	ordered := historyWith(chain, []AttemptSnapshot{attempt},
		retainedObservation(attempt, 198, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PAYMENT),
		retainedObservation(attempt, 199, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REFUND))
	ordered.Records[0].Ordinal = 2
	ordered.Records[1].Ordinal = 1
	if _, err := PlanCreateReplacement(ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
		History: ordered, Reason: "Reject out-of-order retained history"}); err == nil {
		t.Fatal("complete typed history must be in strict Core-assigned append order")
	}
}

func TestPlanObservationCorrectionCannotCreateFreshConfirmationAuthority(t *testing.T) {
	attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(attempt.Preparation)
	pending := retainedObservation(attempt, 190, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING)
	correction := retainedCorrection(attempt, 191, observationReference(pending), tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT), nil)
	input := CorrectionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt, History: historyWith(chain, []AttemptSnapshot{attempt}, pending),
		Correction: correction.GetCorrection()}
	plan, err := PlanCorrection(input)
	if err != nil {
		t.Fatalf("a complete typed correction can be retained without granting new confirmation authority: %v", err)
	}
	if plan.Qualifies || plan.AdvancesHead || !sameRef(plan.Frozen, attempt.FrozenReference) {
		t.Fatalf("changing a nonqualifying event to a qualifying kind through correction cannot advance the effective head: %+v", plan)
	}

	completed := retainedObservation(attempt, 192, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	timeCorrection := retainedCorrection(attempt, 193, observationReference(completed), tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		nil, timestamppb.New(time.Date(2026, time.October, 10, 10, 0, 0, 0, time.UTC)))
	current := attempt
	current.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	confirmedChain := initialChain(current.Preparation)
	confirmedChain.LatestHead = clonePreparation(current.Preparation)
	timeInput := CorrectionInput{Chain: confirmedChain, ExpectedChain: mustChainExpectation(t, confirmedChain), Attempt: current,
		History: historyWith(confirmedChain, []AttemptSnapshot{current}, completed), Correction: timeCorrection.GetCorrection()}
	timePlan, err := PlanCorrection(timeInput)
	if err != nil {
		t.Fatalf("a time-only correction may preserve the retained qualifying history: %v", err)
	}
	if timePlan.AdvancesHead || timePlan.OpensDispute {
		t.Fatalf("a time-only correction cannot replay head advancement or invent a disputed outcome: %+v", timePlan)
	}
}

func TestPlanCorrectionOfRetractedUnusedPredecessorDisputesActiveSuccessor(t *testing.T) {
	predecessor := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(predecessor.Preparation)
	pending := retainedObservation(predecessor, 310, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING)
	retraction := retainedCorrection(predecessor, 311, observationReference(pending),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
	history := historyWith(chain, []AttemptSnapshot{predecessor}, pending, retraction)
	replacementPlan, err := PlanCreateReplacement(ReplacementInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: predecessor, History: history,
		Reason: "Replace an attempt whose pending report was retracted",
	})
	if err != nil {
		t.Fatalf("the unused predecessor should first be eligible for replacement: %v", err)
	}
	if replacementPlan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatalf("replacement should create a collecting successor: %+v", replacementPlan)
	}

	terminal := cloneAttemptSnapshot(predecessor)
	terminal.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	terminal.Preparation.Version++
	terminal.Preparation.ContentHash = testHash(112)
	successor := attemptVariant(terminal, 312, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	successor.Preparation.Predecessor = preparationReference(terminal.Preparation)
	chain.RootPreparation = clonePreparation(terminal.Preparation)
	chain.Branch = &Branch{
		Reference:     sourceRef("bas_branch", testUUID(313), 1, 113),
		Version:       1,
		ActiveAttempt: clonePreparation(successor.Preparation),
	}
	history.Chain = cloneChainSnapshot(chain)
	history.Attempts[0] = cloneAttemptSnapshot(terminal)
	history.Attempts = append(history.Attempts, cloneAttemptSnapshot(successor))

	lateUse := retainedCorrection(terminal, 314, observationReference(pending),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT), nil)
	correctionPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: terminal, History: history,
		Correction: lateUse.GetCorrection(),
	})
	if err != nil {
		t.Fatalf("a correction that reclassifies the predecessor assertion should be retained: %v", err)
	}

	chain.DisputeOverlay = correctionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 3, Observation: lateUse})
	history.LastOrdinal = 3
	successorObservation := ObservationInput{
		Chain: chain, Attempt: successor, History: history,
		Observation: &tammyv1.BasExternalObservationInput{
			EventKind:      tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel:        tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT,
			ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)),
			Reason:         "Confirm the active replacement attempt",
		},
	}
	blockedPlan, blockedErr := PlanObservation(successorObservation)
	if !correctionPlan.OpensDispute ||
		correctionPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED ||
		(blockedErr == nil && blockedPlan.AdvancesHead) {
		t.Fatalf("reclassifying an unused predecessor as used must dispute and block its active successor: correction=%+v successor=%+v err=%v",
			correctionPlan, blockedPlan, blockedErr)
	}

	upholdInput := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: cloneHistory(history),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations: []*tammyv1.SourceRef{
				observationReference(pending), observationReference(retraction), observationReference(lateUse),
			},
			DependentChainManifestHash: dependencyRefsManifest(nil),
			Reason:                     "Uphold the active replacement after reviewing the predecessor assertion",
		},
	}
	upholdPlan, err := PlanResolveDispute(upholdInput)
	if err != nil {
		t.Fatalf("the corrected terminal-predecessor assertion should resolve through the ordinary dispute path: %v", err)
	}
	if upholdPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD {
		t.Fatalf("UPHOLD should clear the terminal-predecessor dispute while preserving the active successor: %+v", upholdPlan)
	}
	resolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(315), Family: cloneRef(chain.Family.Reference),
		Resolution:  proto.Clone(upholdInput.Resolution).(*tammyv1.BasExternalDisputeInput),
		ActorUserId: testUUID(316),
		ResolvedAt:  timestamppb.New(time.Date(2026, time.October, 15, 11, 0, 0, 0, time.UTC)),
		ContentHash: testHash(117),
	}
	chain.DisputeOverlay = upholdPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 4, Resolution: resolution})
	history.LastOrdinal = 4
	successorObservation.Chain = chain
	successorObservation.History = history
	successorPlan, successorErr := PlanObservation(successorObservation)
	if successorErr != nil || !successorPlan.Qualifies || !successorPlan.AdvancesHead || successorPlan.OpensDispute {
		t.Fatalf("only the active replacement's own fresh confirmation may advance after the predecessor dispute is upheld: plan=%+v err=%v",
			successorPlan, successorErr)
	}
}

func TestPlanCorrectionOfRejectedSupersededAttemptToQualifyingOpensDispute(t *testing.T) {
	predecessor := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(predecessor.Preparation)
	rejection := retainedObservation(predecessor, 320, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := historyWith(chain, []AttemptSnapshot{predecessor}, rejection)
	recoveryPlan, err := PlanRejectedRecovery(RejectedRecoveryInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: predecessor, History: history,
		ResolvedRejection: observationReference(rejection), Reason: "Retry only after retaining the exact rejection",
	})
	if err != nil {
		t.Fatalf("the exact rejection should first permit explicit recovery: %v", err)
	}
	if recoveryPlan.SuccessorState != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatalf("rejected recovery should create a collecting successor: %+v", recoveryPlan)
	}

	terminal := cloneAttemptSnapshot(predecessor)
	terminal.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION
	terminal.Preparation.Version++
	terminal.Preparation.ContentHash = testHash(118)
	successor := attemptVariant(terminal, 321, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	successor.Preparation.Predecessor = preparationReference(terminal.Preparation)
	chain.RootPreparation = clonePreparation(terminal.Preparation)
	chain.Branch = &Branch{
		Reference:     sourceRef("bas_branch", testUUID(322), 1, 122),
		Version:       1,
		ActiveAttempt: clonePreparation(successor.Preparation),
	}
	history.Chain = cloneChainSnapshot(chain)
	history.Attempts[0] = cloneAttemptSnapshot(terminal)
	history.Attempts = append(history.Attempts, cloneAttemptSnapshot(successor))

	lateUse := retainedCorrection(terminal, 323, observationReference(rejection),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT), nil)
	correctionPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: terminal, History: history,
		Correction: lateUse.GetCorrection(),
	})
	if err != nil {
		t.Fatalf("a correction that reclassifies a rejected predecessor should be retained: %v", err)
	}
	if !correctionPlan.OpensDispute ||
		correctionPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
		t.Fatalf("reclassifying a rejected terminal assertion as used must reopen a dispute: %+v", correctionPlan)
	}
	chain.DisputeOverlay = correctionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 2, Observation: lateUse})
	history.LastOrdinal = 2
	upholdInput := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations: []*tammyv1.SourceRef{
				observationReference(rejection), observationReference(lateUse),
			},
			DependentChainManifestHash: dependencyRefsManifest(nil),
			Reason:                     "Review the corrected assertion on the rejected predecessor",
		},
	}
	if _, err := PlanResolveDispute(upholdInput); err != nil {
		t.Fatalf("the reclassified rejected assertion should derive a complete upholdable dispute: %v", err)
	}
}

func TestPlanResolveDisputeRetainsHistoricalQualifyingCorrectionAfterLaterRetraction(t *testing.T) {
	predecessor := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(predecessor.Preparation)
	pending := retainedObservation(predecessor, 330, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING)
	retraction := retainedCorrection(predecessor, 331, observationReference(pending),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
	history := historyWith(chain, []AttemptSnapshot{predecessor}, pending, retraction)
	if _, err := PlanCreateReplacement(ReplacementInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: predecessor, History: history,
		Reason: "Replace an unused attempt after retracting its pending report",
	}); err != nil {
		t.Fatalf("the retracted pending attempt should first be eligible for replacement: %v", err)
	}

	terminal := cloneAttemptSnapshot(predecessor)
	terminal.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	terminal.Preparation.Version++
	terminal.Preparation.ContentHash = testHash(130)
	successor := attemptVariant(terminal, 332, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	successor.Preparation.Predecessor = preparationReference(terminal.Preparation)
	chain.RootPreparation = clonePreparation(terminal.Preparation)
	chain.Branch = &Branch{
		Reference:     sourceRef("bas_branch", testUUID(333), 1, 133),
		Version:       1,
		ActiveAttempt: clonePreparation(successor.Preparation),
	}
	history.Chain = cloneChainSnapshot(chain)
	history.Attempts[0] = cloneAttemptSnapshot(terminal)
	history.Attempts = append(history.Attempts, cloneAttemptSnapshot(successor))

	qualifyingCorrection := retainedCorrection(terminal, 334, observationReference(pending),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT), nil)
	firstCorrectionPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: terminal, History: history,
		Correction: qualifyingCorrection.GetCorrection(),
	})
	if err != nil || !firstCorrectionPlan.OpensDispute {
		t.Fatalf("a terminal predecessor reclassified as used should open its first dispute: plan=%+v err=%v", firstCorrectionPlan, err)
	}
	chain.DisputeOverlay = firstCorrectionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 3, Observation: qualifyingCorrection})
	history.LastOrdinal = 3
	firstResolutionInput := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: cloneHistory(history),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations: []*tammyv1.SourceRef{
				observationReference(pending), observationReference(retraction), observationReference(qualifyingCorrection),
			},
			DependentChainManifestHash: dependencyRefsManifest(nil),
			Reason:                     "Uphold the replacement after reviewing the predecessor report",
		},
	}
	firstResolutionPlan, err := PlanResolveDispute(firstResolutionInput)
	if err != nil {
		t.Fatalf("the first terminal-predecessor dispute should be upholdable: %v", err)
	}
	firstResolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(335), Family: cloneRef(chain.Family.Reference),
		Resolution:  proto.Clone(firstResolutionInput.Resolution).(*tammyv1.BasExternalDisputeInput),
		ActorUserId: testUUID(336),
		ResolvedAt:  timestamppb.New(time.Date(2026, time.October, 15, 10, 0, 0, 0, time.UTC)),
		ContentHash: testHash(134),
	}
	chain.DisputeOverlay = firstResolutionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 4, Resolution: firstResolution})
	history.LastOrdinal = 4

	secondCorrection := retainedCorrection(terminal, 337, observationReference(qualifyingCorrection),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION,
		eventKindPointer(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING), nil)
	secondCorrectionPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: terminal, History: history,
		Correction: secondCorrection.GetCorrection(),
	})
	if err != nil || !secondCorrectionPlan.OpensDispute ||
		secondCorrectionPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
		t.Fatalf("correcting the resolved qualifying assertion back to pending should open a new dispute: plan=%+v err=%v",
			secondCorrectionPlan, err)
	}
	chain.DisputeOverlay = secondCorrectionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 5, Observation: secondCorrection})
	history.LastOrdinal = 5
	secondResolutionInput := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations: []*tammyv1.SourceRef{
				observationReference(qualifyingCorrection), observationReference(secondCorrection),
			},
			DependentChainManifestHash: dependencyRefsManifest(nil),
			Reason:                     "Resolve the later correction against the prior qualifying assertion",
		},
	}
	if _, err := PlanResolveDispute(secondResolutionInput); err != nil {
		t.Fatalf("the later dispute should retain the previously qualifying correction and be resolvable: %v", err)
	}
}

func TestPlanCreateReplacementRejectsDanglingTypedResolutionSubjects(t *testing.T) {
	attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	chain := initialChain(attempt.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD
	resolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(194), Family: cloneRef(chain.Family.Reference),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations:       []*tammyv1.SourceRef{sourceRef("bas_external_observation", testUUID(195), 1, 195)},
			DependentChainManifestHash: dependencyManifestHash(nil), Reason: "Malformed retained resolution fixture",
		},
		ActorUserId: testUUID(196), ResolvedAt: timestamppb.New(time.Date(2026, time.October, 12, 11, 0, 0, 0, time.UTC)), ContentHash: testHash(197),
	}
	history := historyWith(chain, []AttemptSnapshot{attempt})
	history.LastOrdinal = 1
	history.Records = []ExternalHistoryRecord{{Ordinal: 1, Resolution: resolution}}
	if _, err := PlanCreateReplacement(ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
		History: history, Reason: "Must validate complete typed resolution subjects"}); err == nil {
		t.Fatal("a retained resolution with a dangling observation reference cannot silently clear or bypass overlays")
	}

	wrongFamily := proto.Clone(resolution).(*tammyv1.BasExternalDisputeResolution)
	wrongFamily.Family = sourceRef("bas_family", testUUID(198), 1, 198)
	history.Records[0].Resolution = wrongFamily
	if _, err := PlanCreateReplacement(ReplacementInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: attempt,
		History: history, Reason: "Must bind retained resolution to this family"}); err == nil {
		t.Fatal("a retained resolution from another family cannot participate in this family history")
	}
}

func TestPlanResolveDisputePinsExactHeadDependenciesAndBothOutcomes(t *testing.T) {
	for _, outcome := range []tammyv1.BasDisputeResolutionOutcome{
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
	} {
		t.Run(outcome.String(), func(t *testing.T) {
			input := disputeResolutionInputFor(t, outcome)
			terminalHead := proto.Clone(input.Chain.LatestHead).(*tammyv1.BasPreparation)
			// The mutable aggregate may have newer overlay/export metadata. The family
			// head and dependent-chain manifest still identify retained confirmed refs.
			input.History.Attempts[0].Preparation.Version++
			input.History.Attempts[0].Preparation.ContentHash = testHash(201)
			input.History.Chain = cloneChainSnapshot(input.Chain)
			plan, err := PlanResolveDispute(input)
			if err != nil {
				t.Fatalf("resolution must use retained confirmed versions and exact derived dependencies: %v", err)
			}
			if !proto.Equal(input.Chain.LatestHead, terminalHead) ||
				input.Chain.LatestHead.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
				t.Fatal("resolving an overlay cannot promote, reopen or rewrite a terminal confirmed preparation")
			}
			if !sameRef(plan.ExpectedChain.LatestHead.GetRetained(), preparationReference(input.Chain.LatestHead)) ||
				!sameRefSlices(plan.AffectedConfirmedChain, input.Resolution.AffectedConfirmedChain) ||
				!sameRefSlices(plan.DisputedObservations, input.Resolution.DisputedObservations) {
				t.Fatalf("resolution intent must pin the exact retained head and dependency/observation set: %+v", plan)
			}
			switch outcome {
			case tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN:
				if plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD || plan.ReserveCorrectiveBranch ||
					!sameRef(plan.ExpectedChain.LatestHead.GetRetained(), preparationReference(input.Chain.LatestHead)) {
					t.Fatalf("UPHOLD clears only the overlay and keeps the terminal head: %+v", plan)
				}
			case tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION:
				if plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED || !plan.ReserveCorrectiveBranch ||
					!plan.ExpectedChain.Branch.GetAbsent() || !plan.ExpectedChain.ActiveAttempt.GetAbsent() {
					t.Fatalf("corrective resolution reserves one branch at the existing head with explicit absent attempt: %+v", plan)
				}
			}
		})
	}

	input := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN)
	input.Resolution.AffectedConfirmedChain = nil
	if _, err := PlanResolveDispute(input); err == nil {
		t.Fatal("resolution must name every affected confirmed dependent, not a caller-truncated subset")
	}

	stale := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN)
	stale.ExpectedChain.LatestHead = &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
	if _, err := PlanResolveDispute(stale); err == nil {
		t.Fatal("resolution must reject a stale caller expectation for the confirmed head")
	}
}

func TestPlanResolveDerivesEveryConfirmedDescendantFromTypedRetainedChain(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	ancestor := attemptVariant(base, 205, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant := attemptVariant(base, 206, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant.Preparation.Predecessor = preparationReference(ancestor.Preparation)
	chain := initialChain(ancestor.Preparation)
	chain.RootPreparation = clonePreparation(ancestor.Preparation)
	chain.EffectiveOriginal = clonePreparation(ancestor.Preparation)
	chain.LatestHead = clonePreparation(descendant.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	ancestorConfirmation := retainedObservation(ancestor, 207, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	ancestorRejection := retainedObservation(ancestor, 208, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	descendantConfirmation := retainedObservation(descendant, 209, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	history := historyWith(chain, []AttemptSnapshot{ancestor, descendant}, ancestorConfirmation, descendantConfirmation, ancestorRejection)
	input := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(ancestor), cloneAttemptSnapshot(descendant)},
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
			DisputedObservations:       []*tammyv1.SourceRef{observationReference(ancestorConfirmation), observationReference(ancestorRejection)},
			AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationReference(ancestor.Preparation), preparationReference(descendant.Preparation)},
			DependentChainManifestHash: dependencyManifestHash([]*tammyv1.SourceRef{preparationReference(ancestor.Preparation), preparationReference(descendant.Preparation)}),
			Reason:                     "Require a correction for the entire retained dependent chain",
		},
	}
	plan, err := PlanResolveDispute(input)
	if err != nil {
		t.Fatalf("resolution must derive its affected dependency refs from the complete retained confirmed chain: %v", err)
	}
	if len(plan.AffectedConfirmedChain) != 2 || !sameRef(plan.AffectedConfirmedChain[0], preparationReference(ancestor.Preparation)) ||
		!sameRef(plan.AffectedConfirmedChain[1], preparationReference(descendant.Preparation)) {
		t.Fatalf("every exact confirmed descendant must be included in order: %+v", plan.AffectedConfirmedChain)
	}

	missingDependency := input
	missingDependency.ConfirmedDependencies = []AttemptSnapshot{ancestor}
	if _, err := PlanResolveDispute(missingDependency); err == nil {
		t.Fatal("omitting an affected retained confirmed descendant must fail closed")
	}
	truncatedResolution := proto.Clone(input.Resolution).(*tammyv1.BasExternalDisputeInput)
	truncatedResolution.AffectedConfirmedChain = truncatedResolution.AffectedConfirmedChain[:1]
	missingDependency = input
	missingDependency.Resolution = truncatedResolution
	if _, err := PlanResolveDispute(missingDependency); err == nil {
		t.Fatal("a caller-supplied dependent-chain subset cannot replace policy-derived dependencies")
	}
	wrongManifest := proto.Clone(input.Resolution).(*tammyv1.BasExternalDisputeInput)
	wrongManifest.DependentChainManifestHash[0] ^= 0xff
	missingDependency = input
	missingDependency.Resolution = wrongManifest
	if _, err := PlanResolveDispute(missingDependency); err == nil {
		t.Fatal("resolution must bind the canonical ordered hash of the complete retained dependent chain")
	}
}

func TestPlanResolveCorrectiveAdoptsCompatibleBranchOrPinsFrozenBarrier(t *testing.T) {
	for _, state := range []tammyv1.BasPreparationState{
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING,
		tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY,
	} {
		t.Run(state.String(), func(t *testing.T) {
			input := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION)
			branchAttempt := preparationAt(210, state)
			branchAttempt.Family = cloneRef(input.Chain.Family.Reference)
			branchAttempt.Predecessor = preparationReference(input.Chain.LatestHead)
			branchAttempt.CurrentCalculation = sourceRef("bas_calculation", testUUID(211), 1, 211)
			branchAttempt.CurrentValidation = sourceRef("bas_validation", testUUID(212), 1, 212)
			branchAttempt.InputRevision = input.Chain.LatestHead.InputRevision
			branchAttempt.InputHash = bytes.Clone(input.Chain.LatestHead.InputHash)
			branchAttempt.SourceSnapshotHash = bytes.Clone(input.Chain.LatestHead.SourceSnapshotHash)
			branch := &Branch{Reference: sourceRef("bas_branch", testUUID(213), 1, 213), Version: 6,
				RootHead: clonePreparation(input.Chain.LatestHead), ActiveAttempt: branchAttempt}
			input.Chain.Branch = branch
			input.ExpectedChain = mustChainExpectation(t, input.Chain)
			input.History.Chain = cloneChainSnapshot(input.Chain)
			before := proto.Clone(branchAttempt).(*tammyv1.BasPreparation)
			plan, err := PlanResolveDispute(input)
			if err != nil {
				t.Fatalf("a branch rooted at the exact confirmed head can be adopted for correction: %v", err)
			}
			if !plan.AdoptedExistingBranch || !plan.ResetMutableAttemptToCollecting || !plan.ClearCurrentReviewPointers ||
				!sameRef(plan.ExpectedChain.Branch.GetRetained(), branch.Reference) || !sameRef(plan.ExpectedChain.ActiveAttempt.GetRetained(), preparationReference(branchAttempt)) {
				t.Fatalf("mutable branch adoption must preserve identity/CAS and force fresh review: %+v", plan)
			}
			if !proto.Equal(branchAttempt, before) || branchAttempt.InputRevision != before.InputRevision || !bytes.Equal(branchAttempt.InputHash, before.InputHash) {
				t.Fatal("adoption is an immutable intent; old financial identity and retained validation facts remain intact")
			}
		})
	}

	wrongRoot := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION)
	wrongRoot.Chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(214), 1, 214), Version: 2,
		RootHead:      preparationAt(215, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED),
		ActiveAttempt: preparationAt(216, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING)}
	wrongRoot.Chain.Branch.RootHead.Family = cloneRef(wrongRoot.Chain.Family.Reference)
	wrongRoot.Chain.Branch.ActiveAttempt.Family = cloneRef(wrongRoot.Chain.Family.Reference)
	wrongRoot.ExpectedChain = nil
	wrongRoot.History.Chain = cloneChainSnapshot(wrongRoot.Chain)
	if _, err := PlanResolveDispute(wrongRoot); err == nil {
		t.Fatal("a branch rooted at another head cannot be adopted; there is no implicit branch cancellation")
	}

	frozen := disputeResolutionInputFor(t, tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION)
	active := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING).Attempt
	active.Preparation.Family = cloneRef(frozen.Chain.Family.Reference)
	active.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF
	frozen.Chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(217), 1, 217), Version: 2,
		RootHead: clonePreparation(frozen.Chain.LatestHead), ActiveAttempt: clonePreparation(active.Preparation)}
	frozen.ExpectedChain = mustChainExpectation(t, frozen.Chain)
	frozen.History.Chain = cloneChainSnapshot(frozen.Chain)
	frozenPlan, err := PlanResolveDispute(frozen)
	if err != nil {
		t.Fatalf("a frozen pre-resolution attempt can be retained behind an exact corrective barrier: %v", err)
	}
	if !sameRef(frozenPlan.FrozenBarrier, active.FrozenReference) || !frozenPlan.PreserveFrozenAttempt {
		t.Fatalf("frozen labels stay immutable and the exact attempt cannot qualify as corrective: %+v", frozenPlan)
	}

	barrierChain := cloneChainSnapshot(frozen.Chain)
	barrierChain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED
	barrierChain.Branch.Resolution = sourceRef("bas_external_dispute_resolution", testUUID(218), 1, 218)
	barrierChain.Branch.FrozenBarrier = cloneRef(active.FrozenReference)
	barrierInput := ObservationInput{Chain: barrierChain, Attempt: active, History: cloneHistory(frozen.History),
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_OTHER, ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)), Reason: "Pre-resolution frozen attempt cannot qualify as the corrective revision"}}
	barrierInput.History.Chain = cloneChainSnapshot(barrierChain)
	barrierInput.History.Attempts = append(barrierInput.History.Attempts, cloneAttemptSnapshot(active))
	barrierPlan, barrierErr := PlanObservation(barrierInput)
	if barrierErr == nil && barrierPlan.AdvancesHead {
		t.Fatal("an exact pre-resolution frozen barrier may not qualify or advance a corrective branch")
	}
}

func TestPlanObservationCannotAdvanceAnUnrelatedAttemptWhileCorrectionIsReserved(t *testing.T) {
	input, resolutionRef := correctiveObservationInput(t)
	input.Chain.Branch.ActiveAttempt = nil
	input.Chain.Branch.Resolution = cloneRef(resolutionRef)
	input.History.Chain = cloneChainSnapshot(input.Chain)
	input.Attempt = cloneAttemptSnapshot(input.History.Attempts[0])
	input.Observation = &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
		Channel:        tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_OTHER,
		ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 13, 10, 0, 0, 0, time.UTC)), Reason: "Unrelated attempt cannot consume the corrective reservation"}
	if _, err := PlanObservation(input); err == nil {
		t.Fatal("an absent first corrective attempt reserves the branch and blocks unrelated advancement")
	}
}

func TestPlanObservationCorrectiveSuccessLinksExactResolutionAndClearsReservation(t *testing.T) {
	input, resolutionRef := correctiveObservationInput(t)
	plan, err := PlanObservation(input)
	if err != nil {
		t.Fatalf("the active successor frozen after corrective resolution may confirm: %v", err)
	}
	if !plan.Qualifies || !plan.AdvancesHead || !sameRef(plan.CompletedResolution, resolutionRef) ||
		plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE || plan.ReserveCorrectiveBranch ||
		!containsRef(plan.AffectedConfirmedChain, preparationReference(input.Chain.LatestHead)) {
		t.Fatalf("corrective success advances with the exact resolution link, affected chain and cleared reservation/overlay: %+v", plan)
	}
}

func TestPlanObservationCompletesCorrectiveBranchAfterNewerDisputeIsUpheld(t *testing.T) {
	corrective := disputeResolutionInputFor(t,
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION)
	initialResolutionPlan, err := PlanResolveDispute(corrective)
	if err != nil {
		t.Fatalf("the initial dispute should reserve a corrective branch: %v", err)
	}
	if initialResolutionPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED ||
		!initialResolutionPlan.ReserveCorrectiveBranch {
		t.Fatalf("the initial resolution must reserve the branch: %+v", initialResolutionPlan)
	}

	head := cloneAttemptSnapshot(corrective.ConfirmedDependencies[0])
	active := attemptVariant(head, 293, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	active.Preparation.Predecessor = preparationReference(head.Preparation)
	chain := cloneChainSnapshot(corrective.Chain)
	firstResolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(290), Family: cloneRef(chain.Family.Reference),
		Resolution:  proto.Clone(corrective.Resolution).(*tammyv1.BasExternalDisputeInput),
		ActorUserId: testUUID(291),
		ResolvedAt:  timestamppb.New(time.Date(2026, time.October, 12, 11, 15, 0, 0, time.UTC)),
		ContentHash: testHash(192),
	}
	firstResolutionRef := sourceRef("bas_external_dispute_resolution", firstResolution.ResolutionId, 1, firstResolution.ContentHash[0])
	chain.DisputeOverlay = initialResolutionPlan.NextOverlay
	chain.Branch = &Branch{
		Reference:     sourceRef("bas_branch", testUUID(294), 1, 94),
		Version:       1,
		RootHead:      clonePreparation(head.Preparation),
		ActiveAttempt: clonePreparation(active.Preparation),
		Resolution:    cloneRef(firstResolutionRef),
	}
	history := cloneHistory(corrective.History)
	history.Chain = cloneChainSnapshot(chain)
	history.Attempts = append(history.Attempts, cloneAttemptSnapshot(active))
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 3, Resolution: firstResolution})
	history.LastOrdinal = 3

	// A later qualifying assertion on the retained head is retracted, opening a
	// second dispute while the original corrective branch remains reserved.
	laterConfirmation := retainedObservation(head, 295, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 4, Observation: laterConfirmation})
	history.LastOrdinal = 4
	correctionRecord := retainedCorrection(head, 296, observationReference(laterConfirmation),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
	correctionPlan, err := PlanCorrection(CorrectionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: head, History: history,
		Correction: correctionRecord.GetCorrection(),
	})
	if err != nil {
		t.Fatalf("retracting the later assertion should open a new dispute: %v", err)
	}
	if !correctionPlan.OpensDispute ||
		correctionPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED {
		t.Fatalf("the later retraction must dispute the retained head: %+v", correctionPlan)
	}
	chain.DisputeOverlay = correctionPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 5, Observation: correctionRecord})
	history.LastOrdinal = 5

	originalConfirmation := corrective.History.Records[0].Observation
	upholdInput := DisputeResolutionInput{
		Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: cloneHistory(history),
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(head)},
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
			DisputedObservations: []*tammyv1.SourceRef{
				observationReference(originalConfirmation), observationReference(laterConfirmation), observationReference(correctionRecord),
			},
			AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationReference(head.Preparation)},
			DependentChainManifestHash: dependencyRefsManifest([]*tammyv1.SourceRef{preparationReference(head.Preparation)}),
			Reason:                     "Uphold the retained head while preserving the corrective workflow",
		},
	}
	upholdPlan, err := PlanResolveDispute(upholdInput)
	if err != nil {
		t.Fatalf("the newer dispute should be independently upheld: %v", err)
	}
	if upholdPlan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD ||
		upholdPlan.ReserveCorrectiveBranch || !sameRef(chain.Branch.Resolution, firstResolutionRef) ||
		!samePreparationAggregate(chain.Branch.ActiveAttempt, active.Preparation) {
		t.Fatalf("UPHOLD should clear the newer overlay and preserve the original corrective branch: %+v", upholdPlan)
	}

	secondResolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(297), Family: cloneRef(chain.Family.Reference),
		Resolution:  proto.Clone(upholdInput.Resolution).(*tammyv1.BasExternalDisputeInput),
		ActorUserId: testUUID(298),
		ResolvedAt:  timestamppb.New(time.Date(2026, time.October, 13, 11, 15, 0, 0, time.UTC)),
		ContentHash: testHash(199),
	}
	chain.DisputeOverlay = upholdPlan.NextOverlay
	history.Chain = cloneChainSnapshot(chain)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 6, Resolution: secondResolution})
	history.LastOrdinal = 6
	input := ObservationInput{
		Chain: chain, Attempt: active, History: history,
		Observation: &tammyv1.BasExternalObservationInput{
			EventKind:      tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel:        tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT,
			ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 14, 9, 0, 0, 0, time.UTC)),
			Reason:         "Fresh confirmation for the active corrective attempt",
		},
	}
	plan, err := PlanObservation(input)
	if err != nil {
		t.Fatalf("the active corrective attempt should confirm after the newer dispute is upheld: %v", err)
	}
	if !plan.Qualifies || !plan.AdvancesHead || !sameRef(plan.CompletedResolution, firstResolutionRef) ||
		!sameRefSlices(plan.AffectedConfirmedChain, []*tammyv1.SourceRef{preparationReference(head.Preparation)}) ||
		plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE || plan.ReserveCorrectiveBranch {
		t.Fatalf("corrective success must complete R1 and clear its reservation after the newer dispute was upheld: %+v", plan)
	}
}

func TestPlanResolveDisputeRejectsTruncatedConfirmedAncestorChain(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	ancestor := attemptVariant(base, 260, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant := attemptVariant(base, 261, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant.Preparation.Predecessor = preparationReference(ancestor.Preparation)
	chain := initialChain(ancestor.Preparation)
	chain.RootPreparation = clonePreparation(ancestor.Preparation)
	chain.EffectiveOriginal = clonePreparation(ancestor.Preparation)
	chain.LatestHead = clonePreparation(descendant.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED

	ancestorConfirmation := retainedObservation(ancestor, 262, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	ancestorRejection := retainedObservation(ancestor, 263, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	descendantConfirmation := retainedObservation(descendant, 264, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	descendantRejection := retainedObservation(descendant, 274, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := historyWith(chain, []AttemptSnapshot{ancestor, descendant}, ancestorConfirmation, ancestorRejection, descendantConfirmation)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 4, Observation: descendantRejection})
	history.LastOrdinal = 4
	fullResolution := &tammyv1.BasExternalDisputeInput{
		Outcome: tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
		DisputedObservations: []*tammyv1.SourceRef{observationReference(ancestorConfirmation), observationReference(ancestorRejection),
			observationReference(descendantConfirmation), observationReference(descendantRejection)},
		AffectedConfirmedChain: []*tammyv1.SourceRef{preparationReference(ancestor.Preparation), preparationReference(descendant.Preparation)},
		DependentChainManifestHash: dependencyManifestHash([]*tammyv1.SourceRef{
			preparationReference(ancestor.Preparation), preparationReference(descendant.Preparation),
		}),
		Reason: "Resolve the contradiction against the complete confirmed dependent chain",
	}
	fullInput := DisputeResolutionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: cloneHistory(history),
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(ancestor), cloneAttemptSnapshot(descendant)}, Resolution: fullResolution}
	if _, err := PlanResolveDispute(fullInput); err != nil {
		t.Fatalf("the complete retained confirmed chain should resolve before testing truncation: %v", err)
	}

	// Model a repository that omitted the ancestor from both independently loaded
	// confirmed-version views while still returning it as a current attempt. A
	// predecessor pointer into that omitted history must make the snapshot fail
	// closed. Even a request and manifest consistently truncated to the descendant
	// cannot repair the omitted confirmed ancestor.
	history.ConfirmedDependencies = []AttemptSnapshot{cloneAttemptSnapshot(descendant)}
	truncatedResolution := proto.Clone(fullResolution).(*tammyv1.BasExternalDisputeInput)
	truncatedResolution.DisputedObservations = []*tammyv1.SourceRef{
		observationReference(descendantConfirmation), observationReference(descendantRejection),
	}
	truncatedResolution.AffectedConfirmedChain = []*tammyv1.SourceRef{preparationReference(descendant.Preparation)}
	truncatedResolution.DependentChainManifestHash = dependencyManifestHash(truncatedResolution.AffectedConfirmedChain)
	input := DisputeResolutionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(descendant)}, Resolution: truncatedResolution}
	if _, err := PlanResolveDispute(input); err == nil {
		t.Fatal("a confirmed chain with a retained predecessor omitted from its exact dependency view must fail closed")
	}
}

func TestPlanObservationKeepsPriorResolutionValidAfterCorrectiveConfirmation(t *testing.T) {
	input, _ := correctiveObservationInput(t)
	correctivePlan, err := PlanObservation(input)
	if err != nil || !correctivePlan.AdvancesHead {
		t.Fatalf("the active corrective attempt should first qualify against the retained resolution: plan=%+v err=%v", correctivePlan, err)
	}

	// Model the transaction's retained result. The resolution's exact dependency
	// manifest remains the chain as it existed at its append ordinal; the newly
	// confirmed corrective descendant must not retroactively invalidate it.
	confirmed := cloneAttemptSnapshot(input.Attempt)
	confirmed.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	confirmed.Preparation.Version++
	confirmed.Preparation.ContentHash = testHash(247)
	chain := cloneChainSnapshot(input.Chain)
	chain.Family.Version++
	chain.LatestHead = clonePreparation(confirmed.Preparation)
	chain.Branch = nil
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE
	history := cloneHistory(input.History)
	history.Chain = cloneChainSnapshot(chain)
	history.Attempts[1] = cloneAttemptSnapshot(confirmed)
	history.ConfirmedDependencies = append(history.ConfirmedDependencies, cloneAttemptSnapshot(confirmed))
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 4,
		Observation: retainedObservation(confirmed, 248, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE)})
	history.LastOrdinal = 4

	duplicate := ObservationInput{Chain: chain, Attempt: confirmed, History: history,
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel:        tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_OTHER,
			ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 13, 10, 0, 0, 0, time.UTC)),
			Reason:         "A duplicate report after corrective confirmation is append-only"}}
	plan, err := PlanObservation(duplicate)
	if err != nil {
		t.Fatalf("a historical resolution must remain valid after its corrective descendant is confirmed: %v", err)
	}
	if !plan.Qualifies || plan.AdvancesHead || plan.OpensDispute {
		t.Fatalf("a duplicate report for the current confirmed corrective head only appends history: %+v", plan)
	}
}

func TestPlanCorrectionReturnsAffectedConfirmedDescendantsAndDisputeOverlay(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	ancestor := attemptVariant(base, 265, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant := attemptVariant(base, 266, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED)
	descendant.Preparation.Predecessor = preparationReference(ancestor.Preparation)
	chain := initialChain(ancestor.Preparation)
	chain.RootPreparation = clonePreparation(ancestor.Preparation)
	chain.EffectiveOriginal = clonePreparation(ancestor.Preparation)
	chain.LatestHead = clonePreparation(descendant.Preparation)
	ancestorConfirmation := retainedObservation(ancestor, 267, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	descendantConfirmation := retainedObservation(descendant, 268, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	history := historyWith(chain, []AttemptSnapshot{ancestor, descendant}, ancestorConfirmation, descendantConfirmation)
	correction := retainedCorrection(ancestor, 269, observationReference(ancestorConfirmation),
		tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
	plan, err := PlanCorrection(CorrectionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), Attempt: ancestor,
		History: history, Correction: correction.GetCorrection()})
	if err != nil {
		t.Fatalf("a typed correction that retracts a confirmed ancestor should be planned as a dispute: %v", err)
	}
	wantAffected := []*tammyv1.SourceRef{preparationReference(ancestor.Preparation), preparationReference(descendant.Preparation)}
	if !plan.OpensDispute || plan.NextOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED ||
		!sameRefSlices(plan.AffectedConfirmedChain, wantAffected) {
		t.Fatalf("ancestor correction must return the exact affected descendant chain and disputed overlay: %+v", plan)
	}
}

func TestPlanObservationRequiresExactActiveAttemptPredecessorChain(t *testing.T) {
	makeInput := func(skipRetainedPredecessor bool) ObservationInput {
		head := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
		head.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
		retainedUnused := attemptVariant(head, 270, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION)
		retainedUnused.Preparation.Predecessor = preparationReference(head.Preparation)
		active := attemptVariant(retainedUnused, 271, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
		active.Preparation.Predecessor = preparationReference(retainedUnused.Preparation)
		if skipRetainedPredecessor {
			active.Preparation.Predecessor = preparationReference(head.Preparation)
		}
		chain := initialChain(head.Preparation)
		chain.RootPreparation = clonePreparation(head.Preparation)
		chain.EffectiveOriginal = clonePreparation(head.Preparation)
		chain.LatestHead = clonePreparation(head.Preparation)
		chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(272), 1, 72), Version: 2,
			RootHead: clonePreparation(head.Preparation), ActiveAttempt: clonePreparation(active.Preparation)}
		history := historyWith(chain, []AttemptSnapshot{head, retainedUnused, active})
		return ObservationInput{Chain: chain, Attempt: active, History: history,
			Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
				Channel:        tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_OTHER,
				ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 13, 11, 0, 0, 0, time.UTC)),
				Reason:         "The branch active attempt must follow its exact retained predecessor"}}
	}

	valid, err := PlanObservation(makeInput(false))
	if err != nil || !valid.AdvancesHead {
		t.Fatalf("a frozen active attempt linked to its exact prior branch attempt should advance: plan=%+v err=%v", valid, err)
	}
	if _, err := PlanObservation(makeInput(true)); err == nil {
		t.Fatal("an active branch attempt that skips a retained predecessor must fail closed")
	}
}

func TestChainKeepsConfirmedReplacementAsEffectiveOriginalSeparateFromRoot(t *testing.T) {
	base := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	root := clonePreparation(base.Preparation)
	root.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION
	replacement := attemptVariant(base, 273, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED).Preparation
	replacement.Predecessor = preparationReference(root)
	chain := initialChain(root)
	chain.RootPreparation = clonePreparation(root)
	chain.EffectiveOriginal = clonePreparation(replacement)
	chain.LatestHead = clonePreparation(replacement)
	expectation, err := BuildChainExpectation(chain)
	if err != nil {
		t.Fatalf("the confirmed replacement can become the effective original while the immutable root remains retained: %v", err)
	}
	if expectation.RootPreparation.Id != root.PreparationId || !sameRef(expectation.LatestHead.GetRetained(), preparationReference(replacement)) ||
		root.PreparationId == replacement.PreparationId {
		t.Fatalf("family root and confirmed effective original must remain distinct exact identities: %+v", expectation)
	}
}

func readyReviewInput() ReviewInput {
	preparation := preparationAt(1, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY)
	preparation.Version = 5
	preparation.ContentHash = testHash(10)
	preparation.InputRevision = 7
	preparation.InputHash = testHash(11)
	preparation.SourceSnapshotHash = testHash(12)
	preparation.CurrentCalculation = sourceRef("bas_calculation", testUUID(2), 1, 20)
	preparation.CurrentValidation = sourceRef("bas_validation", testUUID(3), 1, 30)
	preparation.CreatedAt = timestamppb.New(time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC))
	preparation.UpdatedAt = timestamppb.New(time.Date(2026, time.October, 11, 8, 0, 0, 0, time.UTC))

	calculation := &tammyv1.BasCalculation{
		CalculationId: testUUID(2), OrganisationId: preparation.OrganisationId, Version: 1, ContentHash: testHash(20),
		// This points to the retained preparation version captured before workflow metadata changed.
		Preparation: sourceRef("bas_preparation", preparation.PreparationId, 1, 9), Family: proto.Clone(preparation.Family).(*tammyv1.SourceRef),
		Period: proto.Clone(preparation.Period).(*tammyv1.SourceRef), Profile: proto.Clone(preparation.Profile).(*tammyv1.SourceRef), SourceSnapshotHash: testHash(12),
		CentWorkpaper:        sourceRef("bas_workpaper", testUUID(22), 1, 22),
		WorkpaperSnapshot:    workpaperSnapshot(preparation.OrganisationId),
		CoverageManifestHash: testHash(23), RevisionVectorHash: testHash(24),
		RuleVersion: "test-rules-v1", RuleHash: testHash(25), CalculatorVersion: "test-calculator-v1", CalculatorHash: testHash(26),
		FormVersion: "test-form-v1", FormHash: testHash(27), SchemaVersion: "test-schema-v1", SchemaHash: testHash(28),
		DescriptorVersion: "test-descriptor-v1", DescriptorHash: testHash(29),
		Result:        &tammyv1.BasCalculationResult{Outcome: tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE, Label_9Direction: tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_NIL},
		InputRevision: preparation.InputRevision, InputHash: testHash(11),
		RevisionVector:  &tammyv1.BasSourceRevisionVector{FinancialRevision: 1, LedgerRevision: 2, SettlementRevision: 3, BankingRevision: 4, TaxSourceRevision: 5, OrganisationProfileRevision: 6, RuleBundleRevision: 7, PaygRevision: 8, BasProfileRevision: 9, PeriodRevision: 10},
		PeriodSnapshot:  confirmedPeriod(preparation.Period.Id, preparation.OrganisationId, preparation.Period.Revision, preparation.Period.ContentHash[0]),
		ProfileSnapshot: obligationsProfile(preparation.OrganisationId), PaygwInputs: paygwInputs(), PaygiInputs: paygiInputs(),
		OrganisationSnapshot: organisationSnapshot(preparation.OrganisationId), OrganisationIdentity: sourceRef("organisation", preparation.OrganisationId, 1, 31),
		OrganisationSnapshotHash: testHash(32), CreatedAt: timestamppb.New(time.Date(2026, time.October, 11, 8, 30, 0, 0, time.UTC)),
	}
	validation := &tammyv1.BasValidationSnapshot{
		ValidationId: testUUID(3), Revision: 1, ContentHash: testHash(30), PreparationId: preparation.PreparationId,
		PreparationHash: testHash(9), CalculationId: calculation.CalculationId, CalculationHash: testHash(20), SourceSnapshotHash: testHash(12),
		InputRevision: preparation.InputRevision, InputHash: testHash(11), CreatedAt: timestamppb.New(time.Date(2026, time.October, 11, 8, 45, 0, 0, time.UTC)),
	}
	return ReviewInput{Preparation: preparation, Calculation: calculation, Validation: validation,
		CurrentCapture: CurrentCapture{SourceSnapshotHash: testHash(12), InputRevision: preparation.InputRevision, InputHash: testHash(11),
			RevisionVector: proto.Clone(calculation.RevisionVector).(*tammyv1.BasSourceRevisionVector)}}
}

func readyFreezeInput(review ReviewInput, chain ChainSnapshot) FreezeInput {
	return FreezeInput{Review: cloneReviewInput(review), Chain: cloneChainSnapshot(chain)}
}

func observationInputFor(kind tammyv1.BasExternalEventKind) ObservationInput {
	preparation := preparationAt(1, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	preparation.FrozenBas = sourceRef("bas_frozen", testUUID(8), 1, 80)
	preparation.CurrentCalculation = sourceRef("bas_calculation", testUUID(9), 1, 90)
	preparation.CurrentValidation = sourceRef("bas_validation", testUUID(10), 1, 100)
	chain := initialChain(preparation)
	calculationSnapshot := &tammyv1.BasCalculation{
		CalculationId: testUUID(9), OrganisationId: preparation.OrganisationId, Version: 1, ContentHash: testHash(90),
		Preparation: sourceRef("bas_preparation", preparation.PreparationId, preparation.Version, preparation.ContentHash[0]),
		Family:      proto.Clone(preparation.Family).(*tammyv1.SourceRef), Period: proto.Clone(preparation.Period).(*tammyv1.SourceRef), Profile: proto.Clone(preparation.Profile).(*tammyv1.SourceRef),
		SourceSnapshotHash: testHash(60),
		CentWorkpaper:      sourceRef("bas_workpaper", testUUID(22), 1, 22), WorkpaperSnapshot: workpaperSnapshot(preparation.OrganisationId),
		CoverageManifestHash: testHash(23), RevisionVectorHash: testHash(24),
		RevisionVector: &tammyv1.BasSourceRevisionVector{FinancialRevision: 1, LedgerRevision: 2, SettlementRevision: 3, BankingRevision: 4, TaxSourceRevision: 5, OrganisationProfileRevision: 6, RuleBundleRevision: 7, PaygRevision: 8, BasProfileRevision: 9, PeriodRevision: 10},
		RuleVersion:    "test-rules-v1", RuleHash: testHash(25),
		CalculatorVersion: "test-calculator-v1", CalculatorHash: testHash(26), FormVersion: "test-form-v1", FormHash: testHash(27),
		SchemaVersion: "test-schema-v1", SchemaHash: testHash(28), DescriptorVersion: "test-descriptor-v1", DescriptorHash: testHash(29),
		Result:        &tammyv1.BasCalculationResult{Outcome: tammyv1.BasCalculationOutcome_BAS_CALCULATION_OUTCOME_COMPLETE, Label_9Direction: tammyv1.BasAmountDirection_BAS_AMOUNT_DIRECTION_NIL},
		InputRevision: preparation.InputRevision, InputHash: bytes.Clone(preparation.InputHash),
		PeriodSnapshot:  confirmedPeriod(preparation.Period.Id, preparation.OrganisationId, preparation.Period.Revision, preparation.Period.ContentHash[0]),
		ProfileSnapshot: obligationsProfile(preparation.OrganisationId), PaygwInputs: paygwInputs(), PaygiInputs: paygiInputs(),
		OrganisationSnapshot: organisationSnapshot(preparation.OrganisationId), OrganisationIdentity: sourceRef("organisation", preparation.OrganisationId, 1, 31),
		OrganisationSnapshotHash: testHash(32), CreatedAt: timestamppb.New(time.Date(2026, time.October, 11, 8, 30, 0, 0, time.UTC)),
	}
	attempt := AttemptSnapshot{
		Preparation:     proto.Clone(preparation).(*tammyv1.BasPreparation),
		FrozenReference: proto.Clone(preparation.FrozenBas).(*tammyv1.SourceRef),
		Frozen: &tammyv1.BasFrozenBas{FrozenId: testUUID(8), OrganisationId: preparation.OrganisationId, ContentHash: testHash(80),
			Preparation: sourceRef("bas_preparation", preparation.PreparationId, 1, preparation.ContentHash[0]),
			Family:      proto.Clone(preparation.Family).(*tammyv1.SourceRef), Period: proto.Clone(preparation.Period).(*tammyv1.SourceRef),
			Profile: proto.Clone(preparation.Profile).(*tammyv1.SourceRef), Calculation: proto.Clone(preparation.CurrentCalculation).(*tammyv1.SourceRef),
			Validation:          proto.Clone(preparation.CurrentValidation).(*tammyv1.SourceRef),
			CalculationSnapshot: calculationSnapshot, Acknowledgements: nil,
			ApprovedSnapshotHash: testHash(33), ApprovalActorUserId: testUUID(34), FreshFactorAssertionId: testUUID(35),
			ApprovedAt: timestamppb.New(time.Date(2026, time.October, 11, 8, 55, 0, 0, time.UTC)), ApprovalReason: "Retained test approval artifact",
		},
	}
	return ObservationInput{
		Chain:   chain,
		Attempt: attempt,
		Observation: &tammyv1.BasExternalObservationInput{EventKind: kind, Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT,
			ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)), Reason: "User-recorded external event"},
		History: historyWith(chain, []AttemptSnapshot{attempt}),
	}
}

func finding(severity tammyv1.BasFindingSeverity, code string) *tammyv1.BasValidationFinding {
	return &tammyv1.BasValidationFinding{FindingId: testUUID(uint64(len(code) + 20)), Severity: severity, StableCode: code, SafeMessage: "Review required",
		Sources: []*tammyv1.SourceRef{sourceRef("bas_source", testUUID(uint64(len(code)+40)), 1, byte(len(code)+40))}}
}

func acknowledgement(validation *tammyv1.BasValidationSnapshot, finding *tammyv1.BasValidationFinding) *tammyv1.BasWarningAcknowledgement {
	return &tammyv1.BasWarningAcknowledgement{
		AcknowledgementId:  testUUID(uint64(len(finding.StableCode) + 60)),
		Validation:         sourceRef("bas_validation", validation.ValidationId, validation.Revision, 30),
		Finding:            &tammyv1.SourceRef{Type: "bas_validation_finding", Id: finding.FindingId, Revision: validation.Revision, ContentHash: findingHash(finding)},
		SourceSnapshotHash: bytes.Clone(validation.SourceSnapshotHash), ActorUserId: testUUID(70),
		AcknowledgedAt: timestamppb.New(time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)),
	}
}

func initialChain(preparation *tammyv1.BasPreparation) ChainSnapshot {
	root := proto.Clone(preparation).(*tammyv1.BasPreparation)
	var effectiveOriginal *tammyv1.BasPreparation
	if preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
		effectiveOriginal = clonePreparation(root)
	}
	period := confirmedPeriod(preparation.Period.Id, preparation.OrganisationId, preparation.Period.Revision, preparation.Period.ContentHash[0])
	scope := FamilyScope{OrganisationID: period.OrganisationId, Start: proto.Clone(period.Confirmation.Period.StartDate).(*tammyv1.CivilDate),
		End: proto.Clone(period.Confirmation.Period.EndDate).(*tammyv1.CivilDate), Window: period.Confirmation.Window}
	return ChainSnapshot{Family: &Family{Reference: sourceRef("bas_family", testUUID(5), 1, 50), Version: 3,
		Scope: scope},
		RootPreparation: root, EffectiveOriginal: effectiveOriginal, DisputeOverlay: tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE,
		PeriodReference: proto.Clone(preparation.Period).(*tammyv1.SourceRef)}
}

func confirmedPeriod(id, organisationID string, version uint64, hashSeed byte) *tammyv1.BasConfirmedPeriod {
	return &tammyv1.BasConfirmedPeriod{
		PeriodId: id, OrganisationId: organisationID, Version: version, ContentHash: testHash(hashSeed),
		Confirmation: &tammyv1.BasPeriodConfirmationInput{
			Period:         &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)},
			Window:         tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP,
			Profile:        sourceRef("bas_obligations_profile", testUUID(7), 1, 70),
			NominalDueDate: testCivilDate(2026, 10, 28), DueDateRuleVersion: "test-rules-v1", DueDateRuleHash: testHash(71),
			ConfirmedDueDate: testCivilDate(2026, 10, 28), DueDateKind: tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION,
			SupportState: tammyv1.BasPeriodSupportState_BAS_PERIOD_SUPPORT_STATE_SUPPORTED, Reason: "Confirmed test period",
		},
		ActorUserId: testUUID(72), ConfirmedAt: timestamppb.New(time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)),
	}
}

func workpaperSnapshot(organisationID string) *tammyv1.BasWorkpaper {
	money := func() *tammyv1.Money { return &tammyv1.Money{CurrencyCode: "AUD", MinorUnits: 0} }
	return &tammyv1.BasWorkpaper{
		Id: testUUID(22), OrganisationId: organisationID, Version: 1,
		PeriodStart: testCivilDate(2026, 7, 1), PeriodEnd: testCivilDate(2026, 9, 30),
		Status:  tammyv1.BasWorkpaperStatus_BAS_WORKPAPER_STATUS_DRAFT_NOT_LODGED,
		SalesG1: money(), GstOnSales_1A: money(), GstCredits_1B: money(), NetGstPayable: money(),
		CreatedAt: timestamppb.New(time.Date(2026, time.October, 11, 8, 0, 0, 0, time.UTC)),
		GstBasis:  tammyv1.GstBasis_GST_BASIS_CASH,
	}
}

func obligationsProfile(organisationID string) *tammyv1.BasObligationsProfile {
	unknownAnswer := func() *tammyv1.BasObligationAnswer {
		return &tammyv1.BasObligationAnswer{Applicability: tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN}
	}
	return &tammyv1.BasObligationsProfile{
		ProfileId: testUUID(7), OrganisationId: organisationID, Revision: 1, ContentHash: testHash(70),
		Profile: &tammyv1.BasObligationsProfileInput{
			EffectiveInterval: &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 1, 1)},
			Gst: &tammyv1.BasGstProfileInput{
				Applicability:      tammyv1.BasApplicability_BAS_APPLICABILITY_UNKNOWN,
				RegistrationStatus: tammyv1.BasRegistrationStatus_BAS_REGISTRATION_STATUS_UNKNOWN,
				ReportingMethod:    tammyv1.BasGstMethod_BAS_GST_METHOD_UNKNOWN,
				TurnoverCategory:   tammyv1.BasTurnoverCategory_BAS_TURNOVER_CATEGORY_UNKNOWN,
				GstBasis:           tammyv1.BasGstBasis_BAS_GST_BASIS_UNKNOWN, Cadence: tammyv1.BasCadence_BAS_CADENCE_UNKNOWN,
			},
			Paygw: &tammyv1.BasPaygwProfileInput{
				Applicability: unknownAnswer(), Category: tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNKNOWN,
				Cadence: tammyv1.BasCadence_BAS_CADENCE_UNKNOWN, PrefillRequirement: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNKNOWN,
			},
			Paygi: &tammyv1.BasPaygiProfileInput{
				Applicability: unknownAnswer(), Method: tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_UNKNOWN,
				Cadence: tammyv1.BasCadence_BAS_CADENCE_UNKNOWN,
			},
			OtherObligations: &tammyv1.BasOtherObligationInventory{
				FringeBenefitsTax: unknownAnswer(), WineEqualisationTax: unknownAnswer(), LuxuryCarTax: unknownAnswer(),
				FuelTaxCredits: unknownAnswer(), DeferredCompanyInstalments: unknownAnswer(), DeferredOrImportGst: unknownAnswer(),
				SpecialGstSchemesOrAdjustments: unknownAnswer(), OtherActivityStatementObligations: unknownAnswer(),
			},
			Reason: "Complete test profile fixture",
		},
		ActorUserId: testUUID(36), RecordedAt: timestamppb.New(time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)),
	}
}

func paygwInputs() *tammyv1.BasPaygwInputs {
	amount := func() *tammyv1.BasAmountInput {
		return &tammyv1.BasAmountInput{State: tammyv1.BasAmountState_BAS_AMOUNT_STATE_UNKNOWN}
	}
	return &tammyv1.BasPaygwInputs{
		Category:        tammyv1.BasPaygwCategory_BAS_PAYGW_CATEGORY_UNKNOWN,
		W1TotalPayments: amount(), W2AmountWithheld: amount(), W3OtherWithholding: amount(), W4NoAbnWithholding: amount(),
		PrefillReview: tammyv1.BasPrefillRequirement_BAS_PREFILL_REQUIREMENT_UNKNOWN,
	}
}

func paygiInputs() *tammyv1.BasPaygiInputs {
	return &tammyv1.BasPaygiInputs{
		Method:                tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_NOT_APPLICABLE,
		NoticeOperativeStatus: tammyv1.BasNoticeOperativeStatus_BAS_NOTICE_OPERATIVE_STATUS_UNKNOWN,
		PriorVariation:        tammyv1.BasPriorVariationDisposition_BAS_PRIOR_VARIATION_DISPOSITION_UNKNOWN,
		UnsupportedState:      tammyv1.BasPaygiUnsupportedState_BAS_PAYGI_UNSUPPORTED_STATE_NONE,
		T1ReviewDecision:      tammyv1.BasT1ReviewDecision_BAS_T1_REVIEW_DECISION_NOT_REVIEWED,
	}
}

func organisationSnapshot(organisationID string) *tammyv1.Organisation {
	return &tammyv1.Organisation{
		Id: organisationID, Version: 1, Abn: "51824753556", LegalName: "Test Entity Pty Ltd", DisplayName: "Test Entity",
		EntityType: "company", GstBasis: tammyv1.GstBasis_GST_BASIS_CASH,
		GstReportingFrequency: tammyv1.GstReportingFrequency_GST_REPORTING_FREQUENCY_QUARTERLY,
		FinancialYearEndMonth: 6, VerificationState: tammyv1.OrganisationVerificationState_ORGANISATION_VERIFICATION_STATE_VERIFIED,
		OwnerUserId: testUUID(37), ActiveTaxRuleBundle: sourceRef("tax_rule_bundle", testUUID(38), 1, 38),
	}
}

// Acknowledgements bind the exact canonical protobuf bytes and the validation revision.
func findingHash(finding *tammyv1.BasValidationFinding) []byte {
	canonical, err := (proto.MarshalOptions{Deterministic: true}).Marshal(finding)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(canonical)
	return bytes.Clone(digest[:])
}

func cloneReviewInput(value ReviewInput) ReviewInput {
	acknowledgements := make([]*tammyv1.BasWarningAcknowledgement, len(value.Acknowledgements))
	for index, acknowledgement := range value.Acknowledgements {
		acknowledgements[index] = proto.Clone(acknowledgement).(*tammyv1.BasWarningAcknowledgement)
	}
	return ReviewInput{Preparation: proto.Clone(value.Preparation).(*tammyv1.BasPreparation),
		Calculation: proto.Clone(value.Calculation).(*tammyv1.BasCalculation), Validation: proto.Clone(value.Validation).(*tammyv1.BasValidationSnapshot),
		Acknowledgements: acknowledgements,
		CurrentCapture: CurrentCapture{SourceSnapshotHash: bytes.Clone(value.CurrentCapture.SourceSnapshotHash), InputRevision: value.CurrentCapture.InputRevision,
			InputHash: bytes.Clone(value.CurrentCapture.InputHash), RevisionVector: proto.Clone(value.CurrentCapture.RevisionVector).(*tammyv1.BasSourceRevisionVector)},
	}
}

func cloneChainSnapshot(value ChainSnapshot) ChainSnapshot {
	return ChainSnapshot{Family: &Family{Reference: proto.Clone(value.Family.Reference).(*tammyv1.SourceRef), Version: value.Family.Version, Scope: value.Family.Scope},
		RootPreparation: proto.Clone(value.RootPreparation).(*tammyv1.BasPreparation), EffectiveOriginal: clonePreparation(value.EffectiveOriginal), LatestHead: clonePreparation(value.LatestHead), Branch: cloneBranch(value.Branch),
		DisputeOverlay:  value.DisputeOverlay,
		PeriodReference: proto.Clone(value.PeriodReference).(*tammyv1.SourceRef)}
}

func clonePreparation(value *tammyv1.BasPreparation) *tammyv1.BasPreparation {
	if value == nil {
		return nil
	}
	return proto.Clone(value).(*tammyv1.BasPreparation)
}

func cloneAttemptSnapshot(value AttemptSnapshot) AttemptSnapshot {
	clone := AttemptSnapshot{Preparation: clonePreparation(value.Preparation)}
	if value.FrozenReference != nil {
		clone.FrozenReference = cloneRef(value.FrozenReference)
	}
	if value.Frozen != nil {
		clone.Frozen = proto.Clone(value.Frozen).(*tammyv1.BasFrozenBas)
	}
	return clone
}

func dependencyManifestHash(refs []*tammyv1.SourceRef) []byte {
	var encoded bytes.Buffer
	for _, ref := range refs {
		body, err := (proto.MarshalOptions{Deterministic: true}).Marshal(ref)
		if err != nil {
			panic(err)
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(body)))
		encoded.Write(length[:])
		encoded.Write(body)
	}
	digest := sha256.Sum256(encoded.Bytes())
	return bytes.Clone(digest[:])
}

func cloneBranch(value *Branch) *Branch {
	if value == nil {
		return nil
	}
	clone := *value
	if value.Reference != nil {
		clone.Reference = proto.Clone(value.Reference).(*tammyv1.SourceRef)
	}
	clone.RootHead = clonePreparation(value.RootHead)
	clone.ActiveAttempt = clonePreparation(value.ActiveAttempt)
	if value.Resolution != nil {
		clone.Resolution = proto.Clone(value.Resolution).(*tammyv1.SourceRef)
	}
	if value.FrozenBarrier != nil {
		clone.FrozenBarrier = proto.Clone(value.FrozenBarrier).(*tammyv1.SourceRef)
	}
	return &clone
}

func testCivilDate(year, month, day int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: year, Month: month, Day: day}
}

func testUUID(value uint64) string {
	return "01800000-0000-7000-8000-" + leftPadHex(value, 12)
}

func leftPadHex(value uint64, width int) string {
	const digits = "0123456789abcdef"
	var buf [16]byte
	i := len(buf)
	for value > 0 && i > 0 {
		i--
		buf[i] = digits[value&15]
		value >>= 4
	}
	for len(buf)-i < width {
		i--
		buf[i] = '0'
	}
	return string(buf[i:])
}

func testHash(seed byte) []byte { return bytes.Repeat([]byte{seed}, 32) }

func mustChainExpectation(t *testing.T, chain ChainSnapshot) *tammyv1.BasChainExpectation {
	t.Helper()
	expectation, err := BuildChainExpectation(chain)
	if err != nil {
		t.Fatalf("build exact chain expectation: %v", err)
	}
	return expectation
}

func historyWith(chain ChainSnapshot, attempts []AttemptSnapshot, observations ...*tammyv1.BasExternalObservation) ExternalHistoryInput {
	history := ExternalHistoryInput{Chain: cloneChainSnapshot(chain), LastOrdinal: uint64(len(observations))}
	for _, attempt := range attempts {
		history.Attempts = append(history.Attempts, cloneAttemptSnapshot(attempt))
		if attempt.Preparation != nil && attempt.Preparation.State == tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
			history.ConfirmedDependencies = append(history.ConfirmedDependencies, cloneAttemptSnapshot(attempt))
		}
	}
	for index, observation := range observations {
		history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: uint64(index + 1), Observation: observation})
	}
	return history
}

func cloneHistory(value ExternalHistoryInput) ExternalHistoryInput {
	clone := ExternalHistoryInput{Chain: cloneChainSnapshot(value.Chain), LastOrdinal: value.LastOrdinal}
	for _, attempt := range value.Attempts {
		clone.Attempts = append(clone.Attempts, cloneAttemptSnapshot(attempt))
	}
	for _, attempt := range value.ConfirmedDependencies {
		clone.ConfirmedDependencies = append(clone.ConfirmedDependencies, cloneAttemptSnapshot(attempt))
	}
	for _, record := range value.Records {
		copyRecord := ExternalHistoryRecord{Ordinal: record.Ordinal}
		if record.Observation != nil {
			copyRecord.Observation = proto.Clone(record.Observation).(*tammyv1.BasExternalObservation)
		}
		if record.Resolution != nil {
			copyRecord.Resolution = proto.Clone(record.Resolution).(*tammyv1.BasExternalDisputeResolution)
		}
		clone.Records = append(clone.Records, copyRecord)
	}
	return clone
}

func retainedObservation(attempt AttemptSnapshot, id uint64, kind tammyv1.BasExternalEventKind) *tammyv1.BasExternalObservation {
	return &tammyv1.BasExternalObservation{
		ObservationId: testUUID(id), OrganisationId: attempt.Preparation.OrganisationId,
		FrozenBas: cloneRef(attempt.FrozenReference),
		Payload: &tammyv1.BasExternalObservation_Observation{Observation: &tammyv1.BasExternalObservationInput{
			EventKind: kind, Channel: tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL,
			ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 11, 10, 0, 0, 0, time.UTC)), Reason: "Retained user-recorded event",
		}},
		Trust:       tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
		ActorUserId: testUUID(200), RecordedAt: timestamppb.New(time.Date(2026, time.October, 11, 10, 5, 0, 0, time.UTC)), ContentHash: testHash(byte(id)),
	}
}

func retainedCorrection(attempt AttemptSnapshot, id uint64, target *tammyv1.SourceRef, kind tammyv1.BasCorrectionKind,
	correctedKind *tammyv1.BasExternalEventKind, correctedAt *timestamppb.Timestamp) *tammyv1.BasExternalObservation {
	return &tammyv1.BasExternalObservation{
		ObservationId: testUUID(id), OrganisationId: attempt.Preparation.OrganisationId, FrozenBas: cloneRef(attempt.FrozenReference),
		Payload: &tammyv1.BasExternalObservation_Correction{Correction: &tammyv1.BasExternalObservationCorrectionInput{
			DisputedObservation: cloneRef(target), CorrectionKind: kind, CorrectedEventKind: correctedKind,
			CorrectedClaimedEventAt: correctedAt, Reason: "Retain an exact correction without rewriting prior history",
		}},
		Trust:       tammyv1.BasExternalTrust_BAS_EXTERNAL_TRUST_USER_RECORDED_UNVERIFIED,
		ActorUserId: testUUID(200), RecordedAt: timestamppb.New(time.Date(2026, time.October, 11, 10, 10, 0, 0, time.UTC)),
		ContentHash: testHash(byte(id)), CorrectsObservation: cloneRef(target),
	}
}

func observationReference(observation *tammyv1.BasExternalObservation) *tammyv1.SourceRef {
	return sourceRef("bas_external_observation", observation.ObservationId, 1, observation.ContentHash[0])
}

func attemptVariant(base AttemptSnapshot, id uint64, state tammyv1.BasPreparationState) AttemptSnapshot {
	preparation := clonePreparation(base.Preparation)
	preparation.PreparationId = testUUID(id)
	preparation.Version = 1
	preparation.ContentHash = testHash(byte(id))
	preparation.State = state
	preparation.FrozenBas = sourceRef("bas_frozen", testUUID(id+300), 1, byte(id+300))
	preparation.CurrentCalculation = sourceRef("bas_calculation", testUUID(id+400), 1, byte(id+400))
	preparation.CurrentValidation = sourceRef("bas_validation", testUUID(id+500), 1, byte(id+500))

	frozen := proto.Clone(base.Frozen).(*tammyv1.BasFrozenBas)
	frozen.FrozenId = preparation.FrozenBas.Id
	frozen.ContentHash = bytes.Clone(preparation.FrozenBas.ContentHash)
	frozen.Preparation = preparationReference(preparation)
	frozen.Calculation = cloneRef(preparation.CurrentCalculation)
	frozen.Validation = cloneRef(preparation.CurrentValidation)
	frozen.CalculationSnapshot = proto.Clone(base.Frozen.CalculationSnapshot).(*tammyv1.BasCalculation)
	frozen.CalculationSnapshot.CalculationId = preparation.CurrentCalculation.Id
	frozen.CalculationSnapshot.Version = preparation.CurrentCalculation.Revision
	frozen.CalculationSnapshot.ContentHash = bytes.Clone(preparation.CurrentCalculation.ContentHash)
	frozen.CalculationSnapshot.Preparation = preparationReference(preparation)
	frozen.CalculationSnapshot.InputRevision = preparation.InputRevision
	frozen.CalculationSnapshot.InputHash = bytes.Clone(preparation.InputHash)
	frozen.Family = cloneRef(preparation.Family)
	frozen.Period = cloneRef(preparation.Period)
	frozen.Profile = cloneRef(preparation.Profile)

	return AttemptSnapshot{Preparation: preparation, FrozenReference: cloneRef(preparation.FrozenBas), Frozen: frozen}
}

func rebindAttemptToPeriod(base AttemptSnapshot, period *tammyv1.BasConfirmedPeriod, id uint64) AttemptSnapshot {
	attempt := attemptVariant(base, id, base.Preparation.State)
	profile := obligationsProfile(period.OrganisationId)
	periodRef := sourceRef("bas_period", period.PeriodId, period.Version, period.ContentHash[0])
	profileRef := sourceRef("bas_obligations_profile", profile.ProfileId, profile.Revision, profile.ContentHash[0])
	period.Confirmation.Profile = cloneRef(profileRef)

	attempt.Preparation.Period = cloneRef(periodRef)
	attempt.Preparation.Profile = cloneRef(profileRef)
	attempt.Preparation.SourceSnapshotHash = bytes.Clone(base.Preparation.SourceSnapshotHash)
	attempt.Frozen.Period = cloneRef(periodRef)
	attempt.Frozen.Profile = cloneRef(profileRef)
	attempt.Frozen.CalculationSnapshot.Period = cloneRef(periodRef)
	attempt.Frozen.CalculationSnapshot.Profile = cloneRef(profileRef)
	attempt.Frozen.CalculationSnapshot.PeriodSnapshot = proto.Clone(period).(*tammyv1.BasConfirmedPeriod)
	attempt.Frozen.CalculationSnapshot.ProfileSnapshot = proto.Clone(profile).(*tammyv1.BasObligationsProfile)
	attempt.Frozen.CalculationSnapshot.SourceSnapshotHash = bytes.Clone(attempt.Preparation.SourceSnapshotHash)
	attempt.Frozen.CalculationSnapshot.Preparation = preparationReference(attempt.Preparation)
	attempt.Frozen.Preparation = preparationReference(attempt.Preparation)
	attempt.FrozenReference = cloneRef(attempt.Preparation.FrozenBas)
	return attempt
}

func disputeResolutionInputFor(t *testing.T, outcome tammyv1.BasDisputeResolutionOutcome) DisputeResolutionInput {
	t.Helper()
	attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	attempt.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	chain := initialChain(attempt.Preparation)
	chain.RootPreparation = clonePreparation(attempt.Preparation)
	chain.EffectiveOriginal = clonePreparation(attempt.Preparation)
	chain.LatestHead = clonePreparation(attempt.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED
	completed := retainedObservation(attempt, 202, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	rejection := retainedObservation(attempt, 203, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	history := historyWith(chain, []AttemptSnapshot{attempt}, completed, rejection)
	resolution := &tammyv1.BasExternalDisputeInput{
		Outcome: outcome, DisputedObservations: []*tammyv1.SourceRef{observationReference(completed), observationReference(rejection)},
		AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationReference(attempt.Preparation)},
		DependentChainManifestHash: dependencyManifestHash([]*tammyv1.SourceRef{preparationReference(attempt.Preparation)}),
		Reason:                     "Resolve the exact retained contradictory external history",
	}
	return DisputeResolutionInput{Chain: chain, ExpectedChain: mustChainExpectation(t, chain), History: history,
		ConfirmedDependencies: []AttemptSnapshot{cloneAttemptSnapshot(attempt)}, Resolution: resolution}
}

func correctiveObservationInput(t *testing.T) (ObservationInput, *tammyv1.SourceRef) {
	t.Helper()
	head := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
	head.Preparation.State = tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED
	active := attemptVariant(head, 240, tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF)
	active.Preparation.Predecessor = preparationReference(head.Preparation)
	chain := initialChain(head.Preparation)
	chain.RootPreparation = clonePreparation(head.Preparation)
	chain.EffectiveOriginal = clonePreparation(head.Preparation)
	chain.LatestHead = clonePreparation(head.Preparation)
	chain.DisputeOverlay = tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED
	chain.Branch = &Branch{Reference: sourceRef("bas_branch", testUUID(241), 1, 241), Version: 3,
		RootHead: clonePreparation(head.Preparation), ActiveAttempt: clonePreparation(active.Preparation)}
	completion := retainedObservation(head, 242, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
	rejection := retainedObservation(head, 243, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION)
	resolution := &tammyv1.BasExternalDisputeResolution{
		ResolutionId: testUUID(244), Family: cloneRef(chain.Family.Reference),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
			DisputedObservations:       []*tammyv1.SourceRef{observationReference(completion), observationReference(rejection)},
			AffectedConfirmedChain:     []*tammyv1.SourceRef{preparationReference(head.Preparation)},
			DependentChainManifestHash: dependencyManifestHash([]*tammyv1.SourceRef{preparationReference(head.Preparation)}),
			Reason:                     "Require one corrective revision rooted at the retained confirmed head",
		},
		ActorUserId: testUUID(245), ResolvedAt: timestamppb.New(time.Date(2026, time.October, 12, 11, 15, 0, 0, time.UTC)), ContentHash: testHash(246),
	}
	resolutionRef := sourceRef("bas_external_dispute_resolution", resolution.ResolutionId, 1, 246)
	chain.Branch.Resolution = cloneRef(resolutionRef)
	history := historyWith(chain, []AttemptSnapshot{head, active}, completion, rejection)
	history.Records = append(history.Records, ExternalHistoryRecord{Ordinal: 3, Resolution: resolution})
	history.LastOrdinal = 3
	return ObservationInput{Chain: chain, Attempt: active, History: history,
		Observation: &tammyv1.BasExternalObservationInput{EventKind: tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			Channel:        tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_TAX_AGENT,
			ClaimedEventAt: timestamppb.New(time.Date(2026, time.October, 13, 9, 0, 0, 0, time.UTC)), Reason: "Fresh corrective revision confirmation"}}, resolutionRef
}

func eventKindPointer(kind tammyv1.BasExternalEventKind) *tammyv1.BasExternalEventKind {
	return &kind
}

func containsRef(values []*tammyv1.SourceRef, wanted *tammyv1.SourceRef) bool {
	for _, value := range values {
		if sameRef(value, wanted) {
			return true
		}
	}
	return false
}

func sameFamilyScopeMustMatch(t *testing.T, period *tammyv1.BasConfirmedPeriod, scope FamilyScope) bool {
	t.Helper()
	derived, err := FamilyScopeFromPeriod(period)
	if err != nil {
		t.Fatalf("derive actual family scope: %v", err)
	}
	return SameFamilyScope(derived, scope)
}

func sourceRef(kind, id string, revision uint64, seed byte) *tammyv1.SourceRef {
	return &tammyv1.SourceRef{Type: kind, Id: id, Revision: revision, ContentHash: testHash(seed)}
}

func preparationAt(index uint64, state tammyv1.BasPreparationState) *tammyv1.BasPreparation {
	return &tammyv1.BasPreparation{PreparationId: testUUID(index), OrganisationId: testUUID(1), Version: 1,
		Family: sourceRef("bas_family", testUUID(5), 1, 50), Period: sourceRef("bas_period", testUUID(6), 1, 60),
		Profile: sourceRef("bas_obligations_profile", testUUID(7), 1, 70), State: state,
		DisputeOverlay: tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE,
		ContentHash:    testHash(byte(index)), InputRevision: 1, InputHash: testHash(80), SourceSnapshotHash: testHash(60)}
}
