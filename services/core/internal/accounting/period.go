package accounting

import (
	"context"
	"errors"
	"time"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrInvalidPeriod = errors.New("accounting: invalid period")
	ErrClosedPeriod  = errors.New("accounting: posting date is closed")
)

type PeriodStore interface {
	Close(context.Context, string, *tammyv1.CivilDate, uint64, string, time.Time) (*tammyv1.AccountingPeriod, error)
	Reopen(context.Context, string, uint64, time.Time) (*tammyv1.AccountingPeriod, error)
	IsPostingDateOpen(context.Context, string, *tammyv1.CivilDate) (bool, error)
}

func ValidatePeriod(period *tammyv1.AccountingPeriod) error {
	if period == nil || !ids.IsCanonicalV7(period.Id) || !ids.IsCanonicalV7(period.OrganisationId) ||
		period.Version == 0 || !validCivilDate(period.EndDate) || period.ClosedAt == nil || period.ClosedAt.CheckValid() != nil {
		return ErrInvalidPeriod
	}
	switch period.State {
	case tammyv1.PeriodState_PERIOD_STATE_CLOSED:
		if period.ReopenedAt != nil {
			return ErrInvalidPeriod
		}
	case tammyv1.PeriodState_PERIOD_STATE_OPEN:
		if period.ReopenedAt == nil || period.ReopenedAt.CheckValid() != nil || period.ReopenedAt.AsTime().Before(period.ClosedAt.AsTime()) {
			return ErrInvalidPeriod
		}
	default:
		return ErrInvalidPeriod
	}
	return nil
}

func ReopenPeriodProjection(period *tammyv1.AccountingPeriod, now time.Time) (*tammyv1.AccountingPeriod, error) {
	if ValidatePeriod(period) != nil || period.State != tammyv1.PeriodState_PERIOD_STATE_CLOSED || now.IsZero() || now.Before(period.ClosedAt.AsTime()) {
		return nil, ErrInvalidPeriod
	}
	copy := clonePeriod(period)
	copy.State = tammyv1.PeriodState_PERIOD_STATE_OPEN
	copy.Version++
	copy.ReopenedAt = timestamppb.New(now)
	return copy, nil
}

func clonePeriod(value *tammyv1.AccountingPeriod) *tammyv1.AccountingPeriod {
	return proto.Clone(value).(*tammyv1.AccountingPeriod)
}
