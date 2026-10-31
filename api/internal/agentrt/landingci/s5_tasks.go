package landingci

import (
	"context"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// s5Stages are the pipeline's tasks: the release freeze check that gates
// the run, then the two mandatory S5 tasks (contract keys fetch/check).
var s5Stages = map[string]string{"freeze-check": "", "s5-fetch": "fetch", "s5-check": "check"}

const s5FreezeTask = "bifrost-release-freeze-check"

// s5Child resolves one childReferences entry of a verified run to its TaskRun
// and checks only that it is that run's live, controller-owned child.
func (c *Client) s5Child(ctx context.Context, d dynamic.Interface, run *unstructured.Unstructured, value any) (string, *unstructured.Unstructured, string, error) {
	child, _ := value.(map[string]any)
	stage, _ := child["pipelineTaskName"].(string)
	name, _ := child["name"].(string)
	if _, ok := s5Stages[stage]; !ok || name == "" || child["kind"] != "TaskRun" || child["apiVersion"] != "tekton.dev/v1" {
		return "", nil, "S5_CI_IDENTITY", nil
	}
	task, err := s5Resource(d, c.namespace(), "taskruns").Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return stage, nil, "S5_EVIDENCE_MISSING", nil
	}
	if err != nil {
		return stage, nil, "", err
	}
	if task.GetUID() == "" || task.GetDeletionTimestamp() != nil || !s5OwnedBy(task, run, "PipelineRun") {
		return stage, nil, "S5_CI_IDENTITY", nil
	}
	if child["uid"] != nil && child["uid"] != string(task.GetUID()) {
		return stage, nil, "S5_CI_IDENTITY", nil
	}
	return stage, task, "", nil
}

// s5FailedRun names why an identity-checked run failed (§5.1): a refused
// release freeze check never reached the mandatory tasks and is an
// infrastructure failure the operation may retry in a new generation; a
// failed s5-check is S5_BEHAVIOR; a failed fetch leaves the evidence
// missing. Anything else (the run's own timeout, cancellation) is
// S5_BEHAVIOR: the trusted suite did not complete.
func (c *Client) s5FailedRun(ctx context.Context, d dynamic.Interface, run *unstructured.Unstructured) (string, bool, error) {
	children, _, _ := unstructured.NestedSlice(run.Object, "status", "childReferences")
	failed := map[string]bool{}
	for _, value := range children {
		stage, task, reason, err := c.s5Child(ctx, d, run, value)
		if err != nil || reason == "S5_CI_IDENTITY" {
			return reason, false, err
		}
		if task != nil && runStatus(*task) == "Failed" {
			failed[stage] = true
		}
	}
	switch {
	case failed["freeze-check"] && !failed["s5-fetch"] && !failed["s5-check"]:
		return "S5_EVIDENCE_MISSING", true, nil
	case failed["s5-fetch"] && !failed["s5-check"]:
		return "S5_EVIDENCE_MISSING", false, nil
	}
	return "S5_BEHAVIOR", false, nil
}

