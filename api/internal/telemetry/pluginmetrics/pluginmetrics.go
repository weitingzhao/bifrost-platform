// Package pluginmetrics is the Prometheus exposition of the plugins Trade runs
// on.
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
package pluginmetrics

import (
	"context"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry/probecollect"
)

// ProbeTimeout is per plugin, not for the scrape as a whole. A single shared
// deadline made the probes race each other: market-data is much the slowest
// (deployments, worker pools, freshness) and would spend most of a shared
// budget, leaving the other three a cancelled context and reporting them
// unreachable when they were fine.
const ProbeTimeout = 8 * time.Second

// Write probes the plugins and writes their reachability and gauges.
func Write(ctx context.Context, b *strings.Builder, probes []func(context.Context) probe.PluginHealth) {
	healths := probecollect.Collect(ctx, ProbeTimeout, "server.metrics.pluginProbe", probes)

	observations := make([]telemetry.ReachabilityObservation, 0, len(healths))
	for _, h := range healths {
		observations = append(observations, telemetry.ReachabilityObservation{Name: h.Name, Reachable: h.Reachable, State: string(h.Reachability)})
	}
	states := []string{string(probe.ReachOK), string(probe.ReachDegraded), string(probe.ReachFail), string(probe.ReachUnknown)}
	telemetry.WriteReachability(b, observations, states)

	writeGauges(b, healths)
}

// writeGauges adapts existing probe observations to generic telemetry exposition.
func writeGauges(b *strings.Builder, healths []probe.PluginHealth) {
	var gauges []telemetry.MetricGauge
	for _, h := range healths {
		for _, g := range h.Gauges {
			gauges = append(gauges, telemetry.MetricGauge{Plugin: h.Name, Key: g.Key, Help: g.Help, Labels: g.Labels, Value: g.Value})
		}
	}
	telemetry.WriteGauges(b, gauges)
}
