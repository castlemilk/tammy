package baspreparation

import (
	"bytes"
	"context"
	"errors"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
)

// Additions for profile_repository.go and period_repository.go respectively.
// They intentionally select at most two identity rows. The public profile
// request has no profile_id, so multiple profile identities for one
// organisation are ambiguous and fail closed.

func (repository *ProfileRepository) CurrentForOrganisation(ctx context.Context, organisationID string) (*tammyv1.BasObligationsProfile, *tammyv1.SourceRef, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) {
		return nil, nil, ErrProfileRepository
	}
	rows, err := repository.executor.QueryContext(ctx,
		`SELECT profile_id FROM bas_profiles_v1 WHERE organisation_id=? ORDER BY profile_id LIMIT 2`,
		organisationID)
	if err != nil {
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	var profileID string
	if !rows.Next() {
		err := errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, nil, errors.Join(ErrProfileRepository, err)
		}
		return nil, nil, nil
	}
	if err := rows.Scan(&profileID); err != nil {
		_ = rows.Close()
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	if rows.Next() {
		_ = rows.Close()
		return nil, nil, ErrProfileRepository
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	value, err := repository.GetCurrent(ctx, organisationID, profileID)
	if err != nil || value == nil || value.ProfileId != profileID || value.OrganisationId != organisationID {
		return nil, nil, errors.Join(ErrProfileRepository, err)
	}
	return value, &tammyv1.SourceRef{Type: profileSnapshotSpec.resource, Id: value.ProfileId,
		Revision: value.Revision, ContentHash: bytes.Clone(value.ContentHash)}, nil
}

// CurrentForScope resolves a period head by the complete stable civil range and
// reporting window. The schema's unique identity constraint permits at most one.
func (repository *PeriodRepository) CurrentForScope(ctx context.Context, organisationID string,
	confirmation *tammyv1.BasPeriodConfirmationInput) (*tammyv1.BasConfirmedPeriod, *tammyv1.SourceRef, error) {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || ctx == nil || ctx.Err() != nil ||
		!ids.IsCanonicalV7(organisationID) || confirmation == nil || confirmation.Period == nil {
		return nil, nil, ErrPeriodRepository
	}
	start, ok := basCivilDateSQLText(confirmation.Period.StartDate)
	if !ok {
		return nil, nil, ErrPeriodRepository
	}
	end, ok := basCivilDateSQLText(confirmation.Period.EndDate)
	if !ok {
		return nil, nil, ErrPeriodRepository
	}
	window, ok := basPeriodWindowSQL(confirmation.Window)
	if !ok {
		return nil, nil, ErrPeriodRepository
	}
	rows, err := repository.executor.QueryContext(ctx,
		`SELECT period_id FROM bas_periods_v1
		 WHERE organisation_id=? AND period_start=? AND period_end=? AND reporting_window=? LIMIT 2`,
		organisationID, start, end, window)
	if err != nil {
		return nil, nil, errors.Join(ErrPeriodRepository, err)
	}
	var periodID string
	if !rows.Next() {
		err := errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, nil, errors.Join(ErrPeriodRepository, err)
		}
		return nil, nil, nil
	}
	if err := rows.Scan(&periodID); err != nil {
		_ = rows.Close()
		return nil, nil, errors.Join(ErrPeriodRepository, err)
	}
	if rows.Next() {
		_ = rows.Close()
		return nil, nil, ErrPeriodRepository
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, nil, errors.Join(ErrPeriodRepository, err)
	}
	value, err := repository.GetCurrent(ctx, organisationID, periodID)
	if err != nil || value == nil || value.PeriodId != periodID || value.OrganisationId != organisationID {
		return nil, nil, errors.Join(ErrPeriodRepository, err)
	}
	return value, &tammyv1.SourceRef{Type: periodSnapshotSpec.resource, Id: value.PeriodId,
		Revision: value.Version, ContentHash: bytes.Clone(value.ContentHash)}, nil
}
