package delivery

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

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
	kaniko, unread := true, ""
	dyn, err := s.buildDynamicClient()
	if err != nil {
		unread = "the pipeline definition was not read: " + err.Error()
	} else if pipe, getErr := dyn.Resource(pipelineGVR).Namespace(s.PipelinesNamespace()).Get(ctx, pipelineName, metav1.GetOptions{}); getErr != nil {
		unread = "the pipeline definition was not read: " + getErr.Error()
	} else {
		kaniko, unread = kanikoOf(pipe.Object, s.clusterTasks(ctx, dyn))
	}
	return s.buildPreflight(ctx, pipelineName, kaniko, unread)
}

// buildPreflight checks the amd64 CI pool for a kaniko pipeline (or one whose
// definition was not read: unread says why, and a refusal names it).
func (s *Service) buildPreflight(ctx context.Context, pipelineName string, kaniko bool, unread string) PipelinePreflightResponse {
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
	reason := placement.CIPreflightReason(s.ciNodeInputs(ctx))
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

func (s *Service) enrichPipelineView(ctx context.Context, name, ns string, pipe map[string]any, getTask taskGetter) PipelineView {
	v := PipelineView{Name: name, Namespace: ns}
	kaniko, unread := kanikoOf(pipe, getTask)
	if !kaniko {
		return v
	}
	pf := s.buildPreflight(ctx, name, kaniko, unread)
	ready := pf.BuildReady
	v.BuildReady = &ready
	if !pf.BuildReady {
		v.BlockReason = pf.Reason
	}
	return v
}
