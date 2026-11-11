// Package metriccompute computes delivery readings from durable runtime facts.
package metriccompute

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/metrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type fact struct {
	Kind, Stream, Task, Repo string
	At                       time.Time
	Body                     map[string]any
}
type window struct {
	Kind, Label string
	Start, End  time.Time
}
type sample struct {
	Repo          string
	At            time.Time
	Value, Reuses float64
	Denominator   float64
}

func str(b map[string]any, k string) string { s, _ := b[k].(string); return s }
func stamp(b map[string]any, k string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, str(b, k))
	return t
}
func inside(t time.Time, w window) bool { return !t.Before(w.Start) && t.Before(w.End) }

// Compute writes only missing, closed and settled readings. A pass without
// source records or missing windows never calls the writer.
func Compute(ctx context.Context, db store.DB, now time.Time) (int, error) {
	r, err := metrics.Load()
	if err != nil {
		return 0, err
	}
	return compute(ctx, db, now, r.Definitions())
}
func compute(ctx context.Context, db store.DB, now time.Time, defs []metrics.Definition) (int, error) {
	var facts []fact
	have := map[string]bool{}
	err := store.InTx(ctx, db, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT e.event_type,e.event_stream_id,COALESCE(e.task_id,''),COALESCE(t.repo,''),e.at,e.body_json FROM agentrt.event e LEFT JOIN agentrt.task t ON t.task_id=e.task_id WHERE e.event_type IN ('attempt_started','approval_seen','question_seen','pipeline_seen','metric_label_set','task_state_changed') OR (e.event_type='note' AND e.body_json ? 'landing') ORDER BY e.event_id`)
		if e != nil {
			return e
		}
		for rows.Next() {
			var f fact
			var raw []byte
			if e = rows.Scan(&f.Kind, &f.Stream, &f.Task, &f.Repo, &f.At, &raw); e != nil {
				rows.Close()
				return e
			}
			if e = json.Unmarshal(raw, &f.Body); e != nil {
				rows.Close()
				return e
			}
			facts = append(facts, f)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		rows, e = tx.Query(ctx, `SELECT metric_id,definition_version,scope_kind,scope_ref,label,window_start,window_end FROM agentrt.metric_reading WHERE source='computed' AND scope_kind IN ('all','repo')`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id, kind, repo, label string
			var v int
			var a, b time.Time
			if e = rows.Scan(&id, &v, &kind, &repo, &label, &a, &b); e != nil {
				return e
			}
			have[key(id, v, repo, window{Label: label, Start: a, End: b})] = true
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	c := planReadings(facts, have, now, defs)
	if len(c.Readings) == 0 {
		return 0, nil
	}
	return store.WriteMetricReadings(ctx, db, c)
}

func planReadings(facts []fact, have map[string]bool, now time.Time, defs []metrics.Definition) store.MetricComputation {
	if len(facts) == 0 {
		return store.MetricComputation{}
	}
	repos := map[string]bool{}
	for _, f := range facts {
		if f.Repo != "" {
			repos[f.Repo] = true
		}
		if repo := str(f.Body, "repo"); repo != "" {
			repos[repo] = true
		}
		if strings.HasPrefix(f.Stream, "landing:") {
			repos[strings.TrimPrefix(f.Stream, "landing:")] = true
		}
	}
	var data map[string][]sample
	var first time.Time
	labels := map[string]window{}
	for _, f := range facts {
		if f.Kind == "metric_label_set" {
			labels[str(f.Body, "label")] = window{"span", str(f.Body, "label"), stamp(f.Body, "window_start"), stamp(f.Body, "window_end")}
			continue
		}
		for _, field := range []string{"created_at", "started_at", "completed_at"} {
			t := stamp(f.Body, field)
			if !t.IsZero() && (first.IsZero() || t.Before(first)) {
				first = t
			}
		}
		if first.IsZero() || f.At.Before(first) {
			first = f.At
		}
	}
	scopes := []string{""}
	for repo := range repos {
		if repo != "" {
			scopes = append(scopes, repo)
		}
	}
	sort.Strings(scopes)
	c := store.MetricComputation{Source: "computed"}
	for _, d := range defs {
		// Macro doing / Owner-wait / idle segmentation is not yet extracted.
		// Do not freeze empty rows for an unsupported extractor: implementing it
		// later must still be able to fill these definition versions.
		if d.Extractor == "macro_wall_time" {
			continue
		}
		var windows []window
		for _, w := range labels {
			windows = append(windows, w)
		}
		for _, kind := range d.WindowKinds {
			if kind == "span" {
				continue
			}
			if first.IsZero() {
				continue
			}
			for a := floor(first, kind); ; {
				b := next(a, kind)
				if b.After(now.Add(-time.Duration(d.SettleS) * time.Second)) {
					break
				}
				windows = append(windows, window{Kind: kind, Start: a, End: b})
				a = b
			}
		}
		for _, w := range windows {
			if w.Start.IsZero() || !w.End.After(w.Start) || w.End.After(now.Add(-time.Duration(d.SettleS)*time.Second)) {
				continue
			}
			for _, repo := range scopes {
				if have[key(d.ID, d.Version, repo, w)] {
					continue
				}
				if data == nil {
					data, _ = extract(facts)
				}
				reading := aggregate(d, data[dataKey(d)], repo, w)
				c.Readings = append(c.Readings, reading)
			}
		}
		c.Definitions = append(c.Definitions, store.MetricDefinition{ID: d.ID, Version: d.Version, SHA256: d.SHA256})
	}
	if len(c.Readings) == 0 {
		return store.MetricComputation{}
	}
	return c
}
func key(id string, v int, repo string, w window) string {
	return fmt.Sprintf("%s/%d/%s/%s/%s/%s", id, v, repo, w.Label, w.Start.UTC().Format(time.RFC3339Nano), w.End.UTC().Format(time.RFC3339Nano))
}
func floor(t time.Time, k string) time.Time {
	t = t.UTC()
	if k == "hour" {
		return t.Truncate(time.Hour)
	}
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if k == "week" {
		t = t.AddDate(0, 0, -(int(t.Weekday())+6)%7)
	}
	return t
}
func next(t time.Time, k string) time.Time {
	if k == "hour" {
		return t.Add(time.Hour)
	}
	if k == "week" {
		return t.AddDate(0, 0, 7)
	}
	return t.AddDate(0, 0, 1)
}

func aggregate(d metrics.Definition, samples []sample, repo string, w window) store.MetricReading {
	r := store.MetricReading{MetricID: d.ID, DefinitionVersion: d.Version, DefinitionSHA256: d.SHA256, ScopeKind: "all", ScopeRef: repo, Label: w.Label, WindowKind: w.Kind, WindowStart: w.Start, WindowEnd: w.End, Unit: d.Unit, StatsJSON: map[string]any{}}
	if repo != "" {
		r.ScopeKind = "repo"
	}
	var values []float64
	var total, reuses, denominator float64
	for _, s := range samples {
		if (repo == "" || s.Repo == repo) && inside(s.At, w) {
			v := s.Value
			if d.Extractor == "owner_wait_share" && s.Denominator > 0 {
				v /= s.Denominator
			}
			values = append(values, v)
			total += s.Value
			reuses += s.Reuses
			denominator += s.Denominator
		}
	}
	r.Samples = len(values)
	r.Conclusive = r.Samples >= d.MinSamples
	if len(values) == 0 {
		if d.Extractor == "release_window_occupancy" {
			r.StatsJSON["no_sample_reason"] = "no_durable_window_hold_intervals"
		}
		return r
	}
	sort.Float64s(values)
	percentile := func(p float64) float64 { return values[int(math.Ceil(float64(len(values))*p))-1] }
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	r.StatsJSON = map[string]any{"mean": mean, "p50": percentile(.5), "p90": percentile(.9), "min": values[0], "max": values[len(values)-1], "reuses": reuses}
	value := mean
	if d.Extractor == "owner_wait_share" && denominator > 0 {
		value = total / denominator
		r.StatsJSON["wait_s"] = total
		r.StatsJSON["lead_s"] = denominator
	}
	switch d.Headline {
	case "p50":
		value = percentile(.5)
	case "p90":
		value = percentile(.9)
	case "count":
		value = total
	}
	s := fmt.Sprintf("%.9g", value)
	r.Value = &s
	return r
}

func extract(facts []fact) (map[string][]sample, map[string]bool) {
	out := map[string][]sample{}
	repos := map[string]bool{}
	starts := map[string]time.Time{}
	taskRepos := map[string]string{}
	latest := map[string]fact{}
	ops := map[string]map[string]any{}
	var landed []fact
	for _, f := range facts {
		if f.Repo != "" {
			taskRepos[f.Task] = f.Repo
			repos[f.Repo] = true
		}
		if repo := str(f.Body, "repo"); repo != "" {
			repos[repo] = true
		}
		switch f.Kind {
		case "attempt_started":
			if starts[f.Task].IsZero() || f.At.Before(starts[f.Task]) {
				starts[f.Task] = f.At
			}
		case "approval_seen", "question_seen", "pipeline_seen":
			id := str(f.Body, "question")
			if f.Kind == "approval_seen" {
				id = fmt.Sprint(f.Body["approval"])
			}
			if f.Kind == "pipeline_seen" {
				id = str(f.Body, "run") + "/" + str(f.Body, "uid")
			}
			latest[f.Kind+"/"+id] = f
		case "note":
			if b, ok := f.Body["landing"].(map[string]any); ok {
				op := f.Stream + "/" + str(b, "op")
				if ops[op] == nil {
					ops[op] = map[string]any{}
				}
				for k, v := range b {
					ops[op][k] = v
				}
				if str(b, "kind") == "landed" {
					f.Body = make(map[string]any, len(ops[op]))
					for k, v := range ops[op] {
						f.Body[k] = v
					}
					landed = append(landed, f)
				}
			}
		case "task_state_changed":
			if str(f.Body, "to") == "accepted" {
				out["throughput_tasks"] = append(out["throughput_tasks"], sample{Repo: f.Repo, At: f.At, Value: 1})
			}
		}
	}
	var pipelines, cards []fact
	for _, f := range latest {
		if f.Kind == "pipeline_seen" {
			pipelines = append(pipelines, f)
			continue
		}
		cards = append(cards, f)
		repo := str(f.Body, "repo")
		if repo == "" {
			repo = taskRepos[str(f.Body, "task")]
		}
		id := "question_cards"
		if f.Kind == "approval_seen" {
			id = "approval_cards"
		}
		at := stamp(f.Body, "created_at")
		if !at.IsZero() {
			out[id] = append(out[id], sample{Repo: repo, At: at, Value: 1})
		}
	}
	parents := map[string]string{}
	for _, l := range landed {
		parents[l.Stream+"/"+str(l.Body, "R")] = str(l.Body, "M0")
	}
	contains := func(repo, revision, commit string) bool {
		visited := map[string]bool{}
		for revision != "" && !visited[revision] {
			if revision == commit {
				return true
			}
			visited[revision] = true
			revision = parents["landing:"+repo+"/"+revision]
		}
		return false
	}
	seen := map[string]bool{}
	deliveredTasks := map[string]bool{}
	for _, l := range landed {
		repo := strings.TrimPrefix(l.Stream, "landing:")
		repos[repo] = true
		commit := str(l.Body, "R")
		if commit == "" || seen[repo+commit] {
			continue
		}
		seen[repo+commit] = true
		task := str(l.Body, "task")
		if task == "" {
			task = l.Task
		}
		start := starts[task]
		var prod time.Time
		var runs, reuses float64
		for _, p := range pipelines {
			if str(p.Body, "repo") != repo {
				continue
			}
			revision := str(p.Body, "revision")
			if !contains(repo, revision, commit) {
				continue
			}
			if revision == commit && str(p.Body, "kind") == "ci" {
				if str(p.Body, "tested_by") != "" {
					reuses++
				}
				runs++
			}
			if strings.HasSuffix(str(p.Body, "pipeline"), "deliver-platform-prod") && strings.EqualFold(str(p.Body, "result"), "succeeded") {
				at := stamp(p.Body, "completed_at")
				if !at.Before(l.At) && (prod.IsZero() || at.Before(prod)) {
					prod = at
				}
			}
		}
		out["ci_runs_per_commit"] = append(out["ci_runs_per_commit"], sample{Repo: repo, At: l.At, Value: runs, Reuses: reuses})
		if start.IsZero() || prod.IsZero() || !prod.After(start) {
			continue
		}
		// Lead time is one sample per delivered task, even if it landed
		// more than one main commit. CI retains its per-commit denominator.
		if deliveredTasks[repo+"/"+task] {
			continue
		}
		deliveredTasks[repo+"/"+task] = true
		lead := prod.Sub(start).Seconds()
		out["lead_time_s"] = append(out["lead_time_s"], sample{Repo: repo, At: prod, Value: lead})
		// Union the card intervals so simultaneous cards do not double count wait.
		type interval struct{ a, b time.Time }
		var waits []interval
		for _, c := range cards {
			if str(c.Body, "task") != task {
				continue
			}
			a := stamp(c.Body, "created_at")
			b := stamp(c.Body, "decided_at")
			if c.Kind == "question_seen" {
				b = stamp(c.Body, "answered_at")
			}
			if a.IsZero() || b.IsZero() {
				continue
			}
			if a.Before(start) {
				a = start
			}
			if b.After(prod) {
				b = prod
			}
			if b.After(a) {
				waits = append(waits, interval{a, b})
			}
		}
		sort.Slice(waits, func(i, j int) bool { return waits[i].a.Before(waits[j].a) })
		var seconds float64
		var end time.Time
		for _, w := range waits {
			a := w.a
			if a.Before(end) {
				a = end
			}
			if w.b.After(a) {
				seconds += w.b.Sub(a).Seconds()
				end = w.b
			}
		}
		out["owner_wait_share"] = append(out["owner_wait_share"], sample{Repo: repo, At: prod, Value: seconds, Denominator: lead})
	}
	return out, repos
}

func dataKey(d metrics.Definition) string {
	switch d.Extractor {
	case "task_lead_time":
		return "lead_time_s"
	case "cards_opened":
		if str(d.Params, "kind") == "approval" {
			return "approval_cards"
		}
		return "question_cards"
	case "tasks_accepted":
		return "throughput_tasks"
	case "macro_wall_time":
		return "macro_" + str(d.Params, "state") + "_s"
	default:
		return d.Extractor
	}
}
