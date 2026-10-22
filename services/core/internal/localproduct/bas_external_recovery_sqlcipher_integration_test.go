//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

func TestBASUnusedFrozenAttemptCanBeReplacedThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx, actor := context.Background(), server.authentication
	if addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0xd001) == nil {
		t.Fatal("assign business lodger role")
	}
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0xd101)
	defer factor.Close()

	frozenPreparation, frozen := basRecoveryFreezeNilRoot(t, server, actor, factor, 0xd080)
	before := readBASFreezePreparation(t, server, actor, frozenPreparation)
	request := &tammyv1.CreateBasReplacementRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			FreshFactor: factor.AssertNext("bas_replace_unused_attempt"), IdempotencyKey: testBASCommandKey(0xd020)},
		OrganisationId: server.organisationID, ExpectedVersion: before.CurrentChain.ExpectedFamilyVersion,
		Chain:               proto.Clone(before.CurrentChain).(*tammyv1.BasChainExpectation),
		PredecessorFrozenId: frozen.FrozenId, PredecessorFrozenHash: bytes.Clone(frozen.ContentHash),
		Reason: "Replace the retained frozen statement before it is used for an external lodgement",
	}
	created, err := server.basPreparation.CreateBasReplacement(ctx, brandingRequest(request, server.capability))
	if err != nil || created.Msg == nil || created.Msg.Predecessor == nil || created.Msg.Replacement == nil {
		t.Fatalf("replace unused frozen attempt = %v, %v", created, err)
	}
	old := created.Msg.Predecessor
	next := created.Msg.Replacement
	if next.CreationReason != request.Reason {
		t.Fatalf("replacement reason was not retained: %q", next.CreationReason)
	}
	if old.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
		old.SupersededBy == nil || old.SupersededBy.Id != next.PreparationId ||
		next.Predecessor == nil || next.Predecessor.Id != old.PreparationId ||
		next.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING ||
		next.CurrentCalculation == nil || next.FrozenBas != nil {
		t.Fatalf("replacement must link the frozen predecessor and start a newly captured collecting attempt: old=%v next=%v", old, next)
	}
	unlinked := basRecoveryGetPreparation(t, server, actor, next.Predecessor)
	if unlinked.PreparationId != old.PreparationId || unlinked.Version >= old.Version ||
		unlinked.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_BEFORE_CONFIRMATION ||
		unlinked.SupersededBy != nil {
		t.Fatalf("successor must point to the retained unlinked terminal predecessor version: %v", unlinked)
	}
	activeRead := readBASFreezePreparation(t, server, actor, next)
	if activeRead.Preparation.CreationReason != request.Reason {
		t.Fatal("exact query lost retained replacement reason")
	}
	active := activeRead.CurrentChain
	if active.ActiveAttempt.GetRetained() == nil || active.ActiveAttempt.GetRetained().Id != next.PreparationId ||
		!bytes.Equal(active.ActiveAttempt.GetRetained().ContentHash, next.ContentHash) ||
		activeRead.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE {
		t.Fatalf("replacement did not become the exact active attempt on the unchanged clear chain: %v", active)
	}
	if basFactorConsumed(t, server, request.CommandContext.FreshFactor.AssertionId) != 1 {
		t.Fatal("successful unused replacement did not consume its exact purpose-bound assertion")
	}
	beforeReplayAudit, beforeReplayIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	replayed, err := server.basPreparation.CreateBasReplacement(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(created.Msg, replayed.Msg) {
		t.Fatalf("exact unused replacement replay = %v, %v", replayed, err)
	}
	assertBASApprovalCountsUnchanged(t, beforeReplayAudit, beforeReplayIdempotency, server)
}

