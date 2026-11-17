package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

// GET /api/v1/bus/effect: what the bus's work does, each reading computed
// from its source when asked, none typed in (2026-11-17, the Owner: he cannot
// see where the communication work stands or what it actually does). A
// reading that cannot be taken here says so instead of a number.

type effectItem struct {
	What string `json:"what"`
	When string `json:"when"`
	Ref  string `json:"ref"`
}

type effectRow struct {
	Key        string       `json:"key"`
	Label      string       `json:"label"`
	Value      string       `json:"value"`
	Before     string       `json:"before"`
	How        string       `json:"how"`
	Unmeasured string       `json:"unmeasured,omitempty"`
	Items      []effectItem `json:"items,omitempty"`
}

type busEffect struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Env         string      `json:"env"`
	Rows        []effectRow `json:"rows"`
}

// effectSwitches are the source readers moved onto the bus, each with the
// commit that moved it (nextgen/run-readers.md is the inventory). They are
// the code's own state, so they live beside it.
var effectSwitches = struct {
	runReaders, mainReaders int
	runs, main              []effectItem
}{
	runReaders: 15, mainReaders: 11,
	runs: []effectItem{
		{"pipeline run records read the gate's finished-run events", "2026-11-16", "bus step 2"},
		{"head-release reads the run cache", "2026-11-17 00:57", "f508ab3c"},
		{"pipeline runs page reads the run cache", "2026-11-16 21:44", "display side by side"},
		{"supply chain page reads the run cache", "2026-11-16 21:52", "display side by side"},
		{"release context reads the run cache", "2026-11-16 21:52", "display side by side"},
		{"CI index retired", "2026-11-16 23:19", "CI line"},
	},
	main: []effectItem{
		{"units/config, CI scorecard and domains ask the ledger", "2026-11-17 15:46Z", "162700cb"},
		{"release context asks the ledger", "2026-11-17 15:46Z", "162700cb"},
		{"commit evidence asks the ledger", "2026-11-17 15:46Z", "162700cb"},
		{"plan history asks the ledger", "2026-11-17 15:46Z", "162700cb"},
		{"configuration boundaries (infra main)", "not yet", "infra is not in BUS_GIT_REPOS"},
		{"five gate readers keep asking Gitea by design", "-", "gates read the source"},
	},
}

func (s *Server) handleBusEffect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	out := busEffect{GeneratedAt: now, Env: env}
	out.Rows = append(out.Rows, s.effectMainToSTG(now))
	out.Rows = append(out.Rows, s.effectListRuns(ctx, env)...)
	out.Rows = append(out.Rows, effectRow{
		Key: "switch_runs", Label: "Source readers moved: run lists",
		Value:  fmt.Sprintf("%d / %d", len(effectSwitches.runs), effectSwitches.runReaders),
		Before: "0 / 15 on 2026-11-16 morning",
		How:    "Readers that listed pipeline runs from the cluster, now on the bus or retired; the gate readers left belong to the CI line.",
		Items:  effectSwitches.runs,
	}, effectRow{
		Key: "switch_main", Label: "Source readers moved: where main is",
		Value:  fmt.Sprintf("%d of %d judged; 4 moved, 5 kept on Gitea by design", effectSwitches.mainReaders, effectSwitches.mainReaders),
		Before: "0 on 2026-11-16",
		How:    "Display readers ask the ledger's git.main.advanced for the repositories gate-git watches (bifrost-platform today), Gitea for the rest.",
		Items:  effectSwitches.main,
	})
	out.Rows = append(out.Rows, s.effectWakes(ctx, now))
	out.Rows = append(out.Rows, s.effectRelay(ctx, env)...)
	writeJSON(w, http.StatusOK, out)
}

