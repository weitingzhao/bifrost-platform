package computenodes

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/elasticreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// Moved from cluster/elastic_node_health.go on 2026-11-19 (classifyElasticNode,
// computeWorkloadsUnsatisfied and the host probes), unchanged in behaviour.
// cluster keeps the rollup of core and elastic nodes and asks this for each
// managed node's mode.

// NodeMode says whether a managed compute node is active, standby or
// degraded, with why. Whether an on-demand node that is not Ready is standby
// or degraded is elasticreadings.NodeMode's reading; this gathers what it
// reads, each observation only when it is asked for: the host probe is an SSH
// call and must not run for a node that is Ready or already known degraded.
// forDisplay is true for a page that polls: the host's answer is then the
// kept reading, and nothing may act on it.
func (n *Nodes) NodeMode(ctx context.Context, c *cluster.Service, clientset kubernetes.Interface, node *corev1.Node, forDisplay bool) (mode, detail string) {
	spec := computeNodeSpec(c, node.Name)
	if spec == nil {
		// cluster asks only for managed nodes (Managed); degraded rather
		// than standby, so that a node is never hidden.
		return resourceview.ElasticNodeDegraded, fmt.Sprintf("node %q is not a managed compute node", node.Name)
	}
	hostAnswers := n.hostProbe(forDisplay)
	return elasticreadings.NodeMode(resourceview.ElasticNodeObservation{
		Ready: cluster.NodeReadyStatus(node) == "Ready",
		PendingPods: func() (int, error) {
			pending, _, err := cluster.NodePodCounts(ctx, clientset, node.Name)
			return pending, err
		},
		WorkloadsUnsatisfied: func() (bool, string) { return computeWorkloadsUnsatisfied(ctx, clientset, spec) },
		HostReachable:        func() bool { return hostAnswers(ctx, spec.SSHHost) },
	})
}

func computeWorkloadsUnsatisfied(ctx context.Context, clientset kubernetes.Interface, spec *config.ComputeNodeSpec) (bool, string) {
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

// hostProbe is the live probe, or for a page the kept reading in front of it.
func (n *Nodes) hostProbe(forDisplay bool) func(ctx context.Context, host string) bool {
	live := n.liveProbe
	if live == nil {
		live = probeHostReachableFast
	}
	if !forDisplay || n.hosts == nil {
		return live
	}
	return func(ctx context.Context, host string) bool { return n.hosts.Read(ctx, host, live) }
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
