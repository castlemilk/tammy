package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func assertTypedSnapshotRoundTrip[T proto.Message](t *testing.T, value T, id, org, resource string, revision uint64,
	seal func(T) ([]byte, []byte, error), open func([]byte, string, *tammyv1.SourceRef) (T, error)) {
	t.Helper()
	before := proto.Clone(value).(T)
	wire, hash, err := seal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(value, before) {
		t.Fatal("sealing changed caller-owned data")
	}
	exact := &tammyv1.SourceRef{Type: resource, Id: id, Revision: revision, ContentHash: hash}
	got, err := open(wire, org, exact)
	if err != nil || !got.ProtoReflect().IsValid() {
		t.Fatalf("exact read = %v", err)
	}
	for _, change := range []func(*tammyv1.SourceRef){
		func(ref *tammyv1.SourceRef) { ref.Id = testUUID(3901) },
		func(ref *tammyv1.SourceRef) { ref.Revision++ },
		func(ref *tammyv1.SourceRef) { ref.ContentHash[0] ^= 0xff },
		func(ref *tammyv1.SourceRef) { ref.Type = "wrong_retained_resource" },
	} {
		wrong := proto.Clone(exact).(*tammyv1.SourceRef)
		change(wrong)
		if _, err := open(wire, org, wrong); err == nil {
			t.Fatal("changed exact reference was accepted")
		}
	}
	if _, err := open(wire, testUUID(3902), exact); err == nil {
		t.Fatal("cross-organisation read was accepted")
	}
	if _, err := open(wire[:len(wire)-1], org, exact); err == nil {
		t.Fatal("truncated snapshot was accepted")
	}
}

func TestSnapshotAdditionalTypedFactsRetainExactWholeBodies(t *testing.T) {
	org := testUUID(1)
	t.Run("period", func(t *testing.T) {
		period := confirmedPeriod(testUUID(6), org, 2, 61)
		period.ContentHash = nil
		assertTypedSnapshotRoundTrip(t, period, period.PeriodId, org, "bas_period", period.Version, sealPeriod, openPeriod)
	})
	t.Run("calculation", func(t *testing.T) {
		calculation := readyReviewInput().Calculation
		calculation.ContentHash = nil
		assertTypedSnapshotRoundTrip(t, calculation, calculation.CalculationId, org, "bas_calculation", calculation.Version, sealCalculation, openCalculation)
	})
	t.Run("ordinary observation", func(t *testing.T) {
		attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
		observation := retainedObservation(attempt, 3001, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT)
		observation.ContentHash = nil
		assertTypedSnapshotRoundTrip(t, observation, observation.ObservationId, org, "bas_external_observation", 1, sealObservation, openObservation)
	})
	t.Run("typed correction", func(t *testing.T) {
		attempt := observationInputFor(tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT).Attempt
		observation := retainedCorrection(attempt, 3002, sourceRef("bas_external_observation", testUUID(3001), 1, 41),
			tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_RETRACT_AS_INCORRECT, nil, nil)
		observation.ContentHash = nil
		assertTypedSnapshotRoundTrip(t, observation, observation.ObservationId, org, "bas_external_observation", 1, sealObservation, openObservation)
	})
}

