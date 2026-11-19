package cluster

import (
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeploymentProjectionPreservesNonzeroAge(t *testing.T) {
	// Stay well away from minute/hour/day thresholds; both paths use the
	// unchanged formatAge, rather than introducing a separate clock rule.
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{{90 * time.Second, "1m"}, {90 * time.Minute, "1h"}, {36 * time.Hour, "1d"}} {
		replicas := int32(1)
		d := appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(time.Now().Add(-tc.age))},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
		}
		want, got := baselineDeploymentWorkload(d), deploymentWorkload(d)
		if want.Age != tc.want || !reflect.DeepEqual(want, got) {
			t.Fatalf("age %s: original %#v, extracted %#v; expected age %q", tc.age, want, got, tc.want)
		}
	}
}
