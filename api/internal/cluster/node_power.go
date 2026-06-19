package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

const (
	annoWolMAC      = "bifrost.io/wol-mac"
	annoPowerPolicy = "bifrost.io/power-policy"
)

// ComputeWorkloadStatus — replica summary for a managed compute deployment.
type ComputeWorkloadStatus struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	Label         string `json:"label"`
	Replicas      int32  `json:"replicas"`
	ReadyReplicas int32  `json:"ready_replicas"`
}

// NodePowerResponse — L0 read for on-demand compute node power + workloads.
type NodePowerResponse struct {
	ClusterID           string                  `json:"cluster_id"`
	NodeName            string                  `json:"node_name"`
	ComputeManaged      bool                    `json:"compute_managed"`
	NodeStatus          string                  `json:"node_status"`
	PowerState          string                  `json:"power_state"`
	WolMAC              string                  `json:"wol_mac,omitempty"`
	PowerPolicy         string                  `json:"power_policy,omitempty"`
	PowerManagerActive  string                  `json:"power_manager_active,omitempty"`
	PendingComputePods  int                     `json:"pending_compute_pods"`
	UserPodsOnNode      int                     `json:"user_pods_on_node"`
	Workloads           []ComputeWorkloadStatus `json:"workloads"`
	Reachability        probe.Reachability      `json:"reachability"`
	Detail              string                  `json:"detail"`
	GeneratedAt         time.Time               `json:"generated_at"`
}

func (s *Service) computeNodeSpec(name string) *config.ComputeNodeSpec {
	if s == nil || s.entry == nil {
		return nil
	}
	return s.entry.ComputeNode(name)
}

func (s *Service) isComputeManaged(name string) bool {
	return s.computeNodeSpec(name) != nil
}

func (s *Service) NodePower(ctx context.Context, nodeName string) (NodePowerResponse, error) {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	spec := s.computeNodeSpec(nodeName)
	if spec == nil {
		return NodePowerResponse{}, fmt.Errorf("node %q is not a managed compute node", nodeName)
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		return NodePowerResponse{
			ClusterID:      base.ClusterID,
			NodeName:       nodeName,
			ComputeManaged: true,
			Reachability:   probe.ReachFail,
			Detail:         err.Error(),
			GeneratedAt:    now,
		}, nil
	}

	node, nodeErr := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if nodeErr != nil {
		return NodePowerResponse{
			ClusterID:      base.ClusterID,
			NodeName:       nodeName,
			ComputeManaged: true,
			Reachability:   probe.ReachFail,
			Detail:         fmt.Sprintf("get node: %v", nodeErr),
			GeneratedAt:    now,
		}, nil
	}

	nodeStatus := nodeReadyStatus(node)
	powerState := "offline"
	reach := probe.ReachFail
	if nodeStatus == "Ready" {
		powerState = "online"
		reach = probe.ReachOK
	}

	wolMAC := strings.TrimSpace(spec.WolMAC)
	if wolMAC == "" {
		wolMAC = strings.TrimSpace(node.Annotations[annoWolMAC])
	}
	powerPolicy := strings.TrimSpace(node.Annotations[annoPowerPolicy])
	if powerPolicy == "" {
		powerPolicy = "on-demand"
	}

	pending, userPods, pendingErr := s.countNodePods(ctx, clientset, nodeName)
	workloads, workloadErr := s.computeWorkloadStatuses(ctx, clientset, spec)

	detailParts := []string{fmt.Sprintf("node %s", nodeStatus)}
	if pendingErr != nil {
		detailParts = append(detailParts, pendingErr.Error())
	}
	if workloadErr != nil {
		detailParts = append(detailParts, workloadErr.Error())
	}

	pmActive := s.probePowerManager(ctx, spec)

	return NodePowerResponse{
		ClusterID:          base.ClusterID,
		NodeName:           nodeName,
		ComputeManaged:     true,
		NodeStatus:         nodeStatus,
		PowerState:         powerState,
		WolMAC:             wolMAC,
		PowerPolicy:        powerPolicy,
		PowerManagerActive: pmActive,
		PendingComputePods: pending,
		UserPodsOnNode:     userPods,
		Workloads:          workloads,
		Reachability:       reach,
		Detail:             strings.Join(detailParts, "; "),
		GeneratedAt:        now,
	}, nil
}

