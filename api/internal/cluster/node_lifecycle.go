package cluster

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DrainNodeRequest struct {
	Force        bool `json:"force"`
	DeleteLocal  bool `json:"delete_local_data"`
	GracePeriod  int  `json:"grace_period_seconds"`
}

func (s *Service) CordonNode(ctx context.Context, nodeName string) (ActuationResponse, error) {
	now := time.Now().UTC()
	target := nodeName

	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "cluster.node.cordon", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ActuationResponse{OK: false, Action: "cluster.node.cordon", Target: target, Message: fmt.Sprintf("node %q not found", nodeName), GeneratedAt: now}, err
		}
		return ActuationResponse{OK: false, Action: "cluster.node.cordon", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	if node.Spec.Unschedulable {
		return ActuationResponse{
			OK:          true,
			Action:      "cluster.node.cordon",
			Target:      target,
			Changed:     false,
			Message:     fmt.Sprintf("node %q already cordoned", nodeName),
			GeneratedAt: now,
		}, nil
	}

	node.Spec.Unschedulable = true
	_, err = clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "cluster.node.cordon", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	return ActuationResponse{
		OK:          true,
		Action:      "cluster.node.cordon",
		Target:      target,
		Changed:     true,
		Message:     fmt.Sprintf("node %q cordoned — new pods will not schedule", nodeName),
		GeneratedAt: now,
	}, nil
}

func (s *Service) UncordonNode(ctx context.Context, nodeName string) (ActuationResponse, error) {
	now := time.Now().UTC()
	target := nodeName

	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "cluster.node.uncordon", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ActuationResponse{OK: false, Action: "cluster.node.uncordon", Target: target, Message: fmt.Sprintf("node %q not found", nodeName), GeneratedAt: now}, err
		}
		return ActuationResponse{OK: false, Action: "cluster.node.uncordon", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	if !node.Spec.Unschedulable {
		return ActuationResponse{
			OK:          true,
			Action:      "cluster.node.uncordon",
			Target:      target,
			Changed:     false,
			Message:     fmt.Sprintf("node %q already schedulable", nodeName),
			GeneratedAt: now,
		}, nil
	}

	node.Spec.Unschedulable = false
	_, err = clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "cluster.node.uncordon", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	return ActuationResponse{
		OK:          true,
		Action:      "cluster.node.uncordon",
		Target:      target,
		Changed:     true,
		Message:     fmt.Sprintf("node %q uncordoned — scheduling re-enabled", nodeName),
		GeneratedAt: now,
	}, nil
}

func (s *Service) DrainNode(ctx context.Context, nodeName string, req DrainNodeRequest) (ActuationResponse, error) {
	now := time.Now().UTC()
	target := nodeName

	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "cluster.node.drain", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ActuationResponse{OK: false, Action: "cluster.node.drain", Target: target, Message: fmt.Sprintf("node %q not found", nodeName), GeneratedAt: now}, err
		}
		return ActuationResponse{OK: false, Action: "cluster.node.drain", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	if nodeReadyStatus(node) != "Ready" && !req.Force {
		return ActuationResponse{
			OK:          false,
			Action:      "cluster.node.drain",
			Target:      target,
			Message:     fmt.Sprintf("node %q is not Ready — pass force in body to drain anyway", nodeName),
			GeneratedAt: now,
		}, fmt.Errorf("node not Ready")
	}

	if err := s.drainNodeWithOptions(ctx, nodeName, req); err != nil {
		return ActuationResponse{OK: false, Action: "cluster.node.drain", Target: target, Message: err.Error(), GeneratedAt: now}, err
	}

	return ActuationResponse{
		OK:          true,
		Action:      "cluster.node.drain",
		Target:      target,
		Changed:     true,
		Message:     fmt.Sprintf("drained node %q — workloads evicted", nodeName),
		GeneratedAt: now,
	}, nil
}

func (s *Service) drainNodeWithOptions(ctx context.Context, nodeName string, req DrainNodeRequest) error {
	args := []string{"--ignore-daemonsets", "--delete-emptydir-data"}
	if req.DeleteLocal {
		args = append(args, "--delete-local-data")
	}
	if req.Force {
		args = append(args, "--force")
	}
	grace := req.GracePeriod
	if grace <= 0 {
		grace = 60
	}
	args = append(args, fmt.Sprintf("--grace-period=%d", grace))
	return s.kubectlNode(ctx, "drain", nodeName, args...)
}
