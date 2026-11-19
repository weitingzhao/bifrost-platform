package cluster

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Moved from node_power_test.go on 2026-11-19 with the helpers they test
// (node_k8s.go); the power tests went to package computenodes.

func TestNodeReadyStatus(t *testing.T) {
	node := &corev1.Node{
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}
	if got := NodeReadyStatus(node); got != "False" {
		t.Fatalf("got %q want False", got)
	}
}

func TestIsDaemonSetPod(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "cni"}},
		},
	}
	if !isDaemonSetPod(p) {
		t.Fatal("expected daemonset pod")
	}
}
