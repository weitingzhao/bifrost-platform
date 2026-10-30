package delivery

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
)

func TestSTGWorkloadsFullyScaledDown(t *testing.T) {
	t.Parallel()
	for _, name := range stgDeployments {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			core := kfake.NewSimpleClientset(stgWorkloadWorld()...)
			a := &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }}
			if got, err := a.ReadWorkloads(ctx, stgAPI, stgConsole); err != nil || len(got) != 3 {
				t.Fatal("positive control", got, err)
			}
			dep, err := core.AppsV1().Deployments(stgNamespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			zero := int32(0)
			dep.Spec.Replicas = &zero
			dep.Status.Replicas, dep.Status.UpdatedReplicas = 0, 0
			dep.Status.ReadyReplicas, dep.Status.AvailableReplicas = 0, 0
			if _, err = core.AppsV1().Deployments(stgNamespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err = core.CoreV1().Pods(stgNamespace).Delete(ctx, name+"-pod", metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, read := range []func() (map[string]any, error){func() (map[string]any, error) { return a.ObserveWorkloads(ctx) }, func() (map[string]any, error) { return a.ReadWorkloads(ctx, stgAPI, stgConsole) }} {
				if got, err := read(); err == nil || got != nil {
					t.Fatalf("scaled-down deployment accepted: workloads=%v err=%v", got, err)
				}
			}
		})
	}
}
