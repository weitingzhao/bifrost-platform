package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentbridge"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentdeploy"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentreport"
	"github.com/weitingzhao/bifrost-platform/api/internal/analytics"
	"github.com/weitingzhao/bifrost-platform/api/internal/briefing"
	"github.com/weitingzhao/bifrost-platform/api/internal/buildgate"
	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/console"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/devagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/devsession"
	"github.com/weitingzhao/bifrost-platform/api/internal/driftproposal"
	"github.com/weitingzhao/bifrost-platform/api/internal/escapehatch"
	"github.com/weitingzhao/bifrost-platform/api/internal/flexquery"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesinsight"
	"github.com/weitingzhao/bifrost-platform/api/internal/hermesreadiness"
	"github.com/weitingzhao/bifrost-platform/api/internal/ibgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/lanes"
	"github.com/weitingzhao/bifrost-platform/api/internal/marketdata"
	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/migratewave"
	"github.com/weitingzhao/bifrost-platform/api/internal/network"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatequeue"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/patrol"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
	"github.com/weitingzhao/bifrost-platform/api/internal/research"
	"github.com/weitingzhao/bifrost-platform/api/internal/retrospective"
	"github.com/weitingzhao/bifrost-platform/api/internal/satellite"
	"github.com/weitingzhao/bifrost-platform/api/internal/selfhealth"
	"github.com/weitingzhao/bifrost-platform/api/internal/sessions"
	"github.com/weitingzhao/bifrost-platform/api/internal/sessionsnapshot"
	"github.com/weitingzhao/bifrost-platform/api/internal/stack"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	"github.com/weitingzhao/bifrost-platform/api/internal/topology"
	"github.com/weitingzhao/bifrost-platform/api/internal/tradeagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/vision"
)

type Server struct {
	cfg             *config.Config
	prober          *probe.Prober
	console         *console.Handler
	cluster         *cluster.Handler
	gitops          *gitops.Handler
	mcp             *mcp.Handler
	stack           *stack.Handler
	delivery        *delivery.Handler
	promote         *promote.Handler
	vision          *vision.Handler
	buildgate       *buildgate.Handler
	migratewave     *migratewave.Handler
	tradeagent      *tradeagent.Handler
	devagent        *devagent.Handler
	operatequeue    *operatequeue.Handler
	checklist       *checklist.Handler
	opsagent        *opsagent.Handler
	patrol          *patrol.Handler
	remediation     *remediation.Handler
	agentreport     *agentreport.Handler
	agentbridge     *agentbridge.Handler
	agentgovernance *agentgovernance.Handler
	agentdeploy     *agentdeploy.Handler
	driftproposal   *driftproposal.Handler
	hermesgateway   *hermesgateway.Handler
	hermesreadiness *hermesreadiness.Handler
	hermesinsight   *hermesinsight.Handler
	retrospective   *retrospective.Handler
	satellite       *satellite.Handler
	selfhealth      *selfhealth.Handler
	escapehatch     *escapehatch.Handler
	sessionsnapshot *sessionsnapshot.Handler
	briefing        *briefing.Handler
	network         *network.Handler
	ibgateway       *ibgateway.Handler
	marketdata      *marketdata.Handler
	flexquery       *flexquery.Handler
	research        *research.Handler
	analytics       *analytics.Handler
	telemetry       *telemetry.Handler
	lanes           *lanes.Handler
	sessions        *sessions.Handler
	devSession      *devsession.Handler
	auth            *actuation.AuthService
	audit           *actuation.AuditLog
	jobs            *actuation.JobStore
}

