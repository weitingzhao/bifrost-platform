package evalreadings

import (
	"context"
	"fmt"
	"math"
	"sort"
)

// alertActivationReadings: per alert rule and severity, how many times an
// alert series became active in the window. ALERTS_FOR_STATE holds a series'
// activation time, so each change of its value is a new activation; a series
// already active when the window starts counts once. Drill alerts
// (drill="true") are left out. Rules that never fired have no series and are
// not listed.
//
// Origin: 2026-11-20, the metrics line's step 9 (each rule: what it protects,
// how often it rang in 7 days, who receives it). The first read over the 7
// days before 15:24Z listed 57 rules; the three loudest were kube-prometheus
// defaults (KubePodNotReady about 19.5k activations).
func alertActivationReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "alert_activations"
	d := def("Prometheus ALERTS_FOR_STATE (every rule Prometheus evaluates)",
		"per alertname and severity: sum over alert series of (changes of the activation time over the window + 1); drill alerts left out")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	span := int(w.To.Sub(w.From).Seconds())
	expr := fmt.Sprintf(`sum by (alertname, severity) (changes(ALERTS_FOR_STATE{drill!="true"}[%ds] @ %d) + 1)`, span, w.To.Unix())
	vals, err := prom(ctx, expr)
	if err != nil {
		return []Reading{finish(Reading{Name: name, Unit: "count", Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	var out []Reading
	for _, s := range vals {
		a := s.Labels["alertname"]
		if a == "" {
			continue
		}
		n := int(math.Round(s.Value))
		out = append(out, finish(Reading{Name: name, Labels: map[string]string{"alertname": a, "severity": s.Labels["severity"]}, Value: count(n), Unit: "count", Samples: n, Window: w, Definition: d}, false))
	}
	if len(out) == 0 {
		v := 0.0
		return []Reading{{Name: name, Unit: "count", Window: w, Definition: d, Value: &v, Status: StatusOK, Provenance: "system", Note: "no alert was active in the window"}}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Labels["alertname"] < out[j].Labels["alertname"] })
	return out
}
