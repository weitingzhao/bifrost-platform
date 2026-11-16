package cluster

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

func formatCPU(q resource.Quantity) string {
	return resourceview.FormatCPU(q)
}

func formatMemory(q resource.Quantity) string {
	return resourceview.FormatMemory(q)
}

func usagePercent(used, capacity resource.Quantity) (float64, bool) {
	return resourceview.UsagePercent(used, capacity)
}

func resourceReachability(pct float64) probe.Reachability {
	return resourceview.ResourceReachability(pct)
}

func nodeAllocatable(n corev1.Node) (cpu, memory, storage resource.Quantity) {
	return resourceview.NodeAllocatable(n)
}

func sumAllocatable(nodes []corev1.Node, readyOnly bool) (cpu, memory resource.Quantity) {
	return resourceview.SumAllocatable(nodes, readyOnly)
}

func isNodeReady(n corev1.Node) bool {
	return resourceview.IsNodeReady(n)
}

func countPodsByPhase(pods []corev1.Pod) (running, pending int) {
	return resourceview.CountPodsByPhase(pods)
}
