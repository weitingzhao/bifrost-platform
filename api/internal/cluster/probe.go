package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func (s *Service) Summary(ctx context.Context) SummaryResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	clientset, path, err := s.buildClient()
	if err != nil {
		if ce, ok := err.(*ClientError); ok {
			return SummaryResponse{
				ClusterID:       base.ClusterID,
				Label:           base.Label,
				Distribution:    base.Distribution,
				APIServer:       base.APIServer,
				KubeconfigPath:  path,
				APIReachability: ce.Reachability,
				Reachability:    ce.Reachability,
				Detail:          ce.Detail,
				GeneratedAt:     now,
			}
		}
		return SummaryResponse{
			ClusterID:       base.ClusterID,
			Label:           base.Label,
			Distribution:    base.Distribution,
			APIServer:       base.APIServer,
			KubeconfigPath:  path,
			APIReachability: probe.ReachFail,
			Reachability:    probe.ReachFail,
			Detail:          err.Error(),
			GeneratedAt:     now,
		}
	}

	version, verErr := clientset.Discovery().ServerVersion()
	if verErr != nil {
		return SummaryResponse{
			ClusterID:       base.ClusterID,
			Label:           base.Label,
			Distribution:    base.Distribution,
			APIServer:       base.APIServer,
			KubeconfigPath:  path,
			APIReachability: probe.ReachFail,
			Reachability:    probe.ReachFail,
			Detail:          fmt.Sprintf("API unreachable: %v", verErr),
			GeneratedAt:     now,
		}
	}

	nodes, nodeErr := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if nodeErr != nil {
		return SummaryResponse{
			ClusterID:       base.ClusterID,
			Label:           base.Label,
			Distribution:    base.Distribution,
			APIServer:       base.APIServer,
			KubeconfigPath:  path,
			APIReachability: probe.ReachOK,
			Reachability:    probe.ReachFail,
			Detail:          fmt.Sprintf("list nodes: %v", nodeErr),
			ServerVersion:   version.GitVersion,
			GeneratedAt:     now,
		}
	}

	failing := 0
	runningPods := 0
	pendingPods := 0
	var failingDetails []FailingPodView
	pods, podErr := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if podErr == nil {
		failing = countFailingPods(pods.Items)
		failingDetails = collectFailingPodDetails(pods.Items, now)
		runningPods, pendingPods = countPodsByPhase(pods.Items)
	}

	rollup := s.rollupNodeHealth(ctx, clientset, nodes.Items)
	cpuAlloc, memAlloc := sumAllocatable(nodes.Items, true)

	reach, detail := summaryReachFromNodes(rollup.CoreReady, rollup.CoreTotal, rollup.ElasticDegraded, failing)
	detail = appendElasticStandbyDetail(detail, rollup.ElasticStandby)

	return SummaryResponse{
		ClusterID:            base.ClusterID,
		Label:                base.Label,
		Distribution:         base.Distribution,
		APIServer:            base.APIServer,
		KubeconfigPath:       path,
		APIReachability:      probe.ReachOK,
		Reachability:         reach,
		Detail:               detail,
		ServerVersion:     version.GitVersion,
		NodesReady:        rollup.CoreReady,
		NodesTotal:        rollup.CoreTotal,
		ElasticStandby:    rollup.ElasticStandby,
		ElasticDegraded:   rollup.ElasticDegraded,
		NodesRegistered:   rollup.RegisteredTotal,
		NodesRegisteredReady: rollup.RegisteredReady,
		FailingPods:          failing,
		FailingPodDetails:    failingDetails,
		RunningPods:       runningPods,
		PendingPods:       pendingPods,
		CPUAllocatable:    formatCPU(cpuAlloc),
		MemoryAllocatable: formatMemory(memAlloc),
		GeneratedAt:       now,
	}
}