func New(cfg *config.Config) (*Server, error) {
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
	clusterH.Service().StartDataCloneScheduler(context.Background())
	promoteH := promote.NewHandler(cfg, audit, clusterH)
	prober := probe.NewProber()
	devagentH, err := devagent.NewHandler(cfg.ConfigDir())
	if err != nil {
		return nil, fmt.Errorf("devagent: %w", err)
	}
	devagentH.BindAudit(audit)
	operatequeueH := operatequeue.NewHandler(cfg.ConfigDir(), audit)
	operatequeueH.BindRemediationJobs(remediationH.Store())
	operatequeueH.BindRemediationStarter(remediationH)
	operatequeueH.BindLifecycleObserver(devagentH)
	remediationH.BindTerminalObserver(operatequeueH)
	devagentH.BindOperateQueue(operatequeueH)
	checklistH := checklist.NewHandler(cfg.ConfigDir(), audit)
	checklistH.BindRemediation(remediationH)
	checklistH.BindOperateQueue(operatequeueH)
	patrolH, err := patrol.NewHandler(cfg.ConfigDir())
	if err != nil {
		return nil, fmt.Errorf("patrol: %w", err)
	}
	patrolH.Start(context.Background())
	hermesReadinessH := hermesreadiness.NewHandler()
	hermesInsightH, err := hermesinsight.NewHandlerWithOptions(hermesinsight.HandlerOptions{
		Readiness: hermesReadinessH,
	})
	if err != nil {
		return nil, fmt.Errorf("hermesinsight: %w", err)
	}
	operatequeueH.BindEvidenceSource(operatequeue.EvidenceFunc(func() (operatequeue.EvidenceBundle, error) {
		resp, err := checklistH.Store().Get()
		if err != nil {
			return operatequeue.EvidenceBundle{}, err
		}
		sigs := make([]operatequeue.EvidenceSignal, 0, len(resp.Signals))
		for _, s := range resp.Signals {
			sigs = append(sigs, operatequeue.EvidenceSignal{
				ItemID: s.ItemID, Signal: s.Signal, Detail: s.Detail,
			})
		}
		return operatequeue.BundleFromSignals(sigs, time.Now().UTC()), nil
	}))
	sessionsH := sessions.NewHandler(cfg.ConfigDir(), audit)
	devagentH.BindSessions(sessionsH.Store())
	visionH := vision.NewHandler(cfg, audit)
	visionH.BindPrograms(devagentH)
	return &Server{
		cfg:             cfg,
		prober:          prober,
		console:         console.NewHandlerWithCluster(cfg, clusterH),
		cluster:         clusterH,
		gitops:          gitopsH,
		mcp:             mcp.NewHandler(),
		stack:           stack.NewHandler(cfg, audit),
		delivery:        bindDeliveryCycleHook(delivery.NewHandler(cfg, audit), promoteH),
		promote:         promoteH,
		vision:          visionH,
		buildgate:       buildgate.NewHandler(cfg, audit),
		migratewave:     migratewave.NewHandler(cfg, audit),
		tradeagent:      tradeagent.NewHandler(),
		devagent:        devagentH,
		operatequeue:    operatequeueH,
		checklist:       checklistH,
		opsagent:        opsagent.NewHandler(audit),
		patrol:          patrolH,
		remediation:     remediationH,
		agentreport:     agentreport.NewHandler(),
		agentbridge:     agentbridge.NewHandler(),
		agentgovernance: agentgovernance.NewHandler(remediationH.Store()),
		agentdeploy:     agentdeploy.NewHandler(audit),
		driftproposal:   driftproposal.NewHandler(audit),
		hermesgateway:   hermesgateway.NewHandler(),
		hermesreadiness: hermesReadinessH,
		hermesinsight:   hermesInsightH,
		retrospective:   retrospective.NewHandler(retroAnalyzer),
		satellite:       satellite.NewHandler(cfg),
		selfhealth:      selfhealth.NewHandler(cfg, gitopsH.Service()),
		escapehatch:     escapehatch.NewHandler(cfg, audit),
		sessionsnapshot: sessionsnapshot.NewHandler(),
		briefing:        briefing.NewHandler(cfg, prober, audit, promoteH.Store(), clusterH),
		network:         network.NewHandler(audit),
		ibgateway:       ibgateway.NewHandler(clusterH.Service(), audit),
		marketdata:      marketdata.NewHandler(clusterH.Service()),
		flexquery:       flexquery.NewHandler(clusterH.Service()),
		research:        research.NewHandler(clusterH.Service()),
		analytics:       analytics.NewHandler(clusterH.Service()),
		telemetry:       telemetry.NewHandler(cfg, audit),
		lanes:           lanes.NewHandler(cfg.ConfigDir(), audit),
		sessions:        sessionsH,
		devSession:      devsession.NewHandler(devsession.NewService(cfg, clusterH.Service())),
		auth:            auth,
		audit:           audit,
		jobs:            jobs,
	}, nil
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://127.0.0.1:5180", "http://localhost:5180"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Upgrade", "Connection"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", s.handleHealth)
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/environments", s.handleEnvironments)
		r.Get("/matrix", s.handleMatrix)
		r.Get("/satellite/bus-deep", s.satellite.HandleBusDeep)
		r.Get("/telemetry/overview", s.telemetry.HandleOverview)
		r.Get("/telemetry/query", s.telemetry.HandleQuery)
		r.Get("/telemetry/promql", s.telemetry.HandlePromQL)
		r.Get("/telemetry/alerts", s.telemetry.HandleAlerts)
		r.Get("/telemetry/targets", s.telemetry.HandleTargets)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/telemetry/attention-mute", s.telemetry.HandleAttentionMute)
		})
		r.Get("/mission/verify-payload", s.handleVerifyPayload)
		r.Get("/mission/verify-snapshot", s.handleVerifyMissionSnapshot)
		r.Get("/self-health", s.selfhealth.HandleSelfHealth)
		r.Get("/platform/escape-hatch", s.escapehatch.HandleGet)
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
		r.Get("/network/status", s.network.HandleStatus)
		r.Get("/network/zones", s.network.HandleZones)
		r.Get("/network/policies", s.network.HandlePolicies)
		r.Get("/network/audit", s.network.HandleAudit)
		r.Get("/network/devices", s.network.HandleDevices)
		r.Get("/network/clients", s.network.HandleClients)
		r.Get("/network/health", s.network.HandleHealth)
		r.Get("/network/bandwidth", s.network.HandleBandwidth)
		r.Get("/network/anomalies", s.network.HandleAnomalies)
		r.Get("/network/sla", s.network.HandleSLA)
		r.Get("/plugins/ib-gateway/status", s.ibgateway.HandleStatus)
		r.Get("/plugins/market-data/status", s.marketdata.HandleStatus)
		r.Get("/plugins/flex-query/status", s.flexquery.HandleStatus)
		r.Get("/plugins/analytics/status", s.analytics.HandleStatus)
		r.Get("/research/status", s.research.HandleStatus)
		r.Get("/plugins/research/status", s.research.HandleStatus)
		r.Get("/watchlist/union", s.marketdata.HandleWatchlistUnion)
		// Read-only Plugin API proxy (coverage / analytics / ingest list / JSON probes).
		r.Get("/plugins/market-data/api/*", s.marketdata.HandleAPIProxy)
		r.Get("/plugins/flex-query/api/*", s.flexquery.HandleAPIProxy)
		r.Get("/plugins/analytics/api/*", s.analytics.HandleAPIProxy)
		// Research API (:8795) — preferred /research/* + plugin-style alias.
		r.Get("/research/*", s.research.HandleAPIProxy)
		r.Get("/plugins/research/api/*", s.research.HandleAPIProxy)
		r.Get("/agent/nightly-report", s.agentreport.HandleNightlyReport)
		r.Get("/agent/bridge", s.agentbridge.HandleBridge)
		r.Get("/agent/hermes/readiness", s.hermesreadiness.HandleReadiness)
		r.Get("/agent/hermes/first-task", s.hermesreadiness.HandleFirstTask)
		r.Get("/hermes/insights", s.hermesinsight.HandleList)
		r.Post("/hermes/run-first-task", s.hermesinsight.HandleRunFirstTask)
		r.Get("/agent/governance/performance", s.agentgovernance.HandlePerformance)
		r.Get("/agent/governance/trust-matrix", s.agentgovernance.HandleTrustMatrix)
		r.Get("/agent/governance/capability-map", s.agentgovernance.HandleCapabilityMap)
		r.Get("/agent/governance/snapshot", s.agentgovernance.HandleSnapshot)
		r.Get("/agent/governance/trust-overrides", s.agentgovernance.HandleTrustOverrides)
		r.Get("/agent/smoke", s.agentbridge.HandleSmoke)
		r.Get("/agent/deploy", s.agentdeploy.HandleStatus)
		r.Get("/agent/hermes/health", s.hermesgateway.HandleHealth)
		r.Get("/agent/skills", s.hermesgateway.HandleSkills)
		r.Get("/agent/schedules", s.hermesgateway.HandleSchedules)
		r.Get("/agent/executions", s.hermesgateway.HandleExecutions)
		r.Get("/patrol/skills", s.patrol.HandleListSkills)
		r.Get("/patrol/skills/{id}", s.patrol.HandleGetSkill)
		r.Get("/patrol/runs", s.patrol.HandleListRuns)
		r.Get("/agent/retrospective/report", s.retrospective.HandleReport)
		r.Get("/agent/retrospective/patterns", s.retrospective.HandlePatterns)
		r.Get("/agent/retrospective/insights", s.retrospective.HandleInsights)
		r.Get("/agent/retrospective/defects", s.retrospective.HandleDefects)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/agent/nightly-run", s.agentreport.HandleTriggerNightly)
			r.Post("/agent/deploy", s.agentdeploy.HandleStart)
			r.Post("/session-snapshots", s.sessionsnapshot.HandleSave)
			r.Post("/briefing/session-results", s.briefing.HandleCloseSession)
			r.Post("/briefing/prepare", s.devagent.HandleBriefingPrepare)
			r.Put("/agent/skills/{id}/actuation-level", s.hermesgateway.HandleSkillActuationLevel)
			r.Put("/agent/governance/trust-overrides/{skill_id}", s.agentgovernance.HandlePutTrustOverride)
			r.Put("/patrol/skills/{id}/enable", s.patrol.HandleEnable)
			r.Post("/patrol/trigger/{id}", s.patrol.HandleTrigger)
			r.Post("/patrol/webhook/{event}", s.patrol.HandleWebhook)
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
		r.Get("/operate/queue", s.operatequeue.HandleGetQueue)
		r.Get("/operate/briefs", s.operatequeue.HandleListBriefs)
		r.Get("/operate/drain/status", s.operatequeue.HandleDrainStatus)
		r.Get("/checklist/signals", s.checklist.HandleGetSignals)
		r.Get("/checklist/kpis", s.checklist.HandleGetKPIs)
		r.Get("/lanes", s.lanes.HandleList)
		r.Get("/lanes/{id}", s.lanes.HandleGet)
		r.Get("/sessions", s.sessions.HandleList)
		r.Get("/sessions/{id}", s.sessions.HandleGet)
		r.Get("/agent-tasks", s.agentgovernance.HandleListTasks)
		r.Get("/migrate-streams/catalog", s.migratewave.HandleListCatalog)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/operate/queue", s.operatequeue.HandleEnqueue)
			r.Post("/operate/queue/{id}/execution", s.operatequeue.HandleRecordExecution)
			r.Post("/operate/queue/{id}/close", s.operatequeue.HandleClose)
			r.Post("/operate/queue/{id}/dismiss", s.operatequeue.HandleDismiss)
			r.Post("/operate/sweep", s.operatequeue.HandleSweep)
			r.Post("/operate/briefs/{id}/decide", s.operatequeue.HandleDecideBrief)
			r.Post("/checklist/signals", s.checklist.HandlePostSignals)
			r.Post("/lanes", s.lanes.HandleCreate)
			r.Patch("/lanes/{id}", s.lanes.HandleUpdate)
			r.Delete("/lanes/{id}", s.lanes.HandleDelete)
			r.Post("/sessions", s.sessions.HandleCreate)
		})
		r.Route("/programs", func(r chi.Router) {
			r.Get("/", s.devagent.HandlePrograms)
			r.Get("/templates", s.devagent.HandleListTemplates)
			r.Get("/post-completion/pending", s.devagent.HandleListPendingPostCompletion)
			r.Get("/{programId}", s.devagent.HandleGetProgram)
			r.Get("/{programId}/jobs", s.devagent.HandleProgramJobs)
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleOperator))
				r.Post("/from-template", s.devagent.HandleCreateFromTemplate)
				r.Patch("/{programId}", s.devagent.HandlePatchProgram)
				r.Post("/{programId}/phases/{phaseId}/progress", s.devagent.HandlePhaseProgress)
				r.Post("/{programId}/complete", s.devagent.HandleProgramComplete)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleAdmin))
				r.Post("/{programId}/phases/{phaseId}/signoff", s.devagent.HandlePhaseSignoff)
				r.Post("/post-completion/{itemId}/approve", s.devagent.HandleApprovePostCompletionItem)
				r.Post("/post-completion/{itemId}/reject", s.devagent.HandleRejectPostCompletionItem)
				r.Post("/{programId}/post-completion/no-handoff", s.devagent.HandleNoPostCompletionHandoff)
			})
		})
		r.Get("/promote/release-gate", s.promote.HandleGetReleaseGate)
		r.Get("/promote/release-state", s.promote.HandleGetReleaseState)
		r.Get("/promote/gate-history", s.promote.HandleGetGateHistory)
		r.Get("/promote/release-cycles", s.promote.HandleListReleaseCycles)
		r.Get("/promote/release-cycles/{id}", s.promote.HandleGetReleaseCycle)
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
			r.Post("/network/firewall/apply", s.network.HandleFirewallApply)
			r.Post("/plugins/ib-gateway/control/{action}", s.ibgateway.HandleControl)
			// Ingest enqueue (and other Plugin API writes) — operator auth.
			// Proxy rewrites Authorization to MARKET_DATA_WRITE_TOKEN (not the operator token).
			r.Post("/plugins/market-data/api/*", s.marketdata.HandleAPIProxy)
			r.Delete("/plugins/market-data/api/*", s.marketdata.HandleAPIProxy)
			r.Post("/plugins/flex-query/api/*", s.flexquery.HandleAPIProxy)
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
			r.Post("/platform/escape-hatch/drill", s.escapehatch.HandleRecordDrill)
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
			r.Get("/postgres/backup-status", s.cluster.HandlePostgresBackupStatus)
			r.Get("/data-freshness", s.cluster.HandleDataFreshness)
			r.Get("/data-clone", s.cluster.HandleDataCloneList)
			r.Get("/data-clone/schedule", s.cluster.HandleDataCloneScheduleGet)
			r.Get("/data-clone/{id}", s.cluster.HandleDataCloneStatus)
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
				r.Post("/postgres/backup", s.cluster.HandleTriggerPostgresBackup)
				r.Post("/postgres/wal-store/repair", s.cluster.HandleRepairPostgresWalStore)
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
				r.Post("/addons/kube-prometheus-stack/ensure", s.cluster.HandleEnsureKubePrometheusStack)
				r.Post("/nodes/join", s.cluster.HandleJoinNode)
				r.Post("/nodes/{name}/drain", s.cluster.HandleDrainNode)
				r.Post("/nodes/{name}/poweroff", s.cluster.HandlePowerOffNode)
				r.Post("/data-clone", s.cluster.HandleDataClone)
				r.Put("/data-clone/schedule", s.cluster.HandleDataCloneSchedulePut)
			})
		})
		r.Route("/dev-sessions", func(r chi.Router) {
			r.Get("/", s.devSession.HandleList)
			r.Get("/{name}/logs", s.devSession.HandleLogs)
			r.Group(func(r chi.Router) {
				r.Use(s.auth.Require(actuation.RoleOperator))
				r.Post("/{name}/control", s.devSession.HandleControl)
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
	ds := s.datastoreSnapshot(ctx)

	if envID != "" {
		env, ok := s.cfg.GetEnvironment(envID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "unknown environment: " + envID,
			})
			return
		}
		writeJSON(w, http.StatusOK, s.prober.ProbeEnvironmentWithDatastore(ctx, *env, ds))
		return
	}

	results := make([]probe.MatrixResponse, len(s.cfg.Environments))
	var wg sync.WaitGroup
	for i, env := range s.cfg.Environments {
		wg.Add(1)
		go func(idx int, e config.Environment) {
			defer wg.Done()
			results[idx] = s.prober.ProbeEnvironmentWithDatastore(ctx, e, ds)
		}(i, env)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{"matrices": results})
}

func (s *Server) datastoreSnapshot(ctx context.Context) *probe.DatastoreSnapshot {
	if s.cluster == nil {
		return nil
	}
	snap := s.cluster.DatastoreSnapshot(ctx)
	return &snap
}

func (s *Server) handleVerifyPayload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	matrices, dsSnap := s.probeMissionMatrices(ctx)
	writeJSON(w, http.StatusOK, probe.VerifyPayload(s.cfg.Environments, matrices, dsSnap))
}

