package placement

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// ClusterPools is this package's evaluation in the shape the cluster package
// declares (cluster.PoolEvaluator): the document of GET /cluster/placement
// and each pool's reading for service readiness. A node list that was not
// read ok is carried into the document, as cluster.Service.Placement did
// before the dependency was turned round (2026-11-16).
func ClusterPools(clusterID string, nodes []cluster.PoolNode, reach probe.Reachability, detail string) cluster.PoolEvaluation {
	in := make([]NodeInput, len(nodes))
	for i, n := range nodes {
		in[i] = NodeInput{Name: n.Name, Architecture: n.Architecture, WorkloadLabel: n.WorkloadLabel, CapabilityIDs: n.CapabilityIDs, Reachability: n.Reachability}
	}
	resp := Evaluate(clusterID, in)
	if reach != probe.ReachOK {
		resp.Reachability = reach
		resp.Detail = detail
	}
	pools := make([]cluster.PoolReading, len(resp.Pools))
	for i, p := range resp.Pools {
		pools[i] = cluster.PoolReading{ID: p.ID, Status: string(p.Status), NodesTotal: p.NodesTotal, NodesReady: p.NodesReady}
	}
	return cluster.PoolEvaluation{Body: resp, Pools: pools}
}
