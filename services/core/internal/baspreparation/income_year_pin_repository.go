package baspreparation

import (
	"bytes"
	"context"
	"errors"
	"math"
	"time"

	"buf.build/go/protovalidate"
	"github.com/tammyapp/tammy/services/core/internal/app"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
	"github.com/tammyapp/tammy/services/core/internal/platform/ids"
	"github.com/tammyapp/tammy/services/core/internal/platform/paging"
)

var (
	ErrIncomeYearPinRepository = errors.New("baspreparation: income-year pin repository operation failed")
	ErrIncomeYearPinConflict   = errors.New("baspreparation: PAYGI method conflicts with the retained income-year pin")
)

type IncomeYearPinRepository struct {
	executor app.CommandSQLExecutor
	cursors  *paging.Codec
}

func NewIncomeYearPinRepository(executor app.CommandSQLExecutor, cursors *paging.Codec) (*IncomeYearPinRepository, error) {
	if nilBASRepositoryExecutor(executor) || cursors == nil {
		return nil, ErrIncomeYearPinRepository
	}
	return &IncomeYearPinRepository{executor: executor, cursors: cursors}, nil
}

type incomeYearPin struct {
	organisationID string
	yearID         string
	start          string
	end            string
	method         int64
	periodID       string
	periodVersion  int64
	periodHash     []byte
	profileID      string
	profileVersion int64
	profileHash    []byte
	pinnedAt       string
}

type incomeYearChoice struct {
	yearID string
	start  string
	end    string
	method tammyv1.BasPaygiMethod
}

func incomeYearChoiceFromProfile(input *tammyv1.BasObligationsProfileInput) (*incomeYearChoice, error) {
	if input == nil || input.Paygi == nil {
		return nil, ErrIncomeYearPinRepository
	}
	paygi := input.Paygi
	identity := paygi.ConfirmedIncomeYear
	if identity == nil {
		if paygi.Method == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT ||
			paygi.Method == tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE {
			return nil, ErrIncomeYearPinConflict
		}
		return nil, nil
	}
	if !ids.IsCanonicalV7(identity.IncomeYearId) || identity.Dates == nil ||
		identity.Confirmation == nil || protovalidate.Validate(identity.Confirmation) != nil || (paygi.Method != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT &&
		paygi.Method != tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE) {
		return nil, ErrIncomeYearPinConflict
	}
	start, ok := basCivilDateSQLText(identity.Dates.StartDate)
	if !ok {
		return nil, ErrIncomeYearPinConflict
	}
	end, ok := basCivilDateSQLText(identity.Dates.EndDate)
	if !ok || start > end {
		return nil, ErrIncomeYearPinConflict
	}
	return &incomeYearChoice{yearID: identity.IncomeYearId, start: start, end: end, method: paygi.Method}, nil
}

func periodSQLScope(period *tammyv1.BasConfirmedPeriod) (string, string, bool) {
	if period == nil || period.Confirmation == nil || period.Confirmation.Period == nil {
		return "", "", false
	}
	start, ok := basCivilDateSQLText(period.Confirmation.Period.StartDate)
	if !ok {
		return "", "", false
	}
	end, ok := basCivilDateSQLText(period.Confirmation.Period.EndDate)
	return start, end, ok && start <= end
}

func (choice *incomeYearChoice) containsPeriod(period *tammyv1.BasConfirmedPeriod) bool {
	start, end, ok := periodSQLScope(period)
	return choice != nil && ok && choice.start <= start && end <= choice.end
}

