package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/appreadings"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	datahusbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry/checklistview"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentprogress"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/codehealth"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/devsession"
	"github.com/weitingzhao/bifrost-platform/api/internal/flexquery"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
	"github.com/weitingzhao/bifrost-platform/api/internal/ibgateway"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
	"github.com/weitingzhao/bifrost-platform/api/internal/marketdata"
	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/network"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatorplane"
	"github.com/weitingzhao/bifrost-platform/api/internal/opsagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/patrol"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/research"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"github.com/weitingzhao/bifrost-platform/api/internal/satellite"
	"github.com/weitingzhao/bifrost-platform/api/internal/selfhealth"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
	"github.com/weitingzhao/bifrost-platform/api/internal/telemetry"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
	"github.com/weitingzhao/bifrost-platform/api/internal/topology"
	"github.com/weitingzhao/bifrost-platform/api/internal/tradeagent"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

type Server struct {
	cfg      *config.Config
	prober   *probe.Prober
	cluster  *cluster.Handler
	gitops   *gitops.Handler
	mcp      *mcp.Handler
	delivery *delivery.Handler
	// unitTrees reads commits' trees from Gitea for the unit plan
	// (units_plan.go), keeping the blobs it read.
	unitTrees *giteaTrees
	// unitRunsKept keeps the run view's answers a short while (units_runs.go).
	unitRunsKept unitRunsCache
	// ciScorecardKept keeps the CI scorecard a short while (ci_scorecard.go).
	ciScorecardKept ciScorecardCache
	// ciDomainsKept keeps the CI page's domain graph and the files commits
	// changed (ci_domains.go).
	ciDomainsKept   ciDomainsCache
	unitTreesOnce   sync.Once
	promote         *promote.Handler
	tradeagent      *tradeagent.Handler
	checklist       *checklist.Handler
	opsagent        *opsagent.Handler
	agentgovernance *agentgovernance.Handler
	codehealth      *codehealth.Handler
	lineage         *lineage.Handler
	releases        *releases.Handler
	work            *workactions.Handler
	// The out-of-band operator plane (L-1). It is one deployable: cmd/operator-plane
	// serves exactly these routes beside the operator plane, where a bad
	// platform-api release cannot reach it. plane is nil when OPERATOR_PLANE_URL
	// points at such a process — then mountPlane forwards instead of serving, and
	// nothing here constructs a second patrol autopilot.
	plane         *operatorplane.Plane
	planeURL      string
	mountPlane    func(chi.Router)
	satellite     *satellite.Handler
	selfhealth    *selfhealth.Handler
	network       *network.Handler
	ibgateway     *ibgateway.Handler
	marketdata    *marketdata.Handler
	flexquery     *flexquery.Handler
	research      *research.Handler
	datahusbandry *datahusbandry.Handler
	telemetry     *telemetry.Handler
	devSession    *devsession.Handler
	auth          *actuation.AuthService
	approvals     *approvals.Service
	// approvalsCancel stops this process's sole approval result loop.
	approvalsCancel context.CancelFunc
	// questionRuntime is the Owner-question record. The type lives next to
	// Mount so this file does not name the questions package.
	questionRuntime *questionRuntime
	releasePolicy   *releasepolicy.Engine
	agentThreads    *agentthreads.Handler
	agentProgress   *agentprogress.Handler
	agentRuntime    *agentRuntime
	bus             busWiring // the event bus, step 1 (bus_wire.go)
	// landingHealth is what the workers wired for landing (S3): each reach
	// of the landing cards into the question store and why live is off.
	landingHealth atomic.Value // map[string]any
	authLoaded    bool
	audit         *actuation.AuditLog
	jobs          *actuation.JobStore
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
	jobs := actuation.NewJobStore()
	gitopsH := gitops.NewHandler(cfg, audit)
	role := config.CurrentRole()
	clusterH := cluster.NewHandler(cfg, audit)
	// The compute domain evaluates the node pools; cluster declares the shape.
	clusterH.Service().BindPools(placement.ClusterPools)
	// The tenant domain reads its applications and data from what cluster gathers.
	cluster.BindAppReadings(cluster.AppReadings{ApplicationsDomain: appreadings.EvalApplicationsDomain, FreshnessDatabases: appreadings.FreshnessDatabases})
	ibgatewayH := ibgateway.NewHandler(clusterH.Service(), audit)
	if role.RunsWorkers() {
		ibgatewayH.StartBackground(context.Background(), audit)
	}
	slog.Info("platform-api role",
		"role", role.String(),
		"runs_background_loops", role.RunsWorkers(),
		"note", "workers must stay a singleton — the loops keep per-process last-run state")
	if role.RunsWorkers() {
		clusterH.Service().StartDataCloneScheduler(context.Background())
		clusterH.Service().StartFailedBackupSweep(context.Background())
	}
	promoteH := promote.NewHandler(cfg, audit, clusterH)
	prober := probe.NewProber()
	checklistH := checklist.NewHandler(cfg.ConfigDir(), audit)
	// L-1 lives out of band once OPERATOR_PLANE_URL is set: forward those routes.
	// /patrol/* is never forwarded. This process serves it from the shared
	// statefile and does not start the autopilot — that loop runs on workers.
	planeURL := strings.TrimSpace(os.Getenv("OPERATOR_PLANE_URL"))
	var plane *operatorplane.Plane
	var mountPlane func(chi.Router)
	var patrolH *patrol.Handler
	if planeURL == "" {
		plane, err = operatorplane.New(operatorplane.Deps{
			Auth:      auth,
			Audit:     audit,
			ConfigDir: cfg.ConfigDir(),
		})
		if err != nil {
			return nil, fmt.Errorf("operator plane: %w", err)
		}
		if role.RunsWorkers() {
			plane.StartBackground(context.Background())
		}
		patrolH = plane.Patrol()
		mountPlane = func(r chi.Router) {
			plane.Mount(r)
			operatorplane.MountPatrol(auth, patrolH, r)
		}
	} else {
		mountPlane, err = operatorplane.NewProxyMount(auth, planeURL)
		if err != nil {
			return nil, fmt.Errorf("operator plane proxy: %w", err)
		}
		patrolH, err = patrol.NewHandler(cfg.ConfigDir())
		if err != nil {
			return nil, fmt.Errorf("patrol: %w", err)
		}
		proxy := mountPlane
		mountPlane = func(r chi.Router) {
			proxy(r)
			operatorplane.MountPatrol(auth, patrolH, r)
		}
		slog.Info("operator plane is out of band", "url", planeURL, "note", "L-1 routes proxied; patrol stays local; no patrol autopilot in this process")
	}
	srv := &Server{
		cfg:             cfg,
		prober:          prober,
		cluster:         clusterH,
		gitops:          gitopsH,
		mcp:             mcp.NewHandler(),
		delivery:        delivery.NewHandler(cfg, audit),
		promote:         promoteH,
		tradeagent:      tradeagent.NewHandler(),
		checklist:       checklistH,
		opsagent:        opsagent.NewHandler(audit),
		plane:           plane,
		planeURL:        planeURL,
		mountPlane:      mountPlane,
		agentgovernance: agentgovernance.NewHandler(),
		codehealth:      codehealth.NewHandler(audit),
		satellite:       satellite.NewHandler(cfg),
		selfhealth:      selfhealth.NewHandler(cfg, selfHealthArgoApps(gitopsH.Service())),
		network:         network.NewHandler(audit),
		ibgateway:       ibgatewayH,
		marketdata:      marketdata.NewHandler(clusterH.Service()),
		flexquery:       flexquery.NewHandler(clusterH.Service()),
		research:        research.NewHandler(clusterH.Service()),
		telemetry:       telemetry.NewHandler(cfg, audit),
		devSession:      devsession.NewHandler(devsession.NewService(cfg, clusterH.Service())),
		auth:            auth,
		authLoaded:      authLoaded,
		audit:           audit,
		jobs:            jobs,
		httpMetrics:     newHTTPMetrics(),
	}
	mdH := srv.marketdata
	fqH := srv.flexquery
	rsH := srv.research
	srv.datahusbandry = datahusbandry.NewHandler(datahusbandry.NewService(mdH.Service(), fqH.Service(), rsH.Service()))
	srv.checklist.BindHusbandry(checklistview.Source(srv.datahusbandry.Service()))
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
		releasesSvc.Start(context.Background(), 5*time.Minute)
	}
	srv.releases = releases.NewHandler(releasesSvc)
	titleStore := threadtitles.NewStore(releasesSvc.Status().Namespace, func() (kubernetes.Interface, error) {
		core, _, err := clusterH.Service().KubernetesClient()
		return core, err
	})
	srv.agentgovernance.UseTrustOverrideStore(agentgovernance.NewYAMLTrustOverrideStore(
		filepath.Join(cfg.ConfigDir(), "trust-overrides.yaml"),
	))
	slog.Info("trust overrides store", "location", srv.agentgovernance.TrustOverrideLocation())
	if dir := threadtitles.TranscriptDir(); role.RunsWorkers() && threadtitles.SyncWanted(dir) {
		threadtitles.StartSync(context.Background(), titleStore, threadtitles.NewScanner(dir, 30*24*time.Hour), 3*time.Minute)
	}
	srv.lineage = lineage.NewHandler(lineage.NewService(func(ctx context.Context) (lineage.Access, error) {
		a, err := srv.delivery.GiteaAccess(ctx)
		// The agent organization's branches too (T36), as the result check reads them.
		return lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass,
			AgentOrg: strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))}, err
	}).WithReleases(func(ctx context.Context) ([]lineage.Release, error) {
		recs, err := releasesSvc.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]lineage.Release, 0, len(recs))
		for _, r := range recs {
			repos := make(map[string]string, len(r.Repos))
			for name, b := range r.Repos {
				repos[name] = b.SHA
			}
			out = append(out, lineage.Release{Run: r.Run, Lane: r.Lane, Env: r.Env, Deploys: r.Deploys, At: r.CompletedAt, Repos: repos})
		}
		return out, nil
	})).WithTitles(func(ctx context.Context) (lineage.ThreadTitles, error) {
		tt, err := titleStore.Load(ctx)
		out := lineage.ThreadTitles{Manual: map[string]lineage.TitleAt{}, Transcript: map[string]lineage.TitleAt{}}
		for k, v := range tt.Manual {
			out.Manual[k] = lineage.TitleAt{Title: v.Title, At: v.At}
		}
		for k, v := range tt.Transcripts {
			out.Transcript[k] = lineage.TitleAt{Title: v.Title, At: v.At}
		}
		return out, err
	}, func(ctx context.Context, session, title string) error {
		return titleStore.SetManual(ctx, session, title, time.Now().UTC())
	}).WithTitleReports(func(ctx context.Context, transcript, title string) error {
		return titleStore.ReportTranscript(ctx, transcript, title, time.Now().UTC())
	})
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
	dataDir := strings.TrimSpace(os.Getenv("PLATFORM_DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(cfg.ConfigDir(), "..", "data")
	}
	srv.approvals = approvals.New(filepath.Join(dataDir, "approvals"), audit)
	srv.approvals.SetCreateCheck(srv.approvalCreateCheck)
	// One open landing grant request per operation (review 694 R-02), one
	// open rule approval per rule and agentd deployment per host.
	srv.approvals.SetOpenKey(approvalOpenKey)
	actions.SetRuleChangeLookup(srv.ruleApproveChanges)
	srv.wireQuestions(dataDir, role.RunsWorkers())
	srv.wireReleasePolicy(releasesSvc, dataDir, role.RunsWorkers())
	srv.wireAgentThreads(dataDir, role, titleStore)
	srv.wireAgentProgress(dataDir, role)
	srv.wireAgentRuntime(role)
	if role.RunsAPI() || role.RunsWorkers() {
		srv.delivery.StartRunWindowRecovery()
	}
	srv.wireSessionBindings()
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
	// A request for an action the signed policy covers is run, not answered
	// "call directly" (covered_request.go).
	r.Use(coveredRequestRuns(r))

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
		// (R5:5.5-12 §1.3): it reaches collectorRoutes and nothing else.
		r.Use(s.auth.ConfineCollectors(collectorAllowed(root)))
		// L-1 surface. Served here for convenience while platform-api is healthy;
		// cmd/operator-plane serves the same routes off-cluster for when it is not.
		s.mountPlane(r)
		approvals.Mount(r, s.auth, s.approvals)
		s.mountQuestions(r)
		s.mountAgentRuntime(r)
		s.mountBus(r)
		// Each domain registers its own routes (mount_<domain>.go).
		s.mountFoundation(r)
		s.mountTenant(r)
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
			defer safego.Recover("server.probeEnvironment")
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
	resp := topology.Build(s.cfg.Topology, *env, matrix, s.liveHosts(r.Context()))
	writeJSON(w, http.StatusOK, resp)
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
