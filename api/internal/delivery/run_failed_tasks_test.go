package delivery

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// The failed tasks of a run, read as the cluster keeps them (the shapes are
// those of ci-platform-fskzt and ci-platform-wsbh2, 2026-11-13): a task that
// names two tests, one that names none, one with no failed-tests result, and
// a passed task and another run's task, which are not listed.
func TestRunFailedTasks(t *testing.T) {
	task := func(name, run, pipelineTask, status string, results map[string]string) *unstructured.Unstructured {
		var list []any
		for k, v := range results {
			list = append(list, map[string]any{"name": k, "type": "string", "value": v})
		}
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
			"metadata": map[string]any{"name": name, "namespace": "cicd",
				"labels": map[string]any{"tekton.dev/pipelineRun": run, "tekton.dev/pipelineTask": pipelineTask}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status, "reason": map[string]string{"True": "Succeeded", "False": "Failed"}[status]}},
				"results": list},
		}}
		return u
	}
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{taskRunGVR: "TaskRunList"},
		task("r1-test-api", "r1", "test-api", "False", map[string]string{"api-scope": "full", "failed-tests": "pkg/b.TestB,pkg/a.TestA"}),
		task("r1-test-api-store", "r1", "test-api-store", "False", map[string]string{"failed-tests": ""}),
		task("r1-test-console", "r1", "test-console", "False", nil),
		task("r1-clone", "r1", "clone", "True", nil),
		task("r2-test-api", "r2", "test-api", "False", map[string]string{"failed-tests": "pkg/c.TestC"}))
	s := &Service{dynamicFactory: func() (dynamic.Interface, error) { return client, nil }}
	got, err := s.RunFailedTasks(context.Background(), "r1")
	want := []FailedTask{{Task: "test-api", Tests: []string{"pkg/b.TestB", "pkg/a.TestA"}, NamesTests: true},
		{Task: "test-api-store", NamesTests: true}, {Task: "test-console"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("failed tasks: %+v %v", got, err)
	}
	if got, err = s.RunFailedTasks(context.Background(), "gone"); err != nil || len(got) != 0 {
		t.Fatalf("a run with no tasks kept: %+v %v", got, err)
	}
}
