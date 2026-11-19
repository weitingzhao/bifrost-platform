package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"

	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry/checklistview"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalrules"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	"github.com/weitingzhao/bifrost-platform/api/internal/cidomains"
	"github.com/weitingzhao/bifrost-platform/api/internal/ciscore"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/dataclone"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/devsession"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
	"github.com/weitingzhao/bifrost-platform/api/internal/ibgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/instruments"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
	"github.com/weitingzhao/bifrost-platform/api/internal/marketdata"
	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/network"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatorplane"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/selfhealth"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	tenantassembly "github.com/weitingzhao/bifrost-platform/api/internal/tenant/assembly"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
	"github.com/weitingzhao/bifrost-platform/api/internal/topology"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

type Server struct {
	cfg     *config.Config
	prober  *probe.Prober
	cluster *cluster.Handler
	// dataclone is the tenant's data clone, freshness and data probe
	// (moved out of cluster on 2026-11-17).
	dataclone *dataclone.Handler
	gitops    *gitops.Handler
	mcp       *mcp.Handler
	delivery  *delivery.Handler
	// unitTrees reads commits' trees from Gitea for the unit plan
	// (gitread.Trees), keeping the blobs it read.
	unitTrees *gitread.Trees
	// unitRunsKept keeps the run view's answers a short while (cirunview.Cache).
	unitRunsKept unitRunsCache
	// ciScorecardKept keeps the CI scorecard a short while (ci_read_wire.go).
	ciScorecardKept ciscore.Cache
	// ciDomainsKept keeps the CI page's domain graph and the files commits
	// changed (ci_read_wire.go).
	ciDomainsKept cidomains.Cache
	unitTreesOnce sync.Once
	// instruments answers /governance/instruments (governance_instruments.go).
	instruments     *instruments.Service
	instrumentsOnce sync.Once
	promote         *promote.Handler
	checklist       *checklist.Handler
	opsagent        *opsagent.Handler
	agentgovernance *agentgovernance.Handler
	lineage         *lineage.Handler
	releases        *releases.Handler
	work            *workactions.Handler
	// The out-of-band operator plane (L-1). It is one deployable: cmd/operator-plane
	// serves exactly these routes beside the operator plane, where a bad
	// platform-api release cannot reach it. plane is nil when OPERATOR_PLANE_URL
	// points at such a process — then mountPlane forwards instead of serving, and
	// nothing here constructs a second patrol autopilot.
	plane      *operatorplane.Plane
	planeURL   string
	mountPlane func(chi.Router)
	selfhealth *selfhealth.Handler
	network    *network.Handler
	ibgateway  *ibgateway.Handler
	marketdata *marketdata.Handler
	// tenant is the tenant domain's assembly (tenant/assembly): its handlers and routes.
	tenant      *tenantassembly.Tenant
	telemetry   *telemetry.Handler
	nodeLatency placement.PromNodeMetrics
	devSession  *devsession.Handler
	auth        *actuation.AuthService
	approvals   *approvals.Service
	// approvalsCancel stops this process's sole approval result loop.
	approvalsCancel context.CancelFunc
	// questionRuntime is the Owner-question record; the type lives in
	// agentrt/questionbridge.
	questionRuntime *questionbridge.Runtime
	releasePolicy   *releasepolicy.Engine
	agentThreads    *agentthreads.Handler
	agentRuntime    *agentRuntime
	bus             busWiring // the event bus, step 1 (bus_wire.go)
	// landingHealth is what the workers wired for landing (S3): each reach
	// of the landing cards into the question store and why live is off.
	landingHealth atomic.Value // map[string]any
	authLoaded    bool
	audit         *actuation.AuditLog
	httpMetrics   *httpMetrics
	// pluginProbes, when set, replaces the four plugin health calls on /metrics.
	// Tests inject counters. Production leaves it nil.
	pluginProbes []func(context.Context) probe.PluginHealth
}

