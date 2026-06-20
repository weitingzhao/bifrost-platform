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

func (s *Service) Placement(ctx context.Context) placement.Response {
	nodesResp := s.Nodes(ctx)
	resp := placement.Evaluate(nodesResp.ClusterID, nodeInputsFromViews(nodesResp.Nodes))
	if nodesResp.Reachability != probe.ReachOK {
		resp.Reachability = nodesResp.Reachability
		resp.Detail = nodesResp.Detail
	}
	return resp
}
