package overview

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/proto"
)

const (
	overviewOrganisationID = "018f0000-0000-7000-8000-000000000001"
	overviewActorID        = "018f0000-0000-7000-8000-000000000002"
	overviewSessionID      = "018f0000-0000-7000-8000-000000000003"
)

type overviewAccess struct{ calls int }

func (access *overviewAccess) RequireRead(_ context.Context, authentication *tammyv1.AuthenticationContext) error {
	access.calls++
	if authentication.GetActorUserId() != overviewActorID || authentication.GetSessionId() != overviewSessionID {
		return ErrOverview
	}
	return nil
}

type overviewSnapshots struct {
	calls int
	value Snapshot
}

func (snapshots *overviewSnapshots) Attention(_ context.Context, organisationID string) (Snapshot, error) {
	snapshots.calls++
	if organisationID != overviewOrganisationID {
		return Snapshot{}, ErrOverview
	}
	return snapshots.value, nil
}

func TestAttentionSummaryUsesOneVerifiedSnapshot(t *testing.T) {
	access := &overviewAccess{}
	snapshots := &overviewSnapshots{value: Snapshot{
		DocumentsNeedingReview: 3,
		DocumentsReviewed:      12,
		BankingNeedingReview:   2,
		BankingUnreconciled:    5,
		DraftBASWorkpapers:     1,
		BASStatus:              tammyv1.BasAttentionStatus_BAS_ATTENTION_STATUS_DRAFT_NOT_LODGED,
		Revisions:              RevisionVector{Financial: 9, Ledger: 7, Settlement: 6, Banking: 5, TaxSource: 4, Organisation: 3, Rules: 2},
	}}
	service, err := NewService(ServiceConfig{Access: access, Snapshots: snapshots})
	if err != nil {
		t.Fatal(err)
	}
	request := connect.NewRequest(&tammyv1.GetAttentionSummaryRequest{
		Authentication: &tammyv1.AuthenticationContext{ActorUserId: overviewActorID, SessionId: overviewSessionID},
		OrganisationId: overviewOrganisationID,
		AsOfDate:       &tammyv1.CivilDate{Year: 2026, Month: 8, Day: 10},
		ReportingPeriod: &tammyv1.ReportingPeriod{
			StartDate: &tammyv1.CivilDate{Year: 2026, Month: 7, Day: 1},
			EndDate:   &tammyv1.CivilDate{Year: 2026, Month: 9, Day: 30},
		},
	})
	response, err := service.GetAttentionSummary(context.Background(), request)
	if err != nil {
		t.Fatalf("GetAttentionSummary() error = %v", err)
	}
	if access.calls != 1 || snapshots.calls != 1 {
		t.Fatalf("calls = access %d, snapshots %d; want one each", access.calls, snapshots.calls)
	}
	if response.Msg.DocumentsNeedingReview != 3 || response.Msg.DocumentsReviewedInPeriod != 12 ||
		response.Msg.BankingLinesNeedingReconciliation != 2 || response.Msg.BankingLinesUnreconciledInPeriod != 5 ||
		response.Msg.CurrentDraftBasWorkpapers != 1 || response.Msg.BasStatus != snapshots.value.BASStatus {
		t.Fatalf("GetAttentionSummary() = %#v", response.Msg)
	}
	if response.Msg.Revisions == nil || response.Msg.Revisions.FinancialRevision != 9 ||
		response.Msg.Revisions.LedgerRevision != 7 || response.Msg.Revisions.RuleBundleRevision != 2 {
		t.Fatalf("revisions = %#v", response.Msg.Revisions)
	}
	if !proto.Equal(response.Msg.AsOfDate, request.Msg.AsOfDate) ||
		!proto.Equal(response.Msg.ReportingPeriod, request.Msg.ReportingPeriod) ||
		response.Msg.AsOfDate == request.Msg.AsOfDate || response.Msg.ReportingPeriod == request.Msg.ReportingPeriod {
		t.Fatal("service did not echo owned immutable request dates")
	}
}
