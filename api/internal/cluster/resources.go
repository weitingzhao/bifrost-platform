package cluster

import (
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	resourceDegradedPct = 85.0
	resourceFailPct     = 95.0
)

func formatCPU(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	milli := q.MilliValue()
	if milli%1000 == 0 {
		return fmt.Sprintf("%d", milli/1000)
	}
	return fmt.Sprintf("%dm", milli)
}

func formatMemory(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	bytes := q.Value()
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if bytes >= gib {
		gi := float64(bytes) / float64(gib)
		if math.Mod(gi, 1) == 0 {
			return fmt.Sprintf("%dGi", int(gi))
		}
		return fmt.Sprintf("%.1fGi", gi)
	}
	if bytes >= mib {
		mi := float64(bytes) / float64(mib)
		if math.Mod(mi, 1) == 0 {
			return fmt.Sprintf("%dMi", int(mi))
		}
		return fmt.Sprintf("%.1fMi", mi)
	}
	return fmt.Sprintf("%dKi", bytes/1024)
}

func usagePercent(used, capacity resource.Quantity) (float64, bool) {
	if capacity.IsZero() {
		return 0, false
	}
	usedMilli := used.MilliValue()
	capMilli := capacity.MilliValue()
	if capMilli <= 0 {
		return 0, false
	}
	pct := float64(usedMilli) / float64(capMilli) * 100
	return math.Round(pct*10) / 10, true
}

func resourceReachability(pct float64) probe.Reachability {
	if pct >= resourceFailPct {
		return probe.ReachFail
	}
	if pct >= resourceDegradedPct {
		return probe.ReachDegraded
	}
	return probe.ReachOK
}

func nodeAllocatable(n corev1.Node) (cpu, memory, storage resource.Quantity) {
	if n.Status.Allocatable != nil {
		cpu = n.Status.Allocatable[corev1.ResourceCPU]
		memory = n.Status.Allocatable[corev1.ResourceMemory]
		storage = n.Status.Allocatable[corev1.ResourceEphemeralStorage]
	}
	return cpu, memory, storage
}

func sumAllocatable(nodes []corev1.Node, readyOnly bool) (cpu, memory resource.Quantity) {
	cpu = resource.Quantity{}
	memory = resource.Quantity{}
	for _, n := range nodes {
		if readyOnly && !isNodeReady(n) {
			continue
		}
		c, m, _ := nodeAllocatable(n)
		cpu.Add(c)
		memory.Add(m)
	}
	return cpu, memory
}

func isNodeReady(n corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func countPodsByPhase(pods []corev1.Pod) (running, pending int) {
	for _, p := range pods {
		switch p.Status.Phase {
		case corev1.PodRunning, corev1.PodSucceeded:
			running++
		case corev1.PodPending:
			pending++
		}
	}
	return running, pending
}
