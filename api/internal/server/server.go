package server

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentdeploy"
	"github.com/weitingzhao/bifrost-platform/api/internal/buildgate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentbridge"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentreport"
	"github.com/weitingzhao/bifrost-platform/api/internal/briefing"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/console"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/driftproposal"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/migratewave"
	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
	"github.com/weitingzhao/bifrost-platform/api/internal/retrospective"
	"github.com/weitingzhao/bifrost-platform/api/internal/selfhealth"
	"github.com/weitingzhao/bifrost-platform/api/internal/sessionsnapshot"
	"github.com/weitingzhao/bifrost-platform/api/internal/stack"
	"github.com/weitingzhao/bifrost-platform/api/internal/topology"
	"github.com/weitingzhao/bifrost-platform/api/internal/tradeagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/vision"
)

type Server struct {
	cfg     *config.Config
	prober  *probe.Prober
	console *console.Handler
	cluster *cluster.Handler
	gitops  *gitops.Handler
	mcp     *mcp.Handler
	stack   *stack.Handler
	delivery *delivery.Handler
	promote  *promote.Handler
	vision    *vision.Handler
	buildgate *buildgate.Handler
	migratewave *migratewave.Handler
	tradeagent *tradeagent.Handler
	opsagent     *opsagent.Handler
	remediation  *remediation.Handler
	agentreport  *agentreport.Handler
	agentbridge  *agentbridge.Handler
	agentdeploy  *agentdeploy.Handler
	driftproposal  *driftproposal.Handler
	hermesgateway  *hermesgateway.Handler
	retrospective  *retrospective.Handler
	selfhealth     *selfhealth.Handler
	sessionsnapshot *sessionsnapshot.Handler
	briefing        *briefing.Handler
	auth           *actuation.AuthService
	audit   *actuation.AuditLog
	jobs    *actuation.JobStore
}