func TestSnapshotFamilyAndBranchWorkflowBindAuthoritativeImmutableIdentity(t *testing.T) {
	org := testUUID(1)
	when := readyReviewInput().Preparation.CreatedAt
	family := &tammyv1.BasFamilyIdentity{FamilyId: testUUID(3101), OrganisationId: org,
		PeriodScope: &tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)},
		Window:      tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP, CreatedAt: when}
	assertTypedSnapshotRoundTrip(t, family, family.FamilyId, org, "bas_family", 1, sealFamilyIdentity, openFamilyIdentity)
	familyWire, familyHash, err := sealFamilyIdentity(family)
	if err != nil {
		t.Fatal(err)
	}
	familyRef := &tammyv1.SourceRef{Type: "bas_family", Id: family.FamilyId, Revision: 1, ContentHash: familyHash}
	retainedFamily, err := openFamilyIdentity(familyWire, org, familyRef)
	if err != nil {
		t.Fatal(err)
	}
	state := &tammyv1.BasFamilyState{Family: familyRef, Version: 2,
		RootPreparation: sourceRef("bas_preparation", testUUID(3102), 1, 55),
		LatestHead:      absentIdentity(), ActiveBranch: absentIdentity(),
		DisputeOverlay: tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE, UpdatedAt: when}
	sealState := func(value *tammyv1.BasFamilyState) ([]byte, []byte, error) {
		return sealFamilyState(value, retainedFamily)
	}
	openState := func(wire []byte, org string, ref *tammyv1.SourceRef) (*tammyv1.BasFamilyState, error) {
		return openFamilyState(wire, org, ref, retainedFamily)
	}
	assertTypedSnapshotRoundTrip(t, state, family.FamilyId, org, "bas_family_state", state.Version, sealState, openState)
	wrongState := proto.Clone(state).(*tammyv1.BasFamilyState)
	wrongState.Family.ContentHash[0] ^= 0xff
	if _, _, err := sealState(wrongState); err == nil {
		t.Fatal("workflow accepted another immutable family hash")
	}

	branch := &tammyv1.BasBranchIdentity{BranchId: testUUID(3103), OrganisationId: org, Family: familyRef,
		OriginalPurpose: tammyv1.BasBranchPurpose_BAS_BRANCH_PURPOSE_REVISION, FamilyRootPreparation: state.RootPreparation,
		RootHead: absentIdentity(), CreatedAt: when}
	assertTypedSnapshotRoundTrip(t, branch, branch.BranchId, org, "bas_branch", 1, sealBranchIdentity, openBranchIdentity)
	branchWire, branchHash, err := sealBranchIdentity(branch)
	if err != nil {
		t.Fatal(err)
	}
	branchRef := &tammyv1.SourceRef{Type: "bas_branch", Id: branch.BranchId, Revision: 1, ContentHash: branchHash}
	retainedBranch, err := openBranchIdentity(branchWire, org, branchRef)
	if err != nil {
		t.Fatal(err)
	}
	branchState := &tammyv1.BasBranchState{BranchIdentity: branchRef, Version: 1,
		WorkflowState: tammyv1.BasBranchWorkflowState_BAS_BRANCH_WORKFLOW_STATE_ACTIVE, ActiveAttempt: absentIdentity(), UpdatedAt: when}
	sealBranchWorkflow := func(value *tammyv1.BasBranchState) ([]byte, []byte, error) {
		return sealBranchState(value, retainedBranch)
	}
	openBranchWorkflow := func(wire []byte, org string, ref *tammyv1.SourceRef) (*tammyv1.BasBranchState, error) {
		return openBranchState(wire, org, ref, retainedBranch)
	}
	assertTypedSnapshotRoundTrip(t, branchState, branch.BranchId, org, "bas_branch_state", 1, sealBranchWorkflow, openBranchWorkflow)
	wrongBranch := proto.Clone(branchState).(*tammyv1.BasBranchState)
	wrongBranch.BranchIdentity.Id = testUUID(3104)
	if _, _, err := sealBranchWorkflow(wrongBranch); err == nil {
		t.Fatal("workflow accepted another immutable branch")
	}
}

func TestSnapshotSavedPaygHashBindsPairSeparatelyFromScopedEnvelope(t *testing.T) {
	review := readyReviewInput()
	gw, gi := review.Calculation.PaygwInputs, review.Calculation.PaygiInputs
	gwWire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(gw)
	if err != nil {
		t.Fatal(err)
	}
	giWire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(gi)
	if err != nil {
		t.Fatal(err)
	}
	// Independent framing oracle: neither metadata nor envelope hash participates.
	frame := append([]byte(nil), []byte("tammy.bas.payg-inputs.v1\x00")...)
	for _, wire := range [][]byte{gwWire, giWire} {
		frame = binary.BigEndian.AppendUint64(frame, uint64(len(wire)))
		frame = append(frame, wire...)
	}
	pairHash := sha256.Sum256(frame)
	got, err := hashPaygInputs(gw, gi)
	if err != nil || !bytes.Equal(got, pairHash[:]) {
		t.Fatal("PAYG pair hash must use complete framed deterministic typed inputs")
	}
	value := &tammyv1.BasSavedPaygInputs{InputSnapshotId: testUUID(3201), OrganisationId: review.Preparation.OrganisationId,
		PreparationId: review.Preparation.PreparationId, Preparation: preparationReference(review.Preparation),
		InputRevision: 2, InputHash: pairHash[:], PaygwInputs: gw, PaygiInputs: gi,
		ActorUserId: testUUID(3202), RecordedAt: review.Preparation.CreatedAt}
	assertTypedSnapshotRoundTrip(t, value, value.InputSnapshotId, value.OrganisationId, "bas_saved_payg_inputs", value.InputRevision, sealSavedPaygInputs, openSavedPaygInputs)
	_, envelopeHash, err := sealSavedPaygInputs(value)
	if err != nil {
		t.Fatal(err)
	}
	changed := proto.Clone(value).(*tammyv1.BasSavedPaygInputs)
	changed.ActorUserId = testUUID(3203)
	_, changedEnvelopeHash, err := sealSavedPaygInputs(changed)
	if err != nil || bytes.Equal(changedEnvelopeHash, envelopeHash) {
		t.Fatal("actor metadata must alter envelope hash")
	}
	if !bytes.Equal(changed.InputHash, value.InputHash) {
		t.Fatal("metadata must not alter the PAYG pair identity")
	}
	changed.InputHash[0] ^= 0xff
	if _, _, err := sealSavedPaygInputs(changed); err == nil {
		t.Fatal("mismatched claimed input hash must fail closed")
	}
	if _, err := hashPaygInputs(nil, gi); err == nil {
		t.Fatal("missing half of PAYG inputs was accepted")
	}
}

func absentIdentity() *tammyv1.BasExpectedIdentity {
	return &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}}
}
