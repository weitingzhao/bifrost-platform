package cluster

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestFormatCPU(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"4", "4"},
		{"500m", "500m"},
		{"0", "0"},
	}
	for _, tc := range cases {
		q := resource.MustParse(tc.in)
		if got := formatCPU(q); got != tc.want {
			t.Errorf("formatCPU(%s): got %s want %s", tc.in, got, tc.want)
		}
	}
}

func TestFormatMemory(t *testing.T) {
	q := resource.MustParse("16Gi")
	if got := formatMemory(q); got != "16Gi" {
		t.Fatalf("formatMemory: got %s", got)
	}
	// Typical summed node allocatable (~22.8Gi)
	q2 := resource.MustParse("23866632Ki")
	got := formatMemory(q2)
	if got != "22.8Gi" && got != "23Gi" {
		t.Fatalf("formatMemory summed: got %s", got)
	}
}

func TestUsagePercent(t *testing.T) {
	used := resource.MustParse("500m")
	cap := resource.MustParse("1")
	pct, ok := usagePercent(used, cap)
	if !ok {
		t.Fatal("expected ok")
	}
	if pct != 50 {
		t.Fatalf("pct: got %v want 50", pct)
	}
	_, ok = usagePercent(used, resource.Quantity{})
	if ok {
		t.Fatal("zero capacity should not be ok")
	}
}

func TestResourceReachability(t *testing.T) {
	if resourceReachability(50) != probe.ReachOK {
		t.Fatal("50 should be ok")
	}
	if resourceReachability(90) != probe.ReachDegraded {
		t.Fatal("90 should be degraded")
	}
	if resourceReachability(96) != probe.ReachFail {
		t.Fatal("96 should be fail")
	}
}

func TestNodeViewCapacityFields(t *testing.T) {
	n := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("4"),
				corev1.ResourceMemory:           resource.MustParse("16Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("100Gi"),
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.32.0"},
		},
	}
	v := nodeView(n)
	if v.CPUAllocatable != "4" {
		t.Fatalf("cpu alloc: %s", v.CPUAllocatable)
	}
	if v.MemoryAllocatable != "16Gi" {
		t.Fatalf("mem alloc: %s", v.MemoryAllocatable)
	}
	if v.StorageAllocatable != "100Gi" {
		t.Fatalf("storage alloc: %s", v.StorageAllocatable)
	}
}

func TestCountPodsByPhase(t *testing.T) {
	pods := []corev1.Pod{
		{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
		{Status: corev1.PodStatus{Phase: corev1.PodPending}},
	}
	running, pending := countPodsByPhase(pods)
	if running != 2 || pending != 1 {
		t.Fatalf("running=%d pending=%d", running, pending)
	}
}

func TestTopPodsByCPUFilterAndLimit(t *testing.T) {
	pods := []metricsv1beta1.PodMetrics{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "bifrost", Name: "low"},
			Containers: []metricsv1beta1.ContainerMetrics{{
				Usage: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "bifrost", Name: "high"},
			Containers: []metricsv1beta1.ContainerMetrics{{
				Usage: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "skip"},
			Containers: []metricsv1beta1.ContainerMetrics{{
				Usage: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("9")},
			}},
		},
	}
	filter := map[string]bool{"bifrost": true}
	top := topPodsByCPU(pods, filter, 1)
	if len(top) != 1 {
		t.Fatalf("len: %d", len(top))
	}
	if top[0].Name != "high" {
		t.Fatalf("name: %s", top[0].Name)
	}
}
