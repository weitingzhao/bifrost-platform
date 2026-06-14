package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type layerBComponentSpec struct {
	ID    string
	Label string
	Match string
}

var layerBComponents = []layerBComponentSpec{
	{ID: "prometheus", Label: "Prometheus", Match: "prometheus"},
	{ID: "grafana", Label: "Grafana", Match: "grafana"},
	{ID: "loki", Label: "Loki", Match: "loki"},
	{ID: "alertmanager", Label: "Alertmanager", Match: "alertmanager"},
}

func (s *Service) Observability(ctx context.Context) ObservabilityResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	ns := "monitoring"
	if s.entry != nil {
		ns = s.entry.ResolvedMonitoringNamespace()
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		return failObservability(base, ns, err, now)
	}

	_, nsErr := clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if nsErr != nil {
		return ObservabilityResponse{
			ClusterID:    base.ClusterID,
			Namespace:    ns,
			LayerBStatus: "not_installed",
			Reachability: probe.ReachDegraded,
			Detail:       fmt.Sprintf("namespace %s not found", ns),
			Components:   missingLayerBComponents(),
			DocsURL:      s.entryObservabilityDocsURL(),
			GeneratedAt:  now,
		}
	}

	deployments, _ := clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	statefulSets, _ := clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})

	components := matchLayerBComponents(deployments.Items, statefulSets.Items)
	okCount := countReadyComponents(components)
	layerStatus, reach, detail := aggregateLayerBStatus(okCount, len(layerBComponents))

	resp := ObservabilityResponse{
		ClusterID:    base.ClusterID,
		Namespace:    ns,
		LayerBStatus: layerStatus,
		Reachability: reach,
		Detail:       detail,
		Components:   components,
		DocsURL:      s.entryObservabilityDocsURL(),
		GeneratedAt:  now,
	}

	if s.entry != nil {
		if grafanaURL := s.entry.GrafanaURL(); grafanaURL != "" && componentReach(components, "grafana") == probe.ReachOK {
			resp.GrafanaURL = grafanaURL
		}
		if promURL := s.entry.PrometheusURL(); promURL != "" && componentReach(components, "prometheus") == probe.ReachOK {
			resp.PrometheusURL = promURL
		}
	}

	return resp
}

func matchLayerBComponents(deployments []appsv1.Deployment, statefulSets []appsv1.StatefulSet) []ObservabilityComponentView {
	views := make([]ObservabilityComponentView, 0, len(layerBComponents))
	for _, spec := range layerBComponents {
		if view, ok := findLayerBWorkload(spec, deployments, statefulSets); ok {
			views = append(views, view)
			continue
		}
		views = append(views, missingLayerBComponent(spec))
	}
	return views
}

func findLayerBWorkload(
	spec layerBComponentSpec,
	deployments []appsv1.Deployment,
	statefulSets []appsv1.StatefulSet,
) (ObservabilityComponentView, bool) {
	for _, d := range deployments {
		if strings.Contains(strings.ToLower(d.Name), spec.Match) {
			w := deploymentWorkload(d)
			return observabilityFromWorkload(spec, w), true
		}
	}
	for _, ss := range statefulSets {
		if strings.Contains(strings.ToLower(ss.Name), spec.Match) {
			w := statefulSetWorkload(ss)
			return observabilityFromWorkload(spec, w), true
		}
	}
	return ObservabilityComponentView{}, false
}

func observabilityFromWorkload(spec layerBComponentSpec, w WorkloadView) ObservabilityComponentView {
	detail := w.Status
	if w.Reachability != probe.ReachOK {
		detail = fmt.Sprintf("%s (%s)", w.Status, w.Ready)
	}
	return ObservabilityComponentView{
		ID:           spec.ID,
		Label:        spec.Label,
		Kind:         w.Kind,
		Name:         w.Name,
		Ready:        w.Ready,
		Status:       w.Status,
		Reachability: w.Reachability,
		Detail:       detail,
	}
}

func missingLayerBComponent(spec layerBComponentSpec) ObservabilityComponentView {
	return ObservabilityComponentView{
		ID:           spec.ID,
		Label:        spec.Label,
		Kind:         "—",
		Name:         "—",
		Ready:        "—",
		Status:       "missing",
		Reachability: probe.ReachUnknown,
		Detail:       "not detected in monitoring namespace",
	}
}

func missingLayerBComponents() []ObservabilityComponentView {
	out := make([]ObservabilityComponentView, len(layerBComponents))
	for i, spec := range layerBComponents {
		out[i] = missingLayerBComponent(spec)
	}
	return out
}

func statefulSetWorkload(ss appsv1.StatefulSet) WorkloadView {
	replicas := int32(0)
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	reach := podReachability("Running")
	status := "Ready"
	if ss.Status.ReadyReplicas < replicas {
		reach = podReachability("Pending")
		status = "Progressing"
	}
	if replicas > 0 && ss.Status.ReadyReplicas == 0 {
		reach = podReachability("Failed")
		status = "Unavailable"
	}
	return WorkloadView{
		Namespace:    ss.Namespace,
		Kind:         "StatefulSet",
		Name:         ss.Name,
		Ready:        fmt.Sprintf("%d/%d", ss.Status.ReadyReplicas, replicas),
		Status:       status,
		Restarts:     0,
		Age:          formatAge(ss.CreationTimestamp.Time),
		Reachability: reach,
	}
}

func countReadyComponents(components []ObservabilityComponentView) int {
	n := 0
	for _, c := range components {
		if c.Reachability == probe.ReachOK {
			n++
		}
	}
	return n
}

func componentReach(components []ObservabilityComponentView, id string) probe.Reachability {
	for _, c := range components {
		if c.ID == id {
			return c.Reachability
		}
	}
	return probe.ReachUnknown
}

func aggregateLayerBStatus(okCount, total int) (status string, reach probe.Reachability, detail string) {
	switch {
	case okCount == 0:
		return "not_installed", probe.ReachDegraded, "kube-prometheus-stack not detected (Layer B planned)"
	case okCount < total:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d/%d observability components ready", okCount, total)
	default:
		return "ready", probe.ReachOK, fmt.Sprintf("all %d observability components ready", total)
	}
}

func (s *Service) entryObservabilityDocsURL() string {
	if s.entry == nil {
		return ""
	}
	return s.entry.ObservabilityDocsURL()
}

func failObservability(base baseMeta, ns string, err error, now time.Time) ObservabilityResponse {
	reach := probe.ReachFail
	detail := err.Error()
	if ce, ok := err.(*ClientError); ok {
		reach = ce.Reachability
		detail = ce.Detail
	}
	return ObservabilityResponse{
		ClusterID:    base.ClusterID,
		Namespace:    ns,
		LayerBStatus: "not_installed",
		Reachability: reach,
		Detail:       detail,
		Components:   missingLayerBComponents(),
		GeneratedAt:  now,
	}
}