func (c *Client) s5Tasks(ctx context.Context, d dynamic.Interface, run *unstructured.Unstructured, k S5CIKey, contract s5Contract) (map[string]string, string, error) {
	children, _, _ := unstructured.NestedSlice(run.Object, "status", "childReferences")
	if len(children) > len(s5Stages) {
		return nil, "S5_CI_IDENTITY", nil
	}
	if len(children) < len(s5Stages) {
		return nil, "S5_EVIDENCE_MISSING", nil
	}
	seen := map[string]bool{}
	var results map[string]string
	for _, value := range children {
		stage, task, reason, err := c.s5Child(ctx, d, run, value)
		if err != nil || reason != "" {
			return nil, reason, err
		}
		if seen[stage] {
			return nil, "S5_CI_IDENTITY", nil
		}
		seen[stage] = true
		if runStatus(*task) == "Failed" {
			return nil, "S5_BEHAVIOR", nil
		}
		if runStatus(*task) != "Succeeded" || !s5Completed(*task) {
			return nil, "S5_EVIDENCE_MISSING", nil
		}
		ref, _, _ := unstructured.NestedMap(task.Object, "spec", "taskRef")
		stageKey := s5Stages[stage]
		if stageKey == "" {
			// The freeze check carries no evidence: it has to be the pinned
			// task the verified pipeline names, to have passed, and to be
			// the only child that ran as default with a token.
			if !s5TaskRef(ref, s5FreezeTask) {
				return nil, "S5_CI_IDENTITY", nil
			}
			if reason, err := c.s5FreezePod(ctx, d, task, contract); reason != "" || err != nil {
				return nil, reason, err
			}
			continue
		}
		if !s5TaskRef(ref, "bifrost-s5-"+stageKey) {
			return nil, "S5_CI_IDENTITY", nil
		}
		spec, _, _ := unstructured.NestedMap(task.Object, "spec")
		// Admission/controller defaults are limited to these inert fields.
		allowed := map[string]bool{"taskRef": true, "params": true, "workspaces": true, "serviceAccountName": true,
			"podTemplate": true, "timeout": true, "retries": true}
		for field := range spec {
			if !allowed[field] {
				return nil, "S5_CI_IDENTITY", nil
			}
		}
		// The TaskRun's timeout is the pipeline task's (Tekton copies it,
		// normalized); a longer one is not the contract's run.
		want := s5PipelineTask(contract, stage)
		if spec["serviceAccountName"] != "bifrost-s5" || want == nil || want["timeout"] == nil || s5Duration(spec["timeout"]) != s5Duration(want["timeout"]) {
			return nil, "S5_CI_IDENTITY", nil
		}
		if retries, ok := spec["retries"]; ok && !s5Same(retries, 0) {
			return nil, "S5_CI_IDENTITY", nil
		}
		wantSA, wantPod := s5TaskRunTemplate(contract, stage)
		if wantSA != "bifrost-s5" || wantPod["automountServiceAccountToken"] != false || !s5Same(spec["podTemplate"], wantPod) {
			return nil, "S5_CI_IDENTITY", nil
		}
		p, counts := namedValues(*task, "spec", "params")
		wantParams := s5Params(k)
		wantParams["ciUID"] = string(run.GetUID())
		if !reflect.DeepEqual(p, wantParams) {
			return nil, "S5_CI_IDENTITY", nil
		}
		for _, n := range counts {
			if n != 1 {
				return nil, "S5_CI_IDENTITY", nil
			}
		}
		// status.taskSpec is the pinned Task with these exact parameters
		// substituted; anything else (inline task, command from R) differs.
		resolved, ok, _ := unstructured.NestedMap(task.Object, "status", "taskSpec")
		if !ok || !s5Resolved(resolved, s5Substitute(contract.Tasks[stageKey], p, nil)) {
			return nil, "S5_CI_IDENTITY", nil
		}
		if retries, _, _ := unstructured.NestedSlice(task.Object, "status", "retriesStatus"); len(retries) != 0 {
			return nil, "S5_CI_IDENTITY", nil
		}
		steps, _, _ := unstructured.NestedSlice(task.Object, "status", "steps")
		if len(steps) != 1 {
			return nil, "S5_EVIDENCE_MISSING", nil
		}
		step, _ := steps[0].(map[string]any)
		imageID, _ := step["imageID"].(string)
		terminated, _ := step["terminated"].(map[string]any)
		if step["name"] != stageKey || !s5Same(terminated["exitCode"], 0) || terminated["finishedAt"] == nil || !s5ImageID(imageID, k.CheckerImageDigest) {
			return nil, "S5_CI_IDENTITY", nil
		}
		if reason, err := c.s5Pod(ctx, d, run, task, stageKey, contract); reason != "" || err != nil {
			return nil, reason, err
		}
		if stageKey == "check" {
			results, counts = namedValues(*task, "status", "results")
			for _, n := range counts {
				if n != 1 {
					return nil, "S5_CI_IDENTITY", nil
				}
			}
		}
	}
	return results, "", nil
}

