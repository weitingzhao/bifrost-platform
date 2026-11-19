package subscribe

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// 2026-11-17: the refused landing's reason is the land TaskRun's result
// "refusal" (pipeline-land.yaml, CI line 08:53); the wake reads it there.
func TestTaskRunResult(t *testing.T) {
	tr := unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"params": []any{
		map[string]any{"name": "branch", "value": "refs/heads/ready/x"},
	}}, "status": map[string]any{"results": []any{
		map[string]any{"name": "outcome", "value": ""},
		map[string]any{"name": "refusal", "value": "refused: commit bc6b49b22fa9 has no Change-Id trailer"},
	}}}}
	if got := taskRunValue([]unstructured.Unstructured{{Object: map[string]any{}}, tr}, "refusal"); got != "refused: commit bc6b49b22fa9 has no Change-Id trailer" {
		t.Fatalf("got %q", got)
	}
	// A name the task did not write as a result is read from its params
	// (the tripwire's branch, 2026-11-17).
	if got := taskRunValue([]unstructured.Unstructured{tr}, "branch"); got != "refs/heads/ready/x" {
		t.Fatalf("a param: %q", got)
	}
	if got := taskRunValue([]unstructured.Unstructured{tr}, "absent"); got != "" {
		t.Fatalf("a result the task did not write: %q", got)
	}
}
