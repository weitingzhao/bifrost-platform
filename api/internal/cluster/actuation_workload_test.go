package cluster

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func int32ptr(v int32) *int32 { return &v }

func TestDeploymentWorkloadRolloutCounters(t *testing.T) {
	d := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "bifrost-prod",
			Name:      "daemon",
			Generation: 7,
		},
		Spec: appsv1.DeploymentSpec{Replicas: int32ptr(2)},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:      2,
			UpdatedReplicas:    1,
			AvailableReplicas:  2,
			ObservedGeneration: 7,
		},
	}
	w := deploymentWorkload(d)
	if w.Ready != "2/2" {
		t.Fatalf("ready=%q", w.Ready)
	}
	if w.Status != "Progressing" {
		t.Fatalf("status=%q want Progressing", w.Status)
	}
	if w.DesiredReplicas != 2 || w.UpdatedReplicas != 1 || w.ReadyReplicas != 2 {
		t.Fatalf("counters desired=%d updated=%d ready=%d", w.DesiredReplicas, w.UpdatedReplicas, w.ReadyReplicas)
	}
}

func TestDeploymentWorkloadReadyWhenFullyRolled(t *testing.T) {
	d := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "account-sync", Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: int32ptr(1)},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:      1,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
			ObservedGeneration: 3,
		},
	}
	w := deploymentWorkload(d)
	if w.Status != "Ready" {
		t.Fatalf("status=%q", w.Status)
	}
}
