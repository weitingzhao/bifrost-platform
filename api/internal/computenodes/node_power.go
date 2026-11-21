package computenodes

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Moved from cluster/node_power.go on 2026-11-19, unchanged in behaviour:
// the Kubernetes calls go through cluster (KubernetesClient, NodeReadyStatus,
// NodePodCounts, DrainNodeKubectl, UncordonNodeKubectl).

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
	ClusterID          string                  `json:"cluster_id"`
	NodeName           string                  `json:"node_name"`
	ComputeManaged     bool                    `json:"compute_managed"`
	NodeStatus         string                  `json:"node_status"`
	PowerState         string                  `json:"power_state"`
	WolMAC             string                  `json:"wol_mac,omitempty"`
	PowerPolicy        string                  `json:"power_policy,omitempty"`
	PowerManagerActive string                  `json:"power_manager_active,omitempty"`
	PendingComputePods int                     `json:"pending_compute_pods"`
	UserPodsOnNode     int                     `json:"user_pods_on_node"`
	Workloads          []ComputeWorkloadStatus `json:"workloads"`
	Reachability       probe.Reachability      `json:"reachability"`
	Detail             string                  `json:"detail"`
	GeneratedAt        time.Time               `json:"generated_at"`
}

// NodePower reads a managed compute node's power, pods and workloads.
func NodePower(ctx context.Context, c *cluster.Service, nodeName string) (NodePowerResponse, error) {
	now := time.Now().UTC()
	spec := computeNodeSpec(c, nodeName)
	if spec == nil {
		return NodePowerResponse{}, fmt.Errorf("node %q is not a managed compute node", nodeName)
	}
	clusterID := c.Entry().ID

	clientset, _, err := c.KubernetesClient()
	if err != nil {
		return NodePowerResponse{
			ClusterID:      clusterID,
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
			ClusterID:      clusterID,
			NodeName:       nodeName,
			ComputeManaged: true,
			Reachability:   probe.ReachFail,
			Detail:         fmt.Sprintf("get node: %v", nodeErr),
			GeneratedAt:    now,
		}, nil
	}

	nodeStatus := cluster.NodeReadyStatus(node)
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

	pending, userPods, pendingErr := cluster.NodePodCounts(ctx, clientset, nodeName)
	workloads, workloadErr := computeWorkloadStatuses(ctx, clientset, spec)

	detailParts := []string{fmt.Sprintf("node %s", nodeStatus)}
	if pendingErr != nil {
		detailParts = append(detailParts, pendingErr.Error())
	}
	if workloadErr != nil {
		detailParts = append(detailParts, workloadErr.Error())
	}

	pmActive := probePowerManager(ctx, c, spec)

	return NodePowerResponse{
		ClusterID:          clusterID,
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

// WakeNode sends a managed compute node a Wake-on-LAN packet over SSH.
func WakeNode(ctx context.Context, c *cluster.Service, nodeName string) (cluster.ActuationResponse, error) {
	spec := computeNodeSpec(c, nodeName)
	if spec == nil {
		return cluster.ActuationResponse{}, fmt.Errorf("node %q is not a managed compute node", nodeName)
	}

	mac := strings.TrimSpace(spec.WolMAC)
	if mac == "" {
		clientset, _, err := c.KubernetesClient()
		if err != nil {
			return cluster.ActuationResponse{}, err
		}
		node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			return cluster.ActuationResponse{}, fmt.Errorf("get node: %w", err)
		}
		mac = strings.TrimSpace(node.Annotations[annoWolMAC])
	}
	if mac == "" {
		return cluster.ActuationResponse{}, fmt.Errorf("no WOL MAC configured for node %q", nodeName)
	}

	host := strings.TrimSpace(spec.WolSSHHost)
	if host == "" && c.Entry() != nil {
		host = strings.TrimSpace(c.Entry().SSHHost)
	}
	if host == "" {
		return cluster.ActuationResponse{}, fmt.Errorf("no wol_ssh_host configured")
	}

	out, err := runSSHCommand(ctx, host, "wakeonlan", mac)
	if err != nil {
		return cluster.ActuationResponse{}, fmt.Errorf("wakeonlan via %s: %w (%s)", host, err, strings.TrimSpace(out))
	}

	now := time.Now().UTC()
	return cluster.ActuationResponse{
		OK:          true,
		Action:      "cluster.node.wake",
		Target:      nodeName,
		Changed:     true,
		Message:     fmt.Sprintf("WOL packet sent to %s via %s", mac, host),
		GeneratedAt: now,
	}, nil
}

// PowerOffNode drains a managed compute node and powers it off over SSH; it
// uncordons the node again when the power off fails.
func PowerOffNode(ctx context.Context, c *cluster.Service, nodeName string) (cluster.ActuationResponse, error) {
	spec := computeNodeSpec(c, nodeName)
	if spec == nil {
		return cluster.ActuationResponse{}, fmt.Errorf("node %q is not a managed compute node", nodeName)
	}

	clientset, _, err := c.KubernetesClient()
	if err != nil {
		return cluster.ActuationResponse{}, err
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return cluster.ActuationResponse{}, fmt.Errorf("get node: %w", err)
	}
	if cluster.NodeReadyStatus(node) != "Ready" {
		now := time.Now().UTC()
		return cluster.ActuationResponse{
			OK:          true,
			Action:      "cluster.node.poweroff",
			Target:      nodeName,
			Changed:     false,
			Message:     fmt.Sprintf("node %q already not Ready; skipping drain", nodeName),
			GeneratedAt: now,
		}, nil
	}

	if err := c.DrainNodeKubectl(ctx, nodeName, cluster.DrainNodeRequest{
		Force:       true,
		GracePeriod: 60,
	}); err != nil {
		return powerOffFail(nodeName, fmt.Errorf("drain node: %w", err))
	}

	if err := sshPowerOff(ctx, spec); err != nil {
		if uncErr := c.UncordonNodeKubectl(ctx, nodeName); uncErr != nil {
			err = fmt.Errorf("%w (uncordon failed: %v)", err, uncErr)
		} else {
			err = fmt.Errorf("%w (node uncordoned — safe to retry after fixing SSH/sudo)", err)
		}
		return powerOffFail(nodeName, err)
	}

	now := time.Now().UTC()
	return cluster.ActuationResponse{
		OK:          true,
		Action:      "cluster.node.poweroff",
		Target:      nodeName,
		Changed:     true,
		Message:     fmt.Sprintf("drained %s and sent poweroff", nodeName),
		GeneratedAt: now,
	}, nil
}

func powerOffFail(nodeName string, err error) (cluster.ActuationResponse, error) {
	return cluster.ActuationResponse{
		Action:      "cluster.node.poweroff",
		Target:      nodeName,
		Message:     err.Error(),
		GeneratedAt: time.Now().UTC(),
	}, err
}

func sshPowerOff(ctx context.Context, spec *config.ComputeNodeSpec) error {
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

func computeWorkloadStatuses(ctx context.Context, clientset kubernetes.Interface, spec *config.ComputeNodeSpec) ([]ComputeWorkloadStatus, error) {
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

func probePowerManager(ctx context.Context, c *cluster.Service, spec *config.ComputeNodeSpec) string {
	unit := strings.TrimSpace(spec.PowerManagerUnit)
	if unit == "" {
		return "unknown"
	}
	host := strings.TrimSpace(spec.WolSSHHost)
	if host == "" && c.Entry() != nil {
		host = strings.TrimSpace(c.Entry().SSHHost)
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
