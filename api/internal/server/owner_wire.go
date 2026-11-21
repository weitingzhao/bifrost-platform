package server

import (
	"context"
	"fmt"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	ownerassembly "github.com/weitingzhao/bifrost-platform/api/internal/owner/assembly"
)

// mountOwner hands the owner domain's routes (owner/assembly, since
// 2026-11-19) what the platform lends them: the gates and the handlers
// server answers with.
func (s *Server) mountOwner(r chi.Router) {
	ownerassembly.Mount(r, ownerassembly.Gates{
		Viewer:   s.auth.Require(actuation.RoleViewer),
		Reporter: s.auth.Require(actuation.RoleReporter),
		Operator: s.auth.Require(actuation.RoleOperator),
		Guard:    s.guard,
	}, ownerassembly.Handlers{
		Governance:       s.agentgovernance,
		ApprovalReadings: s.handleApprovalReadings,
		RecordDB:         s.recordDB,
		Instruments:      s.handleInstruments,
		PlanGet:          s.handlePlanGet,
		Plan:             s.handlePlan,
		Apply:            s.handleApply,
		CreateJob:        s.handleCreateJob,
		DeleteFinished:   s.handleDeleteFinished,
		Probe:            s.handleProbe,
	})
}

// wireWorkActions hands the owner's action wiring (owner/assembly, since
// 2026-11-19) what it asks of this process and of the delivery domain: the
// daemon's replicas, the D10 paths, delivery's copy of the policy and its
// mirror sync, the cluster's clients. Lines of New until 2026-11-16.
func (srv *Server) wireWorkActions(cfg *config.Config, clusterH *cluster.Handler) {
	srv.work = ownerassembly.WireWorkActions(ownerassembly.Work{
		ConfigDir:      cfg.ConfigDir(),
		DaemonReplicas: srv.lookupDaemonReplicas,
		D10Paths:       srv.d10Paths,
		OnPolicy:       srv.delivery.SetPolicy,
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
		Cluster: clusterH.Service(),
		Audit:   srv.audit,
	})
}
