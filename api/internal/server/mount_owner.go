package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

// mountOwner registers the owner domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountOwner(r chi.Router) {
	r.Get("/agent/governance/trust-matrix", s.agentgovernance.HandleTrustMatrix)
	r.Get("/agent/governance/trust-overrides", s.agentgovernance.HandleTrustOverrides)
	// What the approval records say, counted by day and requester, each
	// count with the request numbers behind it. Read-only; viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/readings/approvals", s.handleApprovalReadings)
	// The action record (action_record.go): what was done, by whom, why and on
	// which law, newest first, by seat, law, action and time. Read-only.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/records", actionrecord.ListHandler(s.recordDB))
	// The laws, Skills and MCP tools the source at main declares, for the Crew
	// page (governance_instruments.go). Read-only.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/governance/instruments", s.handleInstruments)
	r.Get("/actuation/manifests/plans/{id}", s.handlePlanGet)
	// Recording a skill's own outcome is evidence, not actuation. It sits
	// below operator on purpose: the workload that reports must not also be
	// able to scale deployments or write its own trust override.
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleReporter))
		r.Post("/agent/governance/skill-runs", s.agentgovernance.HandleRecordSkillRun)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Post("/actuation/manifests/plan", s.guard("plan_manifest", s.handlePlan))
		r.Post("/actuation/manifests/apply", s.guard("apply_manifest", s.handleApply))
		r.Post("/actuation/jobs/from-cronjob", s.guard("create_job_from_cronjob", s.handleCreateJob))
		r.Post("/actuation/jobs/delete-finished", s.guard("delete_finished_jobs", s.handleDeleteFinished))
		r.Post("/actuation/probes", s.guard("run_probe_pod", s.handleProbe))
	})
}

// wireWorkActions gives the action catalog what it asks of this process (the
// daemon's replicas, the D10 paths, the actuation policy) and builds the
// generic write actions. Lines of New until 2026-11-16, unchanged.
func (srv *Server) wireWorkActions(cfg *config.Config, clusterH *cluster.Handler) {
	actions.SetDaemonReplicas(func(ctx context.Context, namespace string) (int32, bool) {
		return srv.lookupDaemonReplicas(ctx, namespace)
	})
	actions.SetD10Paths(srv.d10Paths)
	if pol, err := actuationpolicy.LoadFromConfigDir(cfg.ConfigDir()); err != nil {
		slog.Error("actuation policy not loaded; generic write actions refuse", "err", err)
	} else {
		actions.SetActuationPolicy(pol)
		srv.delivery.SetPolicy(pol)
		workSvc := &workactions.Service{
			Policy: pol,
			Sync: func(ctx context.Context, repo, commit string) error {
				res, err := srv.delivery.SyncOne(ctx, repo, commit)
				if err != nil {
					return err
				}
				if !res.Present {
					if res.Error != "" {
						return fmt.Errorf("%s", res.Error)
					}
					return fmt.Errorf("mirror commit is not present")
				}
				return nil
			},
			Clients: workactions.Clients{
				Kube: func() (kubernetes.Interface, error) {
					cs, _, err := clusterH.Service().KubernetesClient()
					return cs, err
				},
				Dynamic: func() (dynamic.Interface, error) {
					rc, _, err := clusterH.Service().RestConfig()
					if err != nil {
						return nil, err
					}
					return dynamic.NewForConfig(rc)
				},
			},
		}
		srv.work = &workactions.Handler{Svc: workSvc, Audit: srv.audit}
		actions.SetPlanLookup(func(ctx context.Context, planID string) (actions.PlanSummary, error) {
			sum, err := workSvc.Summarize(ctx, planID)
			if err != nil {
				return actions.PlanSummary{}, err
			}
			return actions.PlanSummary{Ready: sum.Ready, Namespaces: sum.Namespaces, Daemon: sum.Daemon, Path: sum.Path, Objects: len(sum.Objects), Migrated: sum.Migrated}, nil
		})
	}
}
