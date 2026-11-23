package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/evalreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/evidenceread"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
)

// evalProm lends the evaluation readout an instant PromQL query against the
// default cluster's Prometheus; nil when none is configured.
func (s *Server) evalProm() evalreadings.Prom {
	if s.cfg == nil {
		return nil
	}
	entry := s.cfg.DefaultCluster()
	if entry == nil || entry.PrometheusURL() == "" {
		return nil
	}
	client := telemetry.NewClient(entry.PrometheusURL())
	return func(ctx context.Context, expr string) ([]evalreadings.Sample, error) {
		points, err := client.QueryInstant(ctx, expr)
		if err != nil {
			return nil, err
		}
		out := make([]evalreadings.Sample, 0, len(points))
		for _, p := range points {
			out = append(out, evalreadings.Sample{Labels: p.Labels, Value: p.Value})
		}
		return out, nil
	}
}

// evalFailedTasks lends the evaluation readout the failed tasks of a CI run,
// read from the evidence ledger through the verification domain's public read
// (evidenceread.FailedTasks).
func (s *Server) evalFailedTasks(ctx context.Context, repo, commit, run string) ([]string, error) {
	return evidenceread.FailedTasks(ctx, s.recordDB(), repo, commit, run)
}
