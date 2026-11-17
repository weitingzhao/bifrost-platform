//bifrost:owner delivery

package delivery

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Origin: TestPipelineRunStepsInlineAndResults, whose reading of the run and
// encoding of its results went to cirunview with that code on 2026-11-17
// (TestRunStepsInlineAndResults). What stayed is delivery's part of it: the
// steps route reads this service's cluster and its runs namespace when the
// caller names none, and the pipeline it answers (a reference, or the label
// of an inline run) is the name the waiter then lists that pipeline's runs by.
func TestPipelineRunStepsUseServiceClusterAndFeedRunsLookup(t *testing.T) {
	for _, tc := range []struct{ name, ref, label, pipeline string }{
		{name: "reference", ref: "bifrost-ci-platform", label: "other", pipeline: "bifrost-ci-platform"},
		{name: "inline-labelled", label: "inline-labelled", pipeline: "inline-labelled"},
		{name: "inline-unlabelled"},
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
			if out.Reachability != probe.ReachOK || out.ClusterID != "test" || out.Namespace != "cicd" || out.Pipeline != tc.pipeline || len(out.Tasks) != 1 {
				t.Fatalf("steps = %+v", out)
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
