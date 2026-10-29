package delivery

import (
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// stgExecutionV1 is the immutable execution contract for note.stg_release v1.
// It is deliberately independent of the live Pipeline and shared builder:
// Prepare checks their output, while recovery checks the same requested
// fields plus the narrowly allowed admission defaults below. B5 v1 leaves
// only the pipeline timeout to Tekton; it never requests tasks/finally limits.
// Do not evolve these values in place for historical v1 runs; supporting a
// different contract requires a versioned identity agreed with B5-a/c.
// Return fresh JSON values so one caller cannot alter another's expectation.
func stgExecutionV1() map[string]any {
	return map[string]any{
		"taskRunSpecs": []any{
			map[string]any{"pipelineTaskName": "rollout", "serviceAccountName": "tekton-deliver"},
			map[string]any{"pipelineTaskName": "gitops-sync", "serviceAccountName": "tekton-deliver"},
			map[string]any{"pipelineTaskName": "agentrt-migrate", "serviceAccountName": "tekton-deliver"},
		},
		"taskRunTemplate": map[string]any{
			"serviceAccountName": "default",
			"podTemplate": map[string]any{
				"nodeSelector": map[string]any{"kubernetes.io/arch": "amd64"},
				"tolerations": []any{
					map[string]any{"key": "node-role.kubernetes.io/control-plane", "operator": "Exists", "effect": "NoSchedule"},
				},
			},
		},
		"workspaces": []any{
			map[string]any{
				"name": "build-context",
				"volumeClaimTemplate": map[string]any{
					"spec": map[string]any{
						"accessModes": []any{"ReadWriteOnce"}, "storageClassName": "local-path",
						"resources": map[string]any{"requests": map[string]any{"storage": "5Gi"}},
					},
				},
			},
		},
	}
}

func matchesSTGExecutionV1(obj *unstructured.Unstructured) bool {
	return matchesSTGExecution(obj, true)
}

func matchesSTGExecution(obj *unstructured.Unstructured, admitted bool) bool {
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return false
	}
	// These two fields are checked separately by the existing v1 identity.
	// Work on a copy: comparing an observed run must never rewrite evidence.
	delete(spec, "pipelineRef")
	delete(spec, "params")
	if admitted {
		// PipelineRunSpec.SetDefaults fills this one field from Tekton config
		// when the request omitted it. Its value is deliberately not inferred
		// from today's config during recovery. No other timeout field is a
		// default of our request; unknown keys and malformed values fail closed.
		if value, present := spec["timeouts"]; present {
			timeouts, ok := value.(map[string]any)
			if !ok || len(timeouts) != 1 {
				return false
			}
			pipeline, ok := timeouts["pipeline"].(string)
			duration, err := time.ParseDuration(pipeline)
			if !ok || err != nil || duration < 0 {
				return false
			}
			delete(spec, "timeouts")
		}
		// JSON encoding a core/v1 PVC template adds empty metadata/status.
		// Accept just those empty representations, never actual metadata or
		// volume state, and keep every workspace/spec field in the comparison.
		if workspaces, ok := spec["workspaces"].([]any); ok {
			for _, value := range workspaces {
				workspace, _ := value.(map[string]any)
				claim, _ := workspace["volumeClaimTemplate"].(map[string]any)
				if metadata, ok := claim["metadata"].(map[string]any); ok {
					if timestamp, present := metadata["creationTimestamp"]; present && timestamp == nil {
						delete(metadata, "creationTimestamp")
					}
					if len(metadata) == 0 {
						delete(claim, "metadata")
					}
				}
				if status, ok := claim["status"].(map[string]any); ok && len(status) == 0 {
					delete(claim, "status")
				}
			}
		}
	}
	return reflect.DeepEqual(spec, stgExecutionV1())
}
