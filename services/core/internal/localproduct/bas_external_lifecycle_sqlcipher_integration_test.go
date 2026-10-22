//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"encoding/base32"
	"testing"
	"time"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/identity"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This test exercises the user-visible confirmation path across two statement
// attempts. PENDING is retained as an observation without moving the head;
// only a purpose-bound completed lodgement advances it. A later revision is
// regenerated, validated, frozen and confirmed while the old exact head stays
// readable by its original content hash.
func TestBASExternalLifecycleRevisionConfirmationThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx := context.Background()
	actor := server.authentication
	if user := addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0x9a01); user == nil {
		t.Fatal("assign business lodger role")
	}
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0x9a02)
	defer factor.Close()

	// Start with a fully supported, zero-valued source capture.
	preparation, calculation, validation := prepareBASFreezeReadyNilStatement(t, server, 0x9b00)
	rootChain := readBASFreezeChain(t, server, actor, preparation)
	freezeFactor := factor.AssertNext("bas_freeze_manual_handoff")
	frozenResponse, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(
		makeBASFreezeRequest(server, actor, preparation, calculation, validation, rootChain, freezeFactor, 0x9a11), server.capability))
	if err != nil || frozenResponse.Msg == nil || frozenResponse.Msg.FrozenBas == nil || frozenResponse.Msg.Preparation == nil {
		t.Fatalf("freeze initial supported statement = %v, %v", frozenResponse, err)
	}
	frozen := frozenResponse.Msg.FrozenBas
	frozenPrep := frozenResponse.Msg.Preparation

	// A pending portal report is durable history, but leaves the statement head
	// and attempt pointers exactly where they were.
	beforePendingRead := readBASFreezePreparation(t, server, actor, frozenPrep)
	pendingRequest := recordBASLifecycleObservationRequest(server, actor, frozenPrep,
		beforePendingRead.CurrentChain, frozen, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING,
		nil, 0x9a12)
	beforePendingCounts := basReviewCommandCounts(t, server, frozenPrep.PreparationId)
	pending, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(pendingRequest, server.capability))
	if err != nil || pending.Msg == nil || pending.Msg.Observation == nil || pending.Msg.Preparation == nil {
		t.Fatalf("record pending external event = %v, %v", pending, err)
	}
	if pending.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_FROZEN_FOR_MANUAL_HANDOFF {
		t.Fatalf("pending event changed attempt state to %v", pending.Msg.Preparation.State)
	}
	afterPendingRead := readBASFreezePreparation(t, server, actor, pending.Msg.Preparation)
	if !proto.Equal(beforePendingRead.CurrentChain.LatestHead, afterPendingRead.CurrentChain.LatestHead) ||
		!proto.Equal(beforePendingRead.CurrentChain.Branch, afterPendingRead.CurrentChain.Branch) ||
		!proto.Equal(beforePendingRead.CurrentChain.ActiveAttempt, afterPendingRead.CurrentChain.ActiveAttempt) {
		t.Fatalf("PENDING moved family pointers: before=%v after=%v", beforePendingRead.CurrentChain, afterPendingRead.CurrentChain)
	}
	pendingReplay, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(pendingRequest, server.capability))
	if err != nil || !proto.Equal(pending.Msg, pendingReplay.Msg) {
		t.Fatalf("exact PENDING observation replay = %v, %v", pendingReplay, err)
	}
	beforePendingCounts["audit_events_v1"]++
	beforePendingCounts["command_idempotency_v1"]++
	assertBASPreparationCounts(t, beforePendingCounts, basReviewCommandCounts(t, server, frozenPrep.PreparationId))

	// The same valid TOTP secret can issue adjacent, strictly increasing
	// counters. A freeze-purpose assertion is rejected for confirmation and
	// remains unconsumed; the following correctly scoped assertion succeeds.
	wrongPurpose := factor.AssertNext("bas_freeze_manual_handoff")
	wrongRequest := recordBASLifecycleObservationRequest(server, actor, pending.Msg.Preparation,
		afterPendingRead.CurrentChain, frozen, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
		wrongPurpose, 0x9a13)
	beforeWrongAudit, beforeWrongIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeWrongState := basReviewCommandCounts(t, server, frozenPrep.PreparationId)
	if _, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(wrongRequest, server.capability)); err == nil {
		t.Fatal("freeze-purpose TOTP assertion was accepted for external confirmation")
	}
	if basFactorConsumed(t, server, wrongPurpose.AssertionId) != 0 {
		t.Fatal("wrong-purpose rejection consumed the TOTP assertion")
	}
	assertBASApprovalCountsUnchanged(t, beforeWrongAudit, beforeWrongIdempotency, server)
	assertBASPreparationCounts(t, beforeWrongState, basReviewCommandCounts(t, server, frozenPrep.PreparationId))

	confirmFactor := factor.AssertNext("bas_record_external_confirmation")
	confirmRequest := recordBASLifecycleObservationRequest(server, actor, pending.Msg.Preparation,
		afterPendingRead.CurrentChain, frozen, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_COMPLETED_LODGEMENT,
		confirmFactor, 0x9a14)
	confirmed, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(confirmRequest, server.capability))
	if err != nil || confirmed.Msg == nil || confirmed.Msg.Observation == nil || confirmed.Msg.Preparation == nil ||
		confirmed.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
		t.Fatalf("record approved completed lodgement = %v, %v", confirmed, err)
	}
	if basFactorConsumed(t, server, confirmFactor.AssertionId) != 1 {
		t.Fatal("successful external confirmation did not consume its exact TOTP assertion")
	}
	firstConfirmedRead := readBASFreezePreparation(t, server, actor, confirmed.Msg.Preparation)
	firstHead := firstConfirmedRead.CurrentChain.LatestHead.GetRetained()
	if firstHead == nil || firstHead.Id != confirmed.Msg.Preparation.PreparationId ||
		firstHead.Revision != confirmed.Msg.Preparation.Version || !bytes.Equal(firstHead.ContentHash, confirmed.Msg.Preparation.ContentHash) {
		t.Fatalf("completed lodgement did not advance latest head to its exact preparation version: %v", firstConfirmedRead.CurrentChain)
	}
	beforeConfirmReplayAudit, beforeConfirmReplayIdempotency := basApprovalAuditAndIdempotencyCounts(t, server)
	beforeConfirmReplayState := basReviewCommandCounts(t, server, confirmed.Msg.Preparation.PreparationId)
	confirmReplay, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(confirmRequest, server.capability))
	if err != nil || !proto.Equal(confirmed.Msg, confirmReplay.Msg) {
		t.Fatalf("exact confirmation replay = %v, %v", confirmReplay, err)
	}
	assertBASApprovalCountsUnchanged(t, beforeConfirmReplayAudit, beforeConfirmReplayIdempotency, server)
	assertBASPreparationCounts(t, beforeConfirmReplayState, basReviewCommandCounts(t, server, confirmed.Msg.Preparation.PreparationId))

	// Open a revision against the exact current head and immutable root. The
	// revision builder captures current sources; the explicit regeneration call
	// below proves the revision's public command path is also usable directly.
	chain := firstConfirmedRead.CurrentChain
	if chain.RootPreparation == nil || chain.LatestHead.GetRetained() == nil {
		t.Fatalf("confirmed chain omitted exact root/head: %v", chain)
	}
	oldHead := proto.Clone(chain.LatestHead.GetRetained()).(*tammyv1.SourceRef)
	createRevision, err := server.basPreparation.CreateBasRevision(ctx, brandingRequest(&tammyv1.CreateBasRevisionRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(0x9a20)},
		OrganisationId: server.organisationID, ExpectedVersion: chain.ExpectedFamilyVersion,
		Chain:                            proto.Clone(chain).(*tammyv1.BasChainExpectation),
		EffectiveOriginalPreparationId:   firstHead.Id,
		EffectiveOriginalPreparationHash: bytes.Clone(firstHead.ContentHash),
		LatestConfirmedHeadId:            oldHead.Id, LatestConfirmedHeadHash: bytes.Clone(oldHead.ContentHash),
		Period:  proto.Clone(confirmed.Msg.Preparation.Period).(*tammyv1.SourceRef),
		Profile: proto.Clone(confirmed.Msg.Preparation.Profile).(*tammyv1.SourceRef),
		Reason:  "Correct the retained statement using a new exact source capture",
	}, server.capability))
	if err != nil || createRevision.Msg == nil || createRevision.Msg.Revision == nil || createRevision.Msg.Original == nil {
		t.Fatalf("create revision from exact confirmed head = %v, %v", createRevision, err)
	}
	revisionRead := readBASFreezePreparation(t, server, actor, createRevision.Msg.Revision)
	revision := revisionRead.Preparation
	if revision.CreationReason != "Correct the retained statement using a new exact source capture" {
		t.Fatalf("exact query lost revision creation reason: %q", revision.CreationReason)
	}
	if revision.Predecessor == nil || revision.Predecessor.Id != oldHead.Id || !bytes.Equal(revision.Predecessor.ContentHash, oldHead.ContentHash) ||
		revisionRead.CurrentCalculation == nil {
		t.Fatalf("new revision did not retain predecessor and calculated source state: %v", revisionRead)
	}
	familyVersion, prepVersion := revision.Family.Revision, revision.Version
	revisionRef := &tammyv1.SourceRef{Type: "bas_preparation", Id: revision.PreparationId,
		Revision: revision.Version, ContentHash: bytes.Clone(revision.ContentHash)}
	regenerated, err := server.basPreparation.RegenerateBasPreparation(ctx, brandingRequest(&tammyv1.RegenerateBasPreparationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(0x9a21)},
		OrganisationId: server.organisationID, ExpectedVersion: revision.Version,
		FamilyExpectation: &tammyv1.BasExpectedIdentity{
			ExpectedState:   &tammyv1.BasExpectedIdentity_Retained{Retained: proto.Clone(revision.Family).(*tammyv1.SourceRef)},
			ExpectedVersion: &familyVersion,
		},
		PreparationExpectation: &tammyv1.BasExpectedIdentity{
			ExpectedState:   &tammyv1.BasExpectedIdentity_Retained{Retained: revisionRef},
			ExpectedVersion: &prepVersion,
		},
		Profile: proto.Clone(revision.Profile).(*tammyv1.SourceRef),
		Period:  proto.Clone(revision.Period).(*tammyv1.SourceRef),
	}, server.capability))
	if err != nil || regenerated.Msg == nil || regenerated.Msg.Preparation == nil || regenerated.Msg.Calculation == nil {
		t.Fatalf("regenerate revision through public command = %v, %v", regenerated, err)
	}
	validated, err := server.basPreparation.ValidateBasPreparation(ctx, brandingRequest(
		validateBASReviewRequest(server, regenerated.Msg.Preparation, regenerated.Msg.Calculation, 0x9a22), server.capability))
	if err != nil || validated.Msg == nil || validated.Msg.Preparation == nil || validated.Msg.Validation == nil ||
		validated.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_REVIEW_READY || len(validated.Msg.Validation.Findings) != 0 {
		t.Fatalf("validate regenerated revision = %v, %v", validated, err)
	}

	currentRevisionChain := readBASFreezePreparation(t, server, actor, validated.Msg.Preparation).CurrentChain
	revisionFreezeFactor := factor.AssertNext("bas_freeze_manual_handoff")
	revisionFrozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(makeBASFreezeRequest(
		server, actor, validated.Msg.Preparation, regenerated.Msg.Calculation, validated.Msg.Validation,
		currentRevisionChain, revisionFreezeFactor, 0x9a23), server.capability))
	if err != nil || revisionFrozen.Msg == nil || revisionFrozen.Msg.FrozenBas == nil || revisionFrozen.Msg.Preparation == nil {
		t.Fatalf("freeze reviewed revision = %v, %v", revisionFrozen, err)
	}
	revisionPostFreezeRead := readBASFreezePreparation(t, server, actor, revisionFrozen.Msg.Preparation)
	revisionConfirmationFactor := factor.AssertNext("bas_record_external_confirmation")
	revisionConfirmationRequest := recordBASLifecycleObservationRequest(server, actor,
		revisionFrozen.Msg.Preparation, revisionPostFreezeRead.CurrentChain, revisionFrozen.Msg.FrozenBas,
		tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_ACCEPTANCE, revisionConfirmationFactor, 0x9a24)
	revisionConfirmed, err := server.basPreparation.RecordExternalBasObservation(ctx,
		brandingRequest(revisionConfirmationRequest, server.capability))
	if err != nil || revisionConfirmed.Msg == nil || revisionConfirmed.Msg.Preparation == nil ||
		revisionConfirmed.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
		t.Fatalf("confirm regenerated revision = %v, %v", revisionConfirmed, err)
	}
	if basFactorConsumed(t, server, revisionConfirmationFactor.AssertionId) != 1 {
		t.Fatal("revision confirmation did not consume its exact TOTP assertion")
	}
	finalRead := readBASFreezePreparation(t, server, actor, revisionConfirmed.Msg.Preparation)
	newHead := finalRead.CurrentChain.LatestHead.GetRetained()
	if newHead == nil || newHead.Id != revisionConfirmed.Msg.Preparation.PreparationId ||
		newHead.Id == oldHead.Id || finalRead.CurrentChain.RootPreparation.Id != chain.RootPreparation.Id ||
		!bytes.Equal(finalRead.CurrentChain.RootPreparation.ContentHash, chain.RootPreparation.ContentHash) {
		t.Fatalf("revision confirmation lost immutable root or did not advance to new head: %v", finalRead.CurrentChain)
	}
	oldHeadRead, err := server.basPreparation.GetBasPreparation(ctx, brandingRequest(&tammyv1.GetBasPreparationRequest{
		Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext), OrganisationId: server.organisationID,
		PreparationId: oldHead.Id, PreparationHash: bytes.Clone(oldHead.ContentHash),
	}, server.capability))
	if err != nil || oldHeadRead.Msg == nil || oldHeadRead.Msg.Preparation == nil ||
		oldHeadRead.Msg.Preparation.PreparationId != oldHead.Id || !bytes.Equal(oldHeadRead.Msg.Preparation.ContentHash, oldHead.ContentHash) {
		t.Fatalf("superseded confirmed head is no longer retrievable by exact original identity: %v, %v", oldHeadRead, err)
	}
	if oldHeadRead.Msg.Preparation.State != tammyv1.BasPreparationState_BAS_PREPARATION_STATE_EXTERNAL_CONFIRMATION_RECORDED {
		t.Fatalf("retained old exact head was rewritten: %v", oldHeadRead.Msg.Preparation)
	}
}

