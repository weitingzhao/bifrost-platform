package resourceview

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sort"
	"time"
)

type PodMetricView struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CPU       string `json:"cpu"`
	Memory    string `json:"memory"`
}
type EventView struct {
	Namespace string    `json:"namespace"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Object    string    `json:"object"`
	Message   string    `json:"message"`
	Count     int32     `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

func TopPodsByCPU(pods []metricsv1beta1.PodMetrics, nsFilter map[string]bool, limit int) []PodMetricView {
	type ranked struct {
		view PodMetricView
		cpu  int64
	}
	rankedPods := make([]ranked, 0, len(pods))
	for _, pm := range pods {
		if nsFilter != nil && !nsFilter[pm.Namespace] {
			continue
		}
		if len(pm.Containers) == 0 {
			continue
		}
		cpu := int64(0)
		for i := 0; i < len(pm.Containers); i++ {
			cpu += pm.Containers[i].Usage.Cpu().MilliValue()
		}
		var totalCPU resource.Quantity
		for _, c := range pm.Containers {
			totalCPU.Add(c.Usage[corev1.ResourceCPU])
		}
		var totalMem resource.Quantity
		for _, c := range pm.Containers {
			totalMem.Add(c.Usage[corev1.ResourceMemory])
		}
		cpuStr := FormatCPU(totalCPU)
		memStr := FormatMemory(totalMem)
		rankedPods = append(rankedPods, ranked{
			cpu: cpu,
			view: PodMetricView{
				Namespace: pm.Namespace,
				Name:      pm.Name,
				CPU:       cpuStr,
				Memory:    memStr,
			},
		})
	}
	sort.Slice(rankedPods, func(i, j int) bool {
		return rankedPods[i].cpu > rankedPods[j].cpu
	})
	if len(rankedPods) > limit {
		rankedPods = rankedPods[:limit]
	}
	out := make([]PodMetricView, len(rankedPods))
	for i, r := range rankedPods {
		out[i] = r.view
	}
	return out
}

func Event(e corev1.Event) EventView {
	obj := e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name
	first := e.FirstTimestamp.Time
	last := e.LastTimestamp.Time
	if first.IsZero() && !e.EventTime.IsZero() {
		first = e.EventTime.Time
	}
	if last.IsZero() && !e.EventTime.IsZero() {
		last = e.EventTime.Time
	}
	return EventView{
		Namespace: e.Namespace,
		Type:      e.Type,
		Reason:    e.Reason,
		Object:    obj,
		Message:   e.Message,
		Count:     e.Count,
		FirstSeen: first,
		LastSeen:  last,
	}
}
