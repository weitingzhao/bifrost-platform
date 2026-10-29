package opsfacts

import (
	"fmt"
	"time"
	_ "time/tzdata" // Chicago/DST semantics must also work in minimal runtime images.
)

type WindowKind string

const (
	OwnerToday        WindowKind = "owner_today"
	OwnerWeek         WindowKind = "owner_week"
	TrailingSevenDays WindowKind = "trailing_seven_days"
	RoundToDate       WindowKind = "round_to_date"
	RollingQuotaWeek  WindowKind = "rolling_quota_week"
	SourceUTCDay      WindowKind = "source_utc_day"
)

type Window struct {
	Kind     WindowKind `json:"kind"`
	Timezone string     `json:"timezone"`
	Start    time.Time  `json:"start"`
	End      time.Time  `json:"end"` // exclusive
	Baseline string     `json:"baseline,omitempty"`
}

// Distinct inputs prevent a quota reset or an unverified round date from being
// passed as the other baseline. No default R4 start is inferred from a mockup.
type RoundBaseline struct {
	ID    string
	Start time.Time
}
type QuotaBaseline struct {
	Pool  string
	Reset time.Time
}

func CalendarWindow(kind WindowKind, now time.Time) (Window, error) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return Window{}, err
	}
	if kind == SourceUTCDay {
		loc = time.UTC
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	switch kind {
	case OwnerToday, SourceUTCDay:
	case OwnerWeek:
		start = start.AddDate(0, 0, -(int(local.Weekday())+6)%7)
		end = start.AddDate(0, 0, 7)
	default:
		return Window{}, fmt.Errorf("not a calendar window: %s", kind)
	}
	return Window{Kind: kind, Timezone: loc.String(), Start: start, End: end}, nil
}
func SevenDays(now time.Time) Window {
	return Window{Kind: TrailingSevenDays, Timezone: "UTC", Start: now.UTC().Add(-7 * 24 * time.Hour), End: now.UTC()}
}
func SinceRound(now time.Time, baseline RoundBaseline) (Window, error) {
	if baseline.ID == "" || baseline.Start.IsZero() || baseline.Start.After(now) {
		return Window{}, fmt.Errorf("verified round baseline required")
	}
	return Window{Kind: RoundToDate, Timezone: "UTC", Start: baseline.Start.UTC(), End: now.UTC(), Baseline: baseline.ID}, nil
}
func QuotaWeek(baseline QuotaBaseline) (Window, error) {
	if baseline.Pool == "" || baseline.Reset.IsZero() {
		return Window{}, fmt.Errorf("quota reset baseline required")
	}
	return Window{Kind: RollingQuotaWeek, Timezone: "UTC", Start: baseline.Reset.UTC().Add(-7 * 24 * time.Hour), End: baseline.Reset.UTC(), Baseline: baseline.Pool}, nil
}