func TestBASRetainedUnambiguousRejectionCanBeReplacedThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx, actor := context.Background(), server.authentication
	if addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0xd201) == nil {
		t.Fatal("assign business lodger role")
	}
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0xd301)
	defer factor.Close()

	frozenPreparation, frozen := basRecoveryFreezeNilRoot(t, server, actor, factor, 0xd280)
	current := readBASFreezePreparation(t, server, actor, frozenPreparation)
	rejection, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, frozenPreparation, current.CurrentChain, frozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, nil, 0xd220), server.capability))
	if err != nil || rejection.Msg == nil || rejection.Msg.Observation == nil || rejection.Msg.Preparation == nil {
		t.Fatalf("record exact external rejection = %v, %v", rejection, err)
	}
	afterRejection := readBASFreezePreparation(t, server, actor, rejection.Msg.Preparation)
	if afterRejection.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_NONE ||
		afterRejection.CurrentChain.LatestHead.GetRetained() != nil {
		t.Fatalf("a first rejected, unconfirmed attempt must remain recoverable without manufacturing a dispute/head: %v", afterRejection)
	}
	rejectionRef := &tammyv1.SourceRef{Type: "bas_external_observation", Id: rejection.Msg.Observation.ObservationId,
		Revision: 1, ContentHash: bytes.Clone(rejection.Msg.Observation.ContentHash)}
	request := &tammyv1.ReplaceRejectedBasAttemptRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			FreshFactor: factor.AssertNext("bas_replace_rejected_attempt"), IdempotencyKey: testBASCommandKey(0xd221)},
		OrganisationId: server.organisationID, ExpectedVersion: afterRejection.CurrentChain.ExpectedFamilyVersion,
		Chain:                   proto.Clone(afterRejection.CurrentChain).(*tammyv1.BasChainExpectation),
		RejectedPreparationId:   rejection.Msg.Preparation.PreparationId,
		RejectedPreparationHash: bytes.Clone(rejection.Msg.Preparation.ContentHash),
		ResolvedRejection:       rejectionRef,
		Reason:                  "Retry only after retaining and pinning the exact unambiguous portal rejection",
	}
	created, err := server.basPreparation.ReplaceRejectedBasAttempt(ctx, brandingRequest(request, server.capability))
	if err != nil || created.Msg == nil || created.Msg.RejectedAttempt == nil || created.Msg.ReplacementAttempt == nil {
		t.Fatalf("replace exactly rejected attempt = %v, %v", created, err)
	}
	rejected := created.Msg.RejectedAttempt
	replacement := created.Msg.ReplacementAttempt
	if rejected.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION ||
		rejected.SupersededBy == nil || rejected.SupersededBy.Id != replacement.PreparationId ||
		replacement.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_COLLECTING ||
		replacement.Predecessor == nil || replacement.Predecessor.Id != rejected.PreparationId {
		t.Fatalf("recovery must preserve the rejection, terminalize its exact predecessor and create a collecting successor: rejected=%v successor=%v", rejected, replacement)
	}
	unlinked := basRecoveryGetPreparation(t, server, actor, replacement.Predecessor)
	if unlinked.PreparationId != rejected.PreparationId || unlinked.Version >= rejected.Version ||
		unlinked.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_SUPERSEDED_AFTER_RECORDED_REJECTION ||
		unlinked.SupersededBy != nil {
		t.Fatalf("successor must directly pin the unlinked rejection-terminal version: %v", unlinked)
	}
	if basFactorConsumed(t, server, request.CommandContext.FreshFactor.AssertionId) != 1 {
		t.Fatal("rejected recovery did not consume its exact purpose-bound assertion")
	}
	beforeReplayAudit, beforeReplayIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	replayed, err := server.basPreparation.ReplaceRejectedBasAttempt(ctx, brandingRequest(request, server.capability))
	if err != nil || !proto.Equal(created.Msg, replayed.Msg) {
		t.Fatalf("exact rejected recovery replay = %v, %v", replayed, err)
	}
	assertBASApprovalCountsUnchanged(t, beforeReplayAudit, beforeReplayIdempotency, server)
}

