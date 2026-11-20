package server

import (
	"context"
	"encoding/json"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/evalreadings"
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
// read from the evidence ledger through the verification domain's read
// (evidence.EvidenceByCommit) and its row's statement.predicate.failed_tasks.
func (s *Server) evalFailedTasks(ctx context.Context, repo, commit, run string) ([]string, error) {
	db := s.recordDB()
	if db == nil || repo == "" || commit == "" || run == "" {
		return nil, nil
	}
	rows, err := evidence.EvidenceByCommit(ctx, db, repo, commit, 50)
	if err != nil {
		return nil, err
	}
	for _, e := range rows {
		if e.RunName != run || e.Result != evidence.EvidenceFailed {
			continue
		}
		var st struct {
			Predicate struct {
				FailedTasks []string `json:"failed_tasks"`
			} `json:"predicate"`
		}
		if err := json.Unmarshal(e.Statement, &st); err != nil {
			return nil, err
		}
		return st.Predicate.FailedTasks, nil
	}
	return nil, nil
}
