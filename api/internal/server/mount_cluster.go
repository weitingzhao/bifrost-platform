package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/elasticreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/appreadings"
)

// mountCluster registers the cluster package's routes (the package is
// recorded as mixed: foundation's Kubernetes adapter with blocks not yet
// ruled). Moved from Router on 2026-11-16, unchanged.
func (s *Server) mountCluster(r chi.Router) {
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
		r.Get("/nodes/{name}/power", s.cluster.HandleNodePower)
		r.Get("/placement", s.cluster.HandlePlacement)
		r.Get("/metrics", s.cluster.HandleMetrics)
		r.Get("/observability", s.cluster.HandleObservability)
		r.Get("/namespaces", s.cluster.HandleNamespaces)
		r.Get("/workloads", s.cluster.HandleWorkloads)
		r.Get("/events", s.cluster.HandleEvents)
		// Pod logs can carry SQL, hostnames and request data: never anonymous
		// (TD-204). In-cluster, RBAC also limits them to the Bifrost namespaces.
		r.With(s.auth.Require(actuation.RoleViewer)).Get("/workloads/pods/{namespace}/{name}/logs", s.cluster.HandlePodLogs)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			// runs the ssh sync script and overwrites the host kubeconfig (TD-220)
			r.Post("/sync-kubeconfig", s.guard("sync_kubeconfig", s.cluster.HandleSyncKubeconfig))
			r.Post("/namespaces/ensure-bifrost", s.guard("ensure_bifrost_namespaces", s.cluster.HandleEnsureBifrost))
			r.Post("/postgres/backup", s.guard("trigger_cnpg_backup", s.cluster.HandleTriggerPostgresBackup))
			r.Post("/postgres/wal-store/repair", s.guard("repair_cnpg_wal_store", s.cluster.HandleRepairPostgresWalStore))
			r.Post("/postgres/backups/sweep-failed", s.guard("sweep_failed_backups", s.cluster.HandleSweepExpiredFailedBackups))
			r.Post("/workloads/rollout-restart", s.guard("rollout_restart_deployment", s.cluster.HandleRolloutRestart))
			r.Post("/workloads/scale", s.guard("scale_deployment", s.cluster.HandleScale))
			r.Post("/nodes/{name}/wake", s.guard("wake_compute_node", s.cluster.HandleWakeNode))
			r.Post("/nodes/{name}/cordon", s.guard("cordon_node", s.cluster.HandleCordonNode))
			r.Post("/nodes/{name}/uncordon", s.guard("uncordon_node", s.cluster.HandleUncordonNode))
			r.Delete("/workloads/pods/{namespace}/{name}", s.guard("delete_pod", s.cluster.HandleDeletePod))
		})
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleAdmin))
			r.Post("/kubeconfig-secret/ensure", s.guard("ensure_kubeconfig_secret", s.cluster.HandleEnsureKubeconfigSecret))
			r.Post("/addons/metrics-server/ensure", s.guard("ensure_metrics_server", s.cluster.HandleEnsureMetricsServer))
			r.Post("/addons/kube-prometheus-stack/ensure", s.guard("ensure_kube_prometheus_stack", s.cluster.HandleEnsureKubePrometheusStack))
			r.Post("/nodes/{name}/drain", s.guard("drain_node", s.cluster.HandleDrainNode))
			r.Post("/nodes/{name}/poweroff", s.guard("poweroff_compute_node", s.cluster.HandlePowerOffNode))
			r.Post("/data-clone", s.guard("trigger_data_clone", s.cluster.HandleDataClone))
			r.Put("/data-clone/schedule", s.guard("update_data_clone_schedule", s.cluster.HandleDataCloneSchedulePut))
		})
	})
}

// mountDevSessions registers compute's dev-session routes. Moved from Router
// on 2026-11-16, unchanged.
func (s *Server) mountDevSessions(r chi.Router) {
	r.Route("/dev-sessions", func(r chi.Router) {
		r.Get("/", s.devSession.HandleList)
		r.Get("/{name}/logs", s.devSession.HandleLogs)
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Require(actuation.RoleOperator))
			r.Post("/{name}/control", s.devSession.HandleControl)
		})
	})
}

// bindClusterReadings binds the readings other domains own to the cluster
// package, which gathers their inputs. Lines of New until 2026-11-16.
func bindClusterReadings(clusterH *cluster.Handler) {
	// The compute domain evaluates the node pools; cluster declares the shape.
	clusterH.Service().BindPools(placement.ClusterPools)
	// The tenant domain reads its applications and data from what cluster gathers.
	cluster.BindAppReadings(cluster.AppReadings{ApplicationsDomain: appreadings.EvalApplicationsDomain, FreshnessDatabases: appreadings.FreshnessDatabases})
	// The compute domain reads the elastic domains (GPU, data warehouse) of service readiness.
	cluster.BindComputeReadings(cluster.ComputeReadings{GPUDomain: elasticreadings.GPUDomain, WarehouseDomain: elasticreadings.WarehouseDomain})
}
