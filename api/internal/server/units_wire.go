package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitwatch"
)

// Units as the platform runs them live in unitexec (the plan's dispatch, the
// receiving pass, the evidence-request route) and unitwatch (the daily run);
// this file hands them the cluster, Gitea's trees and the ledger.

type unitTreesFunc = unitexec.TreesFunc

func (s *Server) unitClusterOf() unitexec.Cluster {
	return unitexec.FuncCluster{
		ListFn: func(ctx context.Context, label, selector string) ([]map[string]any, error) {
			items, err := s.delivery.UnitTaskRuns(kubemetrics.WithSite(ctx, "units.dispatch"), label, selector)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(items))
			for _, item := range items {
				out = append(out, item.Object)
			}
			return out, nil
		},
		CreateFn: s.delivery.CreateUnitTaskRun,
	}
}

func (s *Server) unitTreesOf() unitTreesFunc {
	s.unitTreesOnce.Do(func() {
		s.unitTrees = &gitread.Trees{Access: gitAccess(s.delivery.GiteaAccess), Client: &http.Client{Timeout: 20 * time.Second}}
	})
	return unitexec.GiteaTrees(s.unitTrees)
}

// placedUnitConfig is the dispatch configuration with the node the units go to
// now, read from the node profiles.
func (s *Server) placedUnitConfig(ctx context.Context) (unitexec.Config, error) {
	return unitexec.PlacedConfig(ctx, s.delivery.PipelinesNamespace(), delivery.HostIDLabel,
		func(ctx context.Context) (string, string, bool) {
			p, measured := s.delivery.PlaceTask(ctx, delivery.UnitTask)
			return p.Node, p.Why, measured
		})
}

// unitRunsSource has the controller record finished unit tasks at its pipeline
// interval; nil without delivery.
func (s *Server) unitRunsSource() func(context.Context) error {
	if s.delivery == nil {
		return nil
	}
	return unitexec.RunsSource(unitexec.RunsDeps{
		DB:        s.recordDB,
		Trees:     s.unitTreesOf,
		Cluster:   s.unitClusterOf,
		Namespace: s.delivery.PipelinesNamespace,
		Placed:    s.placedUnitConfig,
	})
}

// unitsDailySource is the controller's daily unit run; nil without delivery.
func (s *Server) unitsDailySource() func(context.Context) error {
	if s.delivery == nil {
		return nil
	}
	return unitwatch.DailySource(unitwatch.DailyDeps{
		Head:        s.giteaMainHead,
		FirstParent: s.giteaFirstParent,
		DB:          s.recordDB,
		Trees:       s.unitTreesOf,
		Cluster:     s.unitClusterOf,
		Namespace:   s.delivery.PipelinesNamespace,
		Placed:      s.placedUnitConfig,
	})
}

func (s *Server) handleUnitEvidenceRequest(w http.ResponseWriter, r *http.Request) {
	unitexec.EvidenceRoute{
		RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Sources: func() (unitexec.EvidenceSources, bool) {
			if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
				return unitexec.EvidenceSources{}, false
			}
			return unitexec.EvidenceSources{DB: s.agentRuntime.api.DB, Trees: s.unitTreesOf(), Cluster: s.unitClusterOf(),
				Namespace: s.delivery.PipelinesNamespace()}, true
		},
		Placed: s.placedUnitConfig,
	}.ServeHTTP(w, r)
}