// s5FreezePod checks the freeze check's TaskRun and Pod ran as Tekton's
// merged freeze template says: the default ServiceAccount with its token
// (to read cicd/bifrost-release-freeze) and no claim of the trees. The S5
// tasks are held to bifrost-s5 without a token in s5Tasks/s5Pod.
func (c *Client) s5FreezePod(ctx context.Context, d dynamic.Interface, task *unstructured.Unstructured, contract s5Contract) (string, error) {
	wantSA, wantPod := s5TaskRunTemplate(contract, "freeze-check")
	spec, _, _ := unstructured.NestedMap(task.Object, "spec")
	if spec["serviceAccountName"] != wantSA || !s5Same(spec["podTemplate"], wantPod) || spec["timeout"] == nil {
		return "S5_CI_IDENTITY", nil
	}
	if _, ok := spec["workspaces"]; ok {
		return "S5_CI_IDENTITY", nil
	}
	name, _, _ := unstructured.NestedString(task.Object, "status", "podName")
	if name == "" {
		return "S5_EVIDENCE_MISSING", nil
	}
	pod, err := d.Resource(schema.GroupVersionResource{Version: "v1", Resource: "pods"}).Namespace(c.namespace()).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "S5_EVIDENCE_MISSING", nil
	}
	if err != nil {
		return "", err
	}
	ps, _, _ := unstructured.NestedMap(pod.Object, "spec")
	if !s5OwnedBy(pod, task, "TaskRun") || ps["serviceAccountName"] != wantSA || ps["automountServiceAccountToken"] != true {
		return "S5_CI_IDENTITY", nil
	}
	for _, v := range sliceS5(ps["volumes"]) {
		if _, ok := v.(map[string]any)["persistentVolumeClaim"]; ok {
			return "S5_CI_IDENTITY", nil
		}
	}
	return "", nil
}

func s5OwnedBy(child, parent *unstructured.Unstructured, kind string) bool {
	refs := child.GetOwnerReferences()
	return len(refs) == 1 && refs[0].UID == parent.GetUID() && refs[0].Name == parent.GetName() && refs[0].Kind == kind &&
		refs[0].APIVersion == "tekton.dev/v1" && refs[0].Controller != nil && *refs[0].Controller
}

