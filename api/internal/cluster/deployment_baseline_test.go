package cluster

import (
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
)

func baselineDeploymentWorkload(d appsv1.Deployment) WorkloadView {
	replicas := int32(0)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	ready := d.Status.ReadyReplicas
	updated := d.Status.UpdatedReplicas
	available := d.Status.AvailableReplicas
	reach := podReachability("Running")
	status := "Ready"
	if replicas > 0 && (ready < replicas || updated < replicas || available < replicas) {
		reach = podReachability("Pending")
		status = "Progressing"
	}
	if replicas > 0 && available == 0 {
		reach = podReachability("Failed")
		status = "Unavailable"
	}
	return WorkloadView{
		Namespace:          d.Namespace,
		Kind:               "Deployment",
		Name:               d.Name,
		Ready:              fmt.Sprintf("%d/%d", ready, replicas),
		Status:             status,
		Restarts:           0,
		Age:                formatAge(d.CreationTimestamp.Time),
		Reachability:       reach,
		DesiredReplicas:    replicas,
		ReadyReplicas:      ready,
		UpdatedReplicas:    updated,
		AvailableReplicas:  available,
		Generation:         d.Generation,
		ObservedGeneration: d.Status.ObservedGeneration,
	}
}
