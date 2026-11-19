package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// The Kubernetes side of a node, which the compute domain's node power
// (package computenodes) runs through. These were in node_power.go until
// 2026-11-19, when the power and the elastic node health left for
// computenodes; they are this package's: they know nodes, pods and kubectl,
// nothing of compute.

// NodeReadyStatus is the node's Ready condition: "Ready", the condition's
// status when it is not true, or "NotReady" when the node has none.
func NodeReadyStatus(node *corev1.Node) string {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			if c.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return string(c.Status)
		}
	}
	return "NotReady"
}

func isDaemonSetPod(p *corev1.Pod) bool {
	for _, owner := range p.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// NodePodCounts counts the pods scheduled on a node that no DaemonSet owns,
// and of those the pending ones.
func NodePodCounts(ctx context.Context, clientset kubernetes.Interface, nodeName string) (pending int, userPods int, err error) {
	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	list, listErr := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: fieldSelector})
	if listErr != nil {
		return 0, 0, listErr
	}
	for _, p := range list.Items {
		if isDaemonSetPod(&p) {
			continue
		}
		userPods++
		if p.Status.Phase == corev1.PodPending {
			pending++
		}
	}
	return pending, userPods, nil
}

// DrainNodeKubectl drains a node with kubectl, without the checks and the
// response of DrainNode.
func (s *Service) DrainNodeKubectl(ctx context.Context, nodeName string, req DrainNodeRequest) error {
	return s.drainNodeWithOptions(ctx, nodeName, req)
}

// UncordonNodeKubectl uncordons a node with kubectl, without the checks and
// the response of UncordonNode.
func (s *Service) UncordonNodeKubectl(ctx context.Context, nodeName string) error {
	return s.kubectlNode(ctx, "uncordon", nodeName)
}

func (s *Service) kubectlNode(ctx context.Context, subcommand, nodeName string, extraArgs ...string) error {
	kubeconfig := ""
	if s.entry != nil {
		kubeconfig = s.entry.KubeconfigPath()
	}
	args := append([]string{subcommand, nodeName}, extraArgs...)
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if out, err := cmd.Output(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(string(out))
		}
		return fmt.Errorf("kubectl %s: %w (%s)", subcommand, err, msg)
	}
	return nil
}
