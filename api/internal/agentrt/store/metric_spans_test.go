package store

import (
	"testing"
	"time"
)

func TestMetricSpanPartition(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	interval := func(a, b int) MetricSpanInterval { return MetricSpanInterval{at(a), at(b)} }
	for _, scope := range []MetricSpan{{ScopeKind: "wave", ScopeRef: "wave_a"}, {ScopeKind: "plan_node", ScopeRef: "R6:6.1"}, {ScopeKind: "plan_node", ScopeRef: "R6"}, {ScopeKind: "all", Label: "group:0"}} {
		scope.Start = base
		scope.End = at(100)
		scope.Doing = []MetricSpanInterval{interval(-10, 20), interval(10, 30), interval(70, 120)}
		scope.OwnerWait = []MetricSpanInterval{interval(20, 50), interval(40, 60), interval(80, 90)}
		scope.Accepted = []time.Time{at(30), at(80), at(110)}
		scope.RemainingTasks = 2
		rows, err := ComputeMetricSpan(scope, at(200))
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range []string{"60", "30", "10", "2"} {
			if *rows[i].Value != want {
				t.Fatalf("%s: got %s want %s", scope.ScopeKind, *rows[i].Value, want)
			}
		}
		stats := rows[0].StatsJSON
		if stats["remaining_s"] != float64(100) || stats["wall_s"] != float64(100) {
			t.Fatal(stats)
		}
	}
}

func TestMetricSpanOpenAndZeroThroughput(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)
	rows, err := ComputeMetricSpan(MetricSpan{ScopeKind: "wave", ScopeRef: "w", Start: start, OwnerWait: []MetricSpanInterval{{Start: start}}, RemainingTasks: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if *rows[1].Value != "3600" || !rows[0].WindowEnd.Equal(now) || rows[3].StatsJSON["remaining_s"] != nil {
		t.Fatal(rows)
	}
}

func TestMetricSpanInvalidEvidence(t *testing.T) {
	t.Parallel()
	now := time.Now()
	start := now.Add(-time.Hour)
	for _, s := range []MetricSpan{{ScopeKind: "repo", ScopeRef: "x", Start: start}, {ScopeKind: "wave", Start: start}, {ScopeKind: "wave", ScopeRef: "w", Start: now}, {ScopeKind: "wave", ScopeRef: "w", Start: start, Doing: []MetricSpanInterval{{Start: now, End: start}}}} {
		if _, err := ComputeMetricSpan(s, now); err == nil {
			t.Fatal("invalid span accepted")
		}
	}
}
