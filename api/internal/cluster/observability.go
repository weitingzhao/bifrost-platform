package cluster

import (
	"context"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

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
	okCount := resourceview.CountReadyRequiredComponents(components)
	layerStatus, reach, detail := aggregateLayerBStatus(okCount, resourceview.RequiredLayerBCount())

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
		if grafanaURL := s.entry.GrafanaURL(); grafanaURL != "" && resourceview.ComponentReach(components, "grafana") == probe.ReachOK {
			resp.GrafanaURL = grafanaURL
		}
		if promURL := s.entry.PrometheusURL(); promURL != "" && resourceview.ComponentReach(components, "prometheus") == probe.ReachOK {
			resp.PrometheusURL = promURL
		}
	}

	return resp
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

func matchLayerBComponents(deployments []appsv1.Deployment, statefulSets []appsv1.StatefulSet) []ObservabilityComponentView {
	ds := make([]resourceview.DeploymentView, len(deployments))
	for i, d := range deployments {
		ds[i] = resourceview.DeploymentView(deploymentWorkload(d))
	}
	ss := make([]resourceview.DeploymentView, len(statefulSets))
	for i, s := range statefulSets {
		ss[i] = resourceview.StatefulSet(s, formatAge(s.CreationTimestamp.Time), podReachability)
	}
	return resourceview.MatchLayerBComponents(ds, ss)
}
func missingLayerBComponents() []ObservabilityComponentView {
	return resourceview.MissingLayerBComponents()
}
func aggregateLayerBStatus(okCount, totalRequired int) (string, probe.Reachability, string) {
	return resourceview.AggregateLayerBStatus(okCount, totalRequired)
}