func TestBASCorrectiveResolutionPreservesFrozenAttemptBehindBarrierThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx, actor := context.Background(), server.authentication
	if addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0xd401) == nil {
		t.Fatal("assign business lodger role")
	}
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0xd501)
	defer factor.Close()

	rootPreparation, rootFrozen := basRecoveryFreezeNilRoot(t, server, actor, factor, 0xd480)
	rootAfterFreeze := readBASFreezePreparation(t, server, actor, rootPreparation)
	rootConfirmation, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, rootPreparation, rootAfterFreeze.CurrentChain, rootFrozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
			factor.AssertNext("bas_record_external_confirmation"), 0xd420), server.capability))
	if err != nil || rootConfirmation.Msg == nil || rootConfirmation.Msg.Preparation == nil {
		t.Fatalf("confirm original statement = %v, %v", rootConfirmation, err)
	}

	revision, revisionFrozen := basRecoveryCreateAndFreezeRevision(t, server, actor, factor,
		rootConfirmation.Msg.Preparation, 0xd440)
	revisionCurrent := readBASFreezePreparation(t, server, actor, revision)
	lateRejection, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, revision, revisionCurrent.CurrentChain, rootFrozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, nil, 0xd470), server.capability))
	if err != nil || lateRejection.Msg == nil || lateRejection.Msg.Observation == nil || lateRejection.Msg.Preparation == nil {
		t.Fatalf("record rejection against retained confirmed predecessor = %v, %v", lateRejection, err)
	}
	disputed := readBASFreezePreparation(t, server, actor, lateRejection.Msg.Preparation)
	if disputed.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED ||
		disputed.CurrentDispute == nil || disputed.CurrentChain.Branch.GetRetained() == nil ||
		disputed.CurrentChain.ActiveAttempt.GetRetained() == nil ||
		disputed.CurrentChain.ActiveAttempt.GetRetained().Id != revision.PreparationId {
		t.Fatalf("late predecessor rejection must open a dispute while preserving the frozen active revision: %v", disputed)
	}

	resolved, err := basRecoveryResolveDispute(t, server, actor, factor, lateRejection.Msg.Preparation,
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_REQUIRE_CORRECTIVE_REVISION,
		"Require a corrective successor while preserving the already frozen revision", 0xd4c0)
	if err != nil || resolved == nil || resolved.Resolution == nil || resolved.Preparation == nil {
		t.Fatalf("resolve with required corrective revision = %v, %v", resolved, err)
	}
	afterResolution := readBASFreezePreparation(t, server, actor, resolved.Preparation)
	resolutionRef := &tammyv1.SourceRef{Type: "bas_external_dispute_resolution", Id: resolved.Resolution.ResolutionId, Revision: 1, ContentHash: bytes.Clone(resolved.Resolution.ContentHash)}
	barrier := afterResolution.CurrentFrozenBarrier
	if afterResolution.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED ||
		afterResolution.CurrentBranchResolution == nil ||
		afterResolution.CurrentChain.Branch.GetRetained() == nil ||
		afterResolution.CurrentChain.ActiveAttempt.GetRetained() == nil ||
		afterResolution.CurrentChain.ActiveAttempt.GetRetained().Id != revision.PreparationId ||
		!bytes.Equal(afterResolution.CurrentChain.ActiveAttempt.GetRetained().ContentHash, revision.ContentHash) ||
		barrier == nil || barrier.Id != revisionFrozen.FrozenId ||
		!bytes.Equal(barrier.ContentHash, revisionFrozen.ContentHash) {
		t.Fatalf("corrective resolution must preserve the exact frozen active attempt behind its frozen barrier: %v", afterResolution)
	}

	// The only path that can move past this barrier is an explicit replacement
	// of its exact frozen attempt; the reservation and dispute resolution stay
	// pinned to the same branch.
	replacementRequest := &tammyv1.CreateBasReplacementRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			FreshFactor: factor.AssertNext("bas_replace_unused_attempt"), IdempotencyKey: testBASCommandKey(0xd4c1)},
		OrganisationId: server.organisationID, ExpectedVersion: afterResolution.CurrentChain.ExpectedFamilyVersion,
		Chain:               proto.Clone(afterResolution.CurrentChain).(*tammyv1.BasChainExpectation),
		PredecessorFrozenId: revisionFrozen.FrozenId, PredecessorFrozenHash: bytes.Clone(revisionFrozen.ContentHash),
		Reason: "Replace the preserved frozen corrective attempt while retaining the dispute-resolution reservation",
	}
	replacement, err := server.basPreparation.CreateBasReplacement(ctx, brandingRequest(replacementRequest, server.capability))
	if err != nil || replacement.Msg == nil || replacement.Msg.Replacement == nil {
		t.Fatalf("explicitly replace frozen corrective barrier attempt = %v, %v", replacement, err)
	}
	afterReplacement := readBASFreezePreparation(t, server, actor, replacement.Msg.Replacement)
	if afterReplacement.CurrentFrozenBarrier != nil ||
		afterReplacement.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_CORRECTION_REQUIRED ||
		afterReplacement.CurrentBranchResolution == nil ||
		!proto.Equal(afterReplacement.CurrentBranchResolution, resolutionRef) ||
		afterReplacement.CurrentChain.ActiveAttempt.GetRetained() == nil ||
		afterReplacement.CurrentChain.ActiveAttempt.GetRetained().Id != replacement.Msg.Replacement.PreparationId {
		t.Fatalf("corrective replacement must clear only the old frozen barrier and preserve its exact resolution/branch: %v", afterReplacement)
	}
}

