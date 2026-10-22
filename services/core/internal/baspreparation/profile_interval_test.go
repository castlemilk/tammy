package baspreparation

import (
	"testing"

	tammyv1 "github.com/tammyapp/tammy/services/core/internal/gen/tammy/v1"
)

func TestValidBASProfileIntervalSuccessor(t *testing.T) {
	interval := func(start *tammyv1.CivilDate, end *tammyv1.CivilDate) *tammyv1.BasEffectiveInterval {
		return &tammyv1.BasEffectiveInterval{StartDate: start, EndDate: end}
	}
	current := interval(testCivilDate(2026, 1, 1), nil)
	cases := []struct {
		name string
		next *tammyv1.BasEffectiveInterval
		want bool
	}{
		{"exact open-ended correction", interval(testCivilDate(2026, 1, 1), nil), true},
		{"prospective start closes old open interval logically", interval(testCivilDate(2026, 7, 1), nil), true},
		{"unknown earlier history cannot be backfilled", interval(testCivilDate(2025, 7, 1), nil), false},
		{"same start with changed end is not an exact correction", interval(testCivilDate(2026, 1, 1), testCivilDate(2026, 6, 30)), false},
		{"invalid successor interval", interval(testCivilDate(2026, 8, 1), testCivilDate(2026, 7, 31)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validBASProfileIntervalSuccessor(current, tc.next); got != tc.want {
				t.Fatalf("validBASProfileIntervalSuccessor() = %v, want %v", got, tc.want)
			}
		})
	}

	closed := interval(testCivilDate(2026, 1, 1), testCivilDate(2026, 3, 31))
	if !validBASProfileIntervalSuccessor(closed, interval(testCivilDate(2026, 5, 1), nil)) {
		t.Fatal("a later prospective span after a finite predecessor should be allowed; the gap remains unknown")
	}
	if validBASProfileIntervalSuccessor(closed, interval(testCivilDate(2026, 3, 31), nil)) {
		t.Fatal("a successor may not overlap a finite predecessor")
	}
}

func TestBASProfileIntervalCoversWholePeriodInclusiveAndOpenEnded(t *testing.T) {
	cases := []struct {
		name     string
		interval *tammyv1.BasEffectiveInterval
		period   *tammyv1.BasCivilDateRange
		want     bool
	}{
		{"exact closed bounds", &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)},
			&tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)}, true},
		{"closed interval contains range", &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 6, 1), EndDate: testCivilDate(2026, 10, 31)},
			&tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)}, true},
		{"open end contains future period", &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 1, 1)},
			&tammyv1.BasCivilDateRange{StartDate: testCivilDate(2030, 1, 1), EndDate: testCivilDate(2030, 3, 31)}, true},
		{"period begins before profile", &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)},
			&tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 6, 30), EndDate: testCivilDate(2026, 7, 31)}, false},
		{"period ends after profile", &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 7, 1), EndDate: testCivilDate(2026, 9, 30)},
			&tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 9, 1), EndDate: testCivilDate(2026, 10, 1)}, false},
		{"period reverses dates", &tammyv1.BasEffectiveInterval{StartDate: testCivilDate(2026, 1, 1)},
			&tammyv1.BasCivilDateRange{StartDate: testCivilDate(2026, 9, 30), EndDate: testCivilDate(2026, 7, 1)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := basProfileIntervalCoversPeriod(tc.interval, tc.period); got != tc.want {
				t.Fatalf("basProfileIntervalCoversPeriod() = %v, want %v", got, tc.want)
			}
		})
	}
}
