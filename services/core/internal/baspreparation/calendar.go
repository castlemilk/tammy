package baspreparation

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"buf.build/go/protovalidate"
	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

var (
	ErrCalendarInvalid          = errors.New("baspreparation: invalid civil reporting period")
	ErrCalendarUnsupported      = errors.New("baspreparation: unsupported reporting window")
	ErrPeriodUnconfirmed        = errors.New("baspreparation: external period or due-date authority unconfirmed")
	ErrIncomeYearMethodConflict = errors.New("baspreparation: PAYGI method conflicts with confirmed income year")
)

const DueDateRuleVersion = "au-bas-normal-dates-2026-10-10-v1"

type ReportingCadence string

const (
	ReportingCadenceMonthly   ReportingCadence = "MONTHLY"
	ReportingCadenceQuarterly ReportingCadence = "QUARTERLY"
	ReportingCadenceAnnual    ReportingCadence = "ANNUAL"
)

// ReportingPeriod supplies civil-date guidance. It cannot confer due-date authority.
type ReportingPeriod struct {
	Start, End, SuggestedDueDate *tammyv1.CivilDate
	Cadence                      ReportingCadence
	DueDateRuleVersion           string
}

type ConfirmedDueDateKind string

const (
	DueDateConfirmedNormal   ConfirmedDueDateKind = "CONFIRMED_NORMAL"
	DueDateConfirmedExternal ConfirmedDueDateKind = "CONFIRMED_EXTERNAL"
)

type PeriodConfirmation struct {
	Start, End, ConfirmedDueDate       *tammyv1.CivilDate
	DueDateKind                        ConfirmedDueDateKind
	DueDateEvidence                    *tammyv1.SourceRef
	RegistrationStart, RegistrationEnd *tammyv1.CivilDate
	ObligationCadences                 []ReportingCadence
	CadenceChanged                     bool
}

type ConfirmedReportingPeriod struct {
	ReportingPeriod
	ConfirmedDueDate *tammyv1.CivilDate
	DueDateKind      ConfirmedDueDateKind
	DueDateEvidence  *tammyv1.SourceRef
}

// CandidateReportingPeriods uses the civil calendar, independently of financial-year end.
// Nominal dates are not adjusted for weekends, holidays or inferred extensions.
func CandidateReportingPeriods(year int, cadence ReportingCadence) ([]ReportingPeriod, error) {
	if year < 1 || year > 9998 {
		return nil, ErrCalendarInvalid
	}
	months := 1
	if cadence == ReportingCadenceQuarterly {
		months = 3
	} else if cadence != ReportingCadenceMonthly {
		return nil, ErrCalendarUnsupported
	}
	periods := make([]ReportingPeriod, 0, 12/months)
	for month := 1; month <= 12; month += months {
		start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, months, 0).AddDate(0, 0, -1)
		due := time.Date(end.Year(), end.Month()+1, 21, 0, 0, 0, 0, time.UTC)
		if cadence == ReportingCadenceQuarterly {
			due = time.Date(end.Year(), end.Month()+1, 28, 0, 0, 0, 0, time.UTC)
			if end.Month() == time.December {
				due = time.Date(end.Year()+1, time.February, 28, 0, 0, 0, 0, time.UTC)
			}
		}
		periods = append(periods, ReportingPeriod{Start: civilDate(start), End: civilDate(end), SuggestedDueDate: civilDate(due), Cadence: cadence, DueDateRuleVersion: DueDateRuleVersion})
	}
	return periods, nil
}

func canonicalCalendarPeriod(period ReportingPeriod) bool {
	if !validCalendarDate(period.Start) || !validCalendarDate(period.End) || !validCalendarDate(period.SuggestedDueDate) || period.DueDateRuleVersion != DueDateRuleVersion {
		return false
	}
	periods, err := CandidateReportingPeriods(int(period.Start.Year), period.Cadence)
	if err != nil {
		return false
	}
	for _, candidate := range periods {
		if calendarKey(candidate.Start) == calendarKey(period.Start) && calendarKey(candidate.End) == calendarKey(period.End) && calendarKey(candidate.SuggestedDueDate) == calendarKey(period.SuggestedDueDate) {
			return true
		}
	}
	return false
}