func TestBASUpheldDisputeRetainsConfirmedHeadAndAllowsRevisionThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx, actor := context.Background(), server.authentication
	if addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0xd601) == nil {
		t.Fatal("assign business lodger role")
	}
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0xd701)
	defer factor.Close()

	rootPreparation, rootFrozen := basRecoveryFreezeNilRoot(t, server, actor, factor, 0xd680)
	afterFreeze := readBASFreezePreparation(t, server, actor, rootPreparation)
	confirmed, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, rootPreparation, afterFreeze.CurrentChain, rootFrozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE,
			factor.AssertNext("bas_record_external_confirmation"), 0xd620), server.capability))
	if err != nil || confirmed.Msg == nil || confirmed.Msg.Preparation == nil {
		t.Fatalf("record acceptance for original statement = %v, %v", confirmed, err)
	}
	confirmedHead := readBASFreezePreparation(t, server, actor, confirmed.Msg.Preparation).CurrentChain.LatestHead.GetRetained()
	if confirmedHead == nil {
		t.Fatal("acceptance did not establish the original confirmed head")
	}
	current := readBASFreezePreparation(t, server, actor, confirmed.Msg.Preparation)
	rejection, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(
		recordBASLifecycleObservationRequest(server, actor, confirmed.Msg.Preparation, current.CurrentChain, rootFrozen,
			tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_REJECTION, nil, 0xd621), server.capability))
	if err != nil || rejection.Msg == nil || rejection.Msg.Preparation == nil {
		t.Fatalf("open dispute from contradictory retained rejection = %v, %v", rejection, err)
	}
	resolved, err := basRecoveryResolveDispute(t, server, actor, factor, rejection.Msg.Preparation,
		tammyv1.BasDisputeResolutionOutcome_BAS_DISPUTE_RESOLUTION_OUTCOME_UPHOLD_RECORDED_CHAIN,
		"Uphold the retained accepted chain after reviewing the contradictory report", 0xd622)
	if err != nil || resolved == nil || resolved.Resolution == nil || resolved.Preparation == nil {
		t.Fatalf("uphold disputed chain = %v, %v", resolved, err)
	}
	afterUphold := readBASFreezePreparation(t, server, actor, resolved.Preparation)
	if afterUphold.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_RESOLVED_UPHELD ||
		afterUphold.CurrentChain.LatestHead.GetRetained() == nil ||
		!proto.Equal(afterUphold.CurrentChain.LatestHead.GetRetained(), confirmedHead) ||
		afterUphold.CurrentChain.Branch.GetRetained() != nil {
		t.Fatalf("uphold must clear only the dispute overlay and keep the exact confirmed head: %v", afterUphold)
	}

	revision := basRecoveryCreateRevision(t, server, actor, confirmed.Msg.Preparation, 0xd640)
	if revision == nil || revision.Predecessor == nil || revision.Predecessor.Id != confirmedHead.Id ||
		!bytes.Equal(revision.Predecessor.ContentHash, confirmedHead.ContentHash) {
		t.Fatalf("a new revision after uphold must be based on the exact retained confirmed head: %v", revision)
	}
}

