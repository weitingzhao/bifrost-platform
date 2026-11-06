package landingci

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func failureObject(kind, name string, status map[string]any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": "cicd"}, "status": status}}
	if kind == "PipelineRun" {
		o.Object["spec"] = map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-platform"}}
	}
	return o
}

func succeeded(status, message string) []any {
	return []any{map[string]any{"type": "Succeeded", "status": status, "reason": "x", "message": message}}
}

// R5:5.7-10: a red run is told by its failed TaskRuns, each with its
// message and the end of its failed step's log; a passed TaskRun is not
// told, and a log that cannot be read leaves the message alone.
func TestFailureReadsTheFailedTaskRuns(t *testing.T) {
	run := failureObject("PipelineRun", "landing-ci-x-2", map[string]any{"conditions": succeeded("False", "Tasks Completed: 3 (Failed: 1)"),
		"childReferences": []any{
			map[string]any{"kind": "TaskRun", "name": "landing-ci-x-2-build", "pipelineTaskName": "build"},
			map[string]any{"kind": "TaskRun", "name": "landing-ci-x-2-test", "pipelineTaskName": "go-test"},
		}})
	build := failureObject("TaskRun", "landing-ci-x-2-build", map[string]any{"conditions": succeeded("True", "All Steps have completed executing")})
	test := failureObject("TaskRun", "landing-ci-x-2-test", map[string]any{"conditions": succeeded("False", `"step-test" exited with code 1`),
		"podName": "landing-ci-x-2-test-pod", "steps": []any{
			map[string]any{"name": "setup", "container": "step-setup", "terminated": map[string]any{"exitCode": int64(0)}},
			map[string]any{"name": "test", "container": "step-test", "terminated": map[string]any{"exitCode": int64(1)}},
		}})
	c, _ := s5TestClient(run, build, test)
	c.Logs = func(_ context.Context, pod, container string) (string, error) {
		if pod != "landing-ci-x-2-test-pod" || container != "step-test" {
			t.Fatalf("log of %s/%s", pod, container)
		}
		return strings.Repeat("ok\n", 3000) + "--- FAIL: TestX\nFAIL\n\n", nil
	}
	f, err := c.Failure(context.Background(), "landing-ci-x-2")
	if err != nil {
		t.Fatal(err)
	}
	if f.Pipeline != "bifrost-ci-platform" || f.Run != "landing-ci-x-2" || len(f.TaskRuns) != 1 {
		t.Fatalf("failure: %+v", f)
	}
	got := f.TaskRuns[0]
	if got.Name != "landing-ci-x-2-test" || got.PipelineTask != "go-test" || got.Step != "test" || got.Message != `"step-test" exited with code 1` ||
		!strings.HasSuffix(got.LogTail, "--- FAIL: TestX\nFAIL") || len(got.LogTail) > landing.CIFailureLogBytes {
		t.Fatalf("task run: %+v", got)
	}
	c.Logs = func(context.Context, string, string) (string, error) { return "", errors.New("forbidden") }
	if f, err = c.Failure(context.Background(), "landing-ci-x-2"); err != nil || f.TaskRuns[0].LogTail != "" || f.TaskRuns[0].Message == "" {
		t.Fatalf("unreadable log: %+v %v", f, err)
	}
}

// A red run with no failed TaskRun (cancelled, timed out) is told by its own
// condition's message.
func TestFailureOfARunWithNoFailedTaskRun(t *testing.T) {
	run := failureObject("PipelineRun", "landing-ci-y-1", map[string]any{"conditions": succeeded("False", "PipelineRun timed out")})
	c, _ := s5TestClient(run)
	f, err := c.Failure(context.Background(), "landing-ci-y-1")
	if err != nil || len(f.TaskRuns) != 1 || f.TaskRuns[0].Name != "landing-ci-y-1" || f.TaskRuns[0].Message != "PipelineRun timed out" {
		t.Fatalf("failure: %+v %v", f, err)
	}
}
