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

// A reader that waits for a run reports how long each task took from the
// steps route alone: every task carries its TaskRun's own times, and one
// that has not finished carries no completion time.
func TestPipelineRunStepsCarryTaskTimes(t *testing.T) {
	taskRun := func(task, start, done string) *unstructured.Unstructured {
		status := map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}}}
		if start != "" {
			status["startTime"] = start
		}
		if done != "" {
			status["completionTime"] = done
		} else {
			status["conditions"] = []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
			"metadata": map[string]any{"name": "run-1-" + task, "namespace": "cicd", "labels": map[string]any{
				"tekton.dev/pipelineRun": "run-1", "tekton.dev/pipelineTask": task}},
			"status": status,
		}}
	}
	run := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": "run-1", "namespace": "cicd"},
		"spec":     map[string]any{"pipelineRef": map[string]any{"name": "bifrost-deliver-platform-prod"}},
	}}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
	}, run,
		taskRun("clone-platform", "2026-11-12T21:40:45Z", "2026-11-12T21:40:52Z"),
		taskRun("rollout", "2026-11-12T21:41:10Z", ""))
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })

	out := s.PipelineRunSteps(context.Background(), "cicd", "run-1")
	if out.Reachability != probe.ReachOK || len(out.Tasks) != 2 {
		t.Fatalf("steps = %+v", out)
	}
	clone, rollout := out.Tasks[0], out.Tasks[1]
	if clone.PipelineTask != "clone-platform" || clone.StartTime != "2026-11-12T21:40:45Z" || clone.CompletionTime != "2026-11-12T21:40:52Z" {
		t.Fatalf("clone = %+v", clone)
	}
	if rollout.PipelineTask != "rollout" || rollout.StartTime != "2026-11-12T21:41:10Z" || rollout.CompletionTime != "" {
		t.Fatalf("rollout = %+v", rollout)
	}
}
