package store

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/metrics"
)

// MetricSpanInterval is a half-open interval from recorded activity or cards.
// A zero End means the activity/card is still open at the observation time.
type MetricSpanInterval struct{ Start, End time.Time }

// MetricSpan is evidence for one wave, plan subtree (including a stage root),
// or group. Callers must include each member task once, including descendants.
// End is zero until all member commits are running on PROD; wave closure alone
// does not end delivery. Start is wave_opened or the first task start.
type MetricSpan struct {
	ScopeKind, ScopeRef, Label string
	Start, End                 time.Time
	Doing, OwnerWait           []MetricSpanInterval
	Accepted                   []time.Time // one first acceptance per distinct member task
	RemainingTasks             int
}

var metricSpanGroup = regexp.MustCompile(`^group:[A-Za-z0-9._-]{1,64}$`)

// ComputeMetricSpan partitions elapsed wall time, with doing taking precedence
// over Owner wait. It does not compute AM-2 calendar windows or infer delivery
// from acceptance, landing, STG, or wave_closed.
func ComputeMetricSpan(s MetricSpan, now time.Time) ([]MetricReading, error) {
	if s.ScopeKind != "wave" && s.ScopeKind != "plan_node" && !(s.ScopeKind == "all" && metricSpanGroup.MatchString(s.Label)) {
		return nil, fmt.Errorf("metric span: unsupported scope")
	}
	if (s.ScopeKind == "all") != (s.ScopeRef == "") || (s.ScopeKind != "all" && s.Label != "") || s.RemainingTasks < 0 {
		return nil, fmt.Errorf("metric span: invalid scope or remaining count")
	}
	end := s.End
	if end.IsZero() || end.After(now) {
		end = now
	}
	if s.Start.IsZero() || !end.After(s.Start) {
		return nil, fmt.Errorf("metric span: empty or reversed interval")
	}
	type edge struct {
		at          time.Time
		doing, wait int
	}
	edges := []edge{{at: s.Start}, {at: end}}
	for kind, intervals := range [][]MetricSpanInterval{s.Doing, s.OwnerWait} {
		for _, v := range intervals {
			a, b := v.Start, v.End
			if a.IsZero() {
				return nil, fmt.Errorf("metric span: missing interval start")
			}
			if !b.IsZero() && b.Before(a) {
				return nil, fmt.Errorf("metric span: reversed evidence interval")
			}
			if b.IsZero() || b.After(end) {
				b = end
			}
			if a.Before(s.Start) {
				a = s.Start
			}
			if !b.After(a) {
				continue
			}
			x, y := edge{at: a}, edge{at: b}
			if kind == 0 {
				x.doing = 1
				y.doing = -1
			} else {
				x.wait = 1
				y.wait = -1
			}
			edges = append(edges, x, y)
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].at.Before(edges[j].at) })
	totals := [3]float64{}
	doing, wait := 0, 0
	prev := s.Start
	for _, e := range edges {
		bucket := 2
		if doing > 0 {
			bucket = 0
		} else if wait > 0 {
			bucket = 1
		}
		totals[bucket] += e.at.Sub(prev).Seconds()
		doing += e.doing
		wait += e.wait
		prev = e.at
	}
	accepted := 0
	for _, at := range s.Accepted {
		if !at.Before(s.Start) && !at.After(end) {
			accepted++
		}
	}
	wall := end.Sub(s.Start).Seconds()
	rate := float64(accepted) * 86400 / wall
	stats := map[string]any{"wall_s": wall, "doing_s": totals[0], "owner_wait_s": totals[1], "idle_s": totals[2], "accepted_tasks": accepted, "tasks_per_day": rate, "remaining_tasks": s.RemainingTasks, "remaining_s": nil, "open": s.End.IsZero() || s.End.After(now)}
	if accepted > 0 {
		stats["remaining_s"] = float64(s.RemainingTasks) * 86400 / rate
	}
	r, err := metrics.Load()
	if err != nil {
		return nil, err
	}
	defs := map[string]metrics.Definition{}
	for _, d := range r.Definitions() {
		defs[d.ID] = d
	}
	ids := []string{"macro_doing_s", "macro_owner_wait_s", "macro_idle_s", "throughput_tasks"}
	out := make([]MetricReading, 0, 4)
	for i, id := range ids {
		d := defs[id]
		value := float64(accepted)
		if i < 3 {
			value = totals[i]
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("metric span: non-finite value")
		}
		v := strconv.FormatFloat(value, 'f', -1, 64)
		copyStats := map[string]any{}
		for k, v := range stats {
			copyStats[k] = v
		}
		out = append(out, MetricReading{MetricID: id, DefinitionVersion: d.Version, DefinitionSHA256: d.SHA256, ScopeKind: s.ScopeKind, ScopeRef: s.ScopeRef, Label: s.Label, WindowKind: "span", WindowStart: s.Start, WindowEnd: end, Unit: d.Unit, Samples: 1, Value: &v, StatsJSON: copyStats, Conclusive: true})
	}
	return out, nil
}

// WriteMetricSpans persists span readings with the standard computation event.
// Evidence extraction is the caller's responsibility; use a single observation
// time for every unit in a snapshot.
func WriteMetricSpans(ctx context.Context, db DB, spans []MetricSpan, now time.Time) (int, error) {
	c := MetricComputation{Source: "computed"}
	seen := map[string]bool{}
	for _, s := range spans {
		rows, err := ComputeMetricSpan(s, now)
		if err != nil {
			return 0, err
		}
		c.Readings = append(c.Readings, rows...)
		for _, r := range rows {
			if !seen[r.MetricID] {
				seen[r.MetricID] = true
				c.Definitions = append(c.Definitions, MetricDefinition{ID: r.MetricID, Version: r.DefinitionVersion, SHA256: r.DefinitionSHA256})
			}
		}
	}
	if len(c.Readings) == 0 {
		return 0, nil
	}
	return WriteMetricReadings(ctx, db, c)
}
