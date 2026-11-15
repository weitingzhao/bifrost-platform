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

func TestPipelineRunObservationsAllPagesWithoutLabels(t *testing.T) {
	t.Parallel()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"})
	calls := 0
	dyn.PrependReactor("list", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		a := action.(k8stesting.ListAction)
		if a.GetNamespace() != "cicd" || !a.GetListRestrictions().Labels.Empty() {
			t.Fatalf("filtered list: %+v", action)
		}
		page := &unstructured.UnstructuredList{}
		name := "first"
		if calls == 0 {
			page.SetContinue("next")
		} else {
			name = "second"
		}
		page.Items = []unstructured.Unstructured{{Object: map[string]any{"metadata": map[string]any{"name": name}}}}
		calls++
		return true, page, nil
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	items, err := s.PipelineRunObservations(context.Background())
	if err != nil || calls != 2 || len(items) != 2 || items[1].GetName() != "second" {
		t.Fatalf("items: %+v calls=%d err=%v", items, calls, err)
	}
}

// Origin: 2026-11-14, the post-release check on STG read every retained run
// (1073, 32 MB) each pass and the workers were OOMKilled three times at a
// 512Mi limit; a loop that follows one pipeline asks the cluster for that
// pipeline's runs only.
func TestPipelineRunsOfAsksForOnePipelinesRuns(t *testing.T) {
	t.Parallel()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"})
	calls := 0
	dyn.PrependReactor("list", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		a := action.(k8stesting.ListAction)
		if got := a.GetListRestrictions().Labels.String(); a.GetNamespace() != "cicd" || got != "tekton.dev/pipeline=deliver-x" {
			t.Fatalf("list in %q with selector %q", a.GetNamespace(), got)
		}
		page := &unstructured.UnstructuredList{}
		name := "first"
		if calls == 0 {
			page.SetContinue("next")
		} else {
			name = "second"
		}
		// The fake client drops what the selector does not select, as the cluster does.
		page.Items = []unstructured.Unstructured{{Object: map[string]any{"metadata": map[string]any{"name": name, "labels": map[string]any{"tekton.dev/pipeline": "deliver-x"}}}}}
		calls++
		return true, page, nil
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	items, err := s.PipelineRunsOf(context.Background(), "deliver-x")
	if err != nil || calls != 2 || len(items) != 2 || items[1].GetName() != "second" {
		t.Fatalf("items: %+v calls=%d err=%v", items, calls, err)
	}
	// What is not a label value never reaches the cluster as a selector.
	for _, bad := range []string{"", "a,b", "x=y", "a b"} {
		if _, err := s.PipelineRunsOf(context.Background(), bad); err == nil {
			t.Fatalf("pipeline %q was listed", bad)
		}
	}
	if calls != 2 {
		t.Fatalf("a refused name reached the cluster: calls=%d", calls)
	}
}

// Origin: 2026-11-15, the STG workers were OOMKilled at 512Mi (and PROD's
// peaked at 640Mi) holding every retained run with the Pipeline definition
// Tekton copies into each run's status: 77% of the listed bytes, read by no
// observer. A listed run keeps what the loops read and loses that copy.
func TestListedRunsComeWithoutTheCopiedPipeline(t *testing.T) {
	t.Parallel()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"})
	dyn.PrependReactor("list", "pipelineruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		page := &unstructured.UnstructuredList{}
		page.Items = []unstructured.Unstructured{{Object: map[string]any{
			"metadata": map[string]any{"name": "run-1", "labels": map[string]any{"tekton.dev/pipeline": "deliver-x"},
				"managedFields": []any{map[string]any{"manager": "controller"}}},
			"spec": map[string]any{"pipelineRef": map[string]any{"name": "deliver-x"},
				"params": []any{map[string]any{"name": "revision", "value": "abc"}}},
			"status": map[string]any{
				"pipelineSpec": map[string]any{"tasks": []any{map[string]any{"name": "build"}}},
				"conditions":   []any{map[string]any{"type": "Succeeded", "status": "True"}},
				"results":      []any{map[string]any{"name": "commit", "value": "abc"}},
				"startTime":    "2026-11-15T06:00:00Z",
			},
		}}}
		return true, page, nil
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	for name, list := range map[string]func() ([]unstructured.Unstructured, error){
		"every run": func() ([]unstructured.Unstructured, error) { return s.PipelineRunObservations(context.Background()) },
		"one pipeline's": func() ([]unstructured.Unstructured, error) {
			return s.PipelineRunsOf(context.Background(), "deliver-x")
		},
	} {
		items, err := list()
		if err != nil || len(items) != 1 {
			t.Fatalf("%s: items=%d err=%v", name, len(items), err)
		}
		run := items[0].Object
		if _, has, _ := unstructured.NestedFieldNoCopy(run, "status", "pipelineSpec"); has {
			t.Fatalf("%s: the copied Pipeline is still in the run", name)
		}
		if _, has, _ := unstructured.NestedFieldNoCopy(run, "metadata", "managedFields"); has {
			t.Fatalf("%s: managedFields is still in the run", name)
		}
		// What the loops read is untouched.
		ref, _, _ := unstructured.NestedString(run, "spec", "pipelineRef", "name")
		params, _, _ := unstructured.NestedSlice(run, "spec", "params")
		conditions, _, _ := unstructured.NestedSlice(run, "status", "conditions")
		results, _, _ := unstructured.NestedSlice(run, "status", "results")
		started, _, _ := unstructured.NestedString(run, "status", "startTime")
		if ref != "deliver-x" || len(params) != 1 || len(conditions) != 1 || len(results) != 1 || started == "" || items[0].GetName() != "run-1" {
			t.Fatalf("%s: a field the loops read is gone: %+v", name, run)
		}
	}
}
