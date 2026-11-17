//bifrost:owner coordination
package server

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// 2026-11-17: the refused landing's reason is the land TaskRun's result
// "refusal" (pipeline-land.yaml, CI line 08:53); the wake reads it there.
func TestTaskRunResult(t *testing.T) {
	tr := unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"results": []any{
		map[string]any{"name": "outcome", "value": ""},
		map[string]any{"name": "refusal", "value": "refused: commit bc6b49b22fa9 has no Change-Id trailer"},
	}}}}
	if got := taskRunResult([]unstructured.Unstructured{{Object: map[string]any{}}, tr}, "refusal"); got != "refused: commit bc6b49b22fa9 has no Change-Id trailer" {
		t.Fatalf("got %q", got)
	}
	if got := taskRunResult([]unstructured.Unstructured{tr}, "absent"); got != "" {
		t.Fatalf("a result the task did not write: %q", got)
	}
}
