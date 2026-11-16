package planreadings

import (
	"slices"
	"time"
)

// Display ordering only; these values never authorize execution.
var missionStatusRank = []string{"host_lost", "maybe_stuck", "unknown", "waiting_owner", "stopped", "blocked",
	"slow", "working", "waiting_role", "queued"}

func MostRelevantStatus(statuses []string) string {
	best := len(missionStatusRank)
	for _, s := range statuses {
		if i := slices.Index(missionStatusRank, s); i >= 0 && i < best {
			best = i
		}
	}
	if best == len(missionStatusRank) {
		return ""
	}
	return missionStatusRank[best]
}

func OrderLiveAttempts[T any](live map[string]T, fields func(T) (time.Time, string)) []T {
	list := make([]T, 0, len(live))
	for _, a := range live {
		list = append(list, a)
	}
	slices.SortFunc(list, func(x, y T) int {
		xStarted, xID := fields(x)
		yStarted, yID := fields(y)
		switch {
		case xStarted.IsZero() != yStarted.IsZero():
			if xStarted.IsZero() {
				return 1
			}
			return -1
		case !xStarted.Equal(yStarted):
			return xStarted.Compare(yStarted)
		}
		if xID < yID {
			return -1
		}
		return 1
	})
	return list
}
