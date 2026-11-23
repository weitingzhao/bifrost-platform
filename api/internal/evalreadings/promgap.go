package evalreadings

import (
	"context"
	"fmt"
	"math"
)

// promScrapeStep is how often Prometheus scrapes itself (kube-prometheus-stack
// default, 30 s): a window without gaps has one sample per step.
const promScrapeStep = 30

// promGapReadings: prometheus_gap_minutes, how long in the window Prometheus
// itself recorded nothing. Every Prometheus-backed reading of the window
// misses what happened in a gap (a counter's increase is caught up only if
// its process did not restart). Origin: Audit, 2026-11-22, asked whether
// catalog_domain_reads for 11-21 was hit by the monitoring outage (node 07,
// 23:24Z to about 00:15Z, Prometheus on it); nothing in the archive said so.
// Gaps under 5 minutes are not seen (the lookback of an instant query).
func promGapReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "prometheus_gap_minutes"
	d := def("Prometheus up of its own job (kube-prometheus-stack-prometheus), sampled every 30 s over the window",
		"(window / 30 s - steps with a sample) x 30 s, in minutes; gaps under 5 minutes are not seen")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	vals, err := prom(ctx, fmt.Sprintf(`count_over_time((max(up{job="kube-prometheus-stack-prometheus"}))[%ds:%ds] @ %d)`, span, promScrapeStep, w.To.Unix()))
	if err != nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	if len(vals) == 0 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "no samples of Prometheus itself in the window"}, false)}
	}
	steps := float64(span / promScrapeStep)
	got := vals[0].Value
	gap := math.Max(0, (steps-got)*promScrapeStep/60)
	return []Reading{finish(Reading{Name: name, Value: &gap, Unit: "minutes", Samples: int(got), Window: w, Definition: d}, false)}
}