// ConfirmReportingPeriod keeps independently evidenced authority separate from guidance.
func ConfirmReportingPeriod(period ReportingPeriod, confirmation PeriodConfirmation) (*ConfirmedReportingPeriod, error) {
	if !canonicalCalendarPeriod(period) || !validCalendarDate(confirmation.Start) || !validCalendarDate(confirmation.End) || calendarKey(period.Start) != calendarKey(confirmation.Start) || calendarKey(period.End) != calendarKey(confirmation.End) {
		return nil, ErrCalendarInvalid
	}
	if confirmation.ConfirmedDueDate == nil || confirmation.DueDateEvidence == nil || protovalidate.Validate(confirmation.DueDateEvidence) != nil {
		return nil, ErrPeriodUnconfirmed
	}
	if !validCalendarDate(confirmation.ConfirmedDueDate) || calendarKey(confirmation.ConfirmedDueDate) < calendarKey(period.End) {
		return nil, ErrCalendarInvalid
	}
	if confirmation.DueDateKind != DueDateConfirmedNormal && confirmation.DueDateKind != DueDateConfirmedExternal {
		return nil, ErrPeriodUnconfirmed
	}
	if confirmation.DueDateKind == DueDateConfirmedNormal && calendarKey(confirmation.ConfirmedDueDate) != calendarKey(period.SuggestedDueDate) {
		return nil, ErrPeriodUnconfirmed
	}
	if !validCalendarDate(confirmation.RegistrationStart) || (confirmation.RegistrationEnd != nil && !validCalendarDate(confirmation.RegistrationEnd)) {
		return nil, ErrPeriodUnconfirmed
	}
	if calendarKey(confirmation.RegistrationStart) > calendarKey(period.Start) || (confirmation.RegistrationEnd != nil && calendarKey(confirmation.RegistrationEnd) < calendarKey(period.End)) || confirmation.CadenceChanged {
		return nil, ErrCalendarUnsupported
	}
	if len(confirmation.ObligationCadences) == 0 {
		return nil, ErrPeriodUnconfirmed
	}
	for _, cadence := range confirmation.ObligationCadences {
		if cadence != period.Cadence {
			return nil, ErrCalendarUnsupported
		}
	}
	return &ConfirmedReportingPeriod{
		ReportingPeriod:  ReportingPeriod{Start: cloneCalendarDate(period.Start), End: cloneCalendarDate(period.End), SuggestedDueDate: cloneCalendarDate(period.SuggestedDueDate), Cadence: period.Cadence, DueDateRuleVersion: period.DueDateRuleVersion},
		ConfirmedDueDate: cloneCalendarDate(confirmation.ConfirmedDueDate), DueDateKind: confirmation.DueDateKind,
		DueDateEvidence: &tammyv1.SourceRef{Type: confirmation.DueDateEvidence.Type, Id: confirmation.DueDateEvidence.Id, Revision: confirmation.DueDateEvidence.Revision, ContentHash: append([]byte(nil), confirmation.DueDateEvidence.ContentHash...)},
	}, nil
}

type IncomeYearMethod string

const (
	IncomeYearMethodAmount IncomeYearMethod = "NOTIFIED_AMOUNT"
	IncomeYearMethodRate   IncomeYearMethod = "NOTIFIED_RATE"
)

// IncomeYearMethodPin identifies the externally confirmed income year, including
// substituted accounting periods. Organisation financial-year settings are not authority.
type IncomeYearMethodPin struct {
	IncomeYearID string
	Start, End   *tammyv1.CivilDate
	Confirmed    bool
	Method       IncomeYearMethod
}

func validIncomeYearPin(pin IncomeYearMethodPin) bool {
	return pin.Confirmed && pin.IncomeYearID != "" && validCalendarDate(pin.Start) && validCalendarDate(pin.End) && calendarKey(pin.Start) <= calendarKey(pin.End) && (pin.Method == IncomeYearMethodAmount || pin.Method == IncomeYearMethodRate)
}

func CheckIncomeYearMethod(previous *IncomeYearMethodPin, proposed IncomeYearMethodPin, period ReportingPeriod) error {
	if !validIncomeYearPin(proposed) {
		return ErrPeriodUnconfirmed
	}
	if !canonicalCalendarPeriod(period) {
		return ErrCalendarInvalid
	}
	if calendarKey(period.Start) < calendarKey(proposed.Start) || calendarKey(period.End) > calendarKey(proposed.End) {
		return ErrCalendarUnsupported
	}
	if previous == nil {
		return nil
	}
	if !validIncomeYearPin(*previous) {
		return ErrPeriodUnconfirmed
	}
	if previous.IncomeYearID == proposed.IncomeYearID {
		if calendarKey(previous.Start) != calendarKey(proposed.Start) || calendarKey(previous.End) != calendarKey(proposed.End) || previous.Method != proposed.Method {
			return ErrIncomeYearMethodConflict
		}
	} else if calendarKey(previous.Start) <= calendarKey(proposed.End) && calendarKey(proposed.Start) <= calendarKey(previous.End) {
		return ErrIncomeYearMethodConflict
	}
	return nil
}

func validCalendarDate(date *tammyv1.CivilDate) bool {
	if date == nil || date.Year < 1 || date.Year > 9999 || date.Month < 1 || date.Month > 12 || date.Day < 1 || date.Day > 31 {
		return false
	}
	value := time.Date(int(date.Year), time.Month(date.Month), int(date.Day), 0, 0, 0, 0, time.UTC)
	return value.Year() == int(date.Year) && value.Month() == time.Month(date.Month) && value.Day() == int(date.Day)
}

func civilDate(date time.Time) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: int32(date.Year()), Month: int32(date.Month()), Day: int32(date.Day())}
}

func cloneCalendarDate(date *tammyv1.CivilDate) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: date.Year, Month: date.Month, Day: date.Day}
}

func calendarKey(date *tammyv1.CivilDate) string {
	if date == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", date.Year, date.Month, date.Day)
}

const dueDateRuleMaterialV1 = "BAS-DUE-DATE-RULE-v1\n" +
	"MONTHLY: nominal due date is day 21 of the following civil month; no weekend or holiday adjustment.\n" +
	"QUARTERLY: nominal due date is day 28 of the following month after period end; December-quarter due date is February 28; no weekend or holiday adjustment.\n"

func DueDateRuleFingerprint() []byte {
	digest := sha256.Sum256([]byte(DueDateRuleVersion + "\x00" + dueDateRuleMaterialV1))
	return bytes.Clone(digest[:])
}
