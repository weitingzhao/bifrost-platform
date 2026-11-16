package cluster

import (
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/appreadings"
	"reflect"
	"testing"
	"time"
)

func baselineFreshnessDatabases(databases []DataFreshnessDB, probes []appreadings.ActivityObservation, referenceDB string, lastCloneAt *string, now time.Time) []DataFreshnessDB {
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
		db.Verdict = baselineFreshnessVerdictFromLag(lag)
		db.Detail = fmt.Sprintf("lag_vs_prod=%.1fd · wall_age=%.1fd", lag, age)
	}
	return databases
}
func baselineFreshnessVerdictFromLag(lagDays float64) string {
	switch {
	case lagDays < freshnessFreshDays:
		return "fresh"
	case lagDays < freshnessStaleDays:
		return "aging"
	default:
		return "stale"
	}
}

func TestFreshnessProjectionOriginalParity(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	clone := "2026-11-15T12:00:00Z"
	for _, lag := range []float64{-2, 0, 2.99, 3, 6.99, 7, 20} {
		for _, mode := range []string{"ok", "missing-target", "missing-prod", "target-error", "prod-error"} {
			for _, cloned := range []*string{nil, &clone} {
				prod := now.Add(-24 * time.Hour)
				target := prod.Add(-time.Duration(lag * 24 * float64(time.Hour)))
				obs := []appreadings.ActivityObservation{{At: &target, Sources: []string{"a", "b"}}, {At: &prod, Sources: []string{"prod"}}}
				switch mode {
				case "missing-target":
					obs[0].At = nil
				case "missing-prod":
					obs[1].At = nil
				case "target-error":
					obs[0].Err = errors.New("target unavailable")
				case "prod-error":
					obs[1].Err = errors.New("prod unavailable")
				}
				input := func() []DataFreshnessDB {
					return []DataFreshnessDB{{Name: "bifrost_stg", Environment: "stg", Verdict: "unknown"}, {Name: "bifrost_prod", Environment: "prod", Verdict: "unknown"}}
				}
				got, want := appreadings.FreshnessDatabases(input(), obs, "bifrost_prod", cloned, now), baselineFreshnessDatabases(input(), obs, "bifrost_prod", cloned, now)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("lag=%v mode=%s: %#v != %#v", lag, mode, got, want)
				}
			}
		}
	}
	if got := appreadings.FreshnessDatabases(nil, nil, "prod", nil, now); got != nil {
		t.Fatal("nil changed")
	}
}