func basRecoveryFreezeNilRoot(t *testing.T, server documentPageServer, actor *tammyv1.AuthenticationContext,
	factor *basLifecycleTOTP, keyBase uint64) (*tammyv1.BasPreparation, *tammyv1.BasFrozenBas) {
	t.Helper()
	preparation, calculation, validation := prepareBASFreezeReadyNilStatement(t, server, keyBase)
	chain := readBASFreezeChain(t, server, actor, preparation)
	frozen, err := server.basPreparation.FreezeBasForManualHandoff(context.Background(), brandingRequest(
		makeBASFreezeRequest(server, actor, preparation, calculation, validation, chain,
			factor.AssertNext("bas_freeze_manual_handoff"), keyBase+0x11), server.capability))
	if err != nil || frozen.Msg == nil || frozen.Msg.Preparation == nil || frozen.Msg.FrozenBas == nil {
		t.Fatalf("freeze supported nil root for recovery regression = %v, %v", frozen, err)
	}
	return frozen.Msg.Preparation, frozen.Msg.FrozenBas
}

func basRecoveryCreateRevision(t *testing.T, server documentPageServer, actor *tammyv1.AuthenticationContext,
	confirmed *tammyv1.BasPreparation, keyBase uint64) *tammyv1.BasPreparation {
	t.Helper()
	current := readBASFreezePreparation(t, server, actor, confirmed)
	chain := current.CurrentChain
	head := chain.LatestHead.GetRetained()
	effectiveOriginal := current.EffectiveOriginal
	if head == nil || effectiveOriginal == nil {
		t.Fatalf("confirmed chain lacks exact effective original/head: %v", chain)
	}
	created, err := server.basPreparation.CreateBasRevision(context.Background(), brandingRequest(&tammyv1.CreateBasRevisionRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(keyBase + 0x01)},
		OrganisationId: server.organisationID, ExpectedVersion: chain.ExpectedFamilyVersion,
		Chain:                            proto.Clone(chain).(*tammyv1.BasChainExpectation),
		EffectiveOriginalPreparationId:   effectiveOriginal.Id,
		EffectiveOriginalPreparationHash: bytes.Clone(effectiveOriginal.ContentHash),
		LatestConfirmedHeadId:            head.Id, LatestConfirmedHeadHash: bytes.Clone(head.ContentHash),
		Period:  proto.Clone(confirmed.Period).(*tammyv1.SourceRef),
		Profile: proto.Clone(confirmed.Profile).(*tammyv1.SourceRef),
		Reason:  "Create a new attempt from the exact upheld or confirmed head",
	}, server.capability))
	if err != nil || created.Msg == nil || created.Msg.Revision == nil || created.Msg.Original == nil {
		t.Fatalf("create recovery-path revision = %v, %v", created, err)
	}
	return created.Msg.Revision
}

func basRecoveryCreateAndFreezeRevision(t *testing.T, server documentPageServer,
	actor *tammyv1.AuthenticationContext, factor *basLifecycleTOTP,
	confirmed *tammyv1.BasPreparation, keyBase uint64,
) (*tammyv1.BasPreparation, *tammyv1.BasFrozenBas) {
	t.Helper()
	revision := basRecoveryCreateRevision(t, server, actor, confirmed, keyBase)
	revisionRead := readBASFreezePreparation(t, server, actor, revision)
	if revisionRead.CurrentCalculation == nil {
		t.Fatalf("new revision omitted its source-captured calculation: %v", revisionRead)
	}
	validated, err := server.basPreparation.ValidateBasPreparation(context.Background(), brandingRequest(
		validateBASReviewRequest(server, revision, revisionRead.CurrentCalculation, keyBase+0x02), server.capability))
	if err != nil || validated.Msg == nil || validated.Msg.Preparation == nil || validated.Msg.Validation == nil ||
		validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY ||
		len(validated.Msg.Validation.Findings) != 0 {
		t.Fatalf("validate newly created revision = %v, %v", validated, err)
	}
	readyRead := readBASFreezePreparation(t, server, actor, validated.Msg.Preparation)
	frozen, err := server.basPreparation.FreezeBasForManualHandoff(context.Background(), brandingRequest(
		makeBASFreezeRequest(server, actor, validated.Msg.Preparation, revisionRead.CurrentCalculation,
			validated.Msg.Validation, readyRead.CurrentChain, factor.AssertNext("bas_freeze_manual_handoff"), keyBase+0x03), server.capability))
	if err != nil || frozen.Msg == nil || frozen.Msg.Preparation == nil || frozen.Msg.FrozenBas == nil {
		t.Fatalf("freeze newly validated revision = %v, %v", frozen, err)
	}
	return frozen.Msg.Preparation, frozen.Msg.FrozenBas
}

