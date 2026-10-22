//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBASExportEventsAndLiveCurrentnessPreserveExactFrozenSnapshotThroughRealTLS(t *testing.T) {
	server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
	ctx, actor := context.Background(), server.authentication
	addBusinessLodgerRolePreservingExistingRoles(t, server, actor, 0xc001)
	factor := newBASLifecycleTOTP(t, server, actor, basFreezeApprovalPassword, 0xc002)
	defer factor.Close()
	prep, calc, validation := prepareBASFreezeReadyNilStatement(t, server, 0xc100)
	frozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(makeBASFreezeRequest(server,
		actor, prep, calc, validation, readBASFreezeChain(t, server, actor, prep), factor.AssertNext("bas_freeze_manual_handoff"), 0xc003), server.capability))
	if err != nil {
		t.Fatal(err)
	}
	retained := proto.Clone(frozen.Msg.FrozenBas).(*tammyv1.BasFrozenBas)
	readFrozen := func(want tammyv1.BasLiveCurrentness) {
		t.Helper()
		read, err := server.basPreparation.GetFrozenBas(ctx, brandingRequest(&tammyv1.GetFrozenBasRequest{
			Authentication: actor, OrganisationId: server.organisationID, FrozenId: retained.FrozenId, FrozenHash: bytes.Clone(retained.ContentHash),
		}, server.capability))
		if err != nil || read.Msg.LiveCurrentness != want || !proto.Equal(read.Msg.FrozenBas, retained) {
			t.Fatalf("exact frozen read/currentness %v = %v, %v", want, read, err)
		}
	}
	readFrozen(tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_CURRENT)
	target := &tammyv1.BasExportTarget{ExactTarget: &tammyv1.BasExportTarget_FrozenBas{FrozenBas: proto.Clone(frozen.Msg.Preparation.FrozenBas).(*tammyv1.SourceRef)}}
	var version uint64
	for index, outcome := range []tammyv1.BasExportOutcome{tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_CANCELLED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_FAILED, tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_SUCCEEDED} {
		before := readBASFreezePreparation(t, server, actor, frozen.Msg.Preparation)
		request := &tammyv1.RecordBasExportEventRequest{CommandContext: &tammyv1.CommandContext{Authentication: actor, IdempotencyKey: testBASCommandKey(0xc010 + uint64(index)*4)}, OrganisationId: server.organisationID, ExpectedVersion: version,
			Event: &tammyv1.BasExportEventInput{Target: target, Format: tammyv1.BasExportFormat_BAS_EXPORT_FORMAT_CSV, Phase: tammyv1.BasExportPhase_BAS_EXPORT_PHASE_STARTED, OccurredAt: timestamppb.New(server.activation.Now()), Reason: "/private/local/path-must-not-be-retained.csv"}}
		started, err := server.basPreparation.RecordBasExportEvent(ctx, brandingRequest(request, server.capability))
		if err != nil || started.Msg.Event.Event.Reason != "Export attempt recorded" {
			t.Fatalf("start export=%v,%v", started, err)
		}
		counts := basReviewCommandCounts(t, server, prep.PreparationId)
		replay, err := server.basPreparation.RecordBasExportEvent(ctx, brandingRequest(request, server.capability))
		if err != nil || !proto.Equal(started.Msg, replay.Msg) {
			t.Fatalf("start replay=%v,%v", replay, err)
		}
		assertBASPreparationCounts(t, counts, basReviewCommandCounts(t, server, prep.PreparationId))
		changed := proto.Clone(request).(*tammyv1.RecordBasExportEventRequest)
		changed.Event.Format = tammyv1.BasExportFormat_BAS_EXPORT_FORMAT_PDF
		if _, err := server.basPreparation.RecordBasExportEvent(ctx, brandingRequest(changed, server.capability)); connect.CodeOf(err) != connect.CodeAlreadyExists {
			t.Fatalf("changed export idempotency payload=%v", err)
		}
		version++
		finished := proto.Clone(request).(*tammyv1.RecordBasExportEventRequest)
		finished.CommandContext.IdempotencyKey = testBASCommandKey(0xc011 + uint64(index)*4)
		finished.ExpectedVersion, finished.Event.Phase, finished.Event.Outcome = version, tammyv1.BasExportPhase_BAS_EXPORT_PHASE_FINISHED, &outcome
		if outcome == tammyv1.BasExportOutcome_BAS_EXPORT_OUTCOME_SUCCEEDED {
			digest := sha256.Sum256([]byte("independently rendered artifact"))
			finished.Event.ArtifactHash = digest[:]
		}
		completed, err := server.basPreparation.RecordBasExportEvent(ctx, brandingRequest(finished, server.capability))
		if err != nil || completed.Msg.Event.Event.GetOutcome() != outcome {
			t.Fatalf("finish export=%v,%v", completed, err)
		}
		version++
		after := readBASFreezePreparation(t, server, actor, frozen.Msg.Preparation)
		if !proto.Equal(before, after) {
			t.Fatalf("export altered retained preparation or live chain: before=%v after=%v", before, after)
		}
		readFrozen(tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_CURRENT)
	}
	page, err := server.basPreparation.ListBasExportEvents(ctx, brandingRequest(&tammyv1.ListBasExportEventsRequest{Authentication: actor, OrganisationId: server.organisationID, Target: target, Page: &tammyv1.PageRequest{PageSize: 200}}, server.capability))
	if err != nil || len(page.Msg.Events) != 6 {
		t.Fatalf("retained export history=%v,%v", page, err)
	}
	// A missing live source table must not make independently retained figures
	// inaccessible or misreport them as changed.
	if _, err := server.module.db.ExecContext(ctx, `ALTER TABLE invoices_v1 RENAME TO bas_test_unavailable_invoices`); err != nil {
		t.Fatal(err)
	}
	readFrozen(tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_UNAVAILABLE)
	if _, err := server.module.db.ExecContext(ctx, `ALTER TABLE bas_test_unavailable_invoices RENAME TO invoices_v1`); err != nil {
		t.Fatal(err)
	}
	readFrozen(tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_CURRENT)
	profile := localBASSupportedGSTProfileInput()
	profile.Gst.GstBasis = tammyv1.BasGstBasis_BAS_GST_BASIS_CASH
	profile.Reason = "Corrected current evidence after the retained handoff"
	if _, err := server.basPreparation.SetBasObligationsProfile(ctx, brandingRequest(&tammyv1.SetBasObligationsProfileRequest{CommandContext: &tammyv1.CommandContext{Authentication: actor, IdempotencyKey: testBASCommandKey(0xc030)}, OrganisationId: server.organisationID, ExpectedVersion: 1, Profile: profile}, server.capability)); err != nil {
		t.Fatal(err)
	}
	readFrozen(tammyv1.BasLiveCurrentness_BAS_LIVE_CURRENTNESS_SOURCE_CHANGED)
}