func (s *Service) WakeNode(ctx context.Context, nodeName string) (ActuationResponse, error) {
	spec := s.computeNodeSpec(nodeName)
	if spec == nil {
		return ActuationResponse{}, fmt.Errorf("node %q is not a managed compute node", nodeName)
	}

	mac := strings.TrimSpace(spec.WolMAC)
	if mac == "" {
		clientset, _, err := s.buildClient()
		if err != nil {
			return ActuationResponse{}, err
		}
		node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return ActuationResponse{}, fmt.Errorf("get node: %w", err)
		}
		mac = strings.TrimSpace(node.Annotations[annoWolMAC])
	}
	if mac == "" {
		return ActuationResponse{}, fmt.Errorf("no WOL MAC configured for node %q", nodeName)
	}

	host := strings.TrimSpace(spec.WolSSHHost)
	if host == "" && s.entry != nil {
		host = strings.TrimSpace(s.entry.SSHHost)
	}
	if host == "" {
		return ActuationResponse{}, fmt.Errorf("no wol_ssh_host configured")
	}

	out, err := runSSHCommand(ctx, host, "wakeonlan", mac)
	if err != nil {
		return ActuationResponse{}, fmt.Errorf("wakeonlan via %s: %w (%s)", host, err, strings.TrimSpace(out))
	}

	now := time.Now().UTC()
	return ActuationResponse{
		OK:          true,
		Action:      "cluster.node.wake",
		Target:      nodeName,
		Changed:     true,
		Message:     fmt.Sprintf("WOL packet sent to %s via %s", mac, host),
		GeneratedAt: now,
	}, nil
}

func (s *Service) PowerOffNode(ctx context.Context, nodeName string) (ActuationResponse, error) {
	spec := s.computeNodeSpec(nodeName)
	if spec == nil {
		return ActuationResponse{}, fmt.Errorf("node %q is not a managed compute node", nodeName)
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{}, err
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return ActuationResponse{}, fmt.Errorf("get node: %w", err)
	}
	if nodeReadyStatus(node) != "Ready" {
		now := time.Now().UTC()
		return ActuationResponse{
			OK:          true,
			Action:      "cluster.node.poweroff",
			Target:      nodeName,
			Changed:     false,
			Message:     fmt.Sprintf("node %q already not Ready; skipping drain", nodeName),
			GeneratedAt: now,
		}, nil
	}

	if err := s.drainNode(ctx, nodeName); err != nil {
		return powerOffFail(nodeName, fmt.Errorf("drain node: %w", err))
	}

	if err := s.sshPowerOff(ctx, spec); err != nil {
		if uncErr := s.uncordonNode(ctx, nodeName); uncErr != nil {
			err = fmt.Errorf("%w (uncordon failed: %v)", err, uncErr)
		} else {
			err = fmt.Errorf("%w (node uncordoned — safe to retry after fixing SSH/sudo)", err)
		}
		return powerOffFail(nodeName, err)
	}

	now := time.Now().UTC()
	return ActuationResponse{
		OK:          true,
		Action:      "cluster.node.poweroff",
		Target:      nodeName,
		Changed:     true,
		Message:     fmt.Sprintf("drained %s and sent poweroff", nodeName),
		GeneratedAt: now,
	}, nil
}

func powerOffFail(nodeName string, err error) (ActuationResponse, error) {
	return ActuationResponse{
		Action:      "cluster.node.poweroff",
		Target:      nodeName,
		Message:     err.Error(),
		GeneratedAt: time.Now().UTC(),
	}, err
}

