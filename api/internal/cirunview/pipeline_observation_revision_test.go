package cirunview

// Moved from delivery on 2026-11-22: the parsing is cirunview's since
// 2026-11-17, so its tests live with it (delivery's tests no longer import
// cirunview; tests-reach row pipeline_steps_results_test.go).

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPipelineRunFromUnstructured_RevisionFromLabel(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "tekton.dev/v1",
			"kind":       "PipelineRun",
			"metadata": map[string]any{
				"name":      "deliver-platform-123",
				"namespace": "cicd",
				"labels": map[string]any{
					"tekton.dev/pipeline": "bifrost-deliver-platform",
					"bifrost.io/revision": "platform-v0.4.0",
				},
			},
			"spec": map[string]any{
				"pipelineRef": map[string]any{"name": "bifrost-deliver-platform"},
				"params": []any{
					map[string]any{"name": "revision", "value": "platform-v0.4.0"},
				},
			},
		},
	}
	view := PipelineRunObservation(obj, "bifrost-deliver-platform")
	if view.Revision != "platform-v0.4.0" {
		t.Fatalf("expected revision platform-v0.4.0, got %q", view.Revision)
	}
}

func TestPipelineRunFromUnstructured_RevisionFromParams(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "tekton.dev/v1",
			"kind":       "PipelineRun",
			"metadata": map[string]any{
				"name":      "deliver-platform-456",
				"namespace": "cicd",
			},
			"spec": map[string]any{
				"pipelineRef": map[string]any{"name": "bifrost-deliver-platform"},
				"params": []any{
					map[string]any{"name": "revision", "value": "main"},
				},
			},
		},
	}
	view := PipelineRunObservation(obj, "bifrost-deliver-platform")
	if view.Revision != "main" {
		t.Fatalf("expected revision main from params fallback, got %q", view.Revision)
	}
}

func TestPipelineRunFromUnstructured_NoRevision(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "tekton.dev/v1",
			"kind":       "PipelineRun",
			"metadata": map[string]any{
				"name":      "ci-python-789",
				"namespace": "cicd",
			},
			"spec": map[string]any{
				"pipelineRef": map[string]any{"name": "bifrost-ci-python"},
			},
		},
	}
	view := PipelineRunObservation(obj, "bifrost-ci-python")
	if view.Revision != "" {
		t.Fatalf("expected empty revision for CI run, got %q", view.Revision)
	}
}
