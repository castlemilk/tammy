package baspreparation

import (
	"bytes"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

// validateBASPeriodGuidance verifies server-owned calendar guidance separately
// from the user's actual statement date. A nominal date never grants an extension.
func validateBASPeriodGuidance(input *tammyv1.BasPeriodConfirmationInput) error {
	if input == nil || input.Period == nil || !validCalendarDate(input.Period.StartDate) ||
		!validCalendarDate(input.Period.EndDate) || !validCalendarDate(input.ConfirmedDueDate) ||
		input.DueDateRuleVersion != DueDateRuleVersion || !bytes.Equal(input.DueDateRuleHash, DueDateRuleFingerprint()) ||
		calendarKey(input.ConfirmedDueDate) < calendarKey(input.Period.EndDate) {
		return ErrPeriodUnconfirmed
	}
	var cadence ReportingCadence
	if input.Window == tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_MONTHLY {
		cadence = ReportingCadenceMonthly
	} else if input.Window >= tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP && input.Window <= tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN {
		cadence = ReportingCadenceQuarterly
	} else {
		return ErrCalendarUnsupported
	}
	periods, err := CandidateReportingPeriods(int(input.Period.StartDate.Year), cadence)
	if err != nil {
		return err
	}
	for _, period := range periods {
		if calendarKey(period.Start) != calendarKey(input.Period.StartDate) || calendarKey(period.End) != calendarKey(input.Period.EndDate) {
			continue
		}
		if cadence == ReportingCadenceQuarterly {
			window := map[int32]tammyv1.BasPeriodWindow{7: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JUL_SEP,
				10: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_OCT_DEC, 1: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_JAN_MAR,
				4: tammyv1.BasPeriodWindow_BAS_PERIOD_WINDOW_APR_JUN}[period.Start.Month]
			if input.Window != window {
				return ErrCalendarInvalid
			}
		}
		if calendarKey(input.NominalDueDate) != calendarKey(period.SuggestedDueDate) {
			return ErrPeriodUnconfirmed
		}
		if input.DueDateKind == tammyv1.BasDueDateKind_BAS_DUE_DATE_KIND_NORMAL_SUGGESTION {
			if calendarKey(input.ConfirmedDueDate) != calendarKey(period.SuggestedDueDate) {
				return ErrPeriodUnconfirmed
			}
		} else if len(input.DueDateEvidence) == 0 {
			return ErrPeriodUnconfirmed
		}
		return nil
	}
	return ErrCalendarInvalid
}
