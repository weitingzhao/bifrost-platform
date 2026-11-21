package delivery

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func (s *Service) ciNodeInputs(ctx context.Context) []NodeInput {
	return nodeInputs(s.cluster.Nodes(ctx))
}

// displayNodeInputs reads the nodes once for a page: the kept reading, so a
// powered-off elastic host does not cost the page an SSH timeout per pipeline.
func (s *Service) displayNodeInputs(ctx context.Context) func() []NodeInput {
	var nodes []NodeInput
	read := false
	return func() []NodeInput {
		if !read {
			nodes, read = nodeInputs(s.cluster.NodesForDisplay(ctx)), true
		}
		return nodes
	}
}

func nodeInputs(nodesResp cluster.NodesResponse) []NodeInput {
	out := make([]NodeInput, len(nodesResp.Nodes))
	for i, n := range nodesResp.Nodes {
		out[i] = NodeInput{
			Name:          n.Name,
			Architecture:  n.Architecture,
			WorkloadLabel: n.WorkloadLabel,
			Reachability:  n.Reachability,
		}
	}
	return out
}

// buildPreflight checks the amd64 CI pool for a kaniko pipeline (or one whose
// definition was not read: unread says why, and a refusal names it).
func (s *Service) buildPreflight(ctx context.Context, pipelineName string, kaniko bool, unread string) PipelinePreflightResponse {
	return s.preflightOn(pipelineName, kaniko, unread, func() []NodeInput { return s.ciNodeInputs(ctx) })
}

// preflightOn is buildPreflight on the nodes it is given; nodes is called only
// for a kaniko pipeline.
func (s *Service) preflightOn(pipelineName string, kaniko bool, unread string, nodes func() []NodeInput) PipelinePreflightResponse {
	now := time.Now().UTC()
	resp := PipelinePreflightResponse{
		ClusterID:    s.clusterID(),
		Pipeline:     pipelineName,
		BuildReady:   true,
		Reachability: probe.ReachOK,
		GeneratedAt:  now,
	}
	if !kaniko {
		return resp
	}
	reason := errPlacementUnbound
	if check := placements().CIPreflightReason; check != nil {
		reason = check(nodes())
	}
	if reason != "" {
		if unread != "" {
			reason += " (checked because " + unread + ")"
		}
		resp.BuildReady = false
		resp.Reason = reason
		resp.Reachability = probe.ReachFail
	}
	return resp
}

// enrichPipelineView badges a listed pipeline; the list only shows, so it reads
// the nodes the page read once (Run checks live before it starts anything).
func (s *Service) enrichPipelineView(name, ns string, pipe map[string]any, getTask taskGetter, nodes func() []NodeInput) PipelineView {
	v := PipelineView{Name: name, Namespace: ns}
	kaniko, unread := kanikoOf(pipe, getTask)
	if !kaniko {
		return v
	}
	pf := s.preflightOn(name, kaniko, unread, nodes)
	ready := pf.BuildReady
	v.BuildReady = &ready
	if !pf.BuildReady {
		v.BlockReason = pf.Reason
	}
	return v
}
