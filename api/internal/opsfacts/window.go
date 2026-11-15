package opsfacts

import (
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

func SevenDays(now time.Time) Window {
	return Window{Kind: TrailingSevenDays, Timezone: "UTC", Start: now.UTC().Add(-7 * 24 * time.Hour), End: now.UTC()}
}
