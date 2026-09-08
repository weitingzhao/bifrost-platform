// Package operatorplane assembles the out-of-band operator plane (L-1): the
// surface that repairs the platform, built so it can run somewhere the platform
// is not.
//
// L-1 is defined in console/src/lib/architecture/cicdBootstrapCatalog.ts as the
// engineer standing on the ground next to the rocket — it boards and repairs the
// Ops Platform and Trade, and never shares fate with either. Its executors have
// always lived outside the cluster (the remediation runners on the Mac minis,
// the Hermes gateway, the Git Bridge), but the surface that drives them was
// compiled into platform-api, so a bad platform-api release took out the tool
// you would use to roll that release back.
//
// Every package mounted here is cluster-free — no client-go, no kubeconfig, no
// internal/cluster — which is asserted by TestOperatorPlaneStaysClusterFree.
// What they do touch of the wider platform is deliberately state-free: the
// remediation *client* that calls the runners over HTTP, and the checklist
// *types* the autopilot reads its signals into. Neither reaches into
// platform-api's own stores, so this plane can be served from a second process
// without splitting any state.
package operatorplane

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentbridge"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentdeploy"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentreport"
	"github.com/weitingzhao/bifrost-platform/api/internal/driftproposal"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesinsight"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesreadiness"
	"github.com/weitingzhao/bifrost-platform/api/internal/patrol"
)

// Deps is everything the plane needs. Note what is absent: no cluster service,
// no kubeconfig. That absence is the point.
type Deps struct {
	Auth      *actuation.AuthService
	Audit     *actuation.AuditLog
	ConfigDir string
}

// Plane owns the L-1 handlers and the patrol autopilot loop.
type Plane struct {
	auth *actuation.AuthService

	agentBridge     *agentbridge.Handler
	agentDeploy     *agentdeploy.Handler
	agentReport     *agentreport.Handler
	driftProposal   *driftproposal.Handler
	hermesGateway   *hermesgateway.Handler
	hermesInsight   *hermesinsight.Handler
	hermesReadiness *hermesreadiness.Handler
	patrol          *patrol.Handler
}

func New(d Deps) (*Plane, error) {
	patrolH, err := patrol.NewHandler(d.ConfigDir)
	if err != nil {
		return nil, err
	}
	readiness := hermesreadiness.NewHandler()
	insight, err := hermesinsight.NewHandlerWithOptions(hermesinsight.HandlerOptions{
		Readiness: readiness,
	})
	if err != nil {
		return nil, err
	}
	return &Plane{
		auth:            d.Auth,
		agentBridge:     agentbridge.NewHandler(),
		agentDeploy:     agentdeploy.NewHandler(d.Audit),
		agentReport:     agentreport.NewHandler(),
		driftProposal:   driftproposal.NewHandler(d.Audit),
		hermesGateway:   hermesgateway.NewHandler(),
		hermesInsight:   insight,
		hermesReadiness: readiness,
		patrol:          patrolH,
	}, nil
}

// Patrol exposes the autopilot handler so the host can report whether its loop
// is live.
func (p *Plane) Patrol() *patrol.Handler { return p.patrol }

// StartBackground starts the patrol autopilot. Exactly one process may call it:
// the loop keeps its due-scan marks in memory, so a second caller re-runs work
// the first already did.
func (p *Plane) StartBackground(ctx context.Context) { p.patrol.Start(ctx) }

// StopBackground stops the autopilot.
func (p *Plane) StopBackground() { p.patrol.Stop() }

// Mount registers the L-1 routes on an /api/v1 router. Read paths are viewer
// level; anything that actuates a runner, a deploy or a skill is operator
// gated, matching what platform-api served before the plane was extracted.
func (p *Plane) Mount(r chi.Router) {
	r.Get("/agent/nightly-report", p.agentReport.HandleNightlyReport)
	r.Get("/agent/bridge", p.agentBridge.HandleBridge)
	r.Get("/agent/smoke", p.agentBridge.HandleSmoke)
	r.Get("/agent/deploy", p.agentDeploy.HandleStatus)
	r.Get("/agent/hermes/readiness", p.hermesReadiness.HandleReadiness)
	r.Get("/agent/hermes/first-task", p.hermesReadiness.HandleFirstTask)
	r.Get("/agent/hermes/health", p.hermesGateway.HandleHealth)
	r.Get("/agent/skills", p.hermesGateway.HandleSkills)
	r.Get("/agent/schedules", p.hermesGateway.HandleSchedules)
	r.Get("/agent/executions", p.hermesGateway.HandleExecutions)
	r.Get("/hermes/insights", p.hermesInsight.HandleList)
	r.Post("/hermes/run-first-task", p.hermesInsight.HandleRunFirstTask)
	r.Get("/patrol/skills", p.patrol.HandleListSkills)
	r.Get("/patrol/skills/{id}", p.patrol.HandleGetSkill)
	r.Get("/patrol/runs", p.patrol.HandleListRuns)

	r.Group(func(r chi.Router) {
		r.Use(p.auth.Require(actuation.RoleOperator))
		r.Post("/agent/nightly-run", p.agentReport.HandleTriggerNightly)
		r.Post("/agent/deploy", p.agentDeploy.HandleStart)
		r.Put("/agent/skills/{id}/actuation-level", p.hermesGateway.HandleSkillActuationLevel)
		r.Put("/patrol/skills/{id}/enable", p.patrol.HandleEnable)
		r.Post("/patrol/trigger/{id}", p.patrol.HandleTrigger)
		r.Post("/patrol/webhook/{event}", p.patrol.HandleWebhook)
	})

	r.Route("/agent/drift-proposals", func(r chi.Router) {
		r.Get("/", p.driftProposal.HandleList)
		r.Get("/{id}", p.driftProposal.HandleGet)
		r.Group(func(r chi.Router) {
			r.Use(p.auth.Require(actuation.RoleOperator))
			r.Post("/", p.driftProposal.HandleCreate)
			r.Post("/{id}/approve", p.driftProposal.HandleApprove)
			r.Post("/{id}/reject", p.driftProposal.HandleReject)
		})
	})
}

// Routes is the path set this plane owns, for the host that proxies to it.
func Routes() []string {
	return []string{
		"/agent/nightly-report", "/agent/bridge", "/agent/smoke", "/agent/deploy",
		"/agent/hermes/readiness", "/agent/hermes/first-task", "/agent/hermes/health",
		"/agent/skills", "/agent/schedules", "/agent/executions",
		"/hermes/insights", "/hermes/run-first-task",
		"/patrol/skills", "/patrol/runs",
		"/agent/nightly-run", "/agent/drift-proposals",
	}
}
