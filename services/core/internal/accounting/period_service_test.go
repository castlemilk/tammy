package accounting

import (
	"context"
	"testing"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type periodStoreFake struct{ period *tammyv1.AccountingPeriod }

func (store *periodStoreFake) Close(_ context.Context, organisationID string, end *tammyv1.CivilDate, _ uint64,
	id string, now time.Time,
) (*tammyv1.AccountingPeriod, error) {
	store.period = &tammyv1.AccountingPeriod{Id: id, OrganisationId: organisationID, Version: 1,
		State: tammyv1.PeriodState_PERIOD_STATE_CLOSED, EndDate: cloneCivilDate(end), ClosedAt: timestamppb.New(now)}
	return clonePeriod(store.period), nil
}
func (store *periodStoreFake) Reopen(_ context.Context, _ string, _ uint64, now time.Time) (*tammyv1.AccountingPeriod, error) {
	reopened, err := ReopenPeriodProjection(store.period, now)
	store.period = reopened
	return reopened, err
}
func (store *periodStoreFake) IsPostingDateOpen(context.Context, string, *tammyv1.CivilDate) (bool, error) {
	return store.period == nil || store.period.State == tammyv1.PeriodState_PERIOD_STATE_OPEN, nil
}

type periodFactorFake struct{ purposes []string }

func (factor *periodFactorFake) Consume(_ context.Context, _ *tammyv1.FreshFactorContext, purpose string) error {
	factor.purposes = append(factor.purposes, purpose)
	return nil
}

type periodReportsFake struct{ calls int }

func (reports *periodReportsFake) RequirePeriodReopenAllowed(context.Context, string, string) error {
	reports.calls++
	return nil
}

func TestPeriodServiceRequiresFreshFactorAndRoutesCloseReopen(t *testing.T) {
	store, factor, reports := &periodStoreFake{}, &periodFactorFake{}, &periodReportsFake{}
	runner := &postingCommandRunner{repositories: CommandRepositories{Periods: store, Factors: factor, Reports: reports}}
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	service, err := NewPeriodService(runner, postingAuditFactory{}, func() time.Time { return now },
		func() string { return "018f0000-0000-7000-8000-000000000810" })
	if err != nil {
		t.Fatal(err)
	}
	contextValue := func(key string) *tammyv1.CommandContext {
		return &tammyv1.CommandContext{IdempotencyKey: key,
			Authentication: &tammyv1.AuthenticationContext{ActorUserId: "018f0000-0000-7000-8000-000000000811", SessionId: "018f0000-0000-7000-8000-000000000812"},
			FreshFactor:    &tammyv1.FreshFactorContext{AssertionId: "018f0000-0000-7000-8000-000000000813", Purpose: "factor", AssertedAt: timestamppb.New(now)}}
	}
	closed, err := service.ClosePeriod(context.Background(), &tammyv1.ClosePeriodRequest{CommandContext: contextValue("018f0000-0000-7000-8000-000000000814"),
		OrganisationId: "018f0000-0000-7000-8000-000000000020", EndDate: &tammyv1.CivilDate{Year: 2026, Month: 6, Day: 30}, ExpectedFinancialRevision: 1})
	if err != nil || closed.Period.State != tammyv1.PeriodState_PERIOD_STATE_CLOSED {
		t.Fatalf("ClosePeriod() = %#v, %v", closed, err)
	}
	now = now.Add(time.Hour)
	reopened, err := service.ReopenPeriod(context.Background(), &tammyv1.ReopenPeriodRequest{CommandContext: contextValue("018f0000-0000-7000-8000-000000000815"),
		PeriodId: closed.Period.Id, ExpectedVersion: 1, Reason: "Corrected late transaction"})
	if err != nil || reopened.Period.State != tammyv1.PeriodState_PERIOD_STATE_OPEN || len(factor.purposes) != 2 || reports.calls != 1 {
		t.Fatalf("ReopenPeriod() = %#v, %v factors=%v reports=%d", reopened, err, factor.purposes, reports.calls)
	}
}