func (s *Service) Nodes(ctx context.Context) NodesResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	clientset, _, err := s.buildClient()
	if err != nil {
		return failNodes(base, err, now)
	}

	list, listErr := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if listErr != nil {
		return NodesResponse{
			ClusterID:    base.ClusterID,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("list nodes: %v", listErr),
			Nodes:        nil,
			GeneratedAt:  now,
		}
	}

	rollup := s.rollupNodeHealth(ctx, clientset, list.Items)

	views := make([]NodeView, 0, len(list.Items))
	for _, n := range list.Items {
		v := nodeView(n)
		v.ComputeManaged = s.isComputeManaged(n.Name)
		if mode, ok := rollup.ElasticNodeModes[n.Name]; ok {
			v = applyElasticReachability(v, mode)
		}
		views = append(views, v)
	}

	metricsClient, _ := s.buildMetricsClient()
	if metricsClient != nil {
		views = enrichNodesWithMetrics(ctx, views, list.Items, metricsClient)
	}

	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })

	reach, detail := summaryReachFromNodes(rollup.CoreReady, rollup.CoreTotal, rollup.ElasticDegraded, 0)
	detail = appendElasticStandbyDetail(detail, rollup.ElasticStandby)

	return NodesResponse{
		ClusterID:    base.ClusterID,
		Reachability: reach,
		Detail:       detail,
		Nodes:        views,
		GeneratedAt:  now,
	}
}

func (s *Service) Namespaces(ctx context.Context, filter string) NamespacesResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	clientset, _, err := s.buildClient()
	if err != nil {
		return failNamespaces(base, filter, err, now)
	}

	nsList, nsErr := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if nsErr != nil {
		return NamespacesResponse{
			ClusterID:    base.ClusterID,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("list namespaces: %v", nsErr),
			Filter:       filter,
			Namespaces:   nil,
			GeneratedAt:  now,
		}
	}

	podList, _ := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	podsByNS := groupPodsByNamespace(podList.Items)

	allowed := bifrostNSFilter(s.entry, filter)
	views := make([]NamespaceView, 0)
	for _, ns := range nsList.Items {
		if allowed != nil && !allowed[ns.Name] {
			continue
		}
		pods := podsByNS[ns.Name]
		running, failing := podCounts(pods)
		views = append(views, NamespaceView{
			Name:        ns.Name,
			Status:      string(ns.Status.Phase),
			PodCount:    len(pods),
			RunningPods: running,
			FailingPods: failing,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })

	return NamespacesResponse{
		ClusterID:    base.ClusterID,
		Reachability: probe.ReachOK,
		Detail:       fmt.Sprintf("%d namespaces", len(views)),
		Filter:       filter,
		Namespaces:   views,
		GeneratedAt:  now,
	}
}

func (s *Service) Workloads(ctx context.Context, namespace string) WorkloadsResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	if namespace == "" {
		namespace = "default"
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		return failWorkloads(base, namespace, err, now)
	}

	deployments, deployErr := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if deployErr != nil {
		return WorkloadsResponse{
			ClusterID:    base.ClusterID,
			Namespace:    namespace,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("list deployments: %v", deployErr),
			Workloads:    nil,
			GeneratedAt:  now,
		}
	}
	pods, podErr := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if podErr != nil {
		return WorkloadsResponse{
			ClusterID:    base.ClusterID,
			Namespace:    namespace,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("list pods: %v", podErr),
			Workloads:    nil,
			GeneratedAt:  now,
		}
	}

	views := make([]WorkloadView, 0, len(deployments.Items)+len(pods.Items))
	for _, d := range deployments.Items {
		views = append(views, deploymentWorkload(d))
	}
	for _, p := range pods.Items {
		views = append(views, podWorkload(p))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Kind != views[j].Kind {
			return views[i].Kind < views[j].Kind
		}
		return views[i].Name < views[j].Name
	})

	return WorkloadsResponse{
		ClusterID:    base.ClusterID,
		Namespace:    namespace,
		Reachability: probe.ReachOK,
		Detail:       fmt.Sprintf("%d deployments, %d pods", len(deployments.Items), len(pods.Items)),
		Workloads:    views,
		GeneratedAt:  now,
	}
}

func (s *Service) Events(ctx context.Context, namespace string, limit int) EventsResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		return failEvents(base, namespace, err, now)
	}

	opts := metav1.ListOptions{Limit: int64(limit)}
	var list *corev1.EventList
	var listErr error
	if namespace != "" {
		list, listErr = clientset.CoreV1().Events(namespace).List(ctx, opts)
	} else {
		list, listErr = clientset.CoreV1().Events("").List(ctx, opts)
	}
	if listErr != nil {
		return EventsResponse{
			ClusterID:    base.ClusterID,
			Namespace:    namespace,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("list events: %v", listErr),
			Events:       nil,
			GeneratedAt:  now,
		}
	}

	views := make([]EventView, 0, len(list.Items))
	for _, e := range list.Items {
		views = append(views, eventView(e))
	}
	sort.Slice(views, func(i, j int) bool {
		return views[i].LastSeen.After(views[j].LastSeen)
	})

	return EventsResponse{
		ClusterID:    base.ClusterID,
		Namespace:    namespace,
		Reachability: probe.ReachOK,
		Detail:       fmt.Sprintf("%d events", len(views)),
		Events:       views,
		GeneratedAt:  now,
	}
}

