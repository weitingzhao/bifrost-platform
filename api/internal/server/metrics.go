package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
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

// Per plugin, not for the scrape as a whole. A single shared deadline made the
// probes race each other: market-data is much the slowest (deployments, worker
// pools, freshness) and would spend most of a shared budget, leaving the other
// three a cancelled context and reporting them unreachable when they were fine.
const pluginProbeTimeout = 8 * time.Second

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	probes := []func(context.Context) probe.PluginHealth{
		s.marketdata.PluginHealth,
		s.flexquery.PluginHealth,
		s.ibgateway.PluginHealth,
		s.research.PluginHealth,
	}

	// Concurrent as well as independently bounded, so the scrape costs the
	// slowest plugin rather than the sum of all four.
	healths := make([]probe.PluginHealth, len(probes))
	var wg sync.WaitGroup
	for i, fn := range probes {
		wg.Add(1)
		go func(i int, fn func(context.Context) probe.PluginHealth) {
			// Outside the request stack chi's Recoverer cannot reach these, so
			// one bad probe would take platform-api down and blind the Console
			// with it — the fan-out case safego's package doc names.
			defer safego.Recover("server.metrics.pluginProbe")
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), pluginProbeTimeout)
			defer cancel()
			healths[i] = fn(ctx)
		}(i, fn)
	}
	wg.Wait()

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
