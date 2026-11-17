package appreadings

import (
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

const (
	freshnessFreshDays = 3.0
	freshnessStaleDays = 7.0
)

type ActivityObservation = resourceview.ActivityObservation
type DataFreshnessDB = resourceview.DataFreshnessDB

func FreshnessDatabases(databases []DataFreshnessDB, probes []ActivityObservation, referenceDB string, lastCloneAt *string, now time.Time) []DataFreshnessDB {
	var prodTS *time.Time
	var prodErr error
	for i, db := range databases {
		if db.Name == referenceDB {
			prodTS, prodErr = probes[i].At, probes[i].Err
		}
	}
	for i := range databases {
		db := &databases[i]
		if lastCloneAt != nil && db.Name != referenceDB {
			db.LastCloneAt = lastCloneAt
		}
		ts, sources, qErr := probes[i].At, probes[i].Sources, probes[i].Err
		if qErr != nil {
			db.Verdict = "unknown"
			db.Detail = qErr.Error()
			continue
		}
		db.Sources = sources
		if ts == nil {
			db.Verdict = "unknown"
			db.Detail = "no activity timestamps reported"
			continue
		}
		iso := ts.UTC().Format(time.RFC3339)
		db.LastActivityTS = &iso
		age := now.Sub(ts.UTC()).Hours() / 24.0
		if age < 0 {
			age = 0
		}
		db.AgeDays = &age

		if db.Name == referenceDB {
			db.Verdict = "reference"
			db.Detail = "reference · wall_age=" + fmt.Sprintf("%.1fd", age)
			continue
		}

		if prodTS == nil || prodErr != nil {
			db.Verdict = "unknown"
			if prodErr != nil {
				db.Detail = "prod activity unavailable: " + prodErr.Error()
			} else {
				db.Detail = "prod activity unavailable"
			}
			continue
		}

		lag := prodTS.Sub(ts.UTC()).Hours() / 24.0
		if lag < 0 {
			lag = 0
		}
		db.LagVsProdDays = &lag
		db.StaleDays = &lag // Sync signal = lag (not wall-clock age)
		db.Verdict = FreshnessVerdictFromLag(lag)
		db.Detail = fmt.Sprintf("lag_vs_prod=%.1fd · wall_age=%.1fd", lag, age)
	}
	return databases
}
func FreshnessVerdictFromLag(lagDays float64) string {
	switch {
	case lagDays < freshnessFreshDays:
		return "fresh"
	case lagDays < freshnessStaleDays:
		return "aging"
	default:
		return "stale"
	}
}
