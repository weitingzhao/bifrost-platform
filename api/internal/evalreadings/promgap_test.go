package evalreadings

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 2026-11-22, Audit: was catalog_domain_reads for 11-21 hit by the monitoring
// outage? The 11-21 window read 2789 of 2880 steps (45.5 minutes missing).
func TestPrometheusGapMinutes(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC), 1)
	var asked string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = expr
		return []Sample{{Labels: map[string]string{}, Value: 2789}}, nil
	}
	rs := promGapReadings(context.Background(), prom, w)
	if len(rs) != 1 || rs[0].Status != StatusOK || *rs[0].Value != 45.5 {
		t.Fatalf("reading: %+v", rs)
	}
	if !strings.Contains(asked, "[86400s:30s] @ "+itoa(w.To.Unix())) {
		t.Fatalf("query: %s", asked)
	}
	full := func(context.Context, string) ([]Sample, error) { return []Sample{{Value: 2880}}, nil }
	if rs := promGapReadings(context.Background(), full, w); *rs[0].Value != 0 {
		t.Fatalf("no gap is 0: %+v", rs[0])
	}
}