func (s *Service) sshPowerOff(ctx context.Context, spec *config.ComputeNodeSpec) error {
	gpuHost := strings.TrimSpace(spec.SSHHost)
	if gpuHost == "" {
		return fmt.Errorf("no ssh_host configured for node %q", spec.Name)
	}

	out, err := runSSHCommand(ctx, gpuHost, "sudo", "-n", "systemctl", "poweroff")
	if err == nil {
		return nil
	}
	directErr := fmt.Errorf("direct %s: %w (%s)", gpuHost, err, strings.TrimSpace(out))

	via := strings.TrimSpace(spec.WolSSHHost)
	if via == "" || via == gpuHost {
		return fmt.Errorf("%w — configure NOPASSWD on gpu-server: vision ALL=(ALL) NOPASSWD: /usr/bin/systemctl poweroff", directErr)
	}

	remote := fmt.Sprintf(
		"ssh -o BatchMode=yes -o ConnectTimeout=15 -o StrictHostKeyChecking=accept-new %s 'sudo -n systemctl poweroff'",
		shellQuoteSSHHost(gpuHost),
	)
	out2, err2 := runSSHCommand(ctx, via, "bash", "-lc", remote)
	if err2 != nil {
		return fmt.Errorf(
			"%w; via %s: %w (%s) — configure NOPASSWD on gpu-server or bootstrap→gpu SSH",
			directErr, via, err2, strings.TrimSpace(out2),
		)
	}
	return nil
}

func shellQuoteSSHHost(host string) string {
	return "'" + strings.ReplaceAll(host, "'", `'\"'\"'`) + "'"
}

func (s *Service) drainNode(ctx context.Context, nodeName string) error {
	return s.drainNodeWithOptions(ctx, nodeName, DrainNodeRequest{
		DeleteLocal: true,
		Force:       true,
		GracePeriod: 60,
	})
}

func (s *Service) uncordonNode(ctx context.Context, nodeName string) error {
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

func (s *Service) countNodePods(ctx context.Context, clientset kubernetes.Interface, nodeName string) (pending int, userPods int, err error) {
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

func (s *Service) computeWorkloadStatuses(ctx context.Context, clientset kubernetes.Interface, spec *config.ComputeNodeSpec) ([]ComputeWorkloadStatus, error) {
	out := make([]ComputeWorkloadStatus, 0, len(spec.Workloads))
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
		status := ComputeWorkloadStatus{
			Namespace: ns,
			Name:      name,
			Label:     label,
		}
		dep, err := clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			status.Replicas = 0
			status.ReadyReplicas = 0
		} else {
			if dep.Spec.Replicas != nil {
				status.Replicas = *dep.Spec.Replicas
			}
			status.ReadyReplicas = dep.Status.ReadyReplicas
		}
		out = append(out, status)
	}
	return out, nil
}

func (s *Service) probePowerManager(ctx context.Context, spec *config.ComputeNodeSpec) string {
	unit := strings.TrimSpace(spec.PowerManagerUnit)
	if unit == "" {
		return "unknown"
	}
	host := strings.TrimSpace(spec.WolSSHHost)
	if host == "" && s.entry != nil {
		host = strings.TrimSpace(s.entry.SSHHost)
	}
	if host == "" {
		return "unknown"
	}
	out, err := runSSHCommand(ctx, host, "systemctl", "is-active", unit)
	state := strings.TrimSpace(out)
	if err != nil {
		if state == "inactive" || state == "failed" {
			return state
		}
		return "unknown"
	}
	return state
}

func nodeReadyStatus(node *corev1.Node) string {
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

func runSSHCommand(ctx context.Context, host string, remoteCmd ...string) (string, error) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
		"-o", "StrictHostKeyChecking=accept-new",
		host,
	}
	args = append(args, remoteCmd...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if stderr.Len() > 0 {
		if out != "" {
			out += "\n"
		}
		out += stderr.String()
	}
	return out, err
}
