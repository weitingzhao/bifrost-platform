package delivery

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Origin: 2026-11-15, the workers listed every unit TaskRun each 30 s in one
// unpaged call, each with the copy of its Task Tekton keeps in
// status.taskSpec, while they were being OOMKilled at 512Mi in STG. The list
// is paged and a listed run comes without that copy and without
// managedFields; labels, conditions and results are untouched.
func TestUnitTaskRunsArePagedAndSlim(t *testing.T) {
	t.Parallel()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{taskRunGVR: "TaskRunList"})
	calls := 0
	dyn.PrependReactor("list", "taskruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if got := action.(k8stesting.ListAction).GetListRestrictions().Labels.String(); got != "unit=yes" {
			t.Fatalf("selector %q", got)
		}
		page := &unstructured.UnstructuredList{}
		name := "first"
		if calls == 0 {
			page.SetContinue("next")
		} else {
			name = "second"
		}
		page.Items = []unstructured.Unstructured{{Object: map[string]any{
			"metadata": map[string]any{"name": name, "labels": map[string]any{"unit": "yes"},
				"managedFields": []any{map[string]any{"manager": "controller"}}},
			"status": map[string]any{
				"taskSpec":   map[string]any{"steps": []any{map[string]any{"name": "run", "script": "long"}}},
				"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
				"results":    []any{map[string]any{"name": "verdict", "value": "passed"}},
			},
		}}}
		calls++
		return true, page, nil
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	items, err := s.UnitTaskRuns(context.Background(), "unit=yes", "")
	if err != nil || calls != 2 || len(items) != 2 || items[1].GetName() != "second" {
		t.Fatalf("items=%d calls=%d err=%v", len(items), calls, err)
	}
	for _, item := range items {
		if _, has, _ := unstructured.NestedFieldNoCopy(item.Object, "status", "taskSpec"); has {
			t.Fatalf("%s: the copied Task is still in the run", item.GetName())
		}
		if _, has, _ := unstructured.NestedFieldNoCopy(item.Object, "metadata", "managedFields"); has {
			t.Fatalf("%s: managedFields is still in the run", item.GetName())
		}
		conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		results, _, _ := unstructured.NestedSlice(item.Object, "status", "results")
		if len(conditions) != 1 || len(results) != 1 || item.GetLabels()["unit"] != "yes" {
			t.Fatalf("%s: a field the loop reads is gone: %+v", item.GetName(), item.Object)
		}
	}
}
