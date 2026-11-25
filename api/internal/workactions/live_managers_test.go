package workactions

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

// Decision 2026-11-14 (LAW-6): approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 4. Whether the applier
// applied an object before is read from the cluster's own copy, with a GET
// of the object: its managedFields as they are, "not found" told from a
// read that failed, and no read at all of a kind the applier does not write.
func TestLiveManagersReadsTheObjectItself(t *testing.T) {
	task := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "Task",
		"metadata": map[string]any{"name": "t", "namespace": "delivery"},
	}}
	task.SetManagedFields([]metav1.ManagedFieldsEntry{
		{Manager: "kubectl-client-side-apply", Operation: metav1.ManagedFieldsOperationUpdate},
		{Manager: "bifrost-applier", Operation: metav1.ManagedFieldsOperationApply},
	})
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{{Group: "tekton.dev", Version: "v1", Resource: "tasks"}: "TaskList"}, task)
	cs := kubefake.NewSimpleClientset()
	cs.Fake.Resources = []*metav1.APIResourceList{{GroupVersion: "tekton.dev/v1"}}
	svc := &Service{
		Policy: &actuationpolicy.Policy{
			Delivery: actuationpolicy.Delivery{Namespace: "delivery", Pipeline: "pipeline"},
			Apply: actuationpolicy.Apply{
				CicdResources: []actuationpolicy.Resource{{Group: "tekton.dev", Resource: "tasks", Kind: "Task"}, {Group: "absent.dev", Resource: "things", Kind: "Thing"}},
			},
		},
		Clients: Clients{
			Dynamic: func() (dynamic.Interface, error) { return dyn, nil },
			Kube:    func() (kubernetes.Interface, error) { return cs, nil },
		},
	}
	ctx := context.Background()
	got, found, err := svc.LiveManagers(ctx, "delivery", "Task", "t")
	if err != nil || !found || len(got) != 2 || got[1] != (ManagedBy{Manager: "bifrost-applier", Operation: "Apply"}) {
		t.Fatalf("managers = %+v found %v err %v", got, found, err)
	}
	if got, found, err := svc.LiveManagers(ctx, "delivery", "Task", "new"); err != nil || found || got != nil {
		t.Fatalf("a missing object: %+v found %v err %v", got, found, err)
	}
	for name, at := range map[string][3]string{
		"a kind the applier does not write":  {"delivery", "Secret", "t"},
		"a group the cluster does not serve": {"delivery", "Thing", "t"},
		"another namespace's list":           {"app", "Task", "t"},
	} {
		if _, found, err := svc.LiveManagers(ctx, at[0], at[1], at[2]); err == nil || found {
			t.Errorf("%s: found %v err %v", name, found, err)
		}
	}
}
