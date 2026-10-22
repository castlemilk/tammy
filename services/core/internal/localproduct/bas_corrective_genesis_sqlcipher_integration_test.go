//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestBASCorrectiveResolutionCanGenesisRevisionThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx, actor := context.Background(), server.authentication
	if addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0xaf01) == nil {
		t.Fatal("assign business lodger role")
	}
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0xaf02)
	defer factor.Close()

	prep, calc, validation := prepareBASFreezeReadyNilStatement(t, server, 0xb000)
	chain := readBASFreezeChain(t, server, actor, prep)
	frozenResult, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(
		makeBASFreezeRequest(server, actor, prep, calc, validation, chain,
			factor.AssertNext("bas_freeze_manual_handoff"), 0xaf11), server.capability))
	if err != nil || frozenResult.Msg == nil || frozenResult.Msg.FrozenBas == nil || frozenResult.Msg.Preparation == nil {
		t.Fatalf("freeze initial supported statement = %v, %v", frozenResult, err)
	}
	frozen, frozenPrep := frozenResult.Msg.FrozenBas, frozenResult.Msg.Preparation
	beforeConfirm := readBASFreezePreparation(t, server, actor, frozenPrep)
	confirmation, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, frozenPrep, beforeConfirm.CurrentChain, frozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
			factor.AssertNext("bas_record_external_confirmation"), 0xaf12), server.capability))
	if err != nil || confirmation.Msg == nil || confirmation.Msg.Observation == nil || confirmation.Msg.Preparation == nil {
		t.Fatalf("record first confirmation = %v, %v", confirmation, err)
	}
	confirmedRead := readBASFreezePreparation(t, server, actor, confirmation.Msg.Preparation)
	confirmedHead := confirmedRead.CurrentChain.LatestHead.GetRetained()
	if confirmedHead == nil || confirmedHead.Id != confirmation.Msg.Preparation.PreparationId {
		t.Fatalf("first confirmation did not set current head: %v", confirmedRead.CurrentChain)
	}

	// A contradictory retained rejection makes the current confirmed assertion
	// disputable. It does not itself change the exact confirmed head.
	rejection, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, confirmation.Msg.Preparation,
			confirmedRead.CurrentChain, frozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, nil, 0xaf13), server.capability))
	if err != nil || rejection.Msg == nil || rejection.Msg.Observation == nil || rejection.Msg.Preparation == nil {
		t.Fatalf("record contradictory rejection = %v, %v", rejection, err)
	}
	disputeRead := readBASFreezePreparation(t, server, actor, rejection.Msg.Preparation)
	if disputeRead.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED ||
		disputeRead.CurrentChain.LatestHead.GetRetained() == nil ||
		!proto.Equal(disputeRead.CurrentChain.LatestHead.GetRetained(), confirmedHead) {
		t.Fatalf("rejection did not preserve the confirmed head and open a dispute: %v", disputeRead.CurrentChain)
	}
	currentChain := disputeRead.CurrentChain
	familyRef := currentChain.Family
	if familyRef == nil || disputeRead.CurrentDispute == nil {
		t.Fatalf("dispute omitted exact metadata: %v", disputeRead)
	}
	dispute := disputeRead.CurrentDispute
	resolveRequest := &tammyv1.ResolveExternalBasDisputeRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(0xaf14), FreshFactor: factor.AssertNext("bas_resolve_external_dispute")},
		OrganisationId: server.organisationID, ExpectedVersion: currentChain.ExpectedFamilyVersion,
		Chain: proto.Clone(currentChain).(*tammyv1.BasChainExpectation),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome:                    tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
			DisputedObservations:       dispute.DisputedObservations,
			AffectedConfirmedChain:     dispute.AffectedConfirmedChain,
			DependentChainManifestHash: bytes.Clone(dispute.DependentChainManifestHash),
			Reason:                     "Require a corrective revision after the retained portal rejection",
		},
	}
	resolved, err := server.basPreparation.ResolveExternalBasDispute(ctx, brandingRequest(resolveRequest, server.capability))
	if err != nil || resolved.Msg == nil || resolved.Msg.Resolution == nil || resolved.Msg.Preparation == nil {
		t.Fatalf("resolve correction-required dispute = %v, %v", resolved, err)
	}
	resolvedRead := readBASFreezePreparation(t, server, actor, resolved.Msg.Preparation)
	corrective := resolvedRead.CurrentChain
	if resolvedRead.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED ||
		corrective.Branch.GetRetained() == nil || resolvedRead.CurrentBranchResolution == nil ||
		corrective.ActiveAttempt.GetRetained() != nil || resolvedRead.CurrentFrozenBarrier != nil ||
		!proto.Equal(corrective.LatestHead.GetRetained(), confirmedHead) {
		t.Fatalf("resolution did not retain an empty corrective branch pinned to the exact head: %v", resolvedRead)
	}
	familyIdentity := corrective.Family
	if familyIdentity == nil {
		t.Fatal("corrective chain omitted exact family identity")
	}
	projector, err := newBASOverviewProjector(server.activation.WorkspaceID, server.activation.InstallationKey)
	if err != nil {
		t.Fatal(err)
	}
	reportingPeriod := &tammyv1.ReportingPeriod{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	}
	familyIdentityRevision := familyIdentity.Revision
	regenerate, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(&tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(0xaf15)},
		OrganisationId: server.organisationID, ExpectedVersion: 0,
		FamilyExpectation: &tammyv1.BasExpectedIdentity{
			ExpectedState:   &tammyv1.BasExpectedIdentity_Retained{Retained: proto.Clone(familyIdentity).(*tammyv1.SourceRef)},
			ExpectedVersion: &familyIdentityRevision,
		},
		PreparationExpectation: &tammyv1.BasExpectedIdentity{ExpectedState: &tammyv1.BasExpectedIdentity_Absent{Absent: true}},
		Profile:                proto.Clone(confirmation.Msg.Preparation.Profile).(*tammyv1.SourceRef),
		Period:                 proto.Clone(confirmation.Msg.Preparation.Period).(*tammyv1.SourceRef),
	}, server.capability))
	if err != nil || regenerate.Msg == nil || regenerate.Msg.Preparation == nil || regenerate.Msg.Calculation == nil {
		t.Fatalf("genesis regeneration of resolved corrective branch = %v, %v", regenerate, err)
	}
	correctivePrep := regenerate.Msg.Preparation
	if correctivePrep.Predecessor == nil || correctivePrep.Predecessor.Id != confirmedHead.Id ||
		!bytes.Equal(correctivePrep.Predecessor.ContentHash, confirmedHead.ContentHash) ||
		correctivePrep.Family.Id != familyIdentity.Id || correctivePrep.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING {
		t.Fatalf("corrective attempt lost its exact predecessor/family or did not start collecting: %v", correctivePrep)
	}
	ready, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(
		validateBASReviewRequest(server, correctivePrep, regenerate.Msg.Calculation, 0xaf16), server.capability))
	if err != nil || ready.Msg == nil || ready.Msg.Preparation == nil || ready.Msg.Validation == nil ||
		ready.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || len(ready.Msg.Validation.Findings) != 0 {
		t.Fatalf("validate corrective revision = %v, %v", ready, err)
	}
	readyProjection := projectBASAttentionInOwnReadTx(t, server, projector, reportingPeriod)
	expectedReadyPreparation := &tammyv1.SourceRef{Type: "bas_preparation", Id: ready.Msg.Preparation.PreparationId,
		Revision: ready.Msg.Preparation.Version, ContentHash: bytes.Clone(ready.Msg.Preparation.ContentHash)}
	if readyProjection.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_REVIEW_READY ||
		!proto.Equal(readyProjection.Preparation, expectedReadyPreparation) ||
		!proto.Equal(readyProjection.Family, familyIdentity) || readyProjection.Frozen != nil {
		t.Fatalf("writer-backed review-ready corrective projection = %#v; want exact preparation %v, family %v and no frozen ref",
			readyProjection, expectedReadyPreparation, familyIdentity)
	}
	readyChain := readBASFreezePreparation(t, server, actor, ready.Msg.Preparation).CurrentChain
	correctiveFrozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(
		makeBASFreezeRequest(server, actor, ready.Msg.Preparation, regenerate.Msg.Calculation,
			ready.Msg.Validation, readyChain, factor.AssertNext("bas_freeze_manual_handoff"), 0xaf17), server.capability))
	if err != nil || correctiveFrozen.Msg == nil || correctiveFrozen.Msg.FrozenBas == nil || correctiveFrozen.Msg.Preparation == nil {
		t.Fatalf("freeze corrective revision = %v, %v", correctiveFrozen, err)
	}
	postFreeze := readBASFreezePreparation(t, server, actor, correctiveFrozen.Msg.Preparation)
	frozenProjection := projectBASAttentionInOwnReadTx(t, server, projector, reportingPeriod)
	expectedFrozenPreparation := &tammyv1.SourceRef{Type: "bas_preparation", Id: correctiveFrozen.Msg.Preparation.PreparationId,
		Revision: correctiveFrozen.Msg.Preparation.Version, ContentHash: bytes.Clone(correctiveFrozen.Msg.Preparation.ContentHash)}
	if frozenProjection.Status != tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_FROZEN_FOR_MANUAL_HANDOFF ||
		!proto.Equal(frozenProjection.Preparation, expectedFrozenPreparation) ||
		!proto.Equal(frozenProjection.Family, familyIdentity) ||
		!proto.Equal(frozenProjection.Frozen, correctiveFrozen.Msg.Preparation.FrozenBas) {
		t.Fatalf("writer-backed frozen corrective projection = %#v; want exact preparation %v, family %v and active frozen ref %v",
			frozenProjection, expectedFrozenPreparation, familyIdentity, correctiveFrozen.Msg.Preparation.FrozenBas)
	}
	corrected, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, correctiveFrozen.Msg.Preparation,
			postFreeze.CurrentChain, correctiveFrozen.Msg.FrozenBas,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			factor.AssertNext("bas_record_external_confirmation"), 0xaf18), server.capability))
	if err != nil || corrected.Msg == nil || corrected.Msg.Preparation == nil ||
		corrected.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
		t.Fatalf("confirm corrective revision = %v, %v", corrected, err)
	}
	final := readBASFreezePreparation(t, server, actor, corrected.Msg.Preparation)
	if final.CurrentChain.LatestHead.GetRetained() == nil ||
		final.CurrentChain.LatestHead.GetRetained().Id != corrected.Msg.Preparation.PreparationId ||
		final.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE {
		t.Fatalf("corrective confirmation did not clear dispute and advance exact head: %v", final.CurrentChain)
	}
}
