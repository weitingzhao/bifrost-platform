package cirunview

import (
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
	"testing"
)

func timingOutcome(fn func() any) (value any, failure string) {
	defer func() {
		if p := recover(); p != nil {
			failure = fmt.Sprint(p)
		}
	}()
	return fn(), ""
}
func TestTaskTimingProjectionMatchesOriginal(t *testing.T) {
	objects := []map[string]any{{}, {"metadata": map[string]any{"name": "run-task", "labels": map[string]any{"tekton.dev/pipelineTask": "label-task"}, "creationTimestamp": "2026-11-16T12:00:00Z"}, "spec": map[string]any{"params": []any{map[string]any{"name": "revision", "value": "a"}, map[string]any{"name": "revision", "value": "b"}, map[string]any{"name": "repo", "value": "platform"}, map[string]any{"name": "base", "value": "base"}, map[string]any{"name": "branch", "value": "branch"}, map[string]any{"name": "uiRevision", "value": "ui"}, map[string]any{"name": "unlisted", "value": "must-not-appear"}, map[string]any{"name": "repo", "value": int64(42)}, nil}}, "status": map[string]any{"results": []any{map[string]any{"name": "text", "value": "verbatim\n界"}, map[string]any{"name": "array", "value": []any{"x", int64(2)}}, map[string]any{"name": "null", "value": nil}, map[string]any{"name": "obj", "value": map[string]any{"a": true}}, "invalid"}}}}
	for _, status := range []any{"True", "False", "Unknown", "unexpected", nil} {
		for _, reason := range []string{"Succeeded", "Completed", "Failed", "PipelineRunFailed", "TaskRunFailed", "Running", "Pending", ""} {
			objects = append(objects, map[string]any{"metadata": map[string]any{"name": "other"}, "status": map[string]any{"conditions": []any{map[string]any{"status": status, "reason": reason}}}})
		}
	}
	for _, step := range []any{nil, "bad", map[string]any{"name": "wait"}, map[string]any{"name": "running", "running": map[string]any{"startedAt": "start"}}} {
		objects = append(objects, map[string]any{"status": map[string]any{"steps": []any{step}}})
	}
	for _, code := range []any{int64(0), int64(1), int64(-1), float64(0), "0", nil} {
		objects = append(objects, map[string]any{"status": map[string]any{"steps": []any{map[string]any{"name": "ended", "terminated": map[string]any{"exitCode": code, "startedAt": "start", "finishedAt": "finish"}, "running": map[string]any{"startedAt": "ignored"}}}}})
	}
	objects = append(objects, map[string]any{"status": map[string]any{"conditions": []any{"bad"}, "steps": "wrong", "results": "wrong"}}, map[string]any{"status": map[string]any{"results": []any{map[string]any{"value": make(chan int)}}}})
	for i, obj := range objects {
		u := unstructured.Unstructured{Object: obj}
		for _, run := range []string{"run", "", "other"} {
			pairs := [][2]func() any{{func() any { return baselineTaskTiming(u, run) }, func() any { return TaskTiming(u, run) }}, {func() any { return baselinePipelineRunResults(&u) }, func() any { return Results(&u) }}}
			for j, p := range pairs {
				want, wp := timingOutcome(p[0])
				got, gp := timingOutcome(p[1])
				if !reflect.DeepEqual(want, got) || wp != gp {
					t.Fatalf("object%d pair%d run%q original=%#v/%q current=%#v/%q", i, j, run, want, wp, got, gp)
				}
				w, we := json.Marshal(want)
				g, ge := json.Marshal(got)
				if string(w) != string(g) || fmt.Sprint(we) != fmt.Sprint(ge) {
					t.Fatalf("JSON differs object%d pair%d", i, j)
				}
			}
		}
	}
	// The original internal result helper panics on nil input; preserve that too.
	_, wp := timingOutcome(func() any { return baselinePipelineRunResults(nil) })
	_, gp := timingOutcome(func() any { return Results(nil) })
	if wp == "" || wp != gp {
		t.Fatal("nil input panic changed")
	}
}
