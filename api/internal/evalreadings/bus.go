package evalreadings

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// The bus readings (the metrics line's week-one list, 2026-11-20): how many
// events of each type the relay published, and how many minutes the bus was
// down. Both come from series Prometheus already scrapes; nothing new is
// recorded for them. Prometheus keeps 10 days.

// busEventReadings: per environment and event type, the events the relay
// published to the bus in the window (increase of
// bifrost_bus_relay_published_by_type_total, rounded). Types starting with
// "drill:" are test events and are left out. A type with no events in the
// window is a measured zero.
func busEventReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "bus_events"
	d := def("Prometheus bifrost_bus_relay_published_by_type_total (platform workers' relay), namespaces bifrost-platform-prod and -stg",
		"increase over the window, by environment and event type, rounded; drill:* types left out")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	expr := fmt.Sprintf(`sum by (namespace, type) (increase(bifrost_bus_relay_published_by_type_total{namespace=~"bifrost-platform-(prod|stg)"}[%ds] @ %d))`, span, w.To.Unix())
	vals, err := prom(ctx, expr)
	if err != nil {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	var out []Reading
	for _, s := range vals {
		typ := s.Labels["type"]
		if typ == "" || strings.HasPrefix(typ, "drill:") {
			continue
		}
		n := int(math.Round(s.Value))
		r := Reading{Name: name, Labels: map[string]string{"env": envOf(s.Labels["namespace"]), "type": typ}, Value: count(n), Unit: "count", Samples: n, Window: w, Definition: d}
		r = finish(r, false)
		measuredZero(&r)
		out = append(out, r)
	}
	if len(out) == 0 {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "no relay series in the window"}, false)}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Labels["env"] != out[j].Labels["env"] {
			return out[i].Labels["env"] < out[j].Labels["env"]
		}
		return out[i].Labels["type"] < out[j].Labels["type"]
	})
	return out
}

func envOf(namespace string) string {
	return strings.TrimPrefix(namespace, "bifrost-platform-")
}

// busOutageExprs: the minutes NATS (data/nats, the one bus both environments
// use) had no ready replica, and the minutes the gauge was seen at all.
func busOutageExprs(span int, at int64) (down, seen string) {
	sel := `kube_statefulset_status_replicas_ready{namespace="data",statefulset="nats"}`
	return fmt.Sprintf("count_over_time((%s < 1)[%ds:1m] @ %d)", sel, span, at),
		fmt.Sprintf("count_over_time(%s[%ds:1m] @ %d)", sel, span, at)
}

// busOutageReadings: minutes in the window with NATS not ready, at one-minute
// resolution. Minutes where kube-state-metrics has no series are not counted
// either way; below 90% of the window's minutes seen, the reading is too_few.
// Origin: 2026-11-20 02:55-04:10Z the bus was down about 75 minutes (TD-588);
// the first read of this expression over the 7 days before 15:20Z gave 75.
func busOutageReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "bus_outage_minutes"
	d := def("Prometheus kube-state-metrics kube_statefulset_status_replicas_ready, data/nats",
		"minutes in the window (1-minute steps) with no ready NATS replica")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Unit: "minutes", Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Unit: "minutes", Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	downExpr, seenExpr := busOutageExprs(span, w.To.Unix())
	seen, err := prom(ctx, seenExpr)
	if err != nil {
		return []Reading{finish(Reading{Name: name, Unit: "minutes", Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	n := 0
	for _, s := range seen {
		n += int(s.Value)
	}
	floor := int(0.9 * float64(span) / 60)
	r := Reading{Name: name, Unit: "minutes", Samples: n, MinSamples: floor, Window: w, Definition: d}
	if n == 0 {
		r.Note = "no samples of the gauge in the window"
		return []Reading{finish(r, false)}
	}
	down, err := prom(ctx, downExpr)
	if err != nil {
		r.Note = "Prometheus query failed: " + err.Error()
		return []Reading{finish(Reading{Name: name, Unit: "minutes", Window: w, Definition: d, Note: r.Note}, false)}
	}
	m := 0
	for _, s := range down {
		m += int(s.Value)
	}
	r.Value = count(m)
	r = finish(r, false)
	if n < floor {
		r.Status, r.Value = StatusTooFew, nil
		r.Note = "the gauge covers too little of the window"
	}
	return []Reading{r}
}
