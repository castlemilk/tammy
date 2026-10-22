// Proposed new file: services/core/internal/overview/bas_projection_test.go
package overview

import (
	"bytes"
	"context"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

// This test uses the existing overviewAccess/overviewSnapshots test fixtures;
// overviewSnapshots.Attention must accept a ReportingPeriod and save a cloned
// receivedPeriod as noted in the integration patch.
func TestAttentionSummaryPassesExactPeriodAndCopiesBASRefs(t *testing.T) {
	access := &overviewAccess{}
	period := &tammyv1.ReportingPeriod{
		StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
		EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
	}
	preparation := &tammyv1.SourceRef{Type: "bas_preparation", Id: "018f0000-0000-7000-8000-000000000011", Revision: 4, ContentHash: bytes.Repeat([]byte{1}, 32)}
	family := &tammyv1.SourceRef{Type: "bas_family", Id: "018f0000-0000-7000-8000-000000000012", Revision: 1, ContentHash: bytes.Repeat([]byte{2}, 32)}
	snapshots := &overviewSnapshots{value: Snapshot{
		BASStatus:      tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_REVIEW_READY,
		BASPreparation: preparation, BASFamily: family,
	}}
	service, err := NewService(ServiceConfig{Access: access, Snapshots: snapshots})
	if err != nil {
		t.Fatal(err)
	}
	request := connect.NewRequest(&tammyv1.GetAttentionSummaryRequest{
		Authentication:  &tammyv1.AuthenticationContext{ActorUserId: overviewActorID, SessionId: overviewSessionID},
		OrganisationId:  overviewOrganisationID,
		AsOfDate:        &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
		ReportingPeriod: period,
	})
	response, err := service.GetAttentionSummary(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if snapshots.receivedPeriod == period || !proto.Equal(snapshots.receivedPeriod, period) {
		t.Fatalf("snapshot period = %#v; want an owned clone of exact request period %#v", snapshots.receivedPeriod, period)
	}
	if !proto.Equal(response.Msg.BasPreparation, preparation) || !proto.Equal(response.Msg.BasFamily, family) {
		t.Fatalf("BAS refs = (%v, %v), want exact projection", response.Msg.BasPreparation, response.Msg.BasFamily)
	}
	preparation.ContentHash[0] ^= 0xff
	family.ContentHash[0] ^= 0xff
	period.StartDate.Day = 2
	if response.Msg.BasPreparation.ContentHash[0] == preparation.ContentHash[0] ||
		response.Msg.BasFamily.ContentHash[0] == family.ContentHash[0] ||
		response.Msg.ReportingPeriod.StartDate.Day != 1 || snapshots.receivedPeriod.StartDate.Day != 1 {
		t.Fatal("overview response or projector input retained caller-owned BAS/date memory")
	}
}
