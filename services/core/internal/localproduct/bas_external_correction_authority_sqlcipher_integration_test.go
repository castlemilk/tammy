//go:build tammy_sqlcipher && cgo && ((darwin && arm64) || (windows && amd64))

package localproduct

import (
	"bytes"
	"context"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBASExternalCorrectionRequiresBusinessLodgerThroughRealTLS(t *testing.T) {
	for index, role := range []tammyv1.Role{tammyv1.Role_ROLE_WORKSPACE_ADMIN, tammyv1.Role_ROLE_BUSINESS_PREPARER, tammyv1.Role_ROLE_AUDITOR, tammyv1.Role_ROLE_BUSINESS_LODGER} {
		t.Run(role.String(), func(t *testing.T) {
			server := newDocumentPageServerWithBasis(t, tammyv1.GstBasis_GST_BASIS_CASH)
			ctx := context.Background()
			addBusinessLodgerRolePreservingExistingRoles(t, server, server.authentication, 0x9d01)
			factor := newBASLifecycleTOTP(t, server, server.authentication, basFreezeApprovalPassword, 0x9d02)
			defer factor.Close()
			prep, calc, validation := prepareBASFreezeReadyNilStatement(t, server, 0x9e00)
			chain := readBASFreezeChain(t, server, server.authentication, prep)
			frozen, err := server.basPreparation.FreezeBasForManualHandoff(ctx, brandingRequest(makeBASFreezeRequest(server, server.authentication, prep, calc, validation, chain, factor.AssertNext("bas_freeze_manual_handoff"), 0x9d03), server.capability))
			if err != nil {
				t.Fatal(err)
			}
			chain = readBASFreezePreparation(t, server, server.authentication, frozen.Msg.Preparation).CurrentChain
			pending, err := server.basPreparation.RecordExternalBasObservation(ctx, brandingRequest(recordBASLifecycleObservationRequest(server, server.authentication, frozen.Msg.Preparation, chain, frozen.Msg.FrozenBas, tammyv1.BasExternalEventKind_BAS_EXTERNAL_EVENT_KIND_PENDING, nil, 0x9d04), server.capability))
			if err != nil {
				t.Fatal(err)
			}
			chain = readBASFreezePreparation(t, server, server.authentication, pending.Msg.Preparation).CurrentChain
			actor := server.userWithRole(t, role, 92+index)
			before := basReviewCommandCounts(t, server, prep.PreparationId)
			request := &tammyv1.CorrectExternalBasObservationRequest{CommandContext: &tammyv1.CommandContext{Authentication: actor, IdempotencyKey: testBASCommandKey(0x9d10 + uint64(index))}, OrganisationId: server.organisationID, ExpectedVersion: chain.ExpectedFamilyVersion, Chain: proto.Clone(chain).(*tammyv1.BasChainExpectation), FrozenId: frozen.Msg.FrozenBas.FrozenId, FrozenHash: bytes.Clone(frozen.Msg.FrozenBas.ContentHash), Correction: &tammyv1.BasExternalObservationCorrectionInput{
				DisputedObservation: &tammyv1.SourceRef{Type: "bas_external_observation", Id: pending.Msg.Observation.ObservationId, Revision: 1, ContentHash: bytes.Clone(pending.Msg.Observation.ContentHash)}, CorrectionKind: tammyv1.BasCorrectionKind_BAS_CORRECTION_KIND_REPLACE_ASSERTION, CorrectedClaimedEventAt: timestamppb.New(server.activation.Now()), Reason: "Correct the claimed portal event date without changing the retained history"}}
			response, err := server.basPreparation.CorrectExternalBasObservation(ctx, brandingRequest(request, server.capability))
			if role != tammyv1.Role_ROLE_BUSINESS_LODGER {
				if connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("%v correction=%v,%v; want permission denied", role, response, err)
				}
				assertBASPreparationCounts(t, before, basReviewCommandCounts(t, server, prep.PreparationId))
				return
			}
			if err != nil || response.Msg.Correction == nil || response.Msg.Correction.ActorUserId != actor.ActorUserId {
				t.Fatalf("lodger correction without a fresh purpose=%v,%v", response, err)
			}
		})
	}
}