func basRecoveryResolveDispute(t *testing.T, server documentPageServer,
	actor *tammyv1.AuthenticationContext, factor *basLifecycleTOTP, preparation *tammyv1.BasPreparation,
	outcome tammyv1.BasDisputeResolutionOutcome, reason string, key uint64,
) (*tammyv1.ResolveExternalBasDisputeResponse, error) {
	t.Helper()
	current := readBASFreezePreparation(t, server, actor, preparation)
	dispute := current.CurrentDispute
	if current.CurrentDisputeOverlay != tammyv1.BasDisputeOverlayState_BAS_DISPUTE_OVERLAY_STATE_DISPUTED || dispute == nil {
		t.Fatalf("resolve helper requires the exact current disputed history: %v", current)
	}
	request := &tammyv1.ResolveExternalBasDisputeRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			FreshFactor: factor.AssertNext("bas_resolve_external_dispute"), IdempotencyKey: testBASCommandKey(key)},
		OrganisationId: server.organisationID, ExpectedVersion: current.CurrentChain.ExpectedFamilyVersion,
		Chain: proto.Clone(current.CurrentChain).(*tammyv1.BasChainExpectation),
		Resolution: &tammyv1.BasExternalDisputeInput{
			Outcome: outcome, DisputedObservations: basRecoveryCloneRefs(dispute.DisputedObservations),
			AffectedConfirmedChain:     basRecoveryCloneRefs(dispute.AffectedConfirmedChain),
			DependentChainManifestHash: bytes.Clone(dispute.DependentChainManifestHash), Reason: reason,
		},
	}
	response, err := server.basPreparation.ResolveExternalBasDispute(context.Background(), brandingRequest(request, server.capability))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func basRecoveryGetPreparation(t *testing.T, server documentPageServer,
	actor *tammyv1.AuthenticationContext, exact *tammyv1.SourceRef,
) *tammyv1.BasPreparation {
	t.Helper()
	if exact == nil || exact.Type != "bas_preparation" || exact.Id == "" || exact.Revision == 0 || len(exact.ContentHash) != 32 {
		t.Fatalf("invalid exact preparation reference: %v", exact)
	}
	read, err := server.basPreparation.GetBasPreparation(context.Background(), brandingRequest(&tammyv1.GetBasPreparationRequest{
		Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext), OrganisationId: server.organisationID,
		PreparationId: exact.Id, PreparationHash: bytes.Clone(exact.ContentHash),
	}, server.capability))
	if err != nil || read.Msg == nil || read.Msg.Preparation == nil ||
		read.Msg.Preparation.Version != exact.Revision || !bytes.Equal(read.Msg.Preparation.ContentHash, exact.ContentHash) {
		t.Fatalf("read exact preparation %v = %v, %v", exact, read, err)
	}
	return read.Msg.Preparation
}

func basRecoveryCloneRefs(values []*tammyv1.SourceRef) []*tammyv1.SourceRef {
	if values == nil {
		return nil
	}
	result := make([]*tammyv1.SourceRef, len(values))
	for index, value := range values {
		if value != nil {
			result[index] = proto.Clone(value).(*tammyv1.SourceRef)
		}
	}
	return result
}