// effectMainToSTG: from a platform landing finished to the next STG delivery
// created, over the last 24 hours and the 24 before, from the run cache.
func (s *Server) effectMainToSTG(now time.Time) effectRow {
	row := effectRow{Key: "main_to_stg", Label: "Main moved → STG delivery started",
		Before: "baseline 51 s (2026-11-16); 42–47 s on 2026-11-13..15",
		How:    "Median seconds from a bifrost-platform landing (land-*) finishing to the next bifrost-deliver-platform run being created, last 24 h; read from the bus run cache. Minutes-long ones waited for the release window."}
	if s.delivery == nil {
		row.Unmeasured = "no delivery reader on this platform"
		return row
	}
	lands, ok1 := s.delivery.CachedRunsOf("bifrost-land")
	dels, ok2 := s.delivery.CachedRunsOf("bifrost-deliver-platform")
	if !ok1 || !ok2 {
		row.Unmeasured = "the run cache is not synced in this process"
		return row
	}
	done, created := landedPlatform(lands), createdTimes(dels)
	m, n, fast := latencies(done, created, now.Add(-24*time.Hour), now)
	pm, pn, _ := latencies(done, created, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	if n == 0 {
		row.Value = "no platform landing in the last 24 h"
	} else {
		row.Value = fmt.Sprintf("median %.0f s (%d landings, %d within 60 s)", m, n, fast)
	}
	if pn > 0 {
		row.Before = fmt.Sprintf("median %.0f s the 24 h before (%d); ", pm, pn) + row.Before
	}
	return row
}

func landedPlatform(runs []unstructured.Unstructured) []time.Time {
	var out []time.Time
	for _, u := range runs {
		params, _, _ := unstructured.NestedSlice(u.Object, "spec", "params")
		repo := ""
		for _, p := range params {
			if m, ok := p.(map[string]any); ok && m["name"] == "repo" {
				repo, _ = m["value"].(string)
			}
		}
		end, _, _ := unstructured.NestedString(u.Object, "status", "completionTime")
		if repo != "bifrost-platform" || end == "" || !succeeded(u) {
			continue
		}
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func succeeded(u unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]any); ok && m["type"] == "Succeeded" {
			return m["status"] == "True"
		}
	}
	return false
}

func runsCreatedSince(runs []unstructured.Unstructured, since time.Time) int {
	n := 0
	for _, u := range runs {
		if !u.GetCreationTimestamp().Time.Before(since) {
			n++
		}
	}
	return n
}

func createdTimes(runs []unstructured.Unstructured) []time.Time {
	out := make([]time.Time, 0, len(runs))
	for _, u := range runs {
		out = append(out, u.GetCreationTimestamp().Time)
	}
	return out
}

// latencies pairs each landing finished in [from, to) with the first
// delivery created at or after it (within 30 minutes): the median seconds,
// how many, and how many within 60 seconds.
func latencies(done, created []time.Time, from, to time.Time) (float64, int, int) {
	sort.Slice(created, func(i, j int) bool { return created[i].Before(created[j]) })
	var secs []float64
	fast := 0
	for _, t := range done {
		if t.Before(from) || !t.Before(to) {
			continue
		}
		i := sort.Search(len(created), func(i int) bool { return !created[i].Before(t) })
		if i == len(created) || created[i].Sub(t) > 30*time.Minute {
			continue
		}
		d := created[i].Sub(t).Seconds()
		secs = append(secs, d)
		if d <= 60 {
			fast++
		}
	}
	if len(secs) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(secs)
	m := secs[len(secs)/2]
	if len(secs)%2 == 0 {
		m = (secs[len(secs)/2-1] + secs[len(secs)/2]) / 2
	}
	return m, len(secs), fast
}

