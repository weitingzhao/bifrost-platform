//bifrost:owner delivery

package delivery

import (
	"encoding/json"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The frozen original of the timings view's parameter exposure allowlist,
// which stays in delivery (the task timing decoders it was frozen with are
// compared in cirunview).
func baselineTimingParams(obj unstructured.Unstructured) map[string]string {
	out := map[string]string{}
	items, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
	for _, item := range items {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		// Only references used by this view, never arbitrary parameters.
		switch name {
		case "revision", "repo", "base", "branch", "uiRevision":
			if value, ok := m["value"].(string); ok {
				out[name] = value
			}
		}
	}
	return out
}

// Origin: 2026-11-16 extraction of the task timing decoders to cirunview, which
// froze the original beside the code and compared the two; 2026-11-17 the
// decoders' half of that comparison went to cirunview with them and this half
// stayed with the allowlist. A parameter the list does not name, a repeated
// name and a value that is not a string read as they did.
func TestTimingParamsMatchOriginal(t *testing.T) {
	objects := []map[string]any{{}, {"metadata": map[string]any{"name": "run-task", "labels": map[string]any{"tekton.dev/pipelineTask": "label-task"}, "creationTimestamp": "2026-11-16T12:00:00Z"}, "spec": map[string]any{"params": []any{map[string]any{"name": "revision", "value": "a"}, map[string]any{"name": "revision", "value": "b"}, map[string]any{"name": "repo", "value": "platform"}, map[string]any{"name": "base", "value": "base"}, map[string]any{"name": "branch", "value": "branch"}, map[string]any{"name": "uiRevision", "value": "ui"}, map[string]any{"name": "unlisted", "value": "must-not-appear"}, map[string]any{"name": "repo", "value": int64(42)}, nil}}, "status": map[string]any{"results": []any{map[string]any{"name": "text", "value": "verbatim\n界"}, map[string]any{"name": "array", "value": []any{"x", int64(2)}}, map[string]any{"name": "null", "value": nil}, map[string]any{"name": "obj", "value": map[string]any{"a": true}}, "invalid"}}}, {"spec": map[string]any{"params": "wrong"}}}
	for i, obj := range objects {
		u := unstructured.Unstructured{Object: obj}
		want, got := baselineTimingParams(u), timingParams(u)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("object%d original=%#v current=%#v", i, want, got)
		}
		w, _ := json.Marshal(want)
		g, _ := json.Marshal(got)
		if string(w) != string(g) {
			t.Fatalf("JSON differs object%d", i)
		}
		if _, ok := got["unlisted"]; ok {
			t.Fatalf("object%d: a parameter outside the list is exposed", i)
		}
	}
}
