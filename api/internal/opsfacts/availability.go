package opsfacts

import (
	"encoding/json"
	"math"
	"sort"
	"time"
)

type metricPoint struct {
	Labels    map[string]string `json:"labels"`
	Value     *float64          `json:"value"`
	Timestamp float64           `json:"timestamp"`
}
type metricRead struct {
	Points      *[]metricPoint `json:"points"`
	GeneratedAt time.Time      `json:"generated_at"`
}
type AvailabilitySeries struct {
	Labels        map[string]string `json:"labels"`
	ObservedRatio float64           `json:"observed_ratio"`
	Samples       *int64            `json:"samples"`
	Availability  *float64          `json:"availability"`
	State         State             `json:"state"`
	Reason        string            `json:"reason"`
}
type Availability struct {
	Series       []AvailabilitySeries   `json:"series"`
	SampleCounts Fact[map[string]int64] `json:"sample_counts"`
	Overall      *float64               `json:"overall"`
}

func projectAvailability(averages, counts Fact[metricRead]) Fact[Availability] {
	countFact := mapFact(counts, func(v metricRead) map[string]int64 {
		m := map[string]int64{}
		if validPoints(v.Points, false) {
			for _, p := range *v.Points {
				m[seriesKey(p.Labels)] = int64(*p.Value)
			}
		}
		return m
	})
	if counts.Value != nil {
		if !validPoints(counts.Value.Points, false) {
			invalid(&countFact)
		} else {
			countFact.Coverage = snapshotCoverage()
			countFact.Coverage.Complete = nil
			countFact.Coverage.Reason = "returned sample counts; expected sampling coverage is not established"
			w := SevenDays(countFact.AsOf)
			countFact.Coverage.Window = &w
			if len(*counts.Value.Points) == 0 {
				countFact.State = Empty
			}
			queryFreshness(&countFact, counts.Value.GeneratedAt)
			// generated_at is query execution time, not the newest underlying sample.
		}
	}
	out := mapFact(averages, func(v metricRead) Availability {
		return Availability{Series: []AvailabilitySeries{}, SampleCounts: countFact}
	})
	window := SevenDays(averages.AsOf)
	out.Coverage.Window = &window
	out.Coverage.Reason = "scrape cadence, expected target lifetime, and full-window sample coverage are not established; observed means are not seven-day SLOs"
	if averages.Value == nil {
		return out
	}
	if !validPoints(averages.Value.Points, true) {
		invalid(&out)
		return out
	}
	if len(*averages.Value.Points) == 0 {
		out.State = Unknown
		out.Reason = "no up series measured for this window"
		return out
	}
	for _, p := range *averages.Value.Points {
		row := AvailabilitySeries{Labels: p.Labels, ObservedRatio: *p.Value, State: Unknown, Reason: "full-window sampling coverage is unknown"}
		if countFact.State == OK && countFact.Value != nil {
			if n, ok := (*countFact.Value)[seriesKey(p.Labels)]; ok {
				row.Samples = &n
			}
		}
		out.Value.Series = append(out.Value.Series, row)
	}
	sort.Slice(out.Value.Series, func(i, j int) bool {
		return seriesKey(out.Value.Series[i].Labels) < seriesKey(out.Value.Series[j].Labels)
	})
	out.State = Unknown
	out.Reason = "sample means are available; complete seven-day availability is not measured"
	// Query evaluation time must never be presented as the latest raw sample.
	// It can still detect an old supplied response, conservatively.
	queryFreshness(&out, averages.Value.GeneratedAt)
	return out
}
func seriesKey(labels map[string]string) string { b, _ := json.Marshal(labels); return string(b) }
func validPoints(points *[]metricPoint, ratio bool) bool {
	if points == nil {
		return false
	}
	seen := map[string]bool{}
	for _, p := range *points {
		key := seriesKey(p.Labels)
		if len(p.Labels) == 0 || seen[key] || p.Value == nil || math.IsNaN(*p.Value) || math.IsInf(*p.Value, 0) || *p.Value < 0 {
			return false
		}
		if ratio && *p.Value > 1 {
			return false
		}
		if !ratio && (math.Trunc(*p.Value) != *p.Value || *p.Value >= math.MaxInt64) {
			return false
		}
		seen[key] = true
	}
	return true
}

// The query execution time is useful for response freshness, but cannot serve
// as the watermark of the historical samples being aggregated.
func queryFreshness[T any](f *Fact[T], generated time.Time) {
	if generated.IsZero() {
		return
	}
	if generated.After(f.AsOf.Add(time.Minute)) {
		f.State = Unknown
		f.Reason = "query response timestamp is in the future"
		return
	}
	if f.AsOf.Sub(generated) > 2*time.Minute {
		f.State = Stale
		f.Reason = "query response freshness threshold exceeded"
	}
}
