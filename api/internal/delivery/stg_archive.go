package delivery

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RestoreSTGRun reads only the durable archive, so verification survives both
// finalizer removal and Tekton cleanup. It rechecks the child identity chain;
// the archive's arbitrary JSON is never trusted as a precomputed verdict.
func RestoreSTGRun(id STGRunIdentity, status string, raw map[string]any) (STGRunObservation, error) {
	out := STGRunObservation{Identity: id, Status: status, Raw: raw, Children: map[string]*unstructured.Unstructured{}}
	refs, ok := raw["child_refs"].([]any)
	if !ok || (status != "Succeeded" && status != "Failed") {
		return out, ErrSTGEvidence
	}
	names := map[string]bool{}
	for _, value := range refs {
		ref, ok := value.(map[string]any)
		name, _ := ref["name"].(string)
		task, _ := ref["pipelineTaskName"].(string)
		childRaw, has := ref["task_run"].(map[string]any)
		if !ok || !has || name == "" || task == "" || names[name] || out.Children[task] != nil || ref["kind"] != "TaskRun" || ref["apiVersion"] != "tekton.dev/v1" {
			return out, ErrSTGEvidence
		}
		child := &unstructured.Unstructured{Object: childRaw}
		owner := metav1.GetControllerOf(child)
		if child.GetUID() == "" || child.GetName() != name || child.GetNamespace() != id.Namespace || owner == nil || owner.Kind != "PipelineRun" || owner.APIVersion != "tekton.dev/v1" || owner.Name != id.Name || owner.UID != id.UID {
			return out, ErrSTGEvidence
		}
		if uid, exists := ref["uid"]; exists && uid != string(child.GetUID()) {
			return out, ErrSTGCollision
		}
		if label := child.GetLabels()["tekton.dev/pipelineTask"]; label != "" && label != task {
			return out, ErrSTGEvidence
		}
		if run, exists := ref["pipeline_run"]; exists {
			obj, valid := run.(map[string]any)
			if !valid || out.Run != nil {
				return out, ErrSTGEvidence
			}
			out.Run = &unstructured.Unstructured{Object: obj}
		}
		names[name], out.Children[task] = true, child
	}
	// An empty failed archive supplies neither clone nor build evidence.
	// Input verification must keep it unknown, even on an unchanged baseline.
	if len(refs) == 0 && status == "Failed" {
		return out, nil
	}
	if out.Run == nil || out.Run.GetUID() != id.UID || out.Run.GetName() != id.Name || out.Run.GetNamespace() != id.Namespace || stgRunStatus(out.Run) != status {
		return out, ErrSTGEvidence
	}
	original, _, err := unstructured.NestedSlice(out.Run.Object, "status", "childReferences")
	if err != nil || len(original) != len(refs) {
		return out, ErrSTGEvidence
	}
	seen := map[string]bool{}
	for _, value := range original {
		ref, _ := value.(map[string]any)
		task, _ := ref["pipelineTaskName"].(string)
		child := out.Children[task]
		if child == nil || seen[task] || ref["name"] != child.GetName() || ref["kind"] != "TaskRun" || ref["apiVersion"] != "tekton.dev/v1" {
			return out, ErrSTGEvidence
		}
		if uid, exists := ref["uid"]; exists && uid != string(child.GetUID()) {
			return out, ErrSTGCollision
		}
		seen[task] = true
	}
	return out, nil
}

// BuildDigests accepts independently completed build tasks even if rollout or
// a later task failed. Missing/invalid builds yield no new digest: they cannot
// justify treating an unexplained deployment as this try's effect.
func (o STGRunObservation) BuildDigests() (api, console string) {
	for _, check := range []struct{ task, ref string }{
		{"build-platform-api", "bifrost-kaniko-platform-api-stg"},
		{"build-platform-console", "bifrost-kaniko-platform-console-stg"},
	} {
		value, err := o.archivedResult(check.task, check.ref, "digest")
		if err != nil || !stgDigest.MatchString(value) {
			continue
		}
		if check.task == "build-platform-api" {
			api = value
		} else {
			console = value
		}
	}
	return
}

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
