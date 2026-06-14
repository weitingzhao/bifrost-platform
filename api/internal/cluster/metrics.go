package cluster

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func (s *Service) buildMetricsClient() (metricsclientset.Interface, error) {
	path := s.kubeconfigPath()
	if path == "" {
		return nil, fmt.Errorf("kubeconfig not configured")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, err
	}
	return metricsclientset.NewForConfig(cfg)
}

func (s *Service) Metrics(ctx context.Context, topLimit int) MetricsResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	if topLimit <= 0 {
		topLimit = 8
	}
	if topLimit > 50 {
		topLimit = 50
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		return failMetrics(base, err, now)
	}

	metricsClient, metricsErr := s.buildMetricsClient()
	if metricsErr != nil {
		return MetricsResponse{
			ClusterID:              base.ClusterID,
			Reachability:           probe.ReachDegraded,
			Detail:                 fmt.Sprintf("metrics client: %v", metricsErr),
			MetricsServerAvailable: false,
			MetricsServerDetail:    metricsErr.Error(),
			TopPods:                []PodMetricView{},
			GeneratedAt:            now,
		}
	}

	nodeMetrics, nodeMetricsErr := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if nodeMetricsErr != nil {
		return MetricsResponse{
			ClusterID:              base.ClusterID,
			Reachability:           probe.ReachDegraded,
			Detail:                 fmt.Sprintf("metrics API: %v", nodeMetricsErr),
			MetricsServerAvailable: false,
			MetricsServerDetail:    "metrics API not available (install metrics-server)",
			TopPods:                []PodMetricView{},
			GeneratedAt:            now,
		}
	}

	nodes, listErr := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if listErr != nil {
		return MetricsResponse{
			ClusterID:              base.ClusterID,
			Reachability:           probe.ReachFail,
			Detail:                 fmt.Sprintf("list nodes: %v", listErr),
			MetricsServerAvailable: true,
			TopPods:                []PodMetricView{},
			GeneratedAt:            now,
		}
	}

	nodeByName := make(map[string]corev1.Node, len(nodes.Items))
	for _, n := range nodes.Items {
		nodeByName[n.Name] = n
	}

	var cpuUsedMilli, cpuCapMilli int64
	var memUsed, memCap int64
	for _, nm := range nodeMetrics.Items {
		n, ok := nodeByName[nm.Name]
		if !ok || !isNodeReady(n) {
			continue
		}
		cpuAlloc, memAlloc, _ := nodeAllocatable(n)
		cpuUsage := nm.Usage[corev1.ResourceCPU]
		memUsage := nm.Usage[corev1.ResourceMemory]
		cpuUsedMilli += cpuUsage.MilliValue()
		cpuCapMilli += cpuAlloc.MilliValue()
		memUsed += memUsage.Value()
		memCap += memAlloc.Value()
	}

	resp := MetricsResponse{
		ClusterID:              base.ClusterID,
		Reachability:           probe.ReachOK,
		Detail:                 "metrics API reachable",
		MetricsServerAvailable: true,
		MetricsServerDetail:    "ok",
		TopPods:                []PodMetricView{},
		GeneratedAt:            now,
	}

	if cpuPct, ok := usagePercent(
		*resource.NewMilliQuantity(cpuUsedMilli, resource.DecimalSI),
		*resource.NewMilliQuantity(cpuCapMilli, resource.DecimalSI),
	); ok {
		resp.CPUUsagePercent = &cpuPct
		resp.CPUReachability = resourceReachability(cpuPct)
	}
	if memPct, ok := usagePercent(
		*resource.NewQuantity(memUsed, resource.BinarySI),
		*resource.NewQuantity(memCap, resource.BinarySI),
	); ok {
		resp.MemoryUsagePercent = &memPct
		resp.MemoryReachability = resourceReachability(memPct)
	}

	podMetrics, podMetricsErr := metricsClient.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	if podMetricsErr == nil {
		resp.TopPods = topPodsByCPU(podMetrics.Items, bifrostNSFilter(s.entry, "bifrost"), topLimit)
	}

	return resp
}

func topPodsByCPU(pods []metricsv1beta1.PodMetrics, nsFilter map[string]bool, limit int) []PodMetricView {
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
		cpuStr := formatCPU(totalCPU)
		memStr := formatMemory(totalMem)
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

func enrichNodesWithMetrics(ctx context.Context, views []NodeView, nodes []corev1.Node, metricsClient metricsclientset.Interface) []NodeView {
	if metricsClient == nil {
		return views
	}
	nodeMetrics, err := metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return views
	}
	usageByName := make(map[string]metricsv1beta1.NodeMetrics, len(nodeMetrics.Items))
	for _, nm := range nodeMetrics.Items {
		usageByName[nm.Name] = nm
	}
	nodeByName := make(map[string]corev1.Node, len(nodes))
	for _, n := range nodes {
		nodeByName[n.Name] = n
	}
	out := make([]NodeView, len(views))
	copy(out, views)
	for i := range out {
		nm, ok := usageByName[out[i].Name]
		n, nodeOk := nodeByName[out[i].Name]
		if !ok || !nodeOk {
			continue
		}
		cpuAlloc, memAlloc, _ := nodeAllocatable(n)
		cpuUsage := nm.Usage[corev1.ResourceCPU]
		memUsage := nm.Usage[corev1.ResourceMemory]
		if cpuPct, ok := usagePercent(cpuUsage, cpuAlloc); ok {
			out[i].CPUUsagePercent = &cpuPct
			out[i].CPUReachability = resourceReachability(cpuPct)
		}
		if memPct, ok := usagePercent(memUsage, memAlloc); ok {
			out[i].MemoryUsagePercent = &memPct
			out[i].MemoryReachability = resourceReachability(memPct)
		}
	}
	return out
}

func failMetrics(base baseMeta, err error, now time.Time) MetricsResponse {
	reach := probe.ReachFail
	detail := err.Error()
	if ce, ok := err.(*ClientError); ok {
		reach = ce.Reachability
		detail = ce.Detail
	}
	return MetricsResponse{
		ClusterID:              base.ClusterID,
		Reachability:           reach,
		Detail:                 detail,
		MetricsServerAvailable: false,
		MetricsServerDetail:    detail,
		TopPods:                []PodMetricView{},
		GeneratedAt:            now,
	}
}
