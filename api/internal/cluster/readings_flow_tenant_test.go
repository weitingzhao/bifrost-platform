//bifrost:flow owner=payload contracts=cluster.BindAppReadings

package cluster_test

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/appreadings"
)

// The cluster package's tests read the applications domain of service
// readiness as the server wires it: with the tenant's reading bound
// (server/mount_cluster.go, bindClusterReadings). Until 2026-11-19 an init of
// the internal tests bound it; cluster is foundation's alone since then, so
// the binding is this flow fixture of the tenant's contract. This init runs
// after the package's own and before any test.
func init() {
	cluster.BindAppReadings(cluster.AppReadings{ApplicationsDomain: appreadings.EvalApplicationsDomain})
}
