package delivery

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Kaniko pipelines must schedule TaskRuns on amd64 (see workload placement G2).
var kanikoPipelineNames = map[string]bool{
	"bifrost-deliver-stg":          true,
	"bifrost-build-stg":            true,
	"bifrost-build-frontend-stg": true,
}

func isKanikoPipeline(name string) bool {
	return kanikoPipelineNames[name]
}

func (s *Service) ciNodeInputs(ctx context.Context) []placement.NodeInput {
	nodesResp := s.cluster.Nodes(ctx)
	out := make([]placement.NodeInput, len(nodesResp.Nodes))
	for i, n := range nodesResp.Nodes {
		out[i] = placement.NodeInput{
			Name:          n.Name,
			Architecture:  n.Architecture,
			WorkloadLabel: n.WorkloadLabel,
			Reachability:  n.Reachability,
		}
	}
	return out
}

func (s *Service) PipelinePreflight(ctx context.Context, pipelineName string) PipelinePreflightResponse {
	now := time.Now().UTC()
	resp := PipelinePreflightResponse{
		ClusterID:   s.clusterID(),
		Pipeline:    pipelineName,
		BuildReady:  true,
		Reachability: probe.ReachOK,
		GeneratedAt: now,
	}
	if !isKanikoPipeline(pipelineName) {
		return resp
	}
	reason := placement.CIPreflightReason(s.ciNodeInputs(ctx))
	if reason != "" {
		resp.BuildReady = false
		resp.Reason = reason
		resp.Reachability = probe.ReachFail
	}
	return resp
}

func (s *Service) enrichPipelineView(ctx context.Context, name, ns string) PipelineView {
	v := PipelineView{Name: name, Namespace: ns}
	if !isKanikoPipeline(name) {
		return v
	}
	pf := s.PipelinePreflight(ctx, name)
	ready := pf.BuildReady
	v.BuildReady = &ready
	if !pf.BuildReady {
		v.BlockReason = pf.Reason
	}
	return v
}
