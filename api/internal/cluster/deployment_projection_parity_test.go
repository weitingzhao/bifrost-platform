//bifrost:owner foundation

package cluster

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestDeploymentProjectionMatchesOriginal(t *testing.T) {
	for _, desired := range []int32{-1, 0, 1, 3} {
		for _, ready := range []int32{0, 1, 3} {
			for _, updated := range []int32{0, 1, 3} {
				for _, available := range []int32{0, 1, 3} {
					for _, nilReplicas := range []bool{false, true} {
						d := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: "example", Generation: 7}, Spec: appsv1.DeploymentSpec{Replicas: &desired}, Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: updated, AvailableReplicas: available, ObservedGeneration: 6}}
						if nilReplicas {
							d.Spec.Replicas = nil
						}
						want, got := baselineDeploymentWorkload(d), deploymentWorkload(d)
						if !reflect.DeepEqual(want, got) {
							t.Fatalf("deployment %#v: %#v != %#v", d, want, got)
						}
					}
				}
			}
		}
	}
}
