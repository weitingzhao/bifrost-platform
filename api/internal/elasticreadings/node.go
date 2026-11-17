package elasticreadings

import (
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// NodeMode says whether an on-demand compute node is active, standby or
// degraded, and why. A node that is not Ready is standby only when nothing
// is waiting on it and its host is off: pods pending on it, a workload that
// wants replicas it does not have, or a host that answers while the node is
// not Ready each make it degraded. The observations are asked in this order
// and no further than needed.
//
// It was cluster.classifyElasticNode until 2026-11-16 and is moved as it
// was. Two readers act on the answer: the patrol uncordons by it and the
// delivery preflight refuses a build by it; both still read it through the
// cluster package's node list.
func NodeMode(o resourceview.ElasticNodeObservation) (mode, detail string) {
	if o.Ready {
		return resourceview.ElasticNodeActive, "node Ready"
	}
	pending, err := o.PendingPods()
	if err != nil {
		return resourceview.ElasticNodeDegraded, err.Error()
	}
	if pending > 0 {
		return resourceview.ElasticNodeDegraded, fmt.Sprintf("%d pending pod(s) on node", pending)
	}
	if unsatisfied, why := o.WorkloadsUnsatisfied(); unsatisfied {
		return resourceview.ElasticNodeDegraded, why
	}
	if o.HostReachable() {
		return resourceview.ElasticNodeDegraded, "host online but node not Ready"
	}
	return resourceview.ElasticNodeStandby, "on-demand standby (no pending compute workloads)"
}
