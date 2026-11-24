// Package evalreadings is the evaluation readout: GET /api/v1/readings/evaluation.
//
// Law 93 (Owner 2026-11-19): the numbers an evaluation uses come only from the
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
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
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

// coverageInterval is how often bifrost_metrics:coverage_ratio is recorded
// (interval: 5m in k8s/monitoring/bifrost-metrics-coverage-rules.yaml).
const coverageInterval = 5 * time.Minute

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
func Compute(ctx context.Context, db eventlog.DB, prom Prom, tasks FailedTasks, w Window) ([]Reading, error) {
	var out []Reading
	if db != nil {
		rs, err := ledgerReadings(ctx, db, tasks, w)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
		list := func(ctx context.Context, f actionrecord.Filter) ([]actionrecord.Record, bool, error) {
			return actionrecord.List(ctx, db, f)
		}
		out = append(out, agentActionReadings(ctx, list, w)...)
	} else {
		for _, name := range []string{"ci_run_p50_seconds", "delivery_success_ratio", "owner_actions", "owner_wait_p50_seconds", "prod_deploys", "main_to_prod_p50_seconds", "agent_actions", "action_rollback_declared_ratio", "ci_failures", "guard_blocks", "ask_answer_p50_seconds", "ask_answer_max_seconds", "ask_open_over_4h"} {
			out = append(out, finish(Reading{Name: name, Window: w, Note: "the event ledger is not connected on this platform"}, false))
		}
	}
	out = append(out, promGroups(ctx, prom, w)...)
	out = append(out, unmeasured(w)...)
	return out, nil
}

// promGroups runs the Prometheus-backed readings at the same time and keeps
// their order. Run one after another, a 7-day window's queries took most of
// the handler's 15 seconds (2026-11-20: eight subqueries over 7 days each).
func promGroups(ctx context.Context, prom Prom, w Window) []Reading {
	groups := []func(context.Context, Prom, Window) []Reading{
		promReadings, promGapReadings, coverageByNamespaceReadings, catalogDomainReadings, ciQueueReadings, busEventReadings, busOutageReadings, alertActivationReadings,
	}
	res := make([][]Reading, len(groups))
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Add(1)
		safego.Go("evalreadings.prom", func() {
			defer wg.Done()
			res[i] = g(ctx, prom, w)
		})
	}
	wg.Wait()
	var out []Reading
	for _, r := range res {
		out = append(out, r...)
	}
	return out
}

// unmeasured are the readings the first version names but has no records for.
// plan_nodes_done: the coordinator said on 2026-11-20 to report it unmeasured.
func unmeasured(w Window) []Reading {
	return []Reading{
		finish(Reading{Name: "post_release_fixes", Unit: "count", Window: w, Definition: def("none", "no record says that a release was fixed afterwards"),
			Note: "no raw record yet; main.regression.* is regressions on main, not fixes after a release"}, false),
		finish(Reading{Name: "auto_mode_denials", Unit: "count", Window: w, Definition: def("none", "the auto-mode classifier has no hook that records a refusal"),
			Note: "no record: the classifier's refusals reach no hook"}, false),
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
	// An average over a window the record covers only in part is not the
	// window's: below 90% of the samples the 5-minute record gives over the
	// window, the reading is too_few (2026-11-20: the first read had 1 sample
	// in a 7-day window, because the record started that morning).
	floor := int(0.9 * float64(span) / coverageInterval.Seconds())
	var out []Reading
	for _, s := range vals {
		v := s.Value
		scope := s.Labels["scope"]
		r := Reading{Name: name, Labels: map[string]string{"scope": scope}, Value: &v, Unit: "ratio", Samples: n[scope], MinSamples: floor, Window: w, Definition: d}
		r = finish(r, false)
		if r.Status == StatusOK && r.Samples < floor {
			r.Status, r.Value = StatusTooFew, nil
			r.Note = "the record covers too little of the window"
		}
		out = append(out, r)
	}
	return out
}

// QueueDefinitionVersion is the CI queue reading's own version. 2 (the CI line,
// 2026-11-20): the wait between a pod's creation and its start (scheduling,
// node, image pull) from kube-state-metrics, a run's queue being the longest
// wait of its pods. Version 1, created_at -> started_at of the PipelineRun,
// was only the controller taking the run and read 0 seconds on PROD.
const QueueDefinitionVersion = "2"

// ciQueueRuns is each run's queue: a pod's creation and start times are
// constant gauges, so their max_over_time over the window gives every pod the
// window saw without a subquery (a 7-day subquery timed out on PROD,
// 2026-11-20). A pod is named <run>-<task>-pod, a run <prefix>-<5 characters>.
func ciQueueRuns(span int, at int64) string {
	sel := `{namespace="cicd",pod=~"(ci-[a-z]+|land)-.*"}`
	wait := fmt.Sprintf("max_over_time(kube_pod_start_time%s[%ds] @ %d) - on(namespace,pod) max_over_time(kube_pod_created%s[%ds] @ %d)", sel, span, at, sel, span, at)
	return fmt.Sprintf(`max by (prefix, run) (label_replace(label_replace(%s, "run", "$1", "pod", "((ci-[a-z]+|land)-[a-z0-9]{5}).*"), "prefix", "$1", "pod", "(ci-[a-z]+|land)-.*"))`, wait)
}

// ciQueueReadings: the median of the runs' queues by run prefix (ci-platform,
// ci-infra, land, ...). The wait a landing spends behind the one before it is
// inside its steps and not counted. Prometheus keeps 10 days.
func ciQueueReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "ci_queue_p50_seconds"
	d := def("Prometheus kube-state-metrics kube_pod_start_time - kube_pod_created, namespace cicd, pods of ci-* and land-* runs",
		"median over the window's runs of (the longest pod creation-to-start wait of the run), by run prefix")
	d.Version = QueueDefinitionVersion
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Unit: "seconds", Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, true)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Unit: "seconds", Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, true)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	runs := ciQueueRuns(span, w.To.Unix())
	meds, err := prom(ctx, "quantile by (prefix) (0.5, "+runs+")")
	if err != nil {
		return []Reading{finish(Reading{Name: name, Unit: "seconds", Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, true)}
	}
	counts, _ := prom(ctx, "count by (prefix) ("+runs+")")
	n := map[string]int{}
	for _, c := range counts {
		n[c.Labels["prefix"]] = int(c.Value)
	}
	sort.Slice(meds, func(i, j int) bool { return meds[i].Labels["prefix"] < meds[j].Labels["prefix"] })
	var out []Reading
	for _, m := range meds {
		p := m.Labels["prefix"]
		if p == "" {
			continue
		}
		v := m.Value
		out = append(out, finish(Reading{Name: name, Labels: map[string]string{"run_prefix": p}, Value: &v, Unit: "seconds", Samples: n[p], Window: w, Definition: d}, true))
	}
	if len(out) == 0 {
		return []Reading{finish(Reading{Name: name, Unit: "seconds", Window: w, Definition: d, Note: "no CI pod in the window"}, true)}
	}
	return out
}