type baseMeta struct {
	ClusterID    string
	Label        string
	Distribution string
	APIServer    string
}

func (s *Service) baseMeta(_ time.Time) baseMeta {
	if s.entry == nil {
		return baseMeta{ClusterID: "unknown", Label: "Cluster"}
	}
	return baseMeta{
		ClusterID:    s.entry.ID,
		Label:        s.entry.Label,
		Distribution: s.entry.Distribution,
		APIServer:    s.entry.APIServer,
	}
}

func failNodes(base baseMeta, err error, now time.Time) NodesResponse {
	reach, detail := clientErrFields(err)
	return NodesResponse{
		ClusterID:    base.ClusterID,
		Reachability: reach,
		Detail:       detail,
		Nodes:        nil,
		GeneratedAt:  now,
	}
}

func failNamespaces(base baseMeta, filter string, err error, now time.Time) NamespacesResponse {
	reach, detail := clientErrFields(err)
	return NamespacesResponse{
		ClusterID:    base.ClusterID,
		Reachability: reach,
		Detail:       detail,
		Filter:       filter,
		Namespaces:   nil,
		GeneratedAt:  now,
	}
}

func failWorkloads(base baseMeta, ns string, err error, now time.Time) WorkloadsResponse {
	reach, detail := clientErrFields(err)
	return WorkloadsResponse{
		ClusterID:    base.ClusterID,
		Namespace:    ns,
		Reachability: reach,
		Detail:       detail,
		Workloads:    nil,
		GeneratedAt:  now,
	}
}

func failEvents(base baseMeta, ns string, err error, now time.Time) EventsResponse {
	reach, detail := clientErrFields(err)
	return EventsResponse{
		ClusterID:    base.ClusterID,
		Namespace:    ns,
		Reachability: reach,
		Detail:       detail,
		Events:       nil,
		GeneratedAt:  now,
	}
}

func clientErrFields(err error) (probe.Reachability, string) {
	if ce, ok := err.(*ClientError); ok {
		return ce.Reachability, ce.Detail
	}
	return probe.ReachFail, err.Error()
}

func countReadyNodes(nodes []corev1.Node) (ready, total int) {
	total = len(nodes)
	for _, n := range nodes {
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready++
				break
			}
		}
	}
	return ready, total
}

func nodeReach(ready, total int) (probe.Reachability, string) {
	if total == 0 {
		return probe.ReachFail, "no nodes"
	}
	if ready == total {
		return probe.ReachOK, fmt.Sprintf("%d/%d nodes ready", ready, total)
	}
	if ready == 0 {
		return probe.ReachFail, "no nodes ready"
	}
	return probe.ReachDegraded, fmt.Sprintf("%d/%d nodes ready", ready, total)
}

func nodeView(n corev1.Node) NodeView {
	status := "NotReady"
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			if c.Status == corev1.ConditionTrue {
				status = "Ready"
			} else {
				status = string(c.Status)
			}
			break
		}
	}
	ip := ""
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			ip = a.Address
			break
		}
	}
	reach := probe.ReachOK
	if status != "Ready" {
		reach = probe.ReachFail
	}
	cpuAlloc, memAlloc, storageAlloc := nodeAllocatable(n)
	arch := n.Status.NodeInfo.Architecture
	if arch == "" {
		arch = n.Labels["kubernetes.io/arch"]
	}
	return NodeView{
		Name:               n.Name,
		Status:             status,
		Roles:              nodeRoles(n.Labels),
		Architecture:       arch,
		OSImage:            n.Status.NodeInfo.OSImage,
		WorkloadLabel:      n.Labels["workload"],
		Capabilities:       nodeCapabilities(n.Labels),
		Version:            n.Status.NodeInfo.KubeletVersion,
		InternalIP:         ip,
		Reachability:       reach,
		Unschedulable:      n.Spec.Unschedulable,
		CPUAllocatable:     formatCPU(cpuAlloc),
		MemoryAllocatable:  formatMemory(memAlloc),
		StorageAllocatable: formatMemory(storageAlloc),
	}
}

