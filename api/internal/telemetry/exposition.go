package telemetry

import (
	"fmt"
	"sort"
	"strings"
)

// MetricGauge is a single observed gauge. Formatting does not probe or decide health.
type MetricGauge struct {
	Plugin, Key, Help string
	Labels            map[string]string
	Value             float64
}

// WriteGauges emits the plugin-specific numbers, grouped so each metric
// carries its HELP and TYPE once, as the exposition format requires.
func WriteGauges(b *strings.Builder, gauges []MetricGauge) {
	type keyed struct {
		help  string
		lines []string
	}
	byKey := map[string]*keyed{}
	for _, g := range gauges {
		name := "bifrost_" + g.Key
		e, ok := byKey[name]
		if !ok {
			e = &keyed{help: g.Help}
			byKey[name] = e
		}
		labels := []string{fmt.Sprintf("plugin=%q", g.Plugin)}
		for _, lk := range sortedKeys(g.Labels) {
			labels = append(labels, fmt.Sprintf("%s=%q", lk, g.Labels[lk]))
		}
		e.lines = append(e.lines,
			fmt.Sprintf("%s{%s} %g", name, strings.Join(labels, ","), g.Value))
	}
	for _, name := range sortedMapKeys(byKey) {
		e := byKey[name]
		if e.help != "" {
			fmt.Fprintf(b, "# HELP %s %s\n", name, e.help)
		}
		fmt.Fprintf(b, "# TYPE %s gauge\n", name)
		sort.Strings(e.lines)
		for _, l := range e.lines {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