func recordBASLifecycleObservationRequest(server documentPageServer, actor *tammyv1.AuthenticationContext,
	preparation *tammyv1.BasPreparation, chain *tammyv1.BasChainExpectation, frozen *tammyv1.BasFrozenBas,
	kind tammyv1.BasExternalEventKind, factor *tammyv1.FreshFactorContext, key uint64,
) *tammyv1.RecordExternalBasObservationRequest {
	request := &tammyv1.RecordExternalBasObservationRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(key)},
		OrganisationId: server.organisationID, ExpectedVersion: chain.ExpectedFamilyVersion,
		Chain: proto.Clone(chain).(*tammyv1.BasChainExpectation), FrozenId: frozen.FrozenId,
		FrozenHash: bytes.Clone(frozen.ContentHash),
		Observation: &tammyv1.BasExternalObservationInput{EventKind: kind,
			Channel:           tammyv1.BasExternalChannel_BAS_EXTERNAL_CHANNEL_ONLINE_PORTAL,
			ClaimedEventAt:    timestamppb.New(server.activation.Now().UTC()),
			ExternalReference: basLifecycleStringPointer("ATO-PORTAL-" + testBASCommandKey(key)),
			Evidence: []*tammyv1.SourceRef{{Type: "ato_statement", Id: testBASCommandKey(key + 0x100),
				Revision: 1, ContentHash: bytes.Repeat([]byte{byte(key)}, 32)}},
			Reason: "Recorded against the exact frozen statement using the real external reporting channel"},
	}
	if factor != nil {
		request.CommandContext.FreshFactor = proto.Clone(factor).(*tammyv1.FreshFactorContext)
	}
	return request
}

