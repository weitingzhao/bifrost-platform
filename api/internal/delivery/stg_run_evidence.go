package delivery

import (
	"context"
	"fmt"
	"reflect"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

var stgDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type STGRunIdentity struct {
	Namespace, Name string
	UID             types.UID
}
type STGRunObservation struct {
	Identity STGRunIdentity
	Status   string
	Run      *unstructured.Unstructured
	Children map[string]*unstructured.Unstructured // pipelineTaskName -> TaskRun
	Raw      map[string]any                        // B5-a run_evidence.raw; child_refs includes full TaskRuns
}
type STGContent struct{ P, U, Infra, APIDigest, ConsoleDigest string }

// ObserveRun reads children by childReferences, never by label or name prefix.
// Raw keeps each child's spec/status/metadata under child_refs[].task_run;
// it fits the closed B5-a top-level schema and survives Tekton/Pod cleanup.
func (a *STGReleaseAdapter) ObserveRun(ctx context.Context, id STGRunIdentity) (STGRunObservation, error) {
	out := STGRunObservation{Identity: id, Children: map[string]*unstructured.Unstructured{}}
	if id.Namespace != STGReleaseNamespace || id.Name == "" || id.UID == "" {
		return out, ErrSTGEvidence
	}
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return out, err
	}
	runs := dyn.Resource(pipelineRunGVR).Namespace(id.Namespace)
	run, err := runs.Get(ctx, id.Name, metav1.GetOptions{})
	if err != nil {
		return out, err
	}
	if run.GetUID() != id.UID {
		return out, ErrSTGCollision
	}
	out.Run, out.Status = run, stgRunStatus(run)
	refs, _, err := unstructured.NestedSlice(run.Object, "status", "childReferences")
	if err != nil {
		return out, fmt.Errorf("%w: childReferences", ErrSTGEvidence)
	}
	rawRefs := []any{}
	names := map[string]bool{}
	for _, item := range refs {
		ref, ok := item.(map[string]any)
		name, _ := ref["name"].(string)
		task, _ := ref["pipelineTaskName"].(string)
		if !ok || name == "" || task == "" || ref["kind"] != "TaskRun" || ref["apiVersion"] != "tekton.dev/v1" || names[name] || out.Children[task] != nil {
			return out, fmt.Errorf("%w: ambiguous child reference", ErrSTGEvidence)
		}
		names[name] = true
		child, e := dyn.Resource(taskRunGVR).Namespace(id.Namespace).Get(ctx, name, metav1.GetOptions{})
		if e != nil {
			return out, e
		}
		owner := metav1.GetControllerOf(child)
		if child.GetUID() == "" || owner == nil || owner.Kind != "PipelineRun" || owner.APIVersion != "tekton.dev/v1" || owner.Name != id.Name || owner.UID != id.UID || child.GetName() != name || child.GetNamespace() != id.Namespace {
			return out, fmt.Errorf("%w: child owner", ErrSTGEvidence)
		}
		if uid, exists := ref["uid"]; exists && uid != string(child.GetUID()) {
			return out, ErrSTGCollision
		}
		if label := child.GetLabels()["tekton.dev/pipelineTask"]; label != "" && label != task {
			return out, ErrSTGEvidence
		}
		out.Children[task] = child.DeepCopy()
		copy := map[string]any{}
		for k, v := range ref {
			copy[k] = v
		}
		copy["task_run"] = child.DeepCopy().Object
		// Keep the executed Pipeline spec and run identity once. B5-a's raw
		// object has exactly three array keys; enriching a child preserves
		// that protocol. With no children only run_failed can be concluded.
		if len(rawRefs) == 0 {
			copy["pipeline_run"] = run.DeepCopy().Object
		}
		rawRefs = append(rawRefs, copy)
	}
	// A concurrent status/child update invalidates this observation. The
	// caller retries a read; no partial archive is returned as successful.
	after, err := runs.Get(ctx, id.Name, metav1.GetOptions{})
	if err != nil {
		return out, err
	}
	if after.GetUID() != id.UID || after.GetResourceVersion() != run.GetResourceVersion() {
		return out, ErrSTGUnknown
	}
	results, _, err := unstructured.NestedSlice(run.Object, "status", "results")
	if err != nil {
		return out, ErrSTGEvidence
	}
	conds, _, err := unstructured.NestedSlice(run.Object, "status", "conditions")
	if err != nil {
		return out, ErrSTGEvidence
	}
	if results == nil {
		results = []any{}
	}
	if conds == nil {
		conds = []any{}
	}
	out.Raw = map[string]any{"results": results, "child_refs": rawRefs, "conditions": conds}
	return out, nil
}

