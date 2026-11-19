package placement

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/kubeapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// ClusterPools is this package's evaluation in the shape foundation declares
// (kubeapi.PoolEvaluator, which cluster binds): the document of GET /cluster/placement
// and each pool's reading for service readiness. A node list that was not
// read ok is carried into the document, as cluster.Service.Placement did
// before the dependency was turned round (2026-11-16).
func ClusterPools(clusterID string, nodes []kubeapi.PoolNode, reach probe.Reachability, detail string) kubeapi.PoolEvaluation {
	in := make([]NodeInput, len(nodes))
	for i, n := range nodes {
		in[i] = NodeInput{Name: n.Name, Architecture: n.Architecture, WorkloadLabel: n.WorkloadLabel, CapabilityIDs: n.CapabilityIDs, Reachability: n.Reachability}
	}
	resp := Evaluate(clusterID, in)
	if reach != probe.ReachOK {
		resp.Reachability = reach
		resp.Detail = detail
	}
	pools := make([]kubeapi.PoolReading, len(resp.Pools))
	for i, p := range resp.Pools {
		pools[i] = kubeapi.PoolReading{ID: p.ID, Status: string(p.Status), NodesTotal: p.NodesTotal, NodesReady: p.NodesReady}
	}
	return kubeapi.PoolEvaluation{Body: resp, Pools: pools}
}
