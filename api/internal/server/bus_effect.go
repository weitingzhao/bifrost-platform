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
// from its source when asked (2026-11-17, the Owner: he cannot
// see where the communication work stands or what it actually does). A
// reading that cannot be taken here says so instead of a number.

type effectItem struct {
	What string `json:"what"`
	When string `json:"when"`
	Ref  string `json:"ref"`
}

type effectRow struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	// Before is what the value is compared with, computed too: a
	// baseline the system took at a time (bus/nextgen baselines.yaml,
	// queried at that time) or the same reading over the previous window.
	// Empty when the system has none.
	Before     string       `json:"before,omitempty"`
	BeforeKind string       `json:"before_kind,omitempty"`
	How        string       `json:"how"`
	Unmeasured string       `json:"unmeasured,omitempty"`
	Items      []effectItem `json:"items,omitempty"`
}

type busEffect struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Env         string      `json:"env"`
	Rows        []effectRow `json:"rows"`
}

func (s *Server) handleBusEffect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	out := busEffect{GeneratedAt: now, Env: env}
	out.Rows = append(out.Rows, s.effectMainToSTG(now))
	out.Rows = append(out.Rows, s.effectListRuns(ctx, env)...)
	out.Rows = append(out.Rows, effectSwitchRows()...)
	out.Rows = append(out.Rows, s.effectWakes(ctx, now))
	out.Rows = append(out.Rows, s.effectRelay(ctx, env)...)
	writeJSON(w, http.StatusOK, out)
}

// effectMainToSTG: from a platform landing finished to the next STG delivery
// created, over the last 24 hours and the 24 before, from the run cache.
func (s *Server) effectMainToSTG(now time.Time) effectRow {
	row := effectRow{Key: "main_to_stg", Label: "Main moved → STG delivery started",
		How: "Median seconds from a bifrost-platform landing (land-*) finishing to the next bifrost-deliver-platform run being created, last 24 h; read from the bus run cache. Minutes-long ones waited for the release window."}
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
		row.Before, row.BeforeKind = fmt.Sprintf("median %.0f s the 24 h before (%d landings)", pm, pn), "previous"
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
		for _, k := range []string{"site", "route", "type", "__name__"} {
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
		How: "apiserver_request_total, LIST of pipelineruns, increase over 10 minutes (every client, not only the platform); beside it the runs created in the same 10 minutes and the runs kept, from the bus run cache. Two 10-minute windows differ by their edges: compare with the run counts."}
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
		How: "bifrost_kube_requests_total{site}, LIST of pipelineruns, increase over 10 minutes, namespace " + ns + "; the site is the calling reader."}
	v, err := s.promql(ctx, fmt.Sprintf(`sum by (site) (increase(bifrost_kube_requests_total{verb="LIST",resource="pipelineruns",namespace=%q}[10m]))`, ns))
	if err != nil {
		own.Unmeasured = err.Error()
		s.baselineInto(ctx, &cluster, "list_cluster")
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
		How: "bifrost_bus_readers_*: reads answered from the run cache, sampled comparisons with a list, differences; since this process started."}
	if r, err := s.promql(ctx, fmt.Sprintf(`sum by (__name__) ({__name__=~"bifrost_bus_readers_(cache_served|compared|mismatch)_total",namespace=%q})`, ns)); err != nil {
		readers.Unmeasured = err.Error()
	} else {
		readers.Value = fmt.Sprintf("%.0f from the cache, %.0f sampled, %.0f differences", r["bifrost_bus_readers_cache_served_total"], r["bifrost_bus_readers_compared_total"], r["bifrost_bus_readers_mismatch_total"])
	}
	s.baselineInto(ctx, &cluster, "list_cluster")
	s.baselineInto(ctx, &own, "list_platform_"+env)
	return []effectRow{cluster, own, readers}
}

func (s *Server) effectWakes(ctx context.Context, now time.Time) effectRow {
	row := effectRow{Key: "wakes", Label: "Seats woken by the bus (last 24 h)",
		How: "The dispatcher's wake messages in the ledger: delivered to a seat's inbox, or sent to the duty seat because the recipient was unknown; seconds from the message to the seat acknowledging it (its doorbell wakes it, then it reads)."}
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
		How: "bifrost_bus_relay_* since the workers started; seconds since the cluster gate's last event; per event type, how long ago the relay last published one (bifrost_bus_relay_last_published_timestamp_seconds). Who reads is in Clients below."}
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
	if ages, err := s.promql(ctx, fmt.Sprintf(`time() - max by (type) (bifrost_bus_relay_last_published_timestamp_seconds{namespace=%q})`, ns)); err == nil {
		types := make([]string, 0, len(ages))
		for t := range ages {
			if t != "" {
				types = append(types, t)
			}
		}
		sort.Strings(types)
		for _, t := range types {
			row.Items = append(row.Items, effectItem{What: t, When: "last published " + secondsAgo(ages[t])})
		}
	}
	return []effectRow{row}
}

func secondsAgo(s float64) string {
	switch {
	case s < 120:
		return fmt.Sprintf("%.0f s ago", s)
	case s < 7200:
		return fmt.Sprintf("%.0f min ago", s/60)
	default:
		return fmt.Sprintf("%.1f h ago", s/3600)
	}
}
