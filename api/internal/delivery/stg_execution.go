package delivery

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// stgExecutionV1 is the immutable execution contract for note.stg_release v1.
// It is deliberately independent of the live Pipeline and shared builder:
// Prepare checks their output, while every recovery path checks this same
// contract. A builder/declaration change fails closed before Create.
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
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return false
	}
	// These two fields are checked separately by the existing v1 identity.
	// All remaining spec fields are execution-affecting and must match;
	// metadata and status added by Tekton are outside this comparison.
	delete(spec, "pipelineRef")
	delete(spec, "params")
	return reflect.DeepEqual(spec, stgExecutionV1())
}