func (s *Server) handleVerifyMissionSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	matrices, dsSnap := s.probeMissionMatrices(ctx)
	writeJSON(w, http.StatusOK, probe.VerifyMissionSnapshot(s.cfg.Environments, matrices, dsSnap))
}

func (s *Server) probeMissionMatrices(ctx context.Context) ([]probe.MatrixResponse, probe.DatastoreSnapshot) {
	dsSnap := probe.DatastoreSnapshot{}
	if snap := s.datastoreSnapshot(ctx); snap != nil {
		dsSnap = *snap
	}
	matrices := make([]probe.MatrixResponse, 0, len(s.cfg.Environments))
	for _, env := range s.cfg.Environments {
		matrices = append(matrices, s.prober.ProbeEnvironmentWithDatastore(ctx, env, &dsSnap))
	}
	return matrices, dsSnap
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

	matrix := s.prober.ProbeEnvironmentWithDatastore(r.Context(), *env, s.datastoreSnapshot(r.Context()))
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

func bindDeliveryCycleHook(deliveryH *delivery.Handler, promoteH *promote.Handler) *delivery.Handler {
	if deliveryH == nil || promoteH == nil || promoteH.Service() == nil || promoteH.Service().CycleStore() == nil {
		return deliveryH
	}
	cycles := promoteH.Service().CycleStore()
	deliveryH.BindPipelineStartedHook(func(pipelineName, revision, runName, triggeredBy, agentSessionID string) {
		_, _ = cycles.RecordDeployFromPipeline(pipelineName, revision, runName, triggeredBy, agentSessionID)
	})
	return deliveryH
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