func nodeRoles(labels map[string]string) string {
	var roles []string
	for k, v := range labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(k, "node-role.kubernetes.io/")
			if role == "" {
				role = "control-plane"
			}
			if v != "" {
				roles = append(roles, role)
			}
		}
	}
	if len(roles) == 0 {
		return "worker"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

func bifrostNSFilter(entry *config.ClusterEntry, filter string) map[string]bool {
	if filter != "bifrost" {
		return nil
	}
	if entry == nil || len(entry.BifrostNamespaces) == 0 {
		return nil
	}
	m := make(map[string]bool, len(entry.BifrostNamespaces))
	for _, ns := range entry.BifrostNamespaces {
		m[ns] = true
	}
	return m
}

func groupPodsByNamespace(pods []corev1.Pod) map[string][]corev1.Pod {
	out := make(map[string][]corev1.Pod)
	for _, p := range pods {
		out[p.Namespace] = append(out[p.Namespace], p)
	}
	return out
}

func podCounts(pods []corev1.Pod) (running, failing int) {
	for _, p := range pods {
		phase := p.Status.Phase
		if phase == corev1.PodRunning || phase == corev1.PodSucceeded {
			running++
		}
		if isFailingPod(p) {
			failing++
		}
	}
	return running, failing
}

func collectFailingPodDetails(pods []corev1.Pod, now time.Time) []FailingPodView {
	var out []FailingPodView
	for _, p := range pods {
		if !isFailingPod(p) {
			continue
		}
		reason := failingReason(p)
		age := ""
		if !p.CreationTimestamp.IsZero() {
			d := now.Sub(p.CreationTimestamp.Time)
			if d < time.Hour {
				age = fmt.Sprintf("%dm", int(d.Minutes()))
			} else if d < 24*time.Hour {
				age = fmt.Sprintf("%dh", int(d.Hours()))
			} else {
				age = fmt.Sprintf("%dd", int(d.Hours()/24))
			}
		}
		out = append(out, FailingPodView{
			Namespace: p.Namespace,
			Name:      p.Name,
			Phase:     string(p.Status.Phase),
			Reason:    reason,
			Node:      p.Spec.NodeName,
			Age:       age,
		})
	}
	return out
}

func failingReason(p corev1.Pod) string {
	switch p.Status.Phase {
	case corev1.PodFailed:
		if p.Status.Reason != "" {
			return p.Status.Reason
		}
		return "Failed"
	case corev1.PodUnknown:
		return "Unknown"
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
				msg := reason
				if cs.State.Waiting.Message != "" {
					msg += ": " + cs.State.Waiting.Message
				}
				return msg
			}
		}
	}
	return "failing"
}

func countFailingPods(pods []corev1.Pod) int {
	n := 0
	for _, p := range pods {
		if isFailingPod(p) {
			n++
		}
	}
	return n
}

func isFailingPod(p corev1.Pod) bool {
	switch p.Status.Phase {
	case corev1.PodFailed, corev1.PodUnknown:
		return true
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" {
				return true
			}
		}
	}
	return false
}

func podWorkload(p corev1.Pod) WorkloadView {
	ready := 0
	total := len(p.Status.ContainerStatuses)
	restarts := int32(0)
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += cs.RestartCount
	}
	phase := string(p.Status.Phase)
	reach := podReachability(phase)
	return WorkloadView{
		Namespace:    p.Namespace,
		Kind:         "Pod",
		Name:         p.Name,
		Ready:        fmt.Sprintf("%d/%d", ready, maxInt(total, len(p.Spec.Containers))),
		Status:       phase,
		Restarts:     restarts,
		Age:          formatAge(p.CreationTimestamp.Time),
		Reachability: reach,
	}
}

func podReachability(phase string) probe.Reachability {
	switch phase {
	case "Running", "Succeeded":
		return probe.ReachOK
	case "Pending":
		return probe.ReachDegraded
	default:
		return probe.ReachFail
	}
}

func eventView(e corev1.Event) EventView {
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

func formatAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