func (s *Server) promql(ctx context.Context, q string) (map[string]float64, error) {
	if s.telemetry == nil {
		return nil, fmt.Errorf("no Prometheus reader on this platform")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	points, err := s.telemetry.PromQL(ctx, q)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, p := range points {
		key := ""
		for _, k := range []string{"site", "route", "__name__"} {
			if v := p.Labels[k]; v != "" {
				key = v
				break
			}
		}
		out[key] += p.Value
	}
	return out, nil
}

// effectListRuns: pipeline run lists per 10 minutes, the cluster's own count
// beside this platform's by caller.
func (s *Server) effectListRuns(ctx context.Context, env string) []effectRow {
	cluster := effectRow{Key: "list_cluster", Label: "Pipeline run lists, whole cluster",
		Before: "9,312 per 10 min on 2026-11-16 22:02; 820 on 2026-11-17 02:09",
		How:    "apiserver_request_total, LIST of pipelineruns, increase over 10 minutes (every client, not only the platform); beside it the runs created in the same 10 minutes and the runs kept, from the bus run cache. Two 10-minute windows differ by their edges: compare with the run counts."}
	if v, err := s.promql(ctx, `sum(increase(apiserver_request_total{verb="LIST",resource="pipelineruns"}[10m]))`); err != nil {
		cluster.Unmeasured = err.Error()
	} else {
		cluster.Value = fmt.Sprintf("%.0f per 10 min", v[""])
		// Beside it, how busy CI was: runs created in the same 10 minutes
		// and runs the cluster keeps (other clients list by what is kept).
		if s.delivery != nil {
			if runs, ok := s.delivery.CachedRunsOf(""); ok {
				created, kept := runsCreatedSince(runs, time.Now().Add(-10*time.Minute)), len(runs)
				cluster.Value += fmt.Sprintf(" · %d runs started in the same 10 min, %d kept", created, kept)
			}
		}
	}
	ns := "bifrost-platform-" + env
	own := effectRow{Key: "list_platform", Label: "Pipeline run lists, this platform by caller",
		Before: "about 1,840 per 10 min on 2026-11-17 00:43 (PROD); 179 at 02:09",
		How:    "bifrost_kube_requests_total{site}, LIST of pipelineruns, increase over 10 minutes, namespace " + ns + "; the site is the calling reader."}
	v, err := s.promql(ctx, fmt.Sprintf(`sum by (site) (increase(bifrost_kube_requests_total{verb="LIST",resource="pipelineruns",namespace=%q}[10m]))`, ns))
	if err != nil {
		own.Unmeasured = err.Error()
		return []effectRow{cluster, own}
	}
	type kv struct {
		k string
		v float64
	}
	var all []kv
	total := 0.0
	for k, x := range v {
		total += x
		if math.Round(x) > 0 {
			all = append(all, kv{k, x})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	own.Value = fmt.Sprintf("%.0f per 10 min", total)
	for _, e := range all {
		own.Items = append(own.Items, effectItem{What: e.k, When: fmt.Sprintf("%.0f", e.v)})
	}
	readers := effectRow{Key: "display_reads", Label: "Display pages reading runs",
		Before: "every page view listed the cluster before 2026-11-16 21:44",
		How:    "bifrost_bus_readers_*: reads answered from the run cache, sampled comparisons with a list, differences; since this process started."}
	if r, err := s.promql(ctx, fmt.Sprintf(`sum by (__name__) ({__name__=~"bifrost_bus_readers_(cache_served|compared|mismatch)_total",namespace=%q})`, ns)); err != nil {
		readers.Unmeasured = err.Error()
	} else {
		readers.Value = fmt.Sprintf("%.0f from the cache, %.0f sampled, %.0f differences", r["bifrost_bus_readers_cache_served_total"], r["bifrost_bus_readers_compared_total"], r["bifrost_bus_readers_mismatch_total"])
	}
	return []effectRow{cluster, own, readers}
}

func (s *Server) effectWakes(ctx context.Context, now time.Time) effectRow {
	row := effectRow{Key: "wakes", Label: "Seats woken by the bus (last 24 h)",
		Before: "before 2026-11-16 seats were nudged by the duty seat every 30 minutes",
		How:    "The dispatcher's wake messages in the ledger: delivered to a seat's inbox, or sent to the duty seat because the recipient was unknown; seconds from the message to the seat acknowledging it (its doorbell wakes it, then it reads)."}
	db := s.recordDB()
	if db == nil {
		row.Unmeasured = "the agent runtime database is not connected"
		return row
	}
	w, err := coord.Wakes(ctx, db, now.Add(-24*time.Hour))
	if err != nil {
		row.Unmeasured = err.Error()
		return row
	}
	row.Value = fmt.Sprintf("%d delivered, %d to the duty seat; %d acknowledged, median %.0f s", w.Delivered, w.Undeliverable, w.Acked, w.AckMedianSecs)
	seats := make([]string, 0, len(w.BySeat))
	for k := range w.BySeat {
		seats = append(seats, k)
	}
	sort.Strings(seats)
	for _, k := range seats {
		row.Items = append(row.Items, effectItem{What: k, When: fmt.Sprintf("%d", w.BySeat[k])})
	}
	return row
}

func (s *Server) effectRelay(ctx context.Context, env string) []effectRow {
	ns := "bifrost-platform-" + env
	row := effectRow{Key: "relay", Label: "Bus health",
		Before: "-",
		How:    "bifrost_bus_relay_* since the workers started; seconds since the cluster gate's last event. Each type's latest event and who reads are in Live events and Clients below."}
	v, err := s.promql(ctx, fmt.Sprintf(`sum by (__name__) ({__name__=~"bifrost_bus_relay_(published|errors|drift)_total",namespace=%q})`, ns))
	if err != nil {
		row.Unmeasured = err.Error()
		return []effectRow{row}
	}
	age, err := s.promql(ctx, fmt.Sprintf(`time() - max(bifrost_bus_gate_cluster_last_event_timestamp_seconds{namespace=%q})`, ns))
	last := "unknown"
	if err == nil {
		if a, ok := age[""]; ok {
			last = fmt.Sprintf("%.0f s ago", a)
		}
	}
	row.Value = fmt.Sprintf("%.0f relayed, %.0f errors, %.0f drift; last cluster event %s", v["bifrost_bus_relay_published_total"], v["bifrost_bus_relay_errors_total"], v["bifrost_bus_relay_drift_total"], last)
	return []effectRow{row}
}
