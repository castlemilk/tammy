package accounting

import (
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReopenPeriodProjectionRequiresClosedPredecessor(t *testing.T) {
	closedAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	period := &tammyv1.AccountingPeriod{Id: "018f0000-0000-7000-8000-000000000801",
		OrganisationId: "018f0000-0000-7000-8000-000000000020", Version: 1,
		State: tammyv1.PeriodState_PERIOD_STATE_CLOSED, EndDate: &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 30},
		ClosedAt: timestamppb.New(closedAt)}
	reopened, err := ReopenPeriodProjection(period, closedAt.Add(time.Hour))
	if err != nil || reopened.State != tammyv1.PeriodState_PERIOD_STATE_OPEN || reopened.Version != 2 || reopened.ReopenedAt == nil {
		t.Fatalf("ReopenPeriodProjection() = %#v, %v", reopened, err)
	}
	if period.State != tammyv1.PeriodState_PERIOD_STATE_CLOSED || period.ReopenedAt != nil {
		t.Fatal("predecessor mutated")
	}
	if _, err := ReopenPeriodProjection(reopened, closedAt.Add(2*time.Hour)); err == nil {
		t.Fatal("second reopen accepted")
	}
}
