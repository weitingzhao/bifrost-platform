package evalreadings

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 2026-11-20, the metrics line's step 9: activations per rule from
// ALERTS_FOR_STATE, drill alerts left out in the query, rounded, sorted by
// rule name; a series without alertname is dropped.
func TestAlertActivations(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 7)
	var asked string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = expr
		return []Sample{
			{Labels: map[string]string{"alertname": "KubePodNotReady", "severity": "warning"}, Value: 19556},
			{Labels: map[string]string{"alertname": "BifrostClusterNodeDown", "severity": "critical"}, Value: 1},
			{Labels: map[string]string{}, Value: 3},
		}, nil
	}
	rs := alertActivationReadings(context.Background(), prom, w)
	if len(rs) != 2 || rs[0].Labels["alertname"] != "BifrostClusterNodeDown" || *rs[1].Value != 19556 || rs[1].Status != StatusOK {
		t.Fatalf("readings: %+v", rs)
	}
	if !strings.Contains(asked, `drill!="true"`) || !strings.Contains(asked, "@ "+itoa(w.To.Unix())) {
		t.Fatalf("query: %s", asked)
	}
	none := func(context.Context, string) ([]Sample, error) { return nil, nil }
	if rs := alertActivationReadings(context.Background(), none, w); rs[0].Status != StatusOK || *rs[0].Value != 0 {
		t.Fatalf("no alert active is 0: %+v", rs[0])
	}
}
