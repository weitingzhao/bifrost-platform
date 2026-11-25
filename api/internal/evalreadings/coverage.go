package evalreadings

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// coverageByNamespaceReadings: metric_coverage_by_namespace, the share of a
// namespace's running workloads that have their own metrics (a scrape target
// of their own) at the end of the window, with the ones that have none named
// in the note. Origin: the metrics line's list of what the system cannot see
// (2026-11-21, LAW-8): 45 workloads had no metrics of their own, and the
// coverage record said so only as two numbers (bifrost, infra) that no page
// read. By namespace, because the map already says which domain each
// namespace belongs to (config/domain-ownership.json, workloads); a second
// copy of that table here would drift from it.
func coverageByNamespaceReadings(ctx context.Context, prom Prom, w Window) []Reading {
	const name = "metric_coverage_by_namespace"
	d := def("Prometheus recording rules bifrost_metrics:workload_info and bifrost_metrics:workload_scraped (k8s/monitoring/bifrost-metrics-coverage-rules.yaml)",
		"at the window's end: workloads with a scrape target of their own / running workloads, by namespace; a target that is down still counts as covered")
	if prom == nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus is not reachable from this platform"}, false)}
	}
	if w.Days > 10 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus keeps 10 days"}, false)}
	}
	// The records are written every 5 minutes; a 10-minute lookback finds the
	// last one before the window's end.
	at := w.To.Unix()
	info, err := prom(ctx, fmt.Sprintf("last_over_time(bifrost_metrics:workload_info[10m] @ %d)", at))
	if err != nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	scraped, err := prom(ctx, fmt.Sprintf("last_over_time(bifrost_metrics:workload_scraped[10m] @ %d)", at))
	if err != nil {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "Prometheus query failed: " + err.Error()}, false)}
	}
	if len(info) == 0 {
		return []Reading{finish(Reading{Name: name, Window: w, Definition: d, Note: "no samples of the record at the window's end"}, false)}
	}
	has := map[string]bool{}
	for _, s := range scraped {
		has[s.Labels["namespace"]+"/"+s.Labels["workload"]] = true
	}
	type tally struct {
		total, covered int
		missing        []string
	}
	byNS := map[string]*tally{}
	for _, s := range info {
		ns, wl := s.Labels["namespace"], s.Labels["workload"]
		if ns == "" || wl == "" {
			continue
		}
		t := byNS[ns]
		if t == nil {
			t = &tally{}
			byNS[ns] = t
		}
		t.total++
		if has[ns+"/"+wl] {
			t.covered++
		} else {
			t.missing = append(t.missing, wl)
		}
	}
	names := make([]string, 0, len(byNS))
	for ns := range byNS {
		names = append(names, ns)
	}
	sort.Strings(names)
	out := make([]Reading, 0, len(names))
	for _, ns := range names {
		t := byNS[ns]
		r := finish(Reading{Name: name, Labels: map[string]string{"namespace": ns}, Value: ratio(t.covered, t.total),
			Unit: "ratio", Samples: t.total, Window: w, Definition: d}, false)
		if len(t.missing) > 0 {
			sort.Strings(t.missing)
			r.Note = "no metrics of their own: " + strings.Join(t.missing, ", ")
		}
		out = append(out, r)
	}
	return out
}
