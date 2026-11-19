package server

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gatecluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry/pluginmetrics"
)

// /metrics is every part's own exposition, in order; each part writes itself.

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	// Workers is scraped for its own process series and the maintainer gauges.
	// Plugin health is already on platform-api's ServiceMonitor; probing again
	// from workers doubles that cost.
	if config.CurrentRole() != config.RoleWorkers {
		s.writePluginMetrics(&b, r)
	}
	maintainer.Write(&b)
	actuation.WriteAuthLoaded(&b, s.authLoaded)
	if s.releasePolicy != nil {
		ctx, cancel := context.WithTimeout(r.Context(), pluginmetrics.ProbeTimeout)
		s.releasePolicy.Status(ctx)
		cancel()
	}
	s.writeFullCIMetric(&b)
	releasepolicy.WriteMetrics(&b)
	agentthreads.WriteMetrics(&b)
	relay.WriteMetrics(&b)
	gatecluster.WriteMetrics(&b)
	controller.WritePipelineEventMetrics(&b)
	delivery.WriteBusReaderMetrics(&b)
	readerseen.WriteMetrics(&b)
	kubemetrics.Write(&b)
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
	pluginmetrics.Write(r.Context(), b, s.pluginProbeFns())
}
