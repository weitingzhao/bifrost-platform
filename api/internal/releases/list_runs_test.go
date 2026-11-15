package releases

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Origin: 2026-11-15, the workers were OOMKilled at 512Mi in STG holding
// lists of every retained run; the recorder (every 5 minutes) and Current
// (every 15 s per deployment) each read them in one unpaged call, with the
// Pipeline definition Tekton copies into each run's status: 77% of the
// bytes, read by nothing here. The list is paged and a run comes without
// that copy and managedFields; what a record is built from is untouched.
func TestListRunsIsPagedAndSlim(t *testing.T) {
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"})
	calls := 0
	dyn.PrependReactor("list", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if ns := action.GetNamespace(); ns != "ns-x" {
			t.Fatalf("listed in %q", ns)
		}
		page := &unstructured.UnstructuredList{}
		name := "first"
		if calls == 0 {
			page.SetContinue("next")
		} else {
			name = "second"
		}
		page.Items = []unstructured.Unstructured{{Object: map[string]any{
			"metadata": map[string]any{"name": name, "labels": map[string]any{"tekton.dev/pipeline": "deliver-x"},
				"managedFields": []any{map[string]any{"manager": "controller"}}},
			"spec": map[string]any{"params": []any{map[string]any{"name": "revision", "value": "abc"}},
				"pipelineSpec": map[string]any{"tasks": []any{map[string]any{"name": "clone"}}}},
			"status": map[string]any{
				"pipelineSpec":   map[string]any{"tasks": []any{map[string]any{"name": "clone"}}},
				"conditions":     []any{map[string]any{"type": "Succeeded", "status": "True"}},
				"completionTime": "2026-11-15T06:00:00Z",
			},
		}}}
		calls++
		return true, page, nil
	})
	runs, err := listRuns(context.Background(), dyn, "ns-x")
	if err != nil || calls != 2 || len(runs) != 2 || runs[1].GetName() != "second" {
		t.Fatalf("runs=%d calls=%d err=%v", len(runs), calls, err)
	}
	for _, run := range runs {
		if _, has, _ := unstructured.NestedFieldNoCopy(run.Object, "status", "pipelineSpec"); has {
			t.Fatalf("%s: the copied Pipeline is still in the run", run.GetName())
		}
		if _, has, _ := unstructured.NestedFieldNoCopy(run.Object, "metadata", "managedFields"); has {
			t.Fatalf("%s: managedFields is still in the run", run.GetName())
		}
		// spec.pipelineSpec is the run's own inline definition, which a
		// record's pinned clones are read from: it stays.
		if _, has, _ := unstructured.NestedFieldNoCopy(run.Object, "spec", "pipelineSpec"); !has {
			t.Fatalf("%s: the run's own inline spec is gone", run.GetName())
		}
		conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
		if len(conditions) != 1 || nestedTime(run.Object, "status", "completionTime").IsZero() || run.GetLabels()["tekton.dev/pipeline"] != "deliver-x" {
			t.Fatalf("%s: a field a record reads is gone: %+v", run.GetName(), run.Object)
		}
	}
}
