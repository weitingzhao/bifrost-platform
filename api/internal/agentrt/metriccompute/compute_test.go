package metriccompute

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/metrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestRecordedDelivery(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	f := func(kind string, minute int, b map[string]any) fact {
		return fact{Kind: kind, At: at.Add(time.Duration(minute) * time.Minute), Task: "task", Repo: "repo", Stream: "landing:repo", Body: b}
	}
	ts := func(minute int) string { return at.Add(time.Duration(minute) * time.Minute).Format(time.RFC3339) }
	facts := []fact{
		f("attempt_started", 0, nil), f("attempt_started", 5, nil),
		f("note", 10, map[string]any{"landing": map[string]any{"kind": "prepared", "op": "one", "task": "task", "R": "commit", "M0": "base"}}),
		f("note", 15, map[string]any{"landing": map[string]any{"kind": "landed", "op": "one", "R": "commit"}}),
		f("note", 20, map[string]any{"landing": map[string]any{"kind": "prepared", "op": "two", "task": "other", "R": "later", "M0": "commit"}}),
		f("note", 25, map[string]any{"landing": map[string]any{"kind": "landed", "op": "two", "R": "later"}}),
		f("approval_seen", 4, map[string]any{"approval": 1, "task": "task", "created_at": ts(2)}),
		f("approval_seen", 7, map[string]any{"approval": 1, "task": "task", "created_at": ts(2), "decided_at": ts(7)}),
		f("question_seen", 8, map[string]any{"question": "q", "task": "task", "created_at": ts(4), "answered_at": ts(9)}),
		f("pipeline_seen", 12, map[string]any{"run": "ci", "uid": "ci", "kind": "ci", "repo": "repo", "revision": "commit"}),
		f("pipeline_seen", 13, map[string]any{"run": "reuse", "uid": "reuse", "kind": "ci", "repo": "repo", "revision": "commit", "tested_by": "ci"}),
		f("pipeline_seen", 30, map[string]any{"run": "prod", "uid": "prod", "pipeline": "bifrost-deliver-platform-prod", "kind": "delivery", "result": "Succeeded", "repo": "repo", "revision": "later", "completed_at": ts(30)}),
	}
	data, _ := extract(facts)
	if got := data["lead_time_s"]; len(got) != 1 || got[0].Value != 1800 {
		t.Fatalf("lead samples: %+v", got)
	}
	if got := data["owner_wait_share"][0]; got.Value != 420 || got.Denominator != 1800 {
		t.Fatalf("wait union: %+v", got)
	}
	if len(data["approval_cards"]) != 1 || len(data["question_cards"]) != 1 {
		t.Fatal("cards counted snapshots instead of identities")
	}
	if got := data["ci_runs_per_commit"][0]; got.Value != 2 || got.Reuses != 1 {
		t.Fatalf("CI: %+v", got)
	}
	r, err := metrics.Load()
	if err != nil {
		t.Fatal(err)
	}
	w := window{Kind: "hour", Start: at, End: at.Add(time.Hour)}
	for _, d := range r.Definitions() {
		reading := aggregate(d, data[dataKey(d)], "repo", w)
		if d.ID == "owner_wait_share" {
			v, _ := strconv.ParseFloat(*reading.Value, 64)
			if math.Abs(v-420.0/1800) > 1e-8 {
				t.Fatalf("weighted wait share %v", v)
			}
		}
		if d.ID == "release_window_occupancy" && (reading.Value != nil || reading.Conclusive) {
			t.Fatal("invented release occupancy")
		}
	}
}

func TestReadingFloorsAndBoundaries(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	d := metrics.Definition{ID: "test", Version: 2, Unit: "seconds", Headline: "p50", MinSamples: 2}
	w := window{Kind: "hour", Start: at, End: at.Add(time.Hour)}
	r := aggregate(d, []sample{{Repo: "repo", At: at, Value: 10}, {Repo: "repo", At: w.End, Value: 999}}, "repo", w)
	if r.Samples != 1 || r.Conclusive || r.Value == nil || *r.Value != "10" {
		t.Fatalf("reading %+v", r)
	}
	if floor(at.AddDate(0, 0, 6), "week") != at {
		t.Fatal("week must start Monday UTC")
	}
}

func TestClosedWindowsVersionsAndLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	rt := db.Connect(t, db.Runtime)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	if n, err := Compute(ctx, rt, at); err != nil || n != 0 {
		t.Fatalf("empty: %d %v", n, err)
	}
	// Labels are durable facts and their spans remain fixed when time advances.
	if err := store.SetMetricLabel(ctx, rt, "baseline:before", at, at.Add(time.Hour), "baseline", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMetricLabel(ctx, rt, "group:after", at, at.Add(time.Hour), "group", "owner"); err != nil {
		t.Fatal(err)
	}
	r, err := metrics.Load()
	if err != nil {
		t.Fatal(err)
	}
	defs := r.Definitions()[:1]
	for _, tc := range []struct {
		now  time.Time
		want int
	}{{at.Add(time.Hour), 0}, {at.Add(time.Hour + 5*time.Minute), 2}, {at.Add(2 * time.Hour), 0}} {
		n, e := compute(ctx, rt, tc.now, defs)
		if e != nil || n != tc.want {
			t.Fatalf("pass %s: %d %v want %d", tc.now, n, e, tc.want)
		}
	}
	defs[0].Version++
	if n, e := compute(ctx, rt, at.Add(2*time.Hour), defs); e != nil || n != 2 {
		t.Fatalf("new version: %d %v", n, e)
	}
	var count int
	if err = rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.metric_reading WHERE window_kind='span' AND NOT conclusive AND value IS NULL`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("span versions: %d %v", count, err)
	}
	var events int
	if err = rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type='metrics_computed'`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("idle passes wrote events: %d %v", events, err)
	}
}

func TestWindowScheduleIsSettledVersionedAndIdempotent(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	r, e := metrics.Load()
	if e != nil {
		t.Fatal(e)
	}
	var d metrics.Definition
	for _, v := range r.Definitions() {
		if v.ID == "approval_cards" {
			d = v
		}
	}
	facts := []fact{{Kind: "approval_seen", At: at.Add(10 * time.Minute), Body: map[string]any{"approval": 1, "repo": "repo", "created_at": at.Format(time.RFC3339)}}}
	have := map[string]bool{}
	if c := planReadings(nil, have, at, []metrics.Definition{d}); len(c.Readings) != 0 {
		t.Fatal("empty pass")
	}
	for _, now := range []time.Time{at.Add(59 * time.Minute), at.Add(time.Hour), at.Add(time.Hour + 299*time.Second)} {
		if c := planReadings(facts, have, now, []metrics.Definition{d}); len(c.Readings) != 0 {
			t.Fatalf("wrote open or unsettled window at %s", now)
		}
	}
	now := at.Add(time.Hour + 300*time.Second)
	c := planReadings(facts, have, now, []metrics.Definition{d})
	if len(c.Readings) != 2 {
		t.Fatalf("all and repo: %+v", c.Readings)
	}
	for _, v := range c.Readings {
		if v.WindowKind != "hour" || v.Samples != 1 || v.Value == nil || *v.Value != "1" {
			t.Fatalf("reading %+v", v)
		}
		have[key(v.MetricID, v.DefinitionVersion, v.ScopeRef, window{Label: v.Label, Start: v.WindowStart, End: v.WindowEnd})] = true
	}
	if got := planReadings(facts, have, now, []metrics.Definition{d}); len(got.Readings) != 0 {
		t.Fatal("second pass recomputed")
	}
	d.Version++
	if got := planReadings(facts, have, now, []metrics.Definition{d}); len(got.Readings) != 2 {
		t.Fatal("new version did not recompute")
	}
	// Facts polled later retain their source creation time for window membership.
	facts[0].At = at.Add(24 * time.Hour)
	d.WindowKinds = []string{"day", "week"}
	c = planReadings(facts, nil, at.AddDate(0, 0, 7), []metrics.Definition{d})
	day, week := false, false
	for _, v := range c.Readings {
		if v.Samples == 1 && v.WindowKind == "day" {
			day = true
		}
		if v.Samples == 1 && v.WindowKind == "week" {
			week = true
		}
	}
	if !day || !week {
		t.Fatal("source timestamps lost day/week samples")
	}
}

func TestPersistedCardsWriteOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	rt := db.Connect(t, db.Runtime)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	body := store.ApprovalFact{Number: 1, Status: "pending", Action: "test", Tier: "T2", Repo: "repo", CreatedAt: at}.Body()
	if _, err := store.RecordExternal(ctx, rt, "approval_seen", []map[string]any{body}); err != nil {
		t.Fatal(err)
	}
	r, err := metrics.Load()
	if err != nil {
		t.Fatal(err)
	}
	var defs []metrics.Definition
	for _, d := range r.Definitions() {
		if d.ID == "approval_cards" {
			defs = append(defs, d)
		}
	}
	now := at.Add(time.Hour + 5*time.Minute)
	for _, want := range []int{2, 0} {
		n, e := compute(ctx, rt, now, defs)
		if e != nil || n != want {
			t.Fatalf("write %d %v want %d", n, e, want)
		}
	}
	var count int
	if err = rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.metric_reading WHERE metric_id='approval_cards' AND value=1 AND samples=1 AND conclusive AND window_kind='hour'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("cards %d %v", count, err)
	}
	defs[0].Version++
	if n, e := compute(ctx, rt, now, defs); e != nil || n != 2 {
		t.Fatalf("changed version %d %v", n, e)
	}
	if err = rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.metric_reading`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("old readings retained %d %v", count, err)
	}
}