func New(cfg *config.Config) (*Server, error) {
	auth, err := actuation.LoadAuth(cfg.PlatformAuthPath)
	if errors.Is(err, actuation.ErrDuplicateSeat) {
		return nil, fmt.Errorf("platform startup: %w", err)
	}
	authLoaded := err == nil
	if err != nil {
		// Fails closed (every gated route answers 401), which reads like a bad
		// token unless the cause is said here and in /health.
		slog.Error("platform auth not loaded; every gated route will answer 401",
			"path", cfg.PlatformAuthPath, "err", err)
		auth = &actuation.AuthService{}
	}
	// Platform state (TD-196). In the cluster every state file under
	// PLATFORM_DATA_DIR lives in a ConfigMap of this pod's namespace, so a
	// rollout keeps it and the api and workers pods share it; until 2026-10-07
	// it sat on a per-pod emptyDir. Local runs keep plain files.
	auditPath := ""
	if dataDir := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR")); dataDir != "" && k8sstate.Wanted() {
		if ns := k8sstate.Namespace(); ns != "" {
			statefile.Use(k8sstate.New(ns, k8sstate.ClientsFromEnv()), dataDir)
			auditPath = filepath.Join(dataDir, "audit", "audit-"+config.CurrentRole().String()+".json")
			slog.Info("platform state in ConfigMaps", "namespace", ns, "data_dir", dataDir, "audit", auditPath)
		} else {
			slog.Warn("platform state: no namespace found, keeping files (lost on rollout)")
		}
	}
	audit := actuation.NewAuditLog(auditPath)
	if active, dataDir := statefile.Active(); active && config.CurrentRole() == config.RoleAPI {
		// GET /audit is served here; the workers pod records the loops' actions
		audit.AlsoList(filepath.Join(dataDir, "audit", "audit-workers.json"))
	}
	role := config.CurrentRole()
	clusterH := cluster.NewHandler(cfg, audit)
	gitopsH := gitops.NewHandler(cfg, audit, clusterH.Service())
	bindClusterReadings(clusterH)
	// The tenant's handlers (tenant/assembly); its data clone runs its
	// commands through cluster's PostgreSQL adapter.
	tenantA := tenantassembly.New(cfg, clusterH.Service().Entry(), audit, clusterH.Service())
	dataH, marketdataH, ibgatewayH := tenantA.DataClone, tenantA.MarketData, tenantA.IBGateway
	bindDataClone(clusterH, dataH, marketdataH)
	if role.RunsWorkers() {
		ibgatewayH.StartBackground(context.Background(), audit)
	}
	slog.Info("platform-api role",
		"role", role.String(),
		"runs_background_loops", role.RunsWorkers(),
		"note", "workers must stay a singleton — the loops keep per-process last-run state")
	if role.RunsWorkers() {
		dataH.Service().StartDataCloneScheduler(context.Background())
		clusterH.Service().StartFailedBackupSweep(context.Background())
	}
	promoteH := promote.NewHandler(cfg)
	prober := probe.NewProber()
	checklistH := checklist.NewHandler(cfg.ConfigDir(), audit)
	plane, planeURL, mountPlane, err := newOperatorPlane(cfg, auth, audit, role)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:             cfg,
		prober:          prober,
		cluster:         clusterH,
		dataclone:       dataH,
		gitops:          gitopsH,
		mcp:             mcp.NewHandler(),
		delivery:        delivery.NewHandler(cfg, audit),
		promote:         promoteH,
		checklist:       checklistH,
		opsagent:        opsagent.NewHandler(audit),
		plane:           plane,
		planeURL:        planeURL,
		mountPlane:      mountPlane,
		agentgovernance: agentgovernance.NewHandler(),
		selfhealth:      selfhealth.NewHandler(cfg, selfHealthArgoApps(gitopsH.Service())),
		network:         network.NewHandler(audit),
		ibgateway:       ibgatewayH,
		tenant:          tenantA,
		marketdata:      marketdataH,
		telemetry:       telemetry.NewHandler(cfg, audit),
		devSession:      devsession.NewHandler(devsession.NewService(cfg, clusterH.Service())),
		auth:            auth,
		authLoaded:      authLoaded,
		audit:           audit,
		httpMetrics:     newHTTPMetrics(),
	}
	srv.checklist.BindHusbandry(checklistview.Source(tenantA.Husbandry.Service()))
	// One producer of maintenance signals: only the PROD workers overlay sets
	// CHECKLIST_PROBER=on (TD-253).
	if role.RunsWorkers() && checklist.ProberWanted() {
		srv.checklist.StartProber(context.Background(), checklist.NewProberFromEnv(), checklist.ProberInterval())
	}
	releasesSvc := releases.NewService(cfg.ConfigDir(), func() (kubernetes.Interface, dynamic.Interface, error) {
		core, _, err := clusterH.Service().KubernetesClient()
		if err != nil {
			return nil, nil, err
		}
		rc, _, err := clusterH.Service().RestConfig()
		if err != nil {
			return nil, nil, err
		}
		dyn, err := dynamic.NewForConfig(rc)
		return core, dyn, err
	})
	if role.RunsWorkers() && releases.RecorderWanted() {
		releasesSvc.Start(kubemetrics.WithSite(context.Background(), "releases.tick"), 5*time.Minute)
	}
	srv.releases = releases.NewHandler(releasesSvc)
	if srv.delivery != nil {
		releasesSvc.SetRunReader(srv.delivery.GateRuns)
		srv.delivery.SetNodeMetricsReader(srv.buildNodeMetrics)
		srv.delivery.SetProdPrepare(srv.migrateBeforeProd)
	}
	titleStore := threadtitles.NewStore(releasesSvc.Status().Namespace, func() (kubernetes.Interface, error) {
		core, _, err := clusterH.Service().KubernetesClient()
		return core, err
	})
	srv.agentgovernance.UseTrustOverrideStore(agentgovernance.NewYAMLTrustOverrideStore(
		filepath.Join(cfg.ConfigDir(), "trust-overrides.yaml"),
	))
	slog.Info("trust overrides store", "location", srv.agentgovernance.TrustOverrideLocation())
	srv.wireLineage(releasesSvc, titleStore)
	srv.wireWorkActions(cfg, clusterH)
	dataDir := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(cfg.ConfigDir(), "..", "data")
	}
	srv.approvals = approvals.New(filepath.Join(dataDir, "approvals"), audit)
	srv.approvals.SetCreateCheck(srv.approvalCreateCheck)
	// One open landing grant request per operation (review 694 R-02), one
	// open rule approval per rule and agentd deployment per host.
	srv.approvals.SetOpenKey(approvalrules.OpenKey)
	actions.SetRuleChangeLookup(srv.ruleApproveChanges)
	srv.wireQuestions(dataDir, role.RunsWorkers())
	srv.wireReleasePolicy(releasesSvc, dataDir, role.RunsWorkers())
	srv.wireAgentThreads(dataDir, role, titleStore)
	srv.wireAgentRuntime(role)
	if role.RunsWorkers() {
		// The CI page's domain graph is read ahead of any request (ci_domains.go).
		srv.ciDomainsAhead(context.Background())
	}
	if role.RunsAPI() || role.RunsWorkers() {
		srv.delivery.StartRunWindowRecovery()
	}
	srv.wireSessionBindings()
	srv.wireOpsEvents()
	srv.bindActionExecutors()
	srv.startApprovalLoop(role.RunsWorkers())
	return srv, nil
}

