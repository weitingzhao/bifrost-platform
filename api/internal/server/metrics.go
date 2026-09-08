package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Prometheus exposition for the plugins Trade runs on.
//
// The cluster has a full kube-prometheus-stack and nineteen Bifrost rules, but
// every one of them watches infrastructure or the flex ingest. On 2026-09-07
// the IB gateway could not reach TWS for seventeen hours: its pod never left
// 1/1 Running because it retried in a loop, so no generic Kubernetes alert
// could fire, and nothing exported the gateway's own health. The gateway had
// been publishing that health to redis-ib the whole time and platform-api had
// been reading it — it just never reached Prometheus.
//
// This exports what the status endpoints already compute, so a plugin gains
// alerting by answering PluginHealth and nothing else.

const pluginProbeTimeout = 8 * time.Second

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// A plugin probe that hangs must not hold the scrape open past its timeout.
	ctx, cancel := context.WithTimeout(r.Context(), pluginProbeTimeout)
	defer cancel()

	healths := []probe.PluginHealth{
		s.marketdata.PluginHealth(ctx),
		s.flexquery.PluginHealth(ctx),
		s.ibgateway.PluginHealth(ctx),
		s.research.PluginHealth(ctx),
	}

	var b strings.Builder
	b.WriteString("# HELP bifrost_plugin_reachable Whether platform-api could reach the plugin (1) or not (0)\n")
	b.WriteString("# TYPE bifrost_plugin_reachable gauge\n")
	for _, h := range healths {
		fmt.Fprintf(&b, "bifrost_plugin_reachable{plugin=%q} %d\n", h.Name, boolValue(h.Reachable))
	}

	// One series per state so an alert can say `state="ok"` without string
	// comparison, and a plugin flipping state leaves no stale series behind.
	b.WriteString("# HELP bifrost_plugin_reachability Plugin reachability, one series per state, 1 on the current one\n")
	b.WriteString("# TYPE bifrost_plugin_reachability gauge\n")
	states := []probe.Reachability{probe.ReachOK, probe.ReachDegraded, probe.ReachFail, probe.ReachUnknown}
	for _, h := range healths {
		for _, st := range states {
			fmt.Fprintf(&b, "bifrost_plugin_reachability{plugin=%q,state=%q} %d\n",
				h.Name, st, boolValue(h.Reachability == st))
		}
	}

	writePluginGauges(&b, healths)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// writePluginGauges emits the plugin-specific numbers, grouped so each metric
// carries its HELP and TYPE once, as the exposition format requires.
func writePluginGauges(b *strings.Builder, healths []probe.PluginHealth) {
	type keyed struct {
		help  string
		lines []string
	}
	byKey := map[string]*keyed{}
	for _, h := range healths {
		for _, g := range h.Gauges {
			name := "bifrost_" + g.Key
			e, ok := byKey[name]
			if !ok {
				e = &keyed{help: g.Help}
				byKey[name] = e
			}
			labels := []string{fmt.Sprintf("plugin=%q", h.Name)}
			for _, lk := range sortedKeys(g.Labels) {
				labels = append(labels, fmt.Sprintf("%s=%q", lk, g.Labels[lk]))
			}
			e.lines = append(e.lines,
				fmt.Sprintf("%s{%s} %g", name, strings.Join(labels, ","), g.Value))
		}
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

func boolValue(v bool) int {
	if v {
		return 1
	}
	return 0
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
