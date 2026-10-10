package baspreparation

import (
	"errors"
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func calendarDate(y, m, d int32) *tammyv1.CivilDate {
	return &tammyv1.CivilDate{Year: y, Month: m, Day: d}
}

func TestCalendarUsesCivilMonthsAndFixedAustralianGSTQuarters(t *testing.T) {
	months, err := CandidateReportingPeriods(2028, ReportingCadenceMonthly)
	if err != nil || len(months) != 12 {
		t.Fatalf("monthly calendar: %v, %d periods", err, len(months))
	}
	if got := months[1]; calendarKey(got.Start) != "2028-02-01" || calendarKey(got.End) != "2028-02-29" || calendarKey(got.SuggestedDueDate) != "2028-03-21" {
		t.Fatalf("leap month: %+v", got)
	}
	if calendarKey(months[11].SuggestedDueDate) != "2029-01-21" {
		t.Fatal("December due date did not cross the civil year")
	}
	quarters, err := CandidateReportingPeriods(2026, ReportingCadenceQuarterly)
	if err != nil || len(quarters) != 4 {
		t.Fatalf("quarter calendar: %v", err)
	}
	want := [][3]string{{"2026-01-01", "2026-03-31", "2026-04-28"}, {"2026-04-01", "2026-06-30", "2026-07-28"}, {"2026-07-01", "2026-09-30", "2026-10-28"}, {"2026-10-01", "2026-12-31", "2027-02-28"}}
	for i, period := range quarters {
		if got := [3]string{calendarKey(period.Start), calendarKey(period.End), calendarKey(period.SuggestedDueDate)}; got != want[i] || period.DueDateRuleVersion == "" {
			t.Fatalf("quarter %d: %v want %v", i, got, want[i])
		}
	}
	// There is deliberately no financial-year-end input: GST quarters stay fixed.
	if _, err := CandidateReportingPeriods(2026, ReportingCadenceAnnual); !errors.Is(err, ErrCalendarUnsupported) {
		t.Fatalf("annual calendar should be unsupported: %v", err)
	}
	for _, year := range []int{0, 9999} {
		if _, err := CandidateReportingPeriods(year, ReportingCadenceMonthly); !errors.Is(err, ErrCalendarInvalid) {
			t.Fatalf("unsafe year %d: %v", year, err)
		}
	}
}

func calendarConfirmation() PeriodConfirmation {
	return PeriodConfirmation{
		Start: calendarDate(2026, 7, 1), End: calendarDate(2026, 9, 30),
		ConfirmedDueDate: calendarDate(2026, 10, 28), DueDateKind: DueDateConfirmedNormal,
		DueDateEvidence:    &tammyv1.SourceRef{Type: "reviewed_document", Id: "018f0000-0000-7000-8000-000000000001", Revision: 1, ContentHash: make([]byte, 32)},
		RegistrationStart:  calendarDate(2020, 1, 1),
		ObligationCadences: []ReportingCadence{ReportingCadenceQuarterly, ReportingCadenceQuarterly},
	}
}

func TestCalendarSuggestedDateCannotBecomeConfirmedAuthority(t *testing.T) {
	periods, _ := CandidateReportingPeriods(2026, ReportingCadenceQuarterly)
	period := periods[2]
	confirmation := calendarConfirmation()
	confirmation.ConfirmedDueDate = nil
	if _, err := ConfirmReportingPeriod(period, confirmation); !errors.Is(err, ErrPeriodUnconfirmed) {
		t.Fatalf("guidance supplied authority: %v", err)
	}
	confirmation = calendarConfirmation()
	confirmation.DueDateEvidence = nil
	if _, err := ConfirmReportingPeriod(period, confirmation); !errors.Is(err, ErrPeriodUnconfirmed) {
		t.Fatalf("missing due-date evidence was accepted: %v", err)
	}
	confirmation = calendarConfirmation()
	confirmation.ConfirmedDueDate = calendarDate(2026, 11, 25)
	if _, err := ConfirmReportingPeriod(period, confirmation); !errors.Is(err, ErrPeriodUnconfirmed) {
		t.Fatalf("extension inferred as normal date: %v", err)
	}
	confirmation.DueDateKind = DueDateConfirmedExternal
	confirmed, err := ConfirmReportingPeriod(period, confirmation)
	if err != nil || calendarKey(confirmed.ConfirmedDueDate) != "2026-11-25" || calendarKey(confirmed.SuggestedDueDate) != "2026-10-28" {
		t.Fatalf("explicit external date not kept separately: %+v %v", confirmed, err)
	}
	confirmation.ConfirmedDueDate.Day = 1
	confirmation.DueDateEvidence.ContentHash[0] = 9
	period.Start.Day = 2
	if calendarKey(confirmed.ConfirmedDueDate) != "2026-11-25" {
		t.Fatal("caller mutation changed retained confirmation")
	}
	if calendarKey(confirmed.Start) != "2026-07-01" || confirmed.DueDateEvidence.ContentHash[0] != 0 {
		t.Fatal("caller mutation changed retained period or evidence")
	}
	// February 28, 2027 is Sunday: normal guidance remains the nominal date.
	if calendarKey(periods[3].SuggestedDueDate) != "2027-02-28" {
		t.Fatal("calendar inferred a weekend extension")
	}
}

func TestCalendarRejectsUnsupportedPeriodScope(t *testing.T) {
	periods, _ := CandidateReportingPeriods(2026, ReportingCadenceQuarterly)
	for name, change := range map[string]func(*PeriodConfirmation){
		"registration part-period": func(c *PeriodConfirmation) { c.RegistrationStart = calendarDate(2026, 8, 1) },
		"registration ceased":      func(c *PeriodConfirmation) { c.RegistrationEnd = calendarDate(2026, 9, 1) },
		"cadence change":           func(c *PeriodConfirmation) { c.CadenceChanged = true },
		"mixed windows": func(c *PeriodConfirmation) {
			c.ObligationCadences = append(c.ObligationCadences, ReportingCadenceMonthly)
		},
		"annual obligation": func(c *PeriodConfirmation) { c.ObligationCadences = []ReportingCadence{ReportingCadenceAnnual} },
	} {
		t.Run(name, func(t *testing.T) {
			c := calendarConfirmation()
			change(&c)
			if _, err := ConfirmReportingPeriod(periods[2], c); !errors.Is(err, ErrCalendarUnsupported) {
				t.Fatalf("accepted unsupported scope: %v", err)
			}
		})
	}
	c := calendarConfirmation()
	c.Start = calendarDate(2026, 7, 2)
	if _, err := ConfirmReportingPeriod(periods[2], c); !errors.Is(err, ErrCalendarInvalid) {
		t.Fatalf("noncanonical actual period accepted: %v", err)
	}
	c = calendarConfirmation()
	c.ConfirmedDueDate = calendarDate(2026, 2, 30)
	if _, err := ConfirmReportingPeriod(periods[2], c); !errors.Is(err, ErrCalendarInvalid) {
		t.Fatalf("invalid civil date accepted: %v", err)
	}
}

func TestCalendarRejectsMalformedDueDateEvidenceAndForgedGuidance(t *testing.T) {
	periods, _ := CandidateReportingPeriods(2026, ReportingCadenceQuarterly)
	for name, change := range map[string]func(*tammyv1.SourceRef){
		"missing type":     func(s *tammyv1.SourceRef) { s.Type = "" },
		"bad identifier":   func(s *tammyv1.SourceRef) { s.Id = "not-a-source" },
		"missing revision": func(s *tammyv1.SourceRef) { s.Revision = 0 },
		"bad hash":         func(s *tammyv1.SourceRef) { s.ContentHash = []byte{1} },
	} {
		t.Run(name, func(t *testing.T) {
			c := calendarConfirmation()
			change(c.DueDateEvidence)
			if _, err := ConfirmReportingPeriod(periods[2], c); !errors.Is(err, ErrPeriodUnconfirmed) {
				t.Fatalf("malformed evidence accepted: %v", err)
			}
		})
	}
	periods[2].SuggestedDueDate = calendarDate(2026, 11, 25)
	if _, err := ConfirmReportingPeriod(periods[2], calendarConfirmation()); !errors.Is(err, ErrCalendarInvalid) {
		t.Fatalf("forged guidance accepted: %v", err)
	}
}

func TestPAYGIMethodIsPinnedToConfirmedIncomeYear(t *testing.T) {
	periods, _ := CandidateReportingPeriods(2026, ReportingCadenceQuarterly)
	initial := IncomeYearMethodPin{IncomeYearID: "income-year-2026", Start: calendarDate(2026, 4, 1), End: calendarDate(2027, 3, 31), Confirmed: true, Method: IncomeYearMethodAmount}
	if err := CheckIncomeYearMethod(nil, initial, periods[2]); err != nil {
		t.Fatalf("confirmed substituted income year rejected: %v", err)
	}
	changed := initial
	changed.Method = IncomeYearMethodRate
	if err := CheckIncomeYearMethod(&initial, changed, periods[3]); !errors.Is(err, ErrIncomeYearMethodConflict) {
		t.Fatalf("new GST quarter reset method: %v", err)
	}
	changed = initial
	changed.IncomeYearID = "invented-new-year"
	if err := CheckIncomeYearMethod(&initial, changed, periods[3]); !errors.Is(err, ErrIncomeYearMethodConflict) {
		t.Fatalf("renamed overlapping year reset method: %v", err)
	}
	changed = initial
	changed.Confirmed = false
	if err := CheckIncomeYearMethod(nil, changed, periods[2]); !errors.Is(err, ErrPeriodUnconfirmed) {
		t.Fatalf("unknown year treated as confirmed: %v", err)
	}
	changed = initial
	changed.Start = calendarDate(2026, 8, 1)
	if err := CheckIncomeYearMethod(nil, changed, periods[2]); !errors.Is(err, ErrCalendarUnsupported) {
		t.Fatalf("part-year statement accepted: %v", err)
	}
	if err := CheckIncomeYearMethod(&initial, initial, periods[3]); err != nil {
		t.Fatalf("same confirmed method in a later quarter rejected: %v", err)
	}
	nextPeriods, _ := CandidateReportingPeriods(2027, ReportingCadenceQuarterly)
	next := IncomeYearMethodPin{IncomeYearID: "income-year-2027", Start: calendarDate(2027, 4, 1), End: calendarDate(2028, 3, 31), Confirmed: true, Method: IncomeYearMethodRate}
	if err := CheckIncomeYearMethod(&initial, next, nextPeriods[2]); err != nil {
		t.Fatalf("method change in a confirmed non-overlapping next income year rejected: %v", err)
	}
}