// Verify the actual pod, including credentials/host mounts added outside the
// TaskSpec. Kubernetes status writers and the Tekton controller are trusted.
func (c *Client) s5Pod(ctx context.Context, d dynamic.Interface, run, task *unstructured.Unstructured, stage string, contract s5Contract) (string, error) {
	name, _, _ := unstructured.NestedString(task.Object, "status", "podName")
	if name == "" {
		return "S5_EVIDENCE_MISSING", nil
	}
	pod, err := d.Resource(schema.GroupVersionResource{Version: "v1", Resource: "pods"}).Namespace(c.namespace()).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "S5_EVIDENCE_MISSING", nil
	}
	if err != nil {
		return "", err
	}
	if !s5OwnedBy(pod, task, "TaskRun") {
		return "S5_CI_IDENTITY", nil
	}
	spec, _, _ := unstructured.NestedMap(pod.Object, "spec")
	if spec["serviceAccountName"] != "bifrost-s5" || spec["automountServiceAccountToken"] != false {
		return "S5_CI_IDENTITY", nil
	}
	for _, field := range []string{"hostNetwork", "hostPID", "hostIPC", "shareProcessNamespace"} {
		if v, ok := spec[field]; ok && v != false {
			return "S5_CI_IDENTITY", nil
		}
	}
	for _, field := range []string{"imagePullSecrets", "ephemeralContainers"} {
		if values, _ := spec[field].([]any); len(values) != 0 {
			return "S5_CI_IDENTITY", nil
		}
	}
	volumes, _ := spec["volumes"].([]any)
	workspaces, _, _ := unstructured.NestedSlice(task.Object, "spec", "workspaces")
	if len(workspaces) != 1 {
		return "S5_CI_IDENTITY", nil
	}
	workspace, _ := workspaces[0].(map[string]any)
	pvc, _ := workspace["persistentVolumeClaim"].(map[string]any)
	claim, _ := pvc["claimName"].(string)
	if claim == "" || workspace["name"] != "trees" || len(workspace) != 2 || len(pvc) != 1 {
		return "S5_CI_IDENTITY", nil
	}
	volumeClaim, err := d.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(c.namespace()).Get(ctx, claim, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "S5_EVIDENCE_MISSING", nil
	}
	if err != nil {
		return "", err
	}
	if !s5OwnedBy(volumeClaim, run, "PipelineRun") {
		return "S5_CI_IDENTITY", nil
	}
	treeVolume := ""
	emptyDirs := map[string]bool{}
	for _, v := range volumes {
		volume, _ := v.(map[string]any)
		for field := range volume {
			if field != "name" && field != "emptyDir" && field != "persistentVolumeClaim" && field != "downwardAPI" {
				return "S5_CI_IDENTITY", nil
			}
		}
		name, _ := volume["name"].(string)
		if _, ok := volume["emptyDir"]; ok && len(volume) == 2 {
			emptyDirs[name] = true
		}
		if binding, ok := volume["persistentVolumeClaim"].(map[string]any); ok {
			if treeVolume != "" || binding["claimName"] != claim {
				return "S5_CI_IDENTITY", nil
			}
			treeVolume = name
		}
	}
	if treeVolume == "" {
		return "S5_CI_IDENTITY", nil
	}
	containers, _ := spec["containers"].([]any)
	if len(containers) != 1 {
		return "S5_CI_IDENTITY", nil
	}
	container, _ := containers[0].(map[string]any)
	trustedStep := contract.Tasks[stage]["steps"].([]any)[0].(map[string]any)
	if container["name"] != "step-"+stage || container["image"] != contract.Image || !s5Same(container["securityContext"], trustedStep["securityContext"]) {
		return "S5_CI_IDENTITY", nil
	}
	if envFrom, _ := container["envFrom"].([]any); len(envFrom) != 0 {
		return "S5_CI_IDENTITY", nil
	}
	params, _ := namedValues(*task, "spec", "params")
	// Each variable is exactly the trusted step's: a literal with this run's
	// parameters substituted, or (fetch only, in the contract) the pinned
	// read-only Gitea secret reference. No other source is accepted.
	wantEnv := map[string]any{}
	for _, value := range sliceS5(s5Substitute(trustedStep["env"], params, nil)) {
		e := value.(map[string]any)
		name, _ := e["name"].(string)
		delete(e, "name")
		wantEnv[name] = e
	}
	seenEnv := map[string]bool{}
	for _, v := range sliceS5(container["env"]) {
		env, _ := v.(map[string]any)
		name, _ := env["name"].(string)
		if seenEnv[name] {
			return "S5_CI_IDENTITY", nil
		}
		seenEnv[name] = true
		got := map[string]any{}
		for field, value := range env {
			if field != "name" {
				got[field] = value
			}
		}
		want, ok := wantEnv[name]
		if !ok || !s5Same(got, want) {
			return "S5_CI_IDENTITY", nil
		}
	}
	if len(seenEnv) != len(wantEnv) {
		return "S5_CI_IDENTITY", nil
	}
	readOnly := stage == "fetch"
	for _, v := range sliceS5(container["volumeMounts"]) {
		mount, _ := v.(map[string]any)
		path, _ := mount["mountPath"].(string)
		volume, _ := mount["name"].(string)
		if _, ok := mount["subPath"]; ok {
			return "S5_CI_IDENTITY", nil
		}
		switch {
		case path == "/workspace/trees", strings.HasPrefix(path, "/tekton/"):
		case path == "/tmp" && emptyDirs[volume]:
		// Tekton's own working directory: an emptyDir it adds to every step.
		case path == "/workspace" && volume == "tekton-internal-workspace" && emptyDirs[volume]:
		default:
			return "S5_CI_IDENTITY", nil
		}
		if mount["name"] == treeVolume && path != "/workspace/trees" {
			return "S5_CI_IDENTITY", nil
		}
		if path == "/workspace/trees" && mount["name"] != treeVolume {
			return "S5_CI_IDENTITY", nil
		}
		if mount["mountPath"] == "/workspace/trees" && mount["readOnly"] == true {
			readOnly = true
		}
	}
	if !readOnly {
		return "S5_CI_IDENTITY", nil
	}
	return "", nil
}

func sliceS5(v any) []any { values, _ := v.([]any); return values }
