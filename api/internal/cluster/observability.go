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
	ID      string
	Label   string
	Match   string
	Exclude string
	Phase   string // required | planned
}

var layerBRequired = []layerBComponentSpec{
	{ID: "prometheus", Label: "Prometheus", Match: "prometheus", Exclude: "grafana", Phase: "required"},
	{ID: "grafana", Label: "Grafana", Match: "grafana", Phase: "required"},
	{ID: "alertmanager", Label: "Alertmanager", Match: "alertmanager", Phase: "required"},
}

var layerBPlanned = []layerBComponentSpec{
	{ID: "loki", Label: "Loki", Match: "loki", Phase: "planned"},
}

func allLayerBComponents() []layerBComponentSpec {
	out := make([]layerBComponentSpec, 0, len(layerBRequired)+len(layerBPlanned))
	out = append(out, layerBRequired...)
	out = append(out, layerBPlanned...)
	return out
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
			ClusterID:            base.ClusterID,
			Namespace:            ns,
			LayerBStatus:         "not_installed",
			LayerBInstallEnabled: observabilityInstallEnabled(),
			Reachability:         probe.ReachDegraded,
			Detail:               fmt.Sprintf("namespace %s not found", ns),
			Components:           missingLayerBComponents(),
			DocsURL:              s.entryObservabilityDocsURL(),
			GeneratedAt:          now,
		}
	}

	deployments, _ := clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	statefulSets, _ := clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})

	components := matchLayerBComponents(deployments.Items, statefulSets.Items)
	okCount := countReadyRequiredComponents(components)
	layerStatus, reach, detail := aggregateLayerBStatus(okCount, len(layerBRequired))

	resp := ObservabilityResponse{
		ClusterID:            base.ClusterID,
		Namespace:            ns,
		LayerBStatus:         layerStatus,
		LayerBInstallEnabled: observabilityInstallEnabled(),
		Reachability:         reach,
		Detail:               detail,
		Components:           components,
		DocsURL:              s.entryObservabilityDocsURL(),
		GeneratedAt:          now,
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
	specs := allLayerBComponents()
	views := make([]ObservabilityComponentView, 0, len(specs))
	for _, spec := range specs {
		if view, ok := findLayerBWorkload(spec, deployments, statefulSets); ok {
			views = append(views, view)
			continue
		}
		if spec.Phase == "planned" {
			views = append(views, plannedLayerBComponent(spec))
			continue
		}
		views = append(views, missingLayerBComponent(spec))
	}
	return views
}

func nameMatchesLayerB(spec layerBComponentSpec, name string) bool {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, spec.Match) {
		return false
	}
	if spec.Exclude != "" && strings.Contains(lower, spec.Exclude) {
		return false
	}
	return true
}

func findLayerBWorkload(
	spec layerBComponentSpec,
	deployments []appsv1.Deployment,
	statefulSets []appsv1.StatefulSet,
) (ObservabilityComponentView, bool) {
	for _, d := range deployments {
		if nameMatchesLayerB(spec, d.Name) {
			w := deploymentWorkload(d)
			return observabilityFromWorkload(spec, w), true
		}
	}
	for _, ss := range statefulSets {
		if nameMatchesLayerB(spec, ss.Name) {
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
		Phase:        spec.Phase,
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
		Phase:        spec.Phase,
	}
}

func plannedLayerBComponent(spec layerBComponentSpec) ObservabilityComponentView {
	return ObservabilityComponentView{
		ID:           spec.ID,
		Label:        spec.Label,
		Kind:         "—",
		Name:         "—",
		Ready:        "—",
		Status:       "planned",
		Reachability: probe.ReachUnknown,
		Detail:       "planned for Phase 5",
		Phase:        "planned",
	}
}

func missingLayerBComponents() []ObservabilityComponentView {
	specs := allLayerBComponents()
	out := make([]ObservabilityComponentView, len(specs))
	for i, spec := range specs {
		if spec.Phase == "planned" {
			out[i] = plannedLayerBComponent(spec)
		} else {
			out[i] = missingLayerBComponent(spec)
		}
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

func countReadyRequiredComponents(components []ObservabilityComponentView) int {
	n := 0
	for _, c := range components {
		if c.Phase != "required" {
			continue
		}
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

func aggregateLayerBStatus(okCount, totalRequired int) (status string, reach probe.Reachability, detail string) {
	switch {
	case okCount == 0:
		return "not_installed", probe.ReachDegraded, "kube-prometheus-stack not detected (Layer B planned)"
	case okCount < totalRequired:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d/%d observability components ready", okCount, totalRequired)
	default:
		return "ready", probe.ReachOK, fmt.Sprintf("all %d observability components ready", totalRequired)
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
		ClusterID:            base.ClusterID,
		Namespace:            ns,
		LayerBStatus:         "not_installed",
		LayerBInstallEnabled: observabilityInstallEnabled(),
		Reachability:         reach,
		Detail:               detail,
		Components:           missingLayerBComponents(),
		GeneratedAt:          now,
	}
}
