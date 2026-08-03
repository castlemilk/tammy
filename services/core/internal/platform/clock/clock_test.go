package clock_test

import (
	"testing"
	"time"

	"github.com/tammyapp/tammy/services/core/internal/platform/clock"
)

func TestFixedClockReturnsInjectedInstantInUTC(t *testing.T) {
	local := time.Date(2026, time.August, 3, 10, 11, 12, 123456789, time.FixedZone("AEST", 10*60*60))
	source := clock.NewFixed(local)
	want := local.UTC()
	if got := source.Now(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("now = %s (%s), want %s UTC", got, got.Location(), want)
	}
	if got := source.Now(); got != want {
		t.Fatalf("fixed clock changed: %s", got)
	}
}

func TestClockFunctionIsInjectableAndNormalizesUTC(t *testing.T) {
	calls := 0
	local := time.Date(2026, time.August, 3, 10, 11, 12, 0, time.FixedZone("AEST", 10*60*60))
	source := clock.Func(func() time.Time {
		calls++
		return local
	})
	if got := source.Now(); got != local.UTC() || calls != 1 {
		t.Fatalf("now = %s, calls = %d", got, calls)
	}
}