func basLifecycleStringPointer(value string) *string { return &value }

type basLifecycleTOTP struct {
	server documentPageServer
	actor  *tammyv1.AuthenticationContext
	secret []byte
	factor string
}

func newBASLifecycleTOTP(t *testing.T, server documentPageServer, actor *tammyv1.AuthenticationContext,
	password string, key uint64,
) *basLifecycleTOTP {
	t.Helper()
	ctx := context.Background()
	enrolled, err := server.identity.EnrolTOTP(ctx, brandingRequest(&tammyv1.EnrolTOTPRequest{
		CommandContext: &tammyv1.CommandContext{Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext),
			IdempotencyKey: testBASCommandKey(key)}, CurrentPassword: &tammyv1.SecretInput{Utf8: []byte(password)},
	}, server.capability))
	if err != nil || enrolled.Msg == nil || enrolled.Msg.Factor == nil || enrolled.Msg.ProvisioningSecret == nil {
		t.Fatalf("enrol real BAS lifecycle TOTP = %v, %v", enrolled, err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(string(enrolled.Msg.ProvisioningSecret.Utf8))
	if err != nil {
		t.Fatalf("decode real BAS lifecycle TOTP secret: %v", err)
	}
	confirmedAt := server.activation.Now().UTC()
	confirmed, err := server.identity.ConfirmTOTP(ctx, brandingRequest(&tammyv1.ConfirmTOTPRequest{
		Authentication: proto.Clone(actor).(*tammyv1.AuthenticationContext), FactorId: enrolled.Msg.Factor.Id,
		Code: &tammyv1.TotpCodeInput{Value: identity.TOTPCode(secret, confirmedAt)},
	}, server.capability))
	if err != nil || confirmed.Msg == nil || confirmed.Msg.Factor == nil ||
		confirmed.Msg.Factor.State != tammyv1.FactorState_FACTOR_STATE_ENABLED {
		clear(secret)
		t.Fatalf("confirm real BAS lifecycle TOTP = %v, %v", confirmed, err)
	}
	return &basLifecycleTOTP{server: server, actor: proto.Clone(actor).(*tammyv1.AuthenticationContext),
		secret: secret, factor: enrolled.Msg.Factor.Id}
}

func (session *basLifecycleTOTP) Close() {
	if session != nil {
		clear(session.secret)
		session.secret = nil
	}
}

// AssertNext consumes strictly increasing TOTP counters from one enrollment.
// It waits only when the next non-replay counter lies beyond the verifier's
// one-step future window, then chooses the current or immediately-next step.
func (session *basLifecycleTOTP) AssertNext(purpose string) *tammyv1.FreshFactorContext {
	if session == nil || session.server.identity == nil || len(session.secret) < 16 || purpose == "" {
		panic("invalid BAS lifecycle TOTP session")
	}
	ctx := context.Background()
	var lastCounter int64
	if err := session.server.module.db.QueryRowContext(ctx,
		`SELECT last_counter FROM totp_factors WHERE id = ?`, session.factor).Scan(&lastCounter); err != nil {
		panic(err)
	}
	for {
		current := session.server.activation.Now().UTC().Unix() / 30
		if current >= lastCounter {
			counter := lastCounter + 1
			if current > counter {
				counter = current
			}
			instant := time.Unix(counter*30+1, 0).UTC()
			asserted, err := session.server.identity.AssertTOTP(ctx, brandingRequest(&tammyv1.AssertTOTPRequest{
				Authentication: proto.Clone(session.actor).(*tammyv1.AuthenticationContext),
				Code:           &tammyv1.TotpCodeInput{Value: identity.TOTPCode(session.secret, instant)}, Purpose: purpose,
			}, session.server.capability))
			if err != nil || asserted.Msg == nil || asserted.Msg.FreshFactor == nil || asserted.Msg.FreshFactor.Purpose != purpose {
				panic(connect.NewError(connect.CodeInternal, err))
			}
			return proto.Clone(asserted.Msg.FreshFactor).(*tammyv1.FreshFactorContext)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