func New(cfg *config.Config) *Server {
	auth, err := actuation.LoadAuth(cfg.PlatformAuthPath)
	if err != nil {
		auth = &actuation.AuthService{}
	}
	audit := actuation.NewAuditLog("")
	jobs := actuation.NewJobStore()
	gitopsH := gitops.NewHandler(cfg, audit)
	remediationH := remediation.NewHandler(audit)
	retroAnalyzer := retrospective.NewAnalyzer(remediationH.Store())
	clusterH := cluster.NewHandler(cfg, audit)
	promoteH := promote.NewHandler(cfg, audit)
	prober := probe.NewProber()
	return &Server{
		cfg:     cfg,
		prober:  prober,
		console: console.NewHandlerWithCluster(cfg, clusterH),
		cluster: clusterH,
		gitops:  gitopsH,
		mcp:     mcp.NewHandler(),
		stack:   stack.NewHandler(cfg, audit),
		delivery: delivery.NewHandler(cfg, audit),
		promote:  promoteH,
		vision:    vision.NewHandler(cfg, audit),
		buildgate: buildgate.NewHandler(cfg, audit),
		migratewave: migratewave.NewHandler(cfg, audit),
		tradeagent: tradeagent.NewHandler(),
		opsagent:    opsagent.NewHandler(audit),
		remediation: remediationH,
		agentreport: agentreport.NewHandler(),
		agentbridge: agentbridge.NewHandler(),
		agentdeploy: agentdeploy.NewHandler(audit),
		driftproposal:  driftproposal.NewHandler(audit),
		hermesgateway:  hermesgateway.NewHandler(),
		retrospective:  retrospective.NewHandler(retroAnalyzer),
		selfhealth:     selfhealth.NewHandler(cfg, gitopsH.Service()),
		sessionsnapshot: sessionsnapshot.NewHandler(),
		briefing:        briefing.NewHandler(cfg, prober, audit, promoteH.Store()),
		auth:        auth,
		audit:   audit,
		jobs:    jobs,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://127.0.0.1:5180", "http://localhost:5180"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Upgrade", "Connection"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", s.handleHealth)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/environments", s.handleEnvironments)
		r.Get("/matrix", s.handleMatrix)
		r.Get("/self-health", s.selfhealth.HandleSelfHealth)
		r.Get("/topology", s.handleTopology)
		r.Get("/context", s.handleContext)
		r.Get("/auth/capabilities", s.auth.Capabilities)
		r.Get("/audit", s.audit.HandleList)
		r.Get("/session-snapshots/latest", s.sessionsnapshot.HandleLatest)
		r.Get("/briefing/session-pack", s.briefing.HandleSessionPack)
		r.Get("/briefing/session-results", s.briefing.HandleListSessionResults)
		r.Get("/jobs", s.jobs.HandleList)
		r.Get("/mcp/tools", s.mcp.HandleTools)
		r.Get("/mcp/status", s.mcp.HandleStatus)
		r.Get("/agent/nightly-report", s.agentreport.HandleNightlyReport)
		r.Get("/agent/bridge", s.agentbridge.HandleBridge)
		r.Get("/agent/smoke", s.agentbridge.HandleSmoke)
		r.Get("/agent/deploy", s.agentdeploy.HandleStatus)
		r.Get("/agent/hermes/health", s.hermesgateway.HandleHealth)
		r.Get("/agent/skills", s.hermesgateway.HandleSkills)
		r.Get("/agent/schedules", s.hermesgateway.HandleSchedules)
		r.Get("/agent/executions", s.hermesgateway.HandleExecutions)
		r.Get("/agent/retrospective/report", s.retrospective.HandleReport)
		r.Get("/agent/retrospective/patterns", s.retrospective.HandlePatterns)
		r.Get("/agent/retrospective/insights", s.retrospective.HandleInsights)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/agent/nightly-run", s.agentreport.HandleTriggerNightly)
			r.Post("/agent/deploy", s.agentdeploy.HandleStart)
			r.Post("/session-snapshots", s.sessionsnapshot.HandleSave)
			r.Post("/briefing/session-results", s.briefing.HandleCloseSession)
			r.Put("/agent/skills/{id}/actuation-level", s.hermesgateway.HandleSkillActuationLevel)
		})
		r.Route("/agent/drift-proposals", func(r chi.Router) {
			r.Get("/", s.driftproposal.HandleList)
			r.Get("/{id}", s.driftproposal.HandleGet)
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleOperator))
				r.Post("/", s.driftproposal.HandleCreate)
				r.Post("/{id}/approve", s.driftproposal.HandleApprove)
				r.Post("/{id}/reject", s.driftproposal.HandleReject)
			})
		})
		r.Get("/gitops/apps", s.gitops.HandleApps)
		r.Get("/stack/addons", s.stack.HandleAddons)
		r.Get("/delivery/pipelines", s.delivery.HandlePipelines)
		r.Get("/delivery/supply-chain", s.delivery.HandleSupplyChain)
		r.Get("/delivery/revisions", s.delivery.HandleRevisions)
		r.Get("/delivery/pipelines/{name}/preflight", s.delivery.HandlePipelinePreflight)
		r.Get("/delivery/pipelines/{name}/ref-preflight", s.delivery.HandleRefPreflight)
		r.Get("/delivery/stg/smoke", s.delivery.HandleStgSmoke)
		r.Get("/delivery/dev/smoke", s.delivery.HandleDevSmoke)
		r.Get("/build-phase", s.buildgate.HandleListPhases)
		r.Get("/build-phase/{phase}/gate", s.buildgate.HandleGetGate)
		r.Get("/vision/v1/gate", s.vision.HandleGetV1Gate)
		r.Get("/vision/s3/gate", s.vision.HandleGetS3Gate)
		r.Get("/vision/v2/gate", s.vision.HandleGetV2Gate)
		r.Get("/vision/v3/gate", s.vision.HandleGetV3Gate)
		r.Get("/vision/v4/gate", s.vision.HandleGetV4Gate)
		r.Get("/vision/v5/gate", s.vision.HandleGetV5Gate)
		r.Get("/trade-agent/domains", s.tradeagent.HandleDomains)
		r.Get("/trade-agent/catalog", s.tradeagent.HandleCatalog)
		r.Get("/promote/release-gate", s.promote.HandleGetReleaseGate)
		r.Get("/promote/release-state", s.promote.HandleGetReleaseState)
		r.Get("/promote/gate-history", s.promote.HandleGetGateHistory)
		r.Get("/promote/tier-b", s.promote.HandleGetTierB)
		r.Get("/delivery/pipelines/{name}/runs", s.delivery.HandlePipelineRuns)
		r.Get("/delivery/runs/{id}/logs", s.delivery.HandleRunLogs)
		r.Get("/delivery/runs/{id}/steps", s.delivery.HandleRunSteps)
		r.Route("/remediation", func(r chi.Router) {
			r.Get("/health", s.remediation.HandleHealth)
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleOperator))
				r.Get("/", s.remediation.HandleList)
				r.Post("/start", s.remediation.HandleStart)
				r.Get("/{id}", s.remediation.HandleGet)
				r.Get("/{id}/stream", s.remediation.HandleStream)
				r.Post("/{id}/cancel", s.remediation.HandleCancel)
				r.Post("/{id}/respond", s.remediation.HandleRespond)
			})
		})
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/gitops/apps/{name}/sync", s.gitops.HandleSyncApp)
			r.Post("/delivery/pipelines/{name}/runs", s.delivery.HandleStartPipelineRun)
			r.Post("/delivery/supply-chain/mirror-sync", s.delivery.HandleMirrorSync)
			r.Post("/delivery/supply-chain/dockerfile-configmaps/refresh", s.delivery.HandleRefreshDockerfileCMs)
			r.Post("/ops-agent/alertmanager", s.opsagent.HandleAlertmanager)
			r.Delete("/delivery/runs/{id}", s.delivery.HandleDeletePipelineRun)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleAdmin))
			r.Post("/gitops/apps/{name}/rollback", s.gitops.HandleRollbackApp)
			r.Post("/stack/addons/{name}/install", s.stack.HandleInstallAddon)
			r.Post("/stack/addons/{name}/upgrade", s.stack.HandleUpgradeAddon)
			r.Post("/promote/release-gate", s.promote.HandleRunReleaseGate)
			r.Post("/promote/tier-b/signoff", s.promote.HandleSignTierB)
			r.Post("/build-phase/{phase}/gate", s.buildgate.HandleRunGate)
			r.Post("/build-phase/{phase}/signoff", s.buildgate.HandleSignoff)
			r.Post("/migrate-streams/{streamId}/waves/{waveId}/deliver", s.migratewave.HandleDeliver)
			r.Post("/migrate-streams/{streamId}/waves/{waveId}/signoff", s.migratewave.HandleSignoff)
			r.Post("/vision/v1/gate", s.vision.HandleRunV1Gate)
			r.Post("/vision/v1/signoff", s.vision.HandleSignV1)
			r.Post("/vision/s3/gate", s.vision.HandleRunS3Gate)
			r.Post("/vision/s3/signoff", s.vision.HandleSignS3)
			r.Post("/vision/v2/gate", s.vision.HandleRunV2Gate)
			r.Post("/vision/v2/signoff", s.vision.HandleSignV2)
			r.Post("/vision/v3/gate", s.vision.HandleRunV3Gate)
			r.Post("/vision/v3/signoff", s.vision.HandleSignV3)
			r.Post("/vision/v4/gate", s.vision.HandleRunV4Gate)
			r.Post("/vision/v4/signoff", s.vision.HandleSignV4)
			r.Post("/vision/v5/gate", s.vision.HandleRunV5Gate)
			r.Post("/vision/v5/signoff", s.vision.HandleSignV5)
		})
		r.Get("/console/hosts", s.console.HandleHosts)
		r.Get("/console/ws", s.console.HandleWebSocket)
		r.Route("/cluster", func(r chi.Router) {
			r.Get("/", s.cluster.HandleSummary)
			r.Get("/nodes", s.cluster.HandleNodes)
			r.Get("/governance", s.cluster.HandleGovernance)
			r.Get("/service-readiness", s.cluster.HandleServiceReadiness)
			r.Get("/postgres", s.cluster.HandlePostgresStatus)
			r.Get("/redis", s.cluster.HandleRedisStatus)
			r.Get("/join-profiles", s.cluster.HandleJoinProfiles)
			r.Get("/nodes/{name}/power", s.cluster.HandleNodePower)
			r.Get("/placement", s.cluster.HandlePlacement)
			r.Get("/metrics", s.cluster.HandleMetrics)
			r.Get("/observability", s.cluster.HandleObservability)
			r.Get("/namespaces", s.cluster.HandleNamespaces)
			r.Get("/workloads", s.cluster.HandleWorkloads)
			r.Get("/events", s.cluster.HandleEvents)
			r.Post("/sync-kubeconfig", s.cluster.HandleSyncKubeconfig)
			r.Get("/workloads/pods/{namespace}/{name}/logs", s.cluster.HandlePodLogs)
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleOperator))
				r.Post("/namespaces/ensure-bifrost", s.cluster.HandleEnsureBifrost)
				r.Post("/workloads/rollout-restart", s.cluster.HandleRolloutRestart)
				r.Post("/workloads/scale", s.cluster.HandleScale)
				r.Post("/nodes/{name}/wake", s.cluster.HandleWakeNode)
				r.Post("/nodes/{name}/cordon", s.cluster.HandleCordonNode)
				r.Post("/nodes/{name}/uncordon", s.cluster.HandleUncordonNode)
				r.Delete("/workloads/pods/{namespace}/{name}", s.cluster.HandleDeletePod)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleAdmin))
				r.Post("/kubeconfig-secret/ensure", s.cluster.HandleEnsureKubeconfigSecret)
				r.Post("/addons/metrics-server/ensure", s.cluster.HandleEnsureMetricsServer)
				r.Post("/nodes/join", s.cluster.HandleJoinNode)
				r.Post("/nodes/{name}/drain", s.cluster.HandleDrainNode)
				r.Post("/nodes/{name}/poweroff", s.cluster.HandlePowerOffNode)
			})
		})
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "bifrost-platform-api",
	})
}

