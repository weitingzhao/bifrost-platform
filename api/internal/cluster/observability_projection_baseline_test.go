//bifrost:owner foundation

package cluster

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"strings"
	"testing"
)

type baselineLayerBComponentSpec struct {
	ID      string
	Label   string
	Match   string
	Exclude string
	Phase   string // required | optional
}

var baselineLayerBRequired = []baselineLayerBComponentSpec{
	{ID: "prometheus", Label: "Prometheus", Match: "prometheus", Exclude: "grafana", Phase: "required"},
	{ID: "grafana", Label: "Grafana", Match: "grafana", Phase: "required"},
	{ID: "alertmanager", Label: "Alertmanager", Match: "alertmanager", Phase: "required"},
}

var baselineLayerBPlanned = []baselineLayerBComponentSpec{
	{ID: "loki", Label: "Loki", Match: "loki", Phase: "optional"},
}

func baselineAllLayerBComponents() []baselineLayerBComponentSpec {
	out := make([]baselineLayerBComponentSpec, 0, len(baselineLayerBRequired)+len(baselineLayerBPlanned))
	out = append(out, baselineLayerBRequired...)
	out = append(out, baselineLayerBPlanned...)
	return out
}

func baselineMatchLayerBComponents(deployments []appsv1.Deployment, statefulSets []appsv1.StatefulSet) []ObservabilityComponentView {
	specs := baselineAllLayerBComponents()
	views := make([]ObservabilityComponentView, 0, len(specs))
	for _, spec := range specs {
		if view, ok := baselineFindLayerBWorkload(spec, deployments, statefulSets); ok {
			views = append(views, view)
			continue
		}
		if spec.Phase == "optional" {
			continue
		}
		views = append(views, baselineMissingLayerBComponent(spec))
	}
	return views
}

func baselineNameMatchesLayerB(spec baselineLayerBComponentSpec, name string) bool {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, spec.Match) {
		return false
	}
	if spec.Exclude != "" && strings.Contains(lower, spec.Exclude) {
		return false
	}
	return true
}

func baselineFindLayerBWorkload(
	spec baselineLayerBComponentSpec,
	deployments []appsv1.Deployment,
	statefulSets []appsv1.StatefulSet,
) (ObservabilityComponentView, bool) {
	for _, d := range deployments {
		if baselineNameMatchesLayerB(spec, d.Name) {
			w := deploymentWorkload(d)
			return baselineObservabilityFromWorkload(spec, w), true
		}
	}
	for _, ss := range statefulSets {
		if baselineNameMatchesLayerB(spec, ss.Name) {
			w := baselineStatefulSetWorkload(ss)
			return baselineObservabilityFromWorkload(spec, w), true
		}
	}
	return ObservabilityComponentView{}, false
}

func baselineObservabilityFromWorkload(spec baselineLayerBComponentSpec, w WorkloadView) ObservabilityComponentView {
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

func baselineMissingLayerBComponent(spec baselineLayerBComponentSpec) ObservabilityComponentView {
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

func baselineMissingLayerBComponents() []ObservabilityComponentView {
	out := make([]ObservabilityComponentView, len(baselineLayerBRequired))
	for i, spec := range baselineLayerBRequired {
		out[i] = baselineMissingLayerBComponent(spec)
	}
	return out
}

func baselineStatefulSetWorkload(ss appsv1.StatefulSet) WorkloadView {
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

func baselineAggregateLayerBStatus(okCount, totalRequired int) (status string, reach probe.Reachability, detail string) {
	switch {
	case okCount == 0:
		return "not_installed", probe.ReachDegraded, "kube-prometheus-stack not detected in monitoring namespace"
	case okCount < totalRequired:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d/%d observability components ready", okCount, totalRequired)
	default:
		return "ready", probe.ReachOK, fmt.Sprintf("all %d observability components ready", totalRequired)
	}
}

func TestObservabilityProjectionOriginalParity(t *testing.T) {
	for _, replicas := range []int32{0, 1, 3} {
		for _, ready := range []int32{0, 1, 3} {
			ds := []appsv1.Deployment{{ObjectMeta: metav1.ObjectMeta{Name: "grafana-prometheus"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: ready, AvailableReplicas: ready}}, {ObjectMeta: metav1.ObjectMeta{Name: "PROMETHEUS-server"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: ready, AvailableReplicas: ready}}}
			ss := []appsv1.StatefulSet{{ObjectMeta: metav1.ObjectMeta{Name: "alertmanager"}, Spec: appsv1.StatefulSetSpec{Replicas: &replicas}, Status: appsv1.StatefulSetStatus{ReadyReplicas: ready}}, {ObjectMeta: metav1.ObjectMeta{Name: "loki"}}, {ObjectMeta: metav1.ObjectMeta{Name: "grafana"}}}
			for _, set := range []struct {
				d []appsv1.Deployment
				s []appsv1.StatefulSet
			}{{ds, ss}, {nil, ss}, {ds, nil}, {nil, nil}} {
				got, want := matchLayerBComponents(set.d, set.s), baselineMatchLayerBComponents(set.d, set.s)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%d/%d: %#v != %#v", ready, replicas, got, want)
				}
			}
		}
	}
	for _, total := range []int{0, 1, 3} {
		for _, count := range []int{-1, 0, 1, 2, 3, 4} {
			a, b, c := aggregateLayerBStatus(count, total)
			x, y, z := baselineAggregateLayerBStatus(count, total)
			if a != x || b != y || c != z {
				t.Fatal("aggregate mismatch")
			}
		}
	}
	if !reflect.DeepEqual(missingLayerBComponents(), baselineMissingLayerBComponents()) {
		t.Fatal("missing mismatch")
	}
}
