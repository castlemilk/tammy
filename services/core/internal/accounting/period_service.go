package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrPeriodRepository = errors.New("accounting: period repository failure")
	ErrPeriodConflict   = errors.New("accounting: period conflict")
)

type PeriodRepository struct{ executor app.CommandSQLExecutor }

func NewPeriodRepository(executor app.CommandSQLExecutor) (*PeriodRepository, error) {
	if executor == nil {
		return nil, ErrPeriodRepository
	}
	return &PeriodRepository{executor: executor}, nil
}

func (repository *PeriodRepository) Close(ctx context.Context, organisationID string, endDate *tammyv1.CivilDate,
	expectedFinancialRevision uint64, periodID string, now time.Time,
) (*tammyv1.AccountingPeriod, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		!ids.IsCanonicalV7(periodID) || !validCivilDate(endDate) || expectedFinancialRevision == 0 || now.IsZero() {
		return nil, ErrPeriodRepository
	}
	revision, err := repository.financialRevision(ctx)
	if err != nil {
		return nil, err
	}
	if revision != expectedFinancialRevision {
		return nil, ErrFinancialRevision
	}
	closed, err := repository.hasClosedPeriod(ctx, organisationID)
	if err != nil {
		return nil, err
	}
	if closed {
		return nil, ErrPeriodConflict
	}
	startDate, err := repository.nextStartDate(ctx, organisationID, endDate)
	if err != nil {
		return nil, err
	}
	_, err = repository.executor.ExecContext(ctx, `
		INSERT INTO accounting_periods(id, organisation_id, start_date, end_date, state, version, closed_at, reopened_at)
		VALUES (?, ?, ?, ?, 'CLOSED', 1, ?, NULL)`, periodID, organisationID, startDate,
		civilDateString(endDate), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("%w: close: %v", ErrPeriodRepository, err)
	}
	period := &tammyv1.AccountingPeriod{Id: periodID, OrganisationId: organisationID, Version: 1,
		State: tammyv1.PeriodState_PERIOD_STATE_CLOSED, EndDate: cloneCivilDate(endDate), ClosedAt: timestamppb.New(now)}
	if ValidatePeriod(period) != nil {
		return nil, ErrPeriodRepository
	}
	return period, nil
}

func (repository *PeriodRepository) Reopen(ctx context.Context, periodID string, expectedVersion uint64,
	now time.Time,
) (*tammyv1.AccountingPeriod, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(periodID) || expectedVersion == 0 || now.IsZero() {
		return nil, ErrPeriodRepository
	}
	period, err := repository.get(ctx, periodID)
	if err != nil {
		return nil, err
	}
	if period.Version != expectedVersion || period.State != tammyv1.PeriodState_PERIOD_STATE_CLOSED {
		return nil, ErrPeriodConflict
	}
	reopened, err := ReopenPeriodProjection(period, now)
	if err != nil {
		return nil, err
	}
	result, err := repository.executor.ExecContext(ctx, `
		UPDATE accounting_periods SET state='OPEN', version=version+1, reopened_at=?
		WHERE id=? AND version=? AND state='CLOSED'`, now.UTC().Format(time.RFC3339Nano), periodID, expectedVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: reopen: %v", ErrPeriodRepository, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return nil, ErrPeriodConflict
	}
	return reopened, nil
}

func (repository *PeriodRepository) IsPostingDateOpen(ctx context.Context, organisationID string,
	date *tammyv1.CivilDate,
) (bool, error) {
	if repository == nil || repository.executor == nil || ctx == nil || !ids.IsCanonicalV7(organisationID) || !validCivilDate(date) {
		return false, ErrPeriodRepository
	}
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT count(*) FROM accounting_periods
		WHERE organisation_id=? AND state='CLOSED' AND end_date>=?`, organisationID, civilDateString(date))
	if err != nil {
		return false, ErrPeriodRepository
	}
	defer rows.Close()
	var count int
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		return false, ErrPeriodRepository
	}
	return count == 0, nil
}

func (repository *PeriodRepository) get(ctx context.Context, periodID string) (*tammyv1.AccountingPeriod, error) {
	rows, err := repository.executor.QueryContext(ctx, `
		SELECT id, organisation_id, version, state, end_date, closed_at, reopened_at
		FROM accounting_periods WHERE id=?`, periodID)
	if err != nil {
		return nil, ErrPeriodRepository
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrPeriodConflict
	}
	var period tammyv1.AccountingPeriod
	var state, endDate, closedAt string
	var reopenedAt sql.NullString
	if err := rows.Scan(&period.Id, &period.OrganisationId, &period.Version, &state, &endDate, &closedAt, &reopenedAt); err != nil || rows.Next() || rows.Err() != nil {
		return nil, ErrPeriodRepository
	}
	period.EndDate = parseCivilDate(endDate)
	closedInstant, err := time.Parse(time.RFC3339Nano, closedAt)
	if err != nil {
		return nil, ErrPeriodRepository
	}
	period.ClosedAt = timestamppb.New(closedInstant)
	if state == "CLOSED" {
		period.State = tammyv1.PeriodState_PERIOD_STATE_CLOSED
	} else if state == "OPEN" {
		period.State = tammyv1.PeriodState_PERIOD_STATE_OPEN
	}
	if reopenedAt.Valid {
		instant, err := time.Parse(time.RFC3339Nano, reopenedAt.String)
		if err != nil {
			return nil, ErrPeriodRepository
		}
		period.ReopenedAt = timestamppb.New(instant)
	}
	if ValidatePeriod(&period) != nil {
		return nil, ErrPeriodRepository
	}
	return &period, nil
}

func (repository *PeriodRepository) financialRevision(ctx context.Context) (uint64, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT financial_revision FROM financial_revisions WHERE id=1`)
	if err != nil {
		return 0, ErrPeriodRepository
	}
	defer rows.Close()
	var revision uint64
	if !rows.Next() || rows.Scan(&revision) != nil || rows.Next() || rows.Err() != nil {
		return 0, ErrPeriodRepository
	}
	return revision, nil
}

func (repository *PeriodRepository) hasClosedPeriod(ctx context.Context, organisationID string) (bool, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT count(*) FROM accounting_periods WHERE organisation_id=? AND state='CLOSED'`, organisationID)
	if err != nil {
		return false, ErrPeriodRepository
	}
	defer rows.Close()
	var count int
	if !rows.Next() || rows.Scan(&count) != nil || rows.Next() || rows.Err() != nil {
		return false, ErrPeriodRepository
	}
	return count != 0, nil
}

func (repository *PeriodRepository) nextStartDate(ctx context.Context, organisationID string, endDate *tammyv1.CivilDate) (string, error) {
	rows, err := repository.executor.QueryContext(ctx, `SELECT end_date FROM accounting_periods WHERE organisation_id=? ORDER BY end_date DESC LIMIT 1`, organisationID)
	if err != nil {
		return "", ErrPeriodRepository
	}
	defer rows.Close()
	start := time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)
	if rows.Next() {
		var previous string
		if rows.Scan(&previous) != nil || rows.Next() || rows.Err() != nil {
			return "", ErrPeriodRepository
		}
		parsed, err := time.Parse("2006-01-02", previous)
		if err != nil {
			return "", ErrPeriodRepository
		}
		start = parsed.AddDate(0, 0, 1)
	}
	end, _ := time.Parse("2006-01-02", civilDateString(endDate))
	if start.After(end) {
		return "", ErrPeriodConflict
	}
	return start.Format("2006-01-02"), nil
}

func cloneCivilDate(value *tammyv1.CivilDate) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: value.Year, Month: value.Month, Day: value.Day}
}
