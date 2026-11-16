package server

import (
	"context"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
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
	var b strings.Builder
	// Workers is scraped for its own process series and the maintainer gauges.
	// Plugin health is already on platform-api's ServiceMonitor; probing again
	// from workers doubles that cost.
	if config.CurrentRole() != config.RoleWorkers {
		s.writePluginMetrics(&b, r)
	}
	maintainer.Write(&b)
	b.WriteString("# HELP bifrost_platform_auth_loaded Whether platform-auth.yaml loaded (1) or every gated route answers 401 (0)\n")
	b.WriteString("# TYPE bifrost_platform_auth_loaded gauge\n")
	fmt.Fprintf(&b, "bifrost_platform_auth_loaded %d\n", boolValue(s.authLoaded))
	if s.releasePolicy != nil {
		ctx, cancel := context.WithTimeout(r.Context(), pluginProbeTimeout)
		s.releasePolicy.Status(ctx)
		cancel()
	}
	s.writeFullCIMetric(&b)
	releasepolicy.WriteMetrics(&b)
	agentthreads.WriteMetrics(&b)
	s.httpMetrics.Write(&b)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// writeFullCIMetric writes whether the newest finished daily full CI run is
// red, for the alert rules: only the process whose controller runs that loop
// (workers, PROD) has the answer, and any other writes no series.
func (s *Server) writeFullCIMetric(b *strings.Builder) {
	if s.agentRuntime == nil {
		return
	}
	name, status, ok := s.agentRuntime.fullCI.Load().Latest()
	if !ok {
		return
	}
	cirunview.WriteFullCIMetric(b, name, status)
}

func (s *Server) pluginProbeFns() []func(context.Context) probe.PluginHealth {
	if s.pluginProbes != nil {
		return s.pluginProbes
	}
	return []func(context.Context) probe.PluginHealth{
		s.marketdata.PluginHealth,
		s.flexquery.PluginHealth,
		s.ibgateway.PluginHealth,
		s.research.PluginHealth,
	}
}

func (s *Server) writePluginMetrics(b *strings.Builder, r *http.Request) {
	probes := s.pluginProbeFns()

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

	observations := make([]telemetry.ReachabilityObservation, 0, len(healths))
	for _, h := range healths {
		observations = append(observations, telemetry.ReachabilityObservation{Name: h.Name, Reachable: h.Reachable, State: string(h.Reachability)})
	}
	states := []string{string(probe.ReachOK), string(probe.ReachDegraded), string(probe.ReachFail), string(probe.ReachUnknown)}
	telemetry.WriteReachability(b, observations, states)

	writePluginGauges(b, healths)
}

// writePluginGauges adapts existing probe observations to generic telemetry exposition.
func writePluginGauges(b *strings.Builder, healths []probe.PluginHealth) {
	var gauges []telemetry.MetricGauge
	for _, h := range healths {
		for _, g := range h.Gauges {
			gauges = append(gauges, telemetry.MetricGauge{Plugin: h.Name, Key: g.Key, Help: g.Help, Labels: g.Labels, Value: g.Value})
		}
	}
	telemetry.WriteGauges(b, gauges)
}

func boolValue(v bool) int {
	if v {
		return 1
	}
	return 0
}