func stgRunStatus(run *unstructured.Unstructured) string {
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	count, status := 0, "Unknown"
	for _, item := range conditions {
		m, _ := item.(map[string]any)
		if m["type"] != "Succeeded" {
			continue
		}
		count++
		switch m["status"] {
		case "True":
			status = "Succeeded"
		case "False":
			status = "Failed"
		case "Unknown":
			status = "Running"
		}
	}
	if count != 1 {
		return "Unknown"
	}
	return status
}

// Content accepts only the run's embedded Pipeline definition and actual
// child results. Today's mutable Pipeline is not execution evidence.
func (o STGRunObservation) Content(expected STGContent) (STGContent, error) {
	out := STGContent{}
	if o.Run == nil || o.Status != "Succeeded" {
		return out, ErrSTGEvidence
	}
	ref, _, _ := unstructured.NestedMap(o.Run.Object, "spec", "pipelineRef")
	_, inline, _ := unstructured.NestedFieldNoCopy(o.Run.Object, "spec", "pipelineSpec")
	if inline || !reflect.DeepEqual(ref, map[string]any{"name": STGReleasePipeline}) {
		return out, ErrSTGEvidence
	}
	if _, err := namedStrings(o.Run, "status", "results"); err != nil {
		return out, err
	}
	definitions, _, err := unstructured.NestedSlice(o.Run.Object, "status", "pipelineSpec", "tasks")
	if err != nil || len(definitions) == 0 {
		return out, fmt.Errorf("%w: missing executed Pipeline", ErrSTGEvidence)
	}
	defs := map[string]map[string]any{}
	for _, item := range definitions {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		if name == "" || defs[name] != nil {
			return out, ErrSTGEvidence
		}
		defs[name] = m
	}
	checks := []struct{ task, ref, repo, result string }{
		{"clone-platform", "bifrost-git-clone-gitea", "bifrost-platform", "commit"},
		{"clone-ui", "bifrost-git-clone-gitea", "bifrost-ui", "commit"},
		{"clone-infra", "bifrost-git-clone-gitea", "bifrost-trade-infra", "commit"},
		{"build-platform-api", "bifrost-kaniko-platform-api-stg", "", "digest"},
		{"build-platform-console", "bifrost-kaniko-platform-console-stg", "", "digest"},
		{"gitops-sync", "bifrost-argocd-pin-sync-platform", "", ""},
	}
	values := map[string]string{}
	for _, check := range checks {
		child := o.Children[check.task]
		def := defs[check.task]
		declared, _ := def["taskRef"].(map[string]any)
		if child == nil || def["taskSpec"] != nil || !localTaskRef(declared, check.ref) || stgRunStatus(child) != "Succeeded" {
			return out, fmt.Errorf("%w: %s", ErrSTGEvidence, check.task)
		}
		actual, _, _ := unstructured.NestedMap(child.Object, "spec", "taskRef")
		_, inline, _ := unstructured.NestedFieldNoCopy(child.Object, "spec", "taskSpec")
		if inline || !localTaskRef(actual, check.ref) {
			return out, fmt.Errorf("%w: taskRef %s", ErrSTGEvidence, check.task)
		}
		params, e := namedStrings(child, "spec", "params")
		if e != nil {
			return out, e
		}
		results, e := namedStrings(child, "status", "results")
		if e != nil {
			return out, e
		}
		if check.repo != "" && params["url"] != "http://gitea.cicd.svc.cluster.local:3000/bifrost/"+check.repo+".git" {
			return out, fmt.Errorf("%w: clone URL", ErrSTGEvidence)
		}
		if check.result != "" {
			values[check.task] = results[check.result]
		}
	}
	out = STGContent{P: values["clone-platform"], U: values["clone-ui"], Infra: values["clone-infra"], APIDigest: values["build-platform-api"], ConsoleDigest: values["build-platform-console"]}
	if !callerParamSHA.MatchString(out.P) || !callerParamSHA.MatchString(out.U) || !callerParamSHA.MatchString(out.Infra) || !stgDigest.MatchString(out.APIDigest) || !stgDigest.MatchString(out.ConsoleDigest) {
		return STGContent{}, fmt.Errorf("%w: invalid clone/build results", ErrSTGEvidence)
	}
	if out.P != expected.P || out.U != expected.U || out.Infra != expected.Infra {
		return STGContent{}, fmt.Errorf("%w: wrong_input", ErrSTGEvidence)
	}
	params, _ := namedStrings(o.Children["gitops-sync"], "spec", "params")
	if params["application"] != stgNamespace || params["apiDigest"] != out.APIDigest || params["consoleDigest"] != out.ConsoleDigest || params["deliverRun"] != o.Identity.Name {
		return STGContent{}, fmt.Errorf("%w: pin-sync inputs", ErrSTGEvidence)
	}
	return out, nil
}

func localTaskRef(ref map[string]any, name string) bool {
	if ref["name"] != name {
		return false
	}
	for k, v := range ref {
		if k != "name" && (k != "kind" || v != "Task") {
			return false
		}
	}
	return true
}
