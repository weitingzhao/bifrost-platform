package evalreadings

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 2026-11-21, the metrics line's law-69 list: 45 workloads had no metrics of
// their own and no page showed which. By namespace, the uncovered ones named;
// a workload whose target is down still counts as covered (the record keeps
// it with value 0).
func TestCoverageByNamespace(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 1)
	wl := func(ns, name string, v float64) Sample {
		return Sample{Labels: map[string]string{"namespace": ns, "workload": name}, Value: v}
	}
	var asked []string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = append(asked, expr)
		if strings.Contains(expr, "workload_info") {
			return []Sample{wl("research", "research-api", 1), wl("research", "research-mcp", 1), wl("research", "pine", 1), wl("data", "pg", 1)}, nil
		}
		return []Sample{wl("research", "research-api", 1), wl("data", "pg", 0)}, nil
	}
	rs := coverageByNamespaceReadings(context.Background(), prom, w)
	if len(rs) != 2 || rs[0].Labels["namespace"] != "data" || *rs[0].Value != 1 || rs[0].Note != "" {
		t.Fatalf("data: %+v", rs)
	}
	if rs[1].Samples != 3 || *rs[1].Value != 0.333 || rs[1].Note != "no metrics of their own: pine, research-mcp" {
		t.Fatalf("research: %+v", rs[1])
	}
	for _, q := range asked {
		if !strings.Contains(q, "@ "+itoa(w.To.Unix())) {
			t.Fatalf("query not at the window's end: %s", q)
		}
	}
	if rs := coverageByNamespaceReadings(context.Background(), prom, ClosedWindow(w.To, 14)); len(rs) != 1 || rs[0].Status != StatusUnmeasured {
		t.Fatalf("over 10 days: %+v", rs)
	}
}
