// The read-only tenant/deployment display mapping (was package tenanttopology,
// folded in 2026-11-20: topology was its only user).

package topology

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/topologyview"
)

// tenantAdapter supplies no target-to-node mapping: which machine an API runs on is
// a pod's spec.nodeName, and this projection reads no pods. A hand-written map
// (api-* on mini-pc-a, or on macbook and mac-mini-1 in dev) used to stand in
// for it; targets now attach to nodes only through edges and the node's own
// matrix_targets in topology.yaml, and are otherwise not placed (unknown).
func tenantAdapter() topologyview.Adapter {
	return topologyview.Adapter{EdgeDetail: edgeDetail}
}

func edgeDetail(kind string) (string, bool) {
	if kind == "ib" {
		return "IB/TWS external — not probed by platform (by design)", true
	}
	return "", false
}
