package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type elasticNodeMode string

const (
	elasticModeActive   elasticNodeMode = "active"
	elasticModeStandby  elasticNodeMode = "standby"
	elasticModeDegraded elasticNodeMode = "degraded"
)

type nodeHealthRollup struct {
	CoreReady        int
	CoreTotal        int
	ElasticStandby   int
	ElasticDegraded  int
	RegisteredReady  int
	RegisteredTotal  int
	ElasticNodeModes map[string]elasticNodeMode
}

// rollupNodeHealth counts the core nodes and the elastic ones. Which node is
// elastic (compute-managed) and what mode it is in is compute's answer
// (ComputeNodes, bound by the server); the counts are this package's.
// forDisplay is true for a page that polls (see ComputeNodes.NodeMode).
func (s *Service) rollupNodeHealth(ctx context.Context, clientset kubernetes.Interface, nodes []corev1.Node, forDisplay bool) nodeHealthRollup {
	out := nodeHealthRollup{
		RegisteredTotal:  len(nodes),
		ElasticNodeModes: make(map[string]elasticNodeMode, len(nodes)),
	}
	for _, n := range nodes {
		if NodeReadyStatus(&n) == "Ready" {
			out.RegisteredReady++
		}
		if !s.isComputeManaged(n.Name) {
			out.CoreTotal++
			if NodeReadyStatus(&n) == "Ready" {
				out.CoreReady++
			}
			continue
		}

		mode, _ := s.classifyElasticNode(ctx, clientset, &n, forDisplay)
		out.ElasticNodeModes[n.Name] = mode
		switch mode {
		case elasticModeStandby:
			out.ElasticStandby++
		case elasticModeDegraded:
			out.ElasticDegraded++
			out.CoreTotal++
		case elasticModeActive:
			out.CoreTotal++
			out.CoreReady++
		}
	}
	return out
}

func (s *Service) classifyElasticNode(ctx context.Context, clientset kubernetes.Interface, node *corev1.Node, forDisplay bool) (elasticNodeMode, string) {
	if computeNodes.NodeMode == nil {
		// Degraded rather than standby: standby reads as expected-off and is
		// never a failure, and unbound must not hide a node.
		return elasticModeDegraded, "no reader of elastic node health is bound"
	}
	mode, detail := computeNodes.NodeMode(ctx, s, clientset, node, forDisplay)
	return elasticNodeMode(mode), detail
}

func applyElasticReachability(view NodeView, mode elasticNodeMode) NodeView {
	switch mode {
	case elasticModeStandby:
		// Expected off (powered down / NotReady, no demand) — unknown, never fail.
		view.Reachability = probe.ReachUnknown
		view.ElasticMode = string(elasticModeStandby)
	case elasticModeDegraded:
		view.Reachability = probe.ReachDegraded
		view.ElasticMode = string(elasticModeDegraded)
	case elasticModeActive:
		view.ElasticMode = string(elasticModeActive)
	}
	return view
}

func summaryReachFromNodes(coreReady, coreTotal, elasticDegraded, failingPods int) (probe.Reachability, string) {
	reach := probe.ReachOK
	detail := "cluster API reachable"
	if coreTotal == 0 && elasticDegraded == 0 {
		return probe.ReachFail, "no schedulable nodes"
	}
	if coreReady < coreTotal {
		reach = probe.ReachDegraded
		detail = fmt.Sprintf("%d/%d core nodes ready", coreReady, coreTotal)
	}
	if coreReady == 0 && coreTotal > 0 {
		reach = probe.ReachFail
		detail = "no core nodes ready"
	}
	if elasticDegraded > 0 {
		if reach == probe.ReachOK {
			reach = probe.ReachDegraded
		}
		if detail == "cluster API reachable" {
			detail = fmt.Sprintf("%d elastic node(s) degraded", elasticDegraded)
		} else {
			detail = fmt.Sprintf("%s; %d elastic degraded", detail, elasticDegraded)
		}
	}
	if failingPods > 0 {
		if reach == probe.ReachOK {
			reach = probe.ReachDegraded
		}
		detail = fmt.Sprintf("%s; %d failing pods", detail, failingPods)
	}
	return reach, detail
}

func appendElasticStandbyDetail(detail string, standby int) string {
	if standby <= 0 {
		return detail
	}
	if detail == "" || detail == "cluster API reachable" {
		return fmt.Sprintf("%d elastic standby", standby)
	}
	return fmt.Sprintf("%s; %d elastic standby", detail, standby)
}
