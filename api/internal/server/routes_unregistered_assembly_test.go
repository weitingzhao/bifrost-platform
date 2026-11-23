package server

// Routes served before 2026-11-22 whose handler is in a assembly package and that
// have no contract row yet (routes_registered_test.go). The list only
// shrinks: a route leaves when its domain registers it.
var _ = heldUnregistered(
	"DELETE /api/v1/cluster/workloads/pods/{namespace}/{name}",
	"GET /api/v1/agentrt/whoami",
	"GET /api/v1/bus/effect",
	"GET /api/v1/bus/nextgen",
	"GET /api/v1/bus/problems",
	"GET /api/v1/delivery/released",
	"GET /api/v1/home/layout",
	"GET /api/v1/readings/evaluation",
	"GET /health",
	"GET /metrics",
	"POST /api/v1/bus/acceptance",
	"POST /api/v1/bus/guard-blocks",
	"POST /api/v1/bus/privilege",
	"POST /api/v1/bus/regressions",
	"POST /api/v1/bus/regressions/fixed",
	"POST /api/v1/cluster/addons/kube-prometheus-stack/ensure",
	"POST /api/v1/cluster/addons/metrics-server/ensure",
	"POST /api/v1/cluster/kubeconfig-secret/ensure",
	"POST /api/v1/cluster/namespaces/ensure-bifrost",
	"POST /api/v1/cluster/nodes/{name}/cordon",
	"POST /api/v1/cluster/nodes/{name}/drain",
	"POST /api/v1/cluster/nodes/{name}/poweroff",
	"POST /api/v1/cluster/nodes/{name}/uncordon",
	"POST /api/v1/cluster/nodes/{name}/wake",
	"POST /api/v1/cluster/postgres/backup",
	"POST /api/v1/cluster/postgres/backups/sweep-failed",
	"POST /api/v1/cluster/postgres/wal-store/repair",
	"POST /api/v1/cluster/sync-kubeconfig",
	"POST /api/v1/cluster/workloads/rollout-restart",
	"POST /api/v1/cluster/workloads/scale",
	"POST /api/v1/network/clients/fixed-ip",
	"PUT /api/v1/home/layout",
)
