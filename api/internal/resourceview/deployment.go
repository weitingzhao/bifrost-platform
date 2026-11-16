package resourceview

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	appsv1 "k8s.io/api/apps/v1"
)

type DeploymentView struct {
	Namespace    string             `json:"namespace"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name"`
	Ready        string             `json:"ready"`
	Status       string             `json:"status"`
	Restarts     int32              `json:"restarts"`
	Age          string             `json:"age"`
	Reachability probe.Reachability `json:"reachability"`
	// Deployment rollout counters (0 for Pods / non-Deployments).
	DesiredReplicas    int32 `json:"desired_replicas,omitempty"`
	ReadyReplicas      int32 `json:"ready_replicas,omitempty"`
	UpdatedReplicas    int32 `json:"updated_replicas,omitempty"`
	AvailableReplicas  int32 `json:"available_replicas,omitempty"`
	Generation         int64 `json:"generation,omitempty"`
	ObservedGeneration int64 `json:"observed_generation,omitempty"`
}

func Deployment(d appsv1.Deployment, age string, reachForPhase func(string) probe.Reachability) DeploymentView {
	replicas := int32(0)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	ready := d.Status.ReadyReplicas
	updated := d.Status.UpdatedReplicas
	available := d.Status.AvailableReplicas
	reach := reachForPhase("Running")
	status := "Ready"
	if replicas > 0 && (ready < replicas || updated < replicas || available < replicas) {
		reach = reachForPhase("Pending")
		status = "Progressing"
	}
	if replicas > 0 && available == 0 {
		reach = reachForPhase("Failed")
		status = "Unavailable"
	}
	return DeploymentView{
		Namespace:          d.Namespace,
		Kind:               "Deployment",
		Name:               d.Name,
		Ready:              fmt.Sprintf("%d/%d", ready, replicas),
		Status:             status,
		Restarts:           0,
		Age:                age,
		Reachability:       reach,
		DesiredReplicas:    replicas,
		ReadyReplicas:      ready,
		UpdatedReplicas:    updated,
		AvailableReplicas:  available,
		Generation:         d.Generation,
		ObservedGeneration: d.Status.ObservedGeneration,
	}
}