func (s *Server) handleEnvironments(w http.ResponseWriter, _ *http.Request) {
	envs := make([]map[string]string, 0, len(s.cfg.Environments))
	for _, e := range s.cfg.Environments {
		envs = append(envs, map[string]string{
			"id":    e.ID,
			"label": e.Label,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": envs})
}

func (s *Server) handleMatrix(w http.ResponseWriter, r *http.Request) {
	envID := r.URL.Query().Get("env")
	ctx := r.Context()

	if envID != "" {
		env, ok := s.cfg.GetEnvironment(envID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "unknown environment: " + envID,
			})
			return
		}
		writeJSON(w, http.StatusOK, s.prober.ProbeEnvironment(ctx, *env))
		return
	}

	results := make([]probe.MatrixResponse, len(s.cfg.Environments))
	var wg sync.WaitGroup
	for i, env := range s.cfg.Environments {
		wg.Add(1)
		go func(idx int, e config.Environment) {
			defer wg.Done()
			results[idx] = s.prober.ProbeEnvironment(ctx, e)
		}(i, env)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{"matrices": results})
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Topology == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "topology not loaded",
		})
		return
	}

	envID := r.URL.Query().Get("env")
	if envID == "" {
		envID = "prod"
		if len(s.cfg.Environments) > 0 {
			envID = s.cfg.Environments[0].ID
		}
	}

	env, ok := s.cfg.GetEnvironment(envID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "unknown environment: " + envID,
		})
		return
	}

	matrix := s.prober.ProbeEnvironment(r.Context(), *env)
	resp := topology.Build(s.cfg.Topology, *env, matrix)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContext(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.OpsContext == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "ops context not loaded",
		})
		return
	}
	ctx := promote.OverlayContext(s.cfg.OpsContext, s.promote.Store())
	writeJSON(w, http.StatusOK, ctx)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