// PinAfterConfirmedPeriod must run after PeriodRepository.Append and before the
// caller-owned command transaction commits. It creates no durable state for a
// profile set before the first statement.
func (repository *IncomeYearPinRepository) PinAfterConfirmedPeriod(ctx context.Context, organisationID string,
	periodRef *tammyv1.SourceRef) error {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || repository.cursors == nil ||
		ctx == nil || !ids.IsCanonicalV7(organisationID) ||
		!validBASSourceRef(periodRef, "bas_period", "") {
		return ErrIncomeYearPinRepository
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrIncomeYearPinRepository, err)
	}
	period, profile, profileRef, err := repository.loadExactPeriodProfile(ctx, organisationID, periodRef)
	if err != nil {
		return err
	}
	choice, err := incomeYearChoiceFromProfile(profile.Profile)
	if err != nil {
		return err
	}
	periodStart, periodEnd, ok := periodSQLScope(period)
	if !ok {
		return ErrIncomeYearPinRepository
	}
	if choice != nil && !choice.containsPeriod(period) {
		return ErrIncomeYearPinConflict
	}

	// If any prior pin overlaps this actual statement, the selected profile
	// must continue to name the same exact income year and method. This also
	// prevents bypass by removing confirmed_income_year from a later profile.
	periodOverlaps, err := repository.overlapping(ctx, organisationID, periodStart, periodEnd)
	if err != nil {
		return err
	}
	if len(periodOverlaps) > 1 {
		return ErrIncomeYearPinRepository
	}
	if len(periodOverlaps) == 1 {
		prior := periodOverlaps[0]
		if choice == nil || !sameIncomeYearChoice(prior, choice) {
			return ErrIncomeYearPinConflict
		}
		if err := repository.verifyFirstProof(ctx, prior); err != nil {
			return err
		}
		return nil
	}
	if choice == nil {
		return nil
	}
	byID, err := repository.byYearID(ctx, organisationID, choice.yearID)
	if err != nil {
		return err
	}
	if len(byID) > 1 {
		return ErrIncomeYearPinRepository
	}
	if len(byID) == 1 {
		if !sameIncomeYearChoice(byID[0], choice) {
			return ErrIncomeYearPinConflict
		}
		// A matching identity must have been returned by the statement-range
		// overlap query above because the current period is inside that year.
		return ErrIncomeYearPinRepository
	}

	// A new pin is allowed only if no different year identity overlaps the
	// full externally confirmed date range. The SQL query is organization- and
	// range-scoped and returns at most two rows to fail closed on corrupt state.
	yearOverlaps, err := repository.overlapping(ctx, organisationID, choice.start, choice.end)
	if err != nil {
		return err
	}
	if len(yearOverlaps) != 0 {
		return ErrIncomeYearPinConflict
	}
	if !sameBASSourceRef(period.Confirmation.Profile, profileRef) || profile.OrganisationId != organisationID ||
		period.OrganisationId != organisationID || period.Version != periodRef.Revision || !choice.containsPeriod(period) {
		return ErrIncomeYearPinRepository
	}
	pinnedAt, ok := basTimestampSQLText(period.ConfirmedAt)
	if !ok {
		return ErrIncomeYearPinRepository
	}
	if _, err := repository.executor.ExecContext(ctx, `INSERT INTO bas_income_year_pins_v1(
		organisation_id,income_year_id,income_year_start,income_year_end,method,
		first_period_id,first_period_version,first_period_hash,first_profile_id,first_profile_version,first_profile_hash,pinned_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, organisationID, choice.yearID, choice.start, choice.end, int64(choice.method),
		period.PeriodId, int64(period.Version), bytes.Clone(period.ContentHash), profile.ProfileId, int64(profile.Revision),
		bytes.Clone(profile.ContentHash), pinnedAt); err != nil {
		return errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
	}
	return nil
}

// CheckProfileCompatible is read-only. Set may correct the selected method
// until the first confirmation creates a pin; afterward it cannot introduce a
// different method/year for dates governed by that pin.
func (repository *IncomeYearPinRepository) CheckProfileCompatible(ctx context.Context, organisationID string,
	input *tammyv1.BasObligationsProfileInput) error {
	if repository == nil || nilBASRepositoryExecutor(repository.executor) || repository.cursors == nil ||
		ctx == nil || !ids.IsCanonicalV7(organisationID) || input == nil || input.Paygi == nil {
		return ErrIncomeYearPinRepository
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrIncomeYearPinRepository, err)
	}
	choice, err := incomeYearChoiceFromProfile(input)
	if err != nil {
		return err
	}
	if choice != nil {
		byID, err := repository.byYearID(ctx, organisationID, choice.yearID)
		if err != nil {
			return err
		}
		if len(byID) > 1 {
			return ErrIncomeYearPinRepository
		}
		if len(byID) == 1 {
			if !sameIncomeYearChoice(byID[0], choice) {
				return ErrIncomeYearPinConflict
			}
			if err := repository.verifyFirstProof(ctx, byID[0]); err != nil {
				return err
			}
		}
	}
	if choice != nil {
		yearOverlaps, err := repository.overlapping(ctx, organisationID, choice.start, choice.end)
		if err != nil {
			return err
		}
		if len(yearOverlaps) > 1 {
			return ErrIncomeYearPinRepository
		}
		for _, prior := range yearOverlaps {
			if !sameIncomeYearChoice(prior, choice) {
				return ErrIncomeYearPinConflict
			}
			if err := repository.verifyFirstProof(ctx, prior); err != nil {
				return err
			}
		}
	}
	start, end, ok := basProfileIntervalSQLDates(input.EffectiveInterval)
	if !ok {
		return ErrIncomeYearPinRepository
	}
	overlaps, err := repository.overlapping(ctx, organisationID, start, end)
	if err != nil {
		return err
	}
	if len(overlaps) > 1 {
		return ErrIncomeYearPinRepository
	}
	if len(overlaps) == 0 {
		return nil
	}
	prior := overlaps[0]
	if choice == nil || !sameIncomeYearChoice(prior, choice) {
		return ErrIncomeYearPinConflict
	}
	if err := repository.verifyFirstProof(ctx, prior); err != nil {
		return err
	}
	return nil
}

func sameIncomeYearChoice(prior incomeYearPin, choice *incomeYearChoice) bool {
	return choice != nil && prior.yearID == choice.yearID && prior.start == choice.start && prior.end == choice.end &&
		prior.method == int64(choice.method)
}

func (repository *IncomeYearPinRepository) overlapping(ctx context.Context, organisationID, start, end string) ([]incomeYearPin, error) {
	return repository.readPins(ctx, `SELECT organisation_id,income_year_id,income_year_start,income_year_end,method,
		first_period_id,first_period_version,first_period_hash,first_profile_id,first_profile_version,first_profile_hash,pinned_at
		FROM bas_income_year_pins_v1
		WHERE organisation_id=? AND income_year_start<=? AND income_year_end>=?
		ORDER BY income_year_start,income_year_id LIMIT 2`, organisationID, end, start)
}

func (repository *IncomeYearPinRepository) byYearID(ctx context.Context, organisationID, yearID string) ([]incomeYearPin, error) {
	return repository.readPins(ctx, `SELECT organisation_id,income_year_id,income_year_start,income_year_end,method,
		first_period_id,first_period_version,first_period_hash,first_profile_id,first_profile_version,first_profile_hash,pinned_at
		FROM bas_income_year_pins_v1 WHERE organisation_id=? AND income_year_id=? LIMIT 2`, organisationID, yearID)
}

func (repository *IncomeYearPinRepository) readPins(ctx context.Context, query string, args ...any) ([]incomeYearPin, error) {
	rows, err := repository.executor.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
	}
	defer rows.Close()
	pins := make([]incomeYearPin, 0, 2)
	for rows.Next() {
		var pin incomeYearPin
		if err := rows.Scan(&pin.organisationID, &pin.yearID, &pin.start, &pin.end, &pin.method,
			&pin.periodID, &pin.periodVersion, &pin.periodHash, &pin.profileID, &pin.profileVersion, &pin.profileHash, &pin.pinnedAt); err != nil {
			return nil, errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
		}
		if !validIncomeYearPinRecord(pin) {
			return nil, ErrIncomeYearPinRepository
		}
		pins = append(pins, pin)
	}
	if err, closeErr := rows.Err(), rows.Close(); err != nil || closeErr != nil || ctx.Err() != nil {
		return nil, errors.Join(ErrIncomeYearPinRepository, err, closeErr, ctx.Err())
	}
	return pins, nil
}

func validIncomeYearPinRecord(pin incomeYearPin) bool {
	return ids.IsCanonicalV7(pin.organisationID) && ids.IsCanonicalV7(pin.yearID) &&
		validBASSQLDate(pin.start) && validBASSQLDate(pin.end) && pin.start <= pin.end &&
		(pin.method == int64(tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_AMOUNT) ||
			pin.method == int64(tammyv1.BasPaygiMethod_BAS_PAYGI_METHOD_RATE)) &&
		ids.IsCanonicalV7(pin.periodID) && pin.periodVersion > 0 && pin.periodVersion <= math.MaxInt64 && len(pin.periodHash) == 32 &&
		ids.IsCanonicalV7(pin.profileID) && pin.profileVersion > 0 && pin.profileVersion <= math.MaxInt64 && len(pin.profileHash) == 32 &&
		pin.pinnedAt != ""
}

func validBASSQLDate(value string) bool {
	date, err := time.Parse("2006-01-02", value)
	return err == nil && date.Year() >= 1 && date.Format("2006-01-02") == value
}

func (repository *IncomeYearPinRepository) loadExactPeriodProfile(ctx context.Context, organisationID string,
	periodRef *tammyv1.SourceRef) (*tammyv1.BasConfirmedPeriod, *tammyv1.BasObligationsProfile,
	*tammyv1.SourceRef, error) {
	periods, err := NewPeriodRepository(repository.executor, repository.cursors)
	if err != nil {
		return nil, nil, nil, errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
	}
	period, err := periods.GetExact(ctx, organisationID, periodRef)
	if err != nil || period == nil || period.OrganisationId != organisationID || period.Version != periodRef.Revision ||
		!bytes.Equal(period.ContentHash, periodRef.ContentHash) || !validBASSourceRef(period.Confirmation.GetProfile(), "bas_obligations_profile", "") {
		return nil, nil, nil, errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
	}
	profileRef := period.Confirmation.Profile
	profiles, err := NewProfileRepository(repository.executor, repository.cursors)
	if err != nil {
		return nil, nil, nil, errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
	}
	profile, err := profiles.GetExact(ctx, organisationID, profileRef)
	if err != nil || profile == nil || profile.OrganisationId != organisationID ||
		profile.ProfileId != profileRef.Id || profile.Revision != profileRef.Revision || !bytes.Equal(profile.ContentHash, profileRef.ContentHash) ||
		!basProfileIntervalCoversPeriod(profile.Profile.GetEffectiveInterval(), period.Confirmation.Period) {
		return nil, nil, nil, errors.Join(ErrIncomeYearPinRepository, err, ctx.Err())
	}
	return period, profile, profileRef, nil
}

func (repository *IncomeYearPinRepository) verifyFirstProof(ctx context.Context, pin incomeYearPin) error {
	if !validIncomeYearPinRecord(pin) {
		return ErrIncomeYearPinRepository
	}
	periodRef := &tammyv1.SourceRef{Type: "bas_period", Id: pin.periodID, Revision: uint64(pin.periodVersion), ContentHash: bytes.Clone(pin.periodHash)}
	period, profile, profileRef, err := repository.loadExactPeriodProfile(ctx, pin.organisationID, periodRef)
	if err != nil {
		return err
	}
	pinnedAt, validPinnedAt := basTimestampSQLText(period.ConfirmedAt)
	if !validPinnedAt || pin.pinnedAt != pinnedAt || pin.profileID != profileRef.Id ||
		pin.profileVersion != int64(profileRef.Revision) || !bytes.Equal(pin.profileHash, profileRef.ContentHash) {
		return ErrIncomeYearPinRepository
	}
	choice, err := incomeYearChoiceFromProfile(profile.Profile)
	if err != nil || choice == nil || choice.yearID != pin.yearID || choice.start != pin.start || choice.end != pin.end ||
		int64(choice.method) != pin.method || !sameBASSourceRef(period.Confirmation.Profile, profileRef) ||
		!choice.containsPeriod(period) {
		return ErrIncomeYearPinRepository
	}
	return nil
}
