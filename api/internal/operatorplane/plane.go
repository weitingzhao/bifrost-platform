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
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

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

// route is one entry of the plane's route table. Mount and MountProxy both walk
// this table, so what a proxying host forwards cannot drift from what the plane
// actually serves — the two used to be maintained as separate lists.
type route struct {
	method   string
	pattern  string
	operator bool
	pick     func(*Plane) http.HandlerFunc
}

// routeTable is the L-1 surface. Read paths are viewer level; anything that
// actuates a runner, a deploy or a skill is operator gated, matching what
// platform-api served before the plane was extracted.
func routeTable() []route {
	return []route{
		{"GET", "/agent/nightly-report", false, func(p *Plane) http.HandlerFunc { return p.agentReport.HandleNightlyReport }},
		{"GET", "/agent/bridge", false, func(p *Plane) http.HandlerFunc { return p.agentBridge.HandleBridge }},
		{"GET", "/agent/smoke", false, func(p *Plane) http.HandlerFunc { return p.agentBridge.HandleSmoke }},
		{"GET", "/agent/deploy", false, func(p *Plane) http.HandlerFunc { return p.agentDeploy.HandleStatus }},
		{"GET", "/agent/hermes/readiness", false, func(p *Plane) http.HandlerFunc { return p.hermesReadiness.HandleReadiness }},
		{"GET", "/agent/hermes/first-task", false, func(p *Plane) http.HandlerFunc { return p.hermesReadiness.HandleFirstTask }},
		{"GET", "/agent/hermes/health", false, func(p *Plane) http.HandlerFunc { return p.hermesGateway.HandleHealth }},
		{"GET", "/agent/skills", false, func(p *Plane) http.HandlerFunc { return p.hermesGateway.HandleSkills }},
		{"GET", "/agent/schedules", false, func(p *Plane) http.HandlerFunc { return p.hermesGateway.HandleSchedules }},
		{"GET", "/agent/executions", false, func(p *Plane) http.HandlerFunc { return p.hermesGateway.HandleExecutions }},
		{"GET", "/hermes/insights", false, func(p *Plane) http.HandlerFunc { return p.hermesInsight.HandleList }},
		{"POST", "/hermes/run-first-task", false, func(p *Plane) http.HandlerFunc { return p.hermesInsight.HandleRunFirstTask }},
		{"GET", "/patrol/skills", false, func(p *Plane) http.HandlerFunc { return p.patrol.HandleListSkills }},
		{"GET", "/patrol/skills/{id}", false, func(p *Plane) http.HandlerFunc { return p.patrol.HandleGetSkill }},
		{"GET", "/patrol/runs", false, func(p *Plane) http.HandlerFunc { return p.patrol.HandleListRuns }},

		{"POST", "/agent/nightly-run", true, func(p *Plane) http.HandlerFunc { return p.agentReport.HandleTriggerNightly }},
		{"POST", "/agent/deploy", true, func(p *Plane) http.HandlerFunc { return p.agentDeploy.HandleStart }},
		{"PUT", "/agent/skills/{id}/actuation-level", true, func(p *Plane) http.HandlerFunc { return p.hermesGateway.HandleSkillActuationLevel }},
		{"PUT", "/patrol/skills/{id}/enable", true, func(p *Plane) http.HandlerFunc { return p.patrol.HandleEnable }},
		{"POST", "/patrol/trigger/{id}", true, func(p *Plane) http.HandlerFunc { return p.patrol.HandleTrigger }},
		{"POST", "/patrol/webhook/{event}", true, func(p *Plane) http.HandlerFunc { return p.patrol.HandleWebhook }},

		// The collection is registered both with and without the trailing slash.
		// chi's nested Route used to answer both; the Console calls the bare form
		// and the tests the slashed one, and neither should start 404-ing.
		{"GET", "/agent/drift-proposals", false, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleList }},
		{"GET", "/agent/drift-proposals/", false, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleList }},
		{"GET", "/agent/drift-proposals/{id}", false, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleGet }},
		{"POST", "/agent/drift-proposals", true, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleCreate }},
		{"POST", "/agent/drift-proposals/", true, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleCreate }},
		{"POST", "/agent/drift-proposals/{id}/approve", true, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleApprove }},
		{"POST", "/agent/drift-proposals/{id}/reject", true, func(p *Plane) http.HandlerFunc { return p.driftProposal.HandleReject }},
	}
}

func register(r chi.Router, rt route, auth *actuation.AuthService, h http.HandlerFunc) {
	if rt.operator {
		r.With(auth.Require(actuation.RoleOperator)).Method(rt.method, rt.pattern, h)
		return
	}
	r.Method(rt.method, rt.pattern, h)
}

// Mount registers the L-1 routes on an /api/v1 router, served in this process.
func (p *Plane) Mount(r chi.Router) {
	for _, rt := range routeTable() {
		register(r, rt, p.auth, rt.pick(p))
	}
}

// NewProxyMount returns a mount function that registers the same routes on a
// host router but forwards each call to an operator plane running elsewhere. The
// host still enforces operator auth, so a call that will be refused does not
// cross the network first; the plane checks it again against the same
// platform-auth.yaml. A bad URL fails here, at startup, rather than at mount.
//
// A host that proxies must not also construct a Plane: the patrol autopilot
// keeps its due-scan marks in memory, and exactly one process may hold them.
func NewProxyMount(auth *actuation.AuthService, target string) (func(chi.Router), error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(target), "/"))
	if err != nil {
		return nil, fmt.Errorf("operator plane url %q: %w", target, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("operator plane url %q needs a scheme and host", target)
	}
	proxy := newProxy(base)
	return func(r chi.Router) {
		for _, rt := range routeTable() {
			register(r, rt, auth, proxy.ServeHTTP)
		}
	}, nil
}

func newProxy(base *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(base)
	proxy.Transport = &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	// Say which half is down. Without this the operator sees a bare 502 from
	// platform-api and debugs the wrong process — the exact confusion the split
	// was meant to remove.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("operator plane unreachable", "plane", base.String(), "path", r.URL.Path, "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "operator plane unreachable",
			"plane":  base.String(),
			"detail": err.Error(),
			"hint":   "the L-1 plane runs beside the remediation runners; platform-api itself is up",
		})
	}
	return proxy
}
