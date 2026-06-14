package server

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/console"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/topology"
)

type Server struct {
	cfg     *config.Config
	prober  *probe.Prober
	console *console.Handler
	cluster *cluster.Handler
	auth    *actuation.AuthService
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
	return &Server{
		cfg:     cfg,
		prober:  probe.NewProber(),
		console: console.NewHandler(cfg),
		cluster: cluster.NewHandler(cfg, audit),
		auth:    auth,
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
		r.Get("/topology", s.handleTopology)
		r.Get("/context", s.handleContext)
		r.Get("/auth/capabilities", s.auth.Capabilities)
		r.Get("/audit", s.audit.HandleList)
		r.Get("/jobs", s.jobs.HandleList)
		r.Get("/console/hosts", s.console.HandleHosts)
		r.Get("/console/ws", s.console.HandleWebSocket)
		r.Route("/cluster", func(r chi.Router) {
			r.Get("/", s.cluster.HandleSummary)
			r.Get("/nodes", s.cluster.HandleNodes)
			r.Get("/metrics", s.cluster.HandleMetrics)
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
				r.Delete("/workloads/pods/{namespace}/{name}", s.cluster.HandleDeletePod)
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
	writeJSON(w, http.StatusOK, s.cfg.OpsContext)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
