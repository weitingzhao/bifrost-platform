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
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
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

func (s *Service) rollupNodeHealth(ctx context.Context, clientset kubernetes.Interface, nodes []corev1.Node, hostAnswers hostProbe) nodeHealthRollup {
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

		mode, _ := s.classifyElasticNode(ctx, clientset, &n, spec, hostAnswers)
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
	hostAnswers hostProbe,
) (elasticNodeMode, string) {
	// Whether an on-demand node that is not Ready is standby or degraded is
	// the compute domain's reading (elasticreadings.NodeMode). This package
	// gathers what it reads, each observation only when it is asked for: the
	// host probe is an SSH call and must not run for a node that is Ready or
	// already known degraded.
	if computeReadings.NodeMode == nil {
		// Degraded rather than standby: standby reads as expected-off and is
		// never a failure, and unbound must not hide a node.
		return elasticModeDegraded, "no reader of elastic node health is bound"
	}
	mode, detail := computeReadings.NodeMode(resourceview.ElasticNodeObservation{
		Ready: nodeReadyStatus(node) == "Ready",
		PendingPods: func() (int, error) {
			pending, _, err := s.countNodePods(ctx, clientset, node.Name)
			return pending, err
		},
		WorkloadsUnsatisfied: func() (bool, string) { return s.computeWorkloadsUnsatisfied(ctx, clientset, spec) },
		HostReachable:        func() bool { return hostAnswers(ctx, spec.SSHHost) },
	})
	return elasticNodeMode(mode), detail
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

// hostProbe says whether a host answers.
type hostProbe func(ctx context.Context, host string) bool

func (s *Service) liveHostProbe() hostProbe {
	if s.hostProbeOverride != nil {
		return s.hostProbeOverride
	}
	return probeHostReachableFast
}

// displayedHostProbe is the probe behind what a page shows: the compute
// domain's kept reading when one is bound, otherwise the live probe.
func (s *Service) displayedHostProbe() hostProbe {
	live := s.liveHostProbe()
	read := computeReadings.DisplayedHost
	if read == nil {
		return live
	}
	return func(ctx context.Context, host string) bool { return read(ctx, host, live) }
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
