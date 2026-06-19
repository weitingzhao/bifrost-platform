package cluster

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
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

func (s *Service) rollupNodeHealth(ctx context.Context, clientset kubernetes.Interface, nodes []corev1.Node) nodeHealthRollup {
	out := nodeHealthRollup{
		RegisteredTotal:  len(nodes),
		ElasticNodeModes: make(map[string]elasticNodeMode, len(nodes)),
	}
	for _, n := range nodes {
		if nodeReadyStatus(&n) == "Ready" {
			out.RegisteredReady++
		}
		spec := s.computeNodeSpec(n.Name)
		if spec == nil {
			out.CoreTotal++
			if nodeReadyStatus(&n) == "Ready" {
				out.CoreReady++
			}
			continue
		}

		mode, _ := s.classifyElasticNode(ctx, clientset, &n, spec)
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

func (s *Service) classifyElasticNode(
	ctx context.Context,
	clientset kubernetes.Interface,
	node *corev1.Node,
	spec *config.ComputeNodeSpec,
) (elasticNodeMode, string) {
	if nodeReadyStatus(node) == "Ready" {
		return elasticModeActive, "node Ready"
	}

	pendingOnNode, _, pendingErr := s.countNodePods(ctx, clientset, node.Name)
	if pendingErr != nil {
		return elasticModeDegraded, pendingErr.Error()
	}
	if pendingOnNode > 0 {
		return elasticModeDegraded, fmt.Sprintf("%d pending pod(s) on node", pendingOnNode)
	}

	if unsatisfied, detail := s.computeWorkloadsUnsatisfied(ctx, clientset, spec); unsatisfied {
		return elasticModeDegraded, detail
	}

	if probeHostReachableFast(ctx, spec.SSHHost) {
		return elasticModeDegraded, "host online but node not Ready"
	}

	return elasticModeStandby, "on-demand standby (no pending compute workloads)"
}

func (s *Service) computeWorkloadsUnsatisfied(ctx context.Context, clientset kubernetes.Interface, spec *config.ComputeNodeSpec) (bool, string) {
	for _, w := range spec.Workloads {
		ns := strings.TrimSpace(w.Namespace)
		name := strings.TrimSpace(w.Deployment)
		if ns == "" || name == "" {
			continue
		}
		label := strings.TrimSpace(w.Label)
		if label == "" {
			label = name
		}
		dep, err := clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		desired := int32(0)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		if desired > 0 && dep.Status.ReadyReplicas < desired {
			return true, fmt.Sprintf("%s wants %d replica(s), %d ready", label, desired, dep.Status.ReadyReplicas)
		}
	}
	return false, ""
}

func probeHostReachable(ctx context.Context, sshHost string) bool {
	return probeHostReachableWithTimeout(ctx, sshHost, 15)
}

func probeHostReachableFast(ctx context.Context, sshHost string) bool {
	return probeHostReachableWithTimeout(ctx, sshHost, 3)
}

func probeHostReachableWithTimeout(ctx context.Context, sshHost string, timeoutSec int) bool {
	host := strings.TrimSpace(sshHost)
	if host == "" {
		return false
	}
	_, err := runSSHCommandWithTimeout(ctx, host, timeoutSec, "true")
	return err == nil
}

func applyElasticReachability(view NodeView, mode elasticNodeMode) NodeView {
	switch mode {
	case elasticModeStandby:
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
