package cluster

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func nodeInputsFromViews(views []NodeView) []placement.NodeInput {
	out := make([]placement.NodeInput, len(views))
	for i, n := range views {
		out[i] = placement.NodeInput{
			Name:          n.Name,
			Architecture:  n.Architecture,
			WorkloadLabel: n.WorkloadLabel,
			Reachability:  n.Reachability,
		}
	}
	return out
}

func (s *Service) Placement(ctx context.Context) placement.Response {
	nodesResp := s.Nodes(ctx)
	resp := placement.Evaluate(nodesResp.ClusterID, nodeInputsFromViews(nodesResp.Nodes))
	if nodesResp.Reachability != probe.ReachOK {
		resp.Reachability = nodesResp.Reachability
		resp.Detail = nodesResp.Detail
	}
	return resp
}
