package delivery

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestPipelineRunStepsInlineAndResults(t *testing.T) {
	for _, tc := range []struct {
		name, ref, label, pipeline string
		results                    []any
		want                       []PipelineRunResult
	}{
		{name: "reference", ref: "bifrost-ci-platform", label: "other", pipeline: "bifrost-ci-platform", want: []PipelineRunResult{}},
		{name: "inline-labelled", label: "inline-labelled", pipeline: "inline-labelled", results: []any{
			map[string]any{"name": "ui-commit", "value": "abc123"},
			map[string]any{"name": "infra-commit", "value": "def456"},
			map[string]any{"name": "array", "value": []any{"first", "second"}},
			map[string]any{"name": "object", "value": map[string]any{"key": "value"}},
			map[string]any{"name": "number", "value": int64(42)},
			map[string]any{"name": "boolean", "value": true},
			map[string]any{"name": "null", "value": nil},
			map[string]any{"name": "empty", "value": ""},
		}, want: []PipelineRunResult{
			{Name: "ui-commit", Value: "abc123"}, {Name: "infra-commit", Value: "def456"},
			{Name: "array", Value: `["first","second"]`}, {Name: "object", Value: `{"key":"value"}`},
			{Name: "number", Value: "42"}, {Name: "boolean", Value: "true"}, {Name: "null", Value: "null"}, {Name: "empty", Value: ""},
		}},
		{name: "inline-unlabelled", want: []PipelineRunResult{}},
		{name: "inline-empty-results", results: []any{}, want: []PipelineRunResult{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := map[string]any{"pipelineSpec": map[string]any{"tasks": []any{map[string]any{"name": "check"}}}}
			if tc.ref != "" {
				spec = map[string]any{"pipelineRef": map[string]any{"name": tc.ref}}
			}
			run := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
				"metadata": map[string]any{"name": tc.name, "namespace": "cicd"}, "spec": spec,
			}}
			if tc.label != "" {
				run.SetLabels(map[string]string{"tekton.dev/pipeline": tc.label})
			}
			if tc.results != nil {
				run.Object["status"] = map[string]any{"results": tc.results}
			}
			task := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
				"metadata": map[string]any{"name": tc.name + "-check", "namespace": "cicd", "labels": map[string]any{
					"tekton.dev/pipelineRun": tc.name, "tekton.dev/pipelineTask": "check",
				}},
				"status": map[string]any{"conditions": []any{map[string]any{"status": "True", "reason": "Succeeded"}}},
			}}
			dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
				pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
			}, run, task)
			s := NewService(&config.ClusterEntry{ID: "test"})
			s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
			out := s.PipelineRunSteps(context.Background(), "", tc.name)
			if out.Reachability != probe.ReachOK || out.Namespace != "cicd" || out.Pipeline != tc.pipeline || len(out.Tasks) != 1 || out.Tasks[0].PipelineTask != "check" || out.Tasks[0].Status != "succeeded" {
				t.Fatalf("steps = %+v", out)
			}
			if !reflect.DeepEqual(out.Results, tc.want) {
				t.Fatalf("results = %#v, want %#v", out.Results, tc.want)
			}
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.want) == 0 && !strings.Contains(string(raw), `"results":[]`) {
				t.Fatalf("empty results must be []: %s", raw)
			}
			// Exercise the same steps -> pipeline runs lookup used by the waiter.
			if out.Pipeline != "" {
				runs := s.PipelineRuns(context.Background(), out.Pipeline)
				if runs.Reachability != probe.ReachOK || len(runs.Runs) != 1 || runs.Runs[0].Name != tc.name {
					t.Fatalf("runs = %+v", runs)
				}
			}
		})
	}
}
