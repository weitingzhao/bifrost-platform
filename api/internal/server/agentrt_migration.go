package server

import (
	"context"
	"errors"
	"net/http"

	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migraterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
)

// run_agentrt_migration (LAW-6): the action and what it does live
// in agentrt/migraterun; this file hands it what it stands on and serves the
// route.

const migrationActionID = migraterun.ActionID

// ownMigrations is what the release policy reads of this platform's own
// environment (migraterun.OwnSource), from the runtime and release records
// this server wired.
type ownMigrations struct{ s *Server }

func (o ownMigrations) Own(ctx context.Context) (releasepolicy.OwnMigrations, error) {
	s := o.s
	return migraterun.OwnSource{
		Applied: func() (map[int]string, bool, bool) {
			rt := s.agentRuntime
			if rt == nil || rt.schema == nil {
				return nil, false, false
			}
			applied, ok := rt.schema.Applied()
			return applied, ok, true
		},
		Env: func() string { return s.agentRuntime.env },
		Pipeline: func(lane, env string) (string, bool, error) {
			if s.releases == nil || s.releases.Service() == nil {
				return "", false, errors.New("the release records are not wired")
			}
			pipeline, ok := s.releases.Service().PipelineOf(lane, env)
			return pipeline, ok, nil
		},
	}.Own(ctx)
}

// migrationErrors are the owner domain's classes of error and the identity of
// the approval run, as the migration action says them.
var migrationErrors = migraterun.Errors{
	Transient: actions.Transient, Uncertain: actions.Uncertain,
	ClassifyCreate: actions.ClassifyCreate, Identity: actions.IdentityAnnotations,
}

func (s *Server) migrationRunner() (*migraterun.Runner, error) {
	if s.releasePolicy == nil || s.delivery == nil || s.cluster == nil || s.releases == nil || s.releases.Service() == nil || s.agentRuntime == nil {
		return nil, errors.New("the release policy, delivery, cluster or agent runtime is not wired on this platform")
	}
	facts := s.delivery.PolicyFacts()
	return &migraterun.Runner{
		Env: s.agentRuntime.env, Namespace: k8sstate.Namespace(),
		State: s.releasePolicy.PendingMigrations, Source: s.releasePolicy.SourcePipeline,
		Records: s.releases.Service().List, MainHead: facts.MainHead, FileAt: facts.FileAt,
		Kube: func() (kubernetes.Interface, error) {
			cs, _, err := s.cluster.Service().KubernetesClient()
			return cs, err
		},
		PodLog: migraterun.PodLog,
		Wait:   migraterun.DefaultWait, Poll: migraterun.DefaultPoll,
		Errors: migrationErrors,
	}, nil
}

// runAgentrtMigration is the action's executor: after the Owner approved the
// card, and behind the direct route when the signed policy covers the call.
func (s *Server) runAgentrtMigration(ctx context.Context, params map[string]any) (any, error) {
	m, err := s.migrationRunner()
	if err != nil {
		return nil, err
	}
	return m.Run(ctx, text(params["commit"]))
}

// migrateBeforeProd is the delivery's step before a platform PROD run
// (migraterun.BeforeProd).
func (s *Server) migrateBeforeProd(ctx context.Context, commit string) {
	if s.releasePolicy == nil {
		return
	}
	migraterun.BeforeProd{Pending: s.releasePolicy.PendingMigrations, Evaluate: s.releasePolicy.Evaluate, Run: s.runAgentrtMigration}.Prepare(ctx, commit)
}

// handleAgentrtMigration is POST /api/v1/agentrt/migrations/run {commit}
// (migraterun.Route).
func (s *Server) handleAgentrtMigration(w http.ResponseWriter, r *http.Request) {
	migraterun.Route{Runner: s.migrationRunner, IsUncertain: actions.IsUncertain, IsTransient: actions.IsTransient}.ServeHTTP(w, r)
}
