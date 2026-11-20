// Package evalreadings is the evaluation readout: GET /api/v1/readings/evaluation.
//
// Law 47 (Owner 2026-11-19): the numbers an evaluation uses come only from the
// system. Each number here is computed from records that are kept (the event
// ledger, read only through its declared read shape: eventlog.ReadShape) or
// from Prometheus recording rules, over a window that has already closed, so
// the same window read on any day gives the same value. Every reading carries
// its definition version, window, sample count and source. A number with no
// records is "unmeasured"; one with fewer samples than its floor is "too_few"
// and gives no percentile. The lines being measured only write their raw
// records; the medians and ratios are computed here (the metrics line,
// coordination domain), never by the line they describe.
//
// Origin: the coordinator's daily brief computed its numbers with ad-hoc
// scripts (shared notes benchmark/), and the Owner said on 2026-11-19 that
// "none of the evaluation results come from the system". Raw-record audit and
// the choices behind this package: shared notes metrics/
// eval-readout-step1-records-2026-11-20.md and eval-readout-options-2026-11-20.md.
//
// Read-only: one read-only transaction per request, no cluster listing
// (incident #63). It stores nothing.
package evalreadings

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// DefinitionVersion is the version of every definition in this file. Raise it
// when a definition changes meaning; old and new are then reported apart and an
// old window is never recomputed under the new definition.
const DefinitionVersion = "1"

// Zone is where a day starts and ends: the Owner's zone.
var Zone = func() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// Status of a reading.
const (
	StatusOK         = "ok"
	StatusTooFew     = "too_few"
	StatusUnmeasured = "unmeasured"
)

// Window is a closed span: [From, To).
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Days int       `json:"days"`
	Zone string    `json:"zone"`
}

// ClosedWindow is the last `days` whole days in Zone that ended before now.
func ClosedWindow(now time.Time, days int) Window {
	n := now.In(Zone)
	to := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, Zone)
	from := to.AddDate(0, 0, -days)
	return Window{From: from.UTC(), To: to.UTC(), Days: days, Zone: Zone.String()}
}

// Definition is how a reading is computed.
type Definition struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	Rule    string `json:"rule"`
	Code    string `json:"code"`
}

// Reading is one number.
type Reading struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	Value      *float64          `json:"value"`
	Unit       string            `json:"unit"`
	Status     string            `json:"status"`
	Samples    int               `json:"samples"`
	MinSamples int               `json:"min_samples"`
	Window     Window            `json:"window"`
	Definition Definition        `json:"definition"`
	Provenance string            `json:"provenance"`
	Note       string            `json:"note,omitempty"`
}

// Sample is one Prometheus sample: its labels and value.
type Sample struct {
	Labels map[string]string
	Value  float64
}

// Prom runs an instant PromQL query. nil means Prometheus is not reachable
// from this platform; the Prometheus readings are then unmeasured.
type Prom func(ctx context.Context, expr string) ([]Sample, error)

// MinSamples is the floor under which a median is not given.
const MinSamples = 10

const code = "api/internal/evalreadings/evalreadings.go"

func ratio(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}

func count(n int) *float64 { v := float64(n); return &v }

// finish sets status from the value and samples: a percentile with fewer
// samples than its floor is dropped.
func finish(r Reading, percentile bool) Reading {
	r.Provenance = "system"
	if r.MinSamples == 0 && percentile {
		r.MinSamples = MinSamples
	}
	switch {
	case r.Samples == 0:
		r.Status, r.Value = StatusUnmeasured, nil
	case percentile && r.Samples < r.MinSamples:
		r.Status, r.Value = StatusTooFew, nil
	case r.Value == nil:
		r.Status = StatusUnmeasured
	default:
		r.Status = StatusOK
		v := math.Round(*r.Value*1000) / 1000
		r.Value = &v
	}
	return r
}

// measuredZero turns an empty count into 0: for a count whose records are
// kept (the source writes every event), nothing in the window is a measured
// zero, not missing records.
func measuredZero(r *Reading) {
	if r.Samples == 0 {
		v := 0.0
		r.Value, r.Status = &v, StatusOK
	}
}

func def(source, rule string) Definition {
	return Definition{Version: DefinitionVersion, Source: source, Rule: rule, Code: code}
}

// Compute returns every reading of the window. db nil leaves the ledger
// readings unmeasured; prom nil leaves the Prometheus readings unmeasured.
func Compute(ctx context.Context, db eventlog.DB, prom Prom, w Window) ([]Reading, error) {
	var out []Reading
	if db != nil {
		rs, err := ledgerReadings(ctx, db, w)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	} else {
		for _, name := range []string{"ci_run_p50_seconds", "delivery_success_ratio", "owner_actions", "owner_wait_p50_seconds", "prod_deploys", "main_to_prod_p50_seconds"} {
			out = append(out, finish(Reading{Name: name, Window: w, Note: "the event ledger is not connected on this platform"}, false))
		}
	}
	out = append(out, promReadings(ctx, prom, w)...)
	out = append(out, unmeasured(w)...)
	return out, nil
}

// unmeasured are the readings the first version names but has no records for.
// plan_nodes_done: the coordinator said on 2026-11-20 to report it unmeasured.
func unmeasured(w Window) []Reading {
	return []Reading{
		finish(Reading{Name: "post_release_fixes", Unit: "count", Window: w, Definition: def("none", "no record says that a release was fixed afterwards"),
			Note: "no raw record yet; main.regression.* is regressions on main, not fixes after a release"}, false),
		finish(Reading{Name: "plan_nodes_done", Unit: "count", Window: w, Definition: def("none", "plan.tree.changed is registered but has no events"),
			Note: "no events of plan.tree.changed yet"}, false),
	}
}

// The coverage record started on 2026-11-20 (#659); before that there are no samples.
// promReadings are the Prometheus recording rules that are archived readings.
// Prometheus keeps 10 days, so a window longer than that is unmeasured.
func promReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "metric_coverage_ratio"
	d := def("Prometheus recording rule bifrost_metrics:coverage_ratio (k8s/monitoring/bifrost-metrics-coverage-rules.yaml)",
		"avg_over_time of the 5-minute record over the window, by scope")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	expr := fmt.Sprintf("avg_over_time(bifrost_metrics:coverage_ratio[%ds] @ %d)", span, w.To.Unix())
	samples := fmt.Sprintf("count_over_time(bifrost_metrics:coverage_ratio[%ds] @ %d)", span, w.To.Unix())
	vals, err := prom(ctx, expr)
	if err != nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	ns, _ := prom(ctx, samples)
	n := map[string]int{}
	for _, s := range ns {
		n[s.Labels["scope"]] = int(s.Value)
	}
	if len(vals) == 0 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "no samples of the record in the window"}, false)}
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i].Labels["scope"] < vals[j].Labels["scope"] })
	var out []Reading
	for _, s := range vals {
		v := s.Value
		scope := s.Labels["scope"]
		out = append(out, finish(Reading{Name: name, Labels: map[string]string{"scope": scope}, Value: &v, Unit: "ratio", Samples: n[scope], Window: w, Definition: d}, false))
	}
	return out
}
