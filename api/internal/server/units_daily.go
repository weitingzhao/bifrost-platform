package server

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitwatch"
)

const unitsDailyPassTimeout = 10 * time.Minute

// placedUnitConfig is the dispatch configuration with the node the units go to
// now, read from the node profiles; a slow disk is refused, not fallen back to
// (two tasks timed out on ubt-k3s-05 on 2026-11-18). A platform without node
// metrics keeps what its environment says.
func (s *Server) placedUnitConfig(ctx context.Context) (unitDispatchConfig, error) {
	cfg := unitDispatchConfigFromEnv(s.delivery.PipelinesNamespace())
	p, measured := s.delivery.PlaceTask(ctx, delivery.UnitTask)
	if !measured {
		return cfg, nil
	}
	if p.Node == "" {
		return cfg, fmt.Errorf("no node for the units: %s", p.Why)
	}
	cfg.NodeSelector = map[string]string{delivery.HostIDLabel: p.Node}
	return cfg, nil
}

// unitsDailySource is the controller's daily unit run; nil without delivery.
func (s *Server) unitsDailySource() func(context.Context) error {
	if s.delivery == nil {
		return nil
	}
	d := &unitwatch.Daily{
		Head:        s.giteaMainHead,
		FirstParent: s.giteaFirstParent,
		Now:         time.Now,
		Start: func(ctx context.Context, repo, commit string) error {
			cfg, err := s.placedUnitConfig(ctx)
			if err != nil {
				return err
			}
			_, err = advanceUnits(ctx, s.agentRuntime.api.DB, s.unitTreesOf(), s.unitClusterOf(), cfg, repo, commit, "nightly")
			return err
		},
		Tasks: func(ctx context.Context, repo, commit string) ([]unitwatch.DailyTask, error) {
			plan, err := buildUnitPlan(ctx, s.unitTreesOf(), repo, commit)
			if err != nil {
				return nil, err
			}
			out := make([]unitwatch.DailyTask, 0, len(plan.Units))
			for _, u := range plan.Units {
				out = append(out, unitwatch.DailyTask{Unit: u.Unit, Task: u.Task, Key: u.Key})
			}
			return out, nil
		},
		Verdict: func(ctx context.Context, repo string, t unitwatch.DailyTask) (string, error) {
			rows, err := evidence.EvidenceByKey(ctx, s.agentRuntime.api.DB, repo, t.Unit, t.Task, t.Key)
			if err != nil {
				return "", err
			}
			return evidence.EvidenceVerdict(rows), nil
		},
	}
	return func(ctx context.Context) error {
		if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
			return nil
		}
		if unitDispatchConfigFromEnv(s.delivery.PipelinesNamespace()).Mode == "off" {
			return nil
		}
		d.DB = s.agentRuntime.api.DB
		ctx, cancel := context.WithTimeout(ctx, unitsDailyPassTimeout)
		defer cancel()
		return d.Pass(ctx)
	}
}
