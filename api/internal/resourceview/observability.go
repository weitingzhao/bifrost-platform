package resourceview

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	appsv1 "k8s.io/api/apps/v1"
	"strings"
)

type ObservabilityComponentView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name"`
	Ready        string             `json:"ready"`
	Status       string             `json:"status"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Phase        string             `json:"phase,omitempty"` // required | planned
}

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

func MatchLayerBComponents(deployments []DeploymentView, statefulSets []DeploymentView) []ObservabilityComponentView {
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
	deployments []DeploymentView,
	statefulSets []DeploymentView,
) (ObservabilityComponentView, bool) {
	for _, d := range deployments {
		if nameMatchesLayerB(spec, d.Name) {
			w := d
			return observabilityFromWorkload(spec, w), true
		}
	}
	for _, ss := range statefulSets {
		if nameMatchesLayerB(spec, ss.Name) {
			w := ss
			return observabilityFromWorkload(spec, w), true
		}
	}
	return ObservabilityComponentView{}, false
}

func observabilityFromWorkload(spec layerBComponentSpec, w DeploymentView) ObservabilityComponentView {
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

func MissingLayerBComponents() []ObservabilityComponentView {
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

func StatefulSet(ss appsv1.StatefulSet, age string, reachForPhase func(string) probe.Reachability) DeploymentView {
	replicas := int32(0)
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	reach := reachForPhase("Running")
	status := "Ready"
	if ss.Status.ReadyReplicas < replicas {
		reach = reachForPhase("Pending")
		status = "Progressing"
	}
	if replicas > 0 && ss.Status.ReadyReplicas == 0 {
		reach = reachForPhase("Failed")
		status = "Unavailable"
	}
	return DeploymentView{
		Namespace:    ss.Namespace,
		Kind:         "StatefulSet",
		Name:         ss.Name,
		Ready:        fmt.Sprintf("%d/%d", ss.Status.ReadyReplicas, replicas),
		Status:       status,
		Restarts:     0,
		Age:          age,
		Reachability: reach,
	}
}

func CountReadyRequiredComponents(components []ObservabilityComponentView) int {
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

func ComponentReach(components []ObservabilityComponentView, id string) probe.Reachability {
	for _, c := range components {
		if c.ID == id {
			return c.Reachability
		}
	}
	return probe.ReachUnknown
}

func AggregateLayerBStatus(okCount, totalRequired int) (status string, reach probe.Reachability, detail string) {
	switch {
	case okCount == 0:
		return "not_installed", probe.ReachDegraded, "kube-prometheus-stack not detected (Layer B planned)"
	case okCount < totalRequired:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d/%d observability components ready", okCount, totalRequired)
	default:
		return "ready", probe.ReachOK, fmt.Sprintf("all %d observability components ready", totalRequired)
	}
}

func RequiredLayerBCount() int { return len(layerBRequired) }
