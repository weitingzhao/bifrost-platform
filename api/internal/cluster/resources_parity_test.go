package cluster

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"testing"
)

func TestResourceObservationFullOutputParity(t *testing.T) {
	quantities := []string{"0", "1m", "999m", "1", "1.5", "1023", "1Ki", "1Mi", "1.5Gi", "100Gi"}
	for _, raw := range quantities {
		q := resource.MustParse(raw)
		if formatCPU(q) != baselineFormatCPU(q) || formatMemory(q) != baselineFormatMemory(q) {
			t.Fatal(raw)
		}
		for _, cap := range quantities {
			c := resource.MustParse(cap)
			a, ok := usagePercent(q, c)
			b, want := baselineUsagePercent(q, c)
			if a != b || ok != want {
				t.Fatal(raw, cap)
			}
		}
	}
	for _, pct := range []float64{-1, 0, 84.9, 85, 94.9, 95, 100, 200} {
		if resourceReachability(pct) != baselineResourceReachability(pct) {
			t.Fatal(pct)
		}
	}
	nodes := []corev1.Node{{}, {Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")}}},
		{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi"), corev1.ResourceEphemeralStorage: resource.MustParse("4Gi")}}}}
	for _, node := range nodes {
		a, b, c := nodeAllocatable(node)
		x, y, z := baselineNodeAllocatable(node)
		if a.Cmp(x) != 0 || b.Cmp(y) != 0 || c.Cmp(z) != 0 || isNodeReady(node) != baselineIsNodeReady(node) {
			t.Fatal(node)
		}
	}
	for _, readyOnly := range []bool{false, true} {
		a, b := sumAllocatable(nodes, readyOnly)
		x, y := baselineSumAllocatable(nodes, readyOnly)
		if a.Cmp(x) != 0 || b.Cmp(y) != 0 {
			t.Fatal(readyOnly)
		}
	}
	var pods []corev1.Pod
	for _, phase := range []corev1.PodPhase{corev1.PodRunning, corev1.PodPending, corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown, ""} {
		pods = append(pods, corev1.Pod{Status: corev1.PodStatus{Phase: phase}})
		a, b := countPodsByPhase(pods)
		x, y := baselineCountPodsByPhase(pods)
		if a != x || b != y {
			t.Fatal(phase)
		}
	}
}
