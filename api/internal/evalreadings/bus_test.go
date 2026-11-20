package evalreadings

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 2026-11-20, the metrics line: bus events per type come from the relay's
// counter. The fake gives the shape PROD gave at 15:20Z (fractional
// increases, drill:* test types, a type at 0). Drill types are left out,
// values are rounded, a zero is a measured zero.
func TestBusEventsByType(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 7)
	var asked string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = expr
		return []Sample{
			{Labels: map[string]string{"namespace": "bifrost-platform-prod", "type": "ci.run.finished"}, Value: 1932.15},
			{Labels: map[string]string{"namespace": "bifrost-platform-prod", "type": "drill:Y-5"}, Value: 1.0},
			{Labels: map[string]string{"namespace": "bifrost-platform-stg", "type": "plan.tree.changed"}, Value: 0},
		}, nil
	}
	rs := busEventReadings(context.Background(), prom, w)
	if len(rs) != 2 {
		t.Fatalf("readings: %+v", rs)
	}
	if r := rs[0]; r.Labels["env"] != "prod" || r.Labels["type"] != "ci.run.finished" || r.Status != StatusOK || *r.Value != 1932 {
		t.Fatalf("prod ci.run.finished: %+v", r)
	}
	if r := rs[1]; r.Labels["env"] != "stg" || r.Status != StatusOK || *r.Value != 0 {
		t.Fatalf("a type at 0 is a measured zero: %+v", r)
	}
	if !strings.Contains(asked, "@ "+itoa(w.To.Unix())) {
		t.Fatalf("query not pinned to the window end: %s", asked)
	}
	if rs := busEventReadings(context.Background(), nil, w); rs[0].Status != StatusUnmeasured {
		t.Fatalf("no Prometheus: %+v", rs[0])
	}
}

// 2026-11-20 (TD-588): the bus was down about 75 minutes and the first read of
// this expression gave 75. A window the gauge mostly does not cover is
// too_few; a fully seen window with no down minutes is 0, not unmeasured.
func TestBusOutageMinutes(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 7)
	minutes := float64(7 * 24 * 60)
	prom := func(down, seen float64, downEmpty bool) Prom {
		return func(_ context.Context, expr string) ([]Sample, error) {
			if strings.Contains(expr, "< 1") {
				if downEmpty {
					return nil, nil
				}
				return []Sample{{Value: down}}, nil
			}
			return []Sample{{Value: seen}}, nil
		}
	}
	rs := busOutageReadings(context.Background(), prom(75, minutes, false), w)
	if r := rs[0]; r.Status != StatusOK || *r.Value != 75 || r.Samples != int(minutes) {
		t.Fatalf("75 minutes down: %+v", r)
	}
	rs = busOutageReadings(context.Background(), prom(0, minutes, true), w)
	if r := rs[0]; r.Status != StatusOK || *r.Value != 0 {
		t.Fatalf("no down minutes is 0: %+v", r)
	}
	rs = busOutageReadings(context.Background(), prom(5, 60, false), w)
	if r := rs[0]; r.Status != StatusTooFew || r.Value != nil {
		t.Fatalf("one hour seen in seven days must be too_few: %+v", r)
	}
	rs = busOutageReadings(context.Background(), prom(0, 0, true), w)
	if r := rs[0]; r.Status != StatusUnmeasured {
		t.Fatalf("no gauge: %+v", r)
	}
}
