package cluster

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The node pools belong to the compute domain: which pools there are, which
// node is in which, and when a pool is degraded. The cluster package reads
// the nodes and needs two things back: the document GET /cluster/placement
// answers, and each pool's count and status for service readiness. It
// declares that shape here and the server binds compute's evaluator
// (placement.ClusterPools). Until 2026-11-16 this file imported placement
// and called it, which made the base depend on a domain above it.

// PoolNode is a node as a pool evaluator reads it.
type PoolNode struct {
	Name          string
	Architecture  string
	WorkloadLabel string
	CapabilityIDs []string
	Reachability  probe.Reachability
}

// The pool statuses service readiness tells apart; any other is live.
const (
	PoolPlanned  = "planned"
	PoolDegraded = "degraded"
)

// PoolReading is one pool as service readiness reads it.
type PoolReading struct {
	ID         string
	Status     string
	NodesTotal int
	NodesReady int
}

// PoolEvaluation is an evaluator's answer: Body is the document of
// GET /cluster/placement, as the evaluator writes it; Pools is what service
// readiness reads.
type PoolEvaluation struct {
	Body  any
	Pools []PoolReading
}

// PoolEvaluator evaluates the pools of a cluster's nodes. reach and detail
// are how the node list itself was read; an evaluator carries a reading that
// is not ok into its document.
type PoolEvaluator func(clusterID string, nodes []PoolNode, reach probe.Reachability, detail string) PoolEvaluation

// BindPools sets the evaluator. Without one, placement answers unknown and
// readiness finds no pool.
func (s *Service) BindPools(e PoolEvaluator) { s.pools = e }

func poolNodesFromViews(views []NodeView) []PoolNode {
	out := make([]PoolNode, len(views))
	for i, n := range views {
		out[i] = PoolNode{
			Name:          n.Name,
			Architecture:  n.Architecture,
			WorkloadLabel: n.WorkloadLabel,
			CapabilityIDs: capabilityIDsFromView(n),
			Reachability:  n.Reachability,
		}
	}
	return out
}

func capabilityIDsFromView(n NodeView) []string {
	if len(n.Capabilities) == 0 {
		return nil
	}
	ids := make([]string, len(n.Capabilities))
	for i, c := range n.Capabilities {
		ids[i] = c.ID
	}
	return ids
}

func (s *Service) Placement(ctx context.Context) PoolEvaluation {
	nodesResp := s.Nodes(ctx)
	if s.pools == nil {
		return PoolEvaluation{Body: map[string]any{"cluster_id": nodesResp.ClusterID, "reachability": probe.ReachUnknown,
			"detail": "no pool evaluator is bound", "pools": []any{}, "rules": []any{}, "violations": []any{}}}
	}
	return s.pools(nodesResp.ClusterID, poolNodesFromViews(nodesResp.Nodes), nodesResp.Reachability, nodesResp.Detail)
}
