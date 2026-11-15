package delivery

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (o STGRunObservation) archivedResult(task, taskRef, result string) (string, error) {
	if o.Run == nil {
		return "", ErrSTGEvidence
	}
	definitions, _, err := unstructured.NestedSlice(o.Run.Object, "status", "pipelineSpec", "tasks")
	if err != nil {
		return "", err
	}
	count := 0
	for _, value := range definitions {
		def, _ := value.(map[string]any)
		if def["name"] != task {
			continue
		}
		count++
		ref, _ := def["taskRef"].(map[string]any)
		if def["taskSpec"] != nil || !localTaskRef(ref, taskRef) {
			return "", ErrSTGEvidence
		}
	}
	child := o.Children[task]
	if count != 1 || child == nil || stgRunStatus(child) != "Succeeded" {
		return "", ErrSTGEvidence
	}
	ref, _, _ := unstructured.NestedMap(child.Object, "spec", "taskRef")
	_, inline, _ := unstructured.NestedFieldNoCopy(child.Object, "spec", "taskSpec")
	if inline || !localTaskRef(ref, taskRef) {
		return "", ErrSTGEvidence
	}
	values, err := namedStrings(child, "status", "results")
	if err != nil || values[result] == "" {
		return "", fmt.Errorf("%w: %s result", ErrSTGEvidence, task)
	}
	return values[result], nil
}