// approvalRetryInterval is how often platform-workers retries approvals left
// approved by a transient refusal and moves lapsed leases to unknown.
const approvalRetryInterval = 15 * time.Second

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	// Outside Logger and Recoverer, so a recovered panic is counted as the 500 it
	// answers (TD-195).
	r.Use(s.httpMetrics.Middleware(r))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Kubernetes requests made while answering HTTP are counted under site
	// "http" (kubemetrics, 2026-11-16: which of the platform's LISTs are page
	// reads and which are its loops).
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(kubemetrics.WithSite(req.Context(), "http")))
		})
	})
	// A request for an action the signed policy covers is run, not answered
	// "call directly" (approvalrules.CoveredRequestRuns).
	r.Use(approvalrules.CoveredRequestRuns(r, readAndRestore))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://127.0.0.1:5180", "http://localhost:5180"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Upgrade", "Connection", "X-Bifrost-Session"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", s.handleHealth)
	// Unauthenticated like /health: Prometheus scrapes it, and it exposes only
	// what /api/v1/plugins/*/status already answers anonymously.
	r.Get("/metrics", s.handleMetrics)
	root := r
	r.Route("/api/v1", func(r chi.Router) {
		// One gate for a collector token, before every module mounts
		// (R5:5.5-12 §1.3): it reaches actuation.CollectorRoutes and nothing else.
		r.Use(s.auth.ConfineCollectors(actuation.CollectorAllowed(root, actuation.CollectorRoutes())))
		// L-1 surface. Served here for convenience while platform-api is healthy;
		// cmd/operator-plane serves the same routes off-cluster for when it is not.
		s.mountPlane(r)
		approvals.Mount(r, s.auth, s.approvals)
		s.mountQuestions(r)
		s.mountAgentRuntime(r)
		s.mountBus(r)
		s.mountHomeLayout(r)
		// Each domain registers its own routes (mount_<domain>.go).
		s.mountFoundation(r)
		s.tenant.Mount(r, tenantassembly.Gates{Operator: s.auth.Require(actuation.RoleOperator), Guard: s.guard, GuardIB: s.guardIB, Topology: s.handleTopology})
		s.mountVerification(r)
		s.mountDelivery(r)
		s.mountLineage(r)
		s.mountCoordination(r)
		s.mountOwner(r)
		s.mountCluster(r)
		s.mountDevSessions(r)
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// contained_panics is not decoration: a non-zero count means some worker
	// died mid-flight and the surface it feeds may be stale while /health is ok.
	role := config.CurrentRole()
	body := map[string]any{
		"status":           "ok",
		"service":          "bifrost-platform-api",
		"role":             role.String(),
		"background_loops": role.RunsWorkers(),
		"contained_panics": safego.Contained(),
		"operator_plane":   s.operatorPlaneMode(),
		"auth_loaded":      s.authLoaded,
	}
	// Whether the database has this binary's agentrt migrations (TD-411):
	// behind is said here, and the platform keeps serving.
	for k, v := range s.agentRuntimeSchema() {
		body[k] = v
	}
	// Whether this process's runtime routes take agentd's calls: the
	// workers' controller asks it through the Service (review 528 A1b).
	body["agentrt_ready"] = s.agentRuntime != nil && s.agentRuntime.late != nil && s.agentRuntime.late.Ready()
	if h, ok := s.landingHealth.Load().(map[string]any); ok {
		body["landing"] = h
	}
	writeJSON(w, http.StatusOK, body)
}

// operatorPlaneMode tells an operator which process is answering the L-1 routes,
// so a 502 from them is not mistaken for platform-api being down.
func (s *Server) operatorPlaneMode() string {
	if s.planeURL != "" {
		return "proxy:" + s.planeURL
	}
	return "in-process"
}

// probeRoutes are the connectivity reads (probe.Routes).
func (s *Server) probeRoutes() probe.Routes {
	return probe.Routes{Cfg: s.cfg, Prober: s.prober, Snapshot: s.datastoreSnapshot}
}

func (s *Server) handleEnvironments(w http.ResponseWriter, r *http.Request) {
	s.probeRoutes().HandleEnvironments(w, r)
}

func (s *Server) handleMatrix(w http.ResponseWriter, r *http.Request) {
	s.probeRoutes().HandleMatrix(w, r)
}

func (s *Server) datastoreSnapshot(ctx context.Context) *probe.DatastoreSnapshot {
	if s.cluster == nil {
		return nil
	}
	snap := s.cluster.DatastoreSnapshot(ctx)
	return &snap
}

func (s *Server) handleVerifyPayload(w http.ResponseWriter, r *http.Request) {
	s.probeRoutes().HandleVerifyPayload(w, r)
}

func (s *Server) handleVerifyMissionSnapshot(w http.ResponseWriter, r *http.Request) {
	s.probeRoutes().HandleVerifyMissionSnapshot(w, r)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	topology.Route{Cfg: s.cfg, Live: s.liveHosts,
		Matrix: func(ctx context.Context, env config.Environment) probe.MatrixResponse {
			return s.prober.ProbeEnvironmentWithDatastore(ctx, env, s.datastoreSnapshot(ctx))
		}}.ServeHTTP(w, r)
}

// liveHosts reads which machines are cluster nodes and which hosts Prometheus
// scrapes, for the topology answer. Either source may be missing (no cluster
// in a local run, Prometheus down): the answer then keeps what the file says.
func (s *Server) liveHosts(ctx context.Context) topology.Live {
	live := topology.Live{ClusterNodes: map[string]string{}}
	if s.cluster != nil {
		nodes := s.cluster.ClusterNodes(ctx)
		if nodes.Reachability != probe.ReachFail && len(nodes.Nodes) > 0 {
			live.ClusterKnown = true
			for _, n := range nodes.Nodes {
				if n.InternalIP != "" {
					live.ClusterNodes[n.InternalIP] = n.Name
				}
			}
		}
	}
	if s.telemetry != nil {
		live.Scraped = s.telemetry.ScrapedHosts(ctx)
	}
	return live
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	s.promote.HandleOpsContext(s.cfg.OpsContext)(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// selfHealthArgoApps is the delivery domain's Argo CD reader in the shape
// self-health declares (selfhealth.ArgoApps); nil without a gitops service.
func selfHealthArgoApps(svc *gitops.Service) selfhealth.ArgoApps {
	if svc == nil {
		return nil
	}
	return func(ctx context.Context) []selfhealth.ArgoApp {
		resp := svc.Apps(ctx)
		out := make([]selfhealth.ArgoApp, 0, len(resp.Apps))
		for _, app := range resp.Apps {
			out = append(out, selfhealth.ArgoApp{Name: app.Name, SyncStatus: app.SyncStatus, HealthStatus: app.HealthStatus})
		}
		return out
	}
}
