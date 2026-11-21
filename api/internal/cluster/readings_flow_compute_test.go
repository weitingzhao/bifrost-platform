//bifrost:flow owner=compute contracts=cluster.BindComputeReadings

package cluster_test

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/elasticreadings"
)

// The cluster package's tests read the GPU, data warehouse and general
// workers domains of service readiness as the server wires them: with
// compute's readings bound (server/mount_cluster.go, bindClusterReadings).
// Until 2026-11-19 an init of the internal tests bound them; cluster is
// foundation's alone since then, so the binding is this flow fixture of
// compute's contract. This init runs after the package's own and before any
// test.
func init() {
	cluster.BindComputeReadings(cluster.ComputeReadings{GPUDomain: elasticreadings.GPUDomain, WarehouseDomain: elasticreadings.WarehouseDomain, WorkersDomain: elasticreadings.WorkersDomain})
}
