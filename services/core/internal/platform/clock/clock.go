// Package clock defines injectable time sources for deterministic application code.
package clock

import "time"

// Clock supplies core-authored instants.
type Clock interface {
	Now() time.Time
}

// Func adapts an injected function into a Clock and normalizes its result to UTC.
type Func func() time.Time

func (function Func) Now() time.Time {
	if function == nil {
		return time.Time{}
	}
	return function().UTC()
}

// Fixed is an immutable deterministic clock.
type Fixed struct {
	instant time.Time
}

// NewFixed creates a clock that always returns instant in UTC.
func NewFixed(instant time.Time) Fixed {
	return Fixed{instant: instant.UTC()}
}

func (fixed Fixed) Now() time.Time {
	return fixed.instant
}
