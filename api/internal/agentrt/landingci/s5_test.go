package landingci

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func s5TestKey() S5CIKey {
	h := strings.Repeat("a", 64)
	// A key the landing store can persist: closed env/tenant, ULID task and
	// attempt, op bound to the task (the shared landing.S5CIKey contract).
	return S5CIKey{Schema: 2, Env: "stg", Tenant: "dev", Repo: "bifrost-platform", Task: "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", Attempt: "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", Op: "land:t_01ARZ3NDEKTSV4RRFFQ69G5FAV:1",
		Base: strings.Repeat("1", 40), A: strings.Repeat("2", 40), M0: strings.Repeat("3", 40), R: strings.Repeat("4", 40),
		BaselineTree: strings.Repeat("5", 40), ResultTree: strings.Repeat("6", 40), UISHA: strings.Repeat("7", 40), RuleVersion: 3,
		RuleDigest: h, ProfileDigest: h, CheckerImageDigest: "sha256:" + h, ToolchainDigest: h, CallersDigest: h, TypesDigest: h, TestsDigest: h,
		ReviewBindingDigest: h, PatchEvidenceDigest: h, Generation: 1}
}
func s5TestContract(t *testing.T) s5Contract {
	t.Helper()
	var c s5Contract
	raw := strings.ReplaceAll(string(s5ContractJSON), "sha256:"+strings.Repeat("0", 64), s5TestKey().CheckerImageDigest)
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	k := s5TestKey()
	c.Materials = &s5Materials{ProfileDigest: k.ProfileDigest, ToolchainDigest: k.ToolchainDigest, CallersDigest: k.CallersDigest, TypesDigest: k.TypesDigest, TestsDigest: k.TestsDigest, UISHA: k.UISHA}
	return c
}
func s5TestStatus() map[string]any {
	return map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}, "startTime": "2026-10-30T12:00:01Z", "completionTime": "2026-10-30T12:01:00Z"}
}
func s5Object(kind, name, uid string, spec, status map[string]any) *unstructured.Unstructured {
	version := "tekton.dev/v1"
	if kind == "Pod" || kind == "PersistentVolumeClaim" {
		version = "v1"
	}
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": version, "kind": kind, "metadata": map[string]any{"name": name, "namespace": "cicd", "uid": uid, "creationTimestamp": "2026-10-30T12:00:00Z"}, "spec": spec, "status": status}}
}
func s5Own(child, parent *unstructured.Unstructured) {
	yes := true
	child.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "tekton.dev/v1", Kind: parent.GetKind(), Name: parent.GetName(), UID: parent.GetUID(), Controller: &yes}})
}
func s5TestResults(k S5CIKey, uid string) map[string]string {
	e := s5Bindings(k, uid)
	e["passed"] = true
	e["findings"] = []any{}
	e["files"] = []any{map[string]any{"path": "console/src/lib/patrol/cronSchedule.ts", "beforeBlob": k.Base, "afterBlob": k.R}}
	raw, _ := canonicalS5(e)
	d, _ := k.Digest()
	return map[string]string{"base-commit": k.M0, "revision-commit": k.R, "baseline-tree": k.BaselineTree, "result-tree": k.ResultTree, "ui-commit": k.UISHA, "key-digest": d, "generation": s5Params(k)["generation"], "evidence": string(raw), "evidence-digest": s5Hash(raw)}
}

// tektonStored applies, independently of the reader's normalization, what
// Tekton v1.6 was observed to store for a run of this contract (see
// testdata/tekton-real-landing-ci.json and TestS5RealTekton*): parameters and
// context substituted, taskRef kind defaulted, metav1.Duration encoding,
// pipeline results description "", workspace readOnly false dropped.
func tektonStored(t *testing.T, v any, replace map[string]string) any {
	t.Helper()
	b, _ := json.Marshal(v)
	text := string(b)
	for from, to := range replace {
		quoted, _ := json.Marshal(to)
		text = strings.ReplaceAll(text, from, string(quoted[1:len(quoted)-1]))
	}
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	var walk func(any, string)
	walk = func(v any, key string) {
		switch x := v.(type) {
		case map[string]any:
			for k, value := range x {
				walk(value, k)
			}
			if key == "taskRef" {
				x["kind"] = "Task"
			}
			if d, ok := map[any]string{"600s": "10m0s", "1200s": "20m0s"}[x["timeout"]]; ok {
				x["timeout"] = d
			}
			if x["readOnly"] == false {
				delete(x, "readOnly")
			}
			if key == "results" {
				if _, ok := x["value"]; ok {
					x["description"] = ""
				}
			}
		case []any:
			for _, value := range x {
				walk(value, key)
			}
		}
	}
	walk(out, "")
	return out
}

// tektonVolumes are the Tekton-owned volumes and step mounts of the real
// clone Pod (testdata), minus its token and workspace volumes.
func tektonVolumes(t *testing.T) ([]any, []any) {
	t.Helper()
	pod := s5RealFixture(t)["pod"].(map[string]any)["spec"].(map[string]any)
	volumes := []any{}
	for _, v := range pod["volumes"].([]any) {
		name := v.(map[string]any)["name"].(string)
		if strings.HasPrefix(name, "tekton-") {
			volumes = append(volumes, v)
		}
	}
	mounts := []any{}
	for _, m := range pod["containers"].([]any)[0].(map[string]any)["volumeMounts"].([]any) {
		name := m.(map[string]any)["name"].(string)
		if strings.HasPrefix(name, "tekton-") {
			mounts = append(mounts, m)
		}
	}
	return volumes, mounts
}

// Both sides of every mutation start with the same real embedded contract,
// in the shape Tekton stores it: three children (freeze-check first),
// resolved specs, normalized durations, round-tripped claim template and the
// Pod's Tekton-owned volumes and mounts.
func s5Fixture(t *testing.T, k S5CIKey) (s5Contract, S5RunIdentity, []*unstructured.Unstructured) {
	t.Helper()
	c := s5TestContract(t)
	name, _ := k.RunName()
	digest, _ := k.Digest()
	id := S5RunIdentity{Run: name, UID: "server-run-uid", KeyDigest: digest, Generation: k.Generation}
	status := s5TestStatus()
	replace := map[string]string{"$(context.pipelineRun.uid)": id.UID}
	for p, v := range s5Params(k) {
		replace["$(params."+p+")"] = v
	}
	status["pipelineSpec"] = tektonStored(t, c.Pipeline, replace)
	status["results"] = s5Named(s5TestResults(k, id.UID))
	spec := s5RunSpec(k, c)
	spec["timeouts"] = map[string]any{"pipeline": "20m0s"}
	ws := spec["workspaces"].([]any)[0].(map[string]any)["volumeClaimTemplate"].(map[string]any)
	ws["metadata"], ws["status"] = map[string]any{"creationTimestamp": nil}, map[string]any{}
	run := s5Object("PipelineRun", name, id.UID, spec, status)
	objects := []*unstructured.Unstructured{run}
	children := []any{map[string]any{"name": name + "-freeze-check", "kind": "TaskRun", "apiVersion": "tekton.dev/v1", "pipelineTaskName": "freeze-check"}}
	internalVolumes, internalMounts := tektonVolumes(t)
	for _, stage := range []string{"fetch", "check"} {
		name := "task-" + stage
		params := s5Params(k)
		params["ciUID"] = id.UID
		template := c.RunTemplate["taskRunTemplate"].(map[string]any)
		spec := map[string]any{"taskRef": map[string]any{"kind": "Task", "name": "bifrost-s5-" + stage}, "params": s5Named(params), "serviceAccountName": "bifrost-s5", "timeout": "10m0s", "podTemplate": template["podTemplate"], "workspaces": []any{map[string]any{"name": "trees", "persistentVolumeClaim": map[string]any{"claimName": "trees"}}}}
		st := s5TestStatus()
		taskReplace := map[string]string{}
		for p, v := range params {
			taskReplace["$(params."+p+")"] = v
		}
		st["taskSpec"] = tektonStored(t, c.Tasks[stage], taskReplace)
		st["podName"] = "pod-" + stage
		st["steps"] = []any{map[string]any{"name": stage, "container": "step-" + stage, "imageID": c.Image, "terminated": map[string]any{"exitCode": int64(0), "finishedAt": "2026-10-30T12:01:00Z", "reason": "Completed"}}}
		if stage == "check" {
			st["results"] = s5Named(s5TestResults(k, id.UID))
		}
		task := s5Object("TaskRun", name, "uid-"+name, spec, st)
		s5Own(task, run)
		step := c.Tasks[stage]["steps"].([]any)[0].(map[string]any)
		env := []any{}
		for _, v := range step["env"].([]any) {
			e := v.(map[string]any)
			if value, ok := e["value"].(string); ok {
				for p, x := range params {
					value = strings.ReplaceAll(value, "$(params."+p+")", x)
				}
				env = append(env, map[string]any{"name": e["name"], "value": value})
			} else {
				env = append(env, e)
			}
		}
		volumes := append([]any{map[string]any{"name": "scratch", "emptyDir": map[string]any{"sizeLimit": "128Mi"}}, map[string]any{"name": "ws-s5tree", "persistentVolumeClaim": map[string]any{"claimName": "trees"}}}, internalVolumes...)
		treeMount := map[string]any{"name": "ws-s5tree", "mountPath": "/workspace/trees"}
		if stage == "check" {
			treeMount["readOnly"] = true
		}
		mounts := append([]any{treeMount, map[string]any{"name": "scratch", "mountPath": "/tmp"}}, internalMounts...)
		ps := map[string]any{"serviceAccountName": "bifrost-s5", "automountServiceAccountToken": false, "volumes": volumes, "containers": []any{map[string]any{"name": "step-" + stage, "image": c.Image, "securityContext": step["securityContext"], "env": env, "volumeMounts": mounts}}}
		pod := s5Object("Pod", "pod-"+stage, "uid-pod-"+stage, ps, map[string]any{})
		s5Own(pod, task)
		children = append(children, map[string]any{"name": name, "kind": "TaskRun", "apiVersion": "tekton.dev/v1", "pipelineTaskName": "s5-" + stage})
		objects = append(objects, task, pod)
	}
	status["childReferences"] = children
	claim := s5Object("PersistentVolumeClaim", "trees", "uid-trees", map[string]any{}, map[string]any{})
	s5Own(claim, run)
	// Tekton v1.6.0 runs the freeze check with its taskRunSpecs entry merged
	// field by field over taskRunTemplate (written out here, not computed by
	// the reader's helper): default SA, and the token the override grants.
	freezePodTemplate := map[string]any{}
	for k, v := range c.RunTemplate["taskRunTemplate"].(map[string]any)["podTemplate"].(map[string]any) {
		freezePodTemplate[k] = v
	}
	for k, v := range c.RunTemplate["taskRunSpecs"].([]any)[0].(map[string]any)["podTemplate"].(map[string]any) {
		freezePodTemplate[k] = v
	}
	freezeStatus := s5TestStatus()
	freezeStatus["podName"] = "pod-freeze"
	freeze := s5Object("TaskRun", name+"-freeze-check", "uid-freeze", map[string]any{"taskRef": map[string]any{"kind": "Task", "name": "bifrost-release-freeze-check"},
		"serviceAccountName": "default", "podTemplate": freezePodTemplate, "timeout": "20m0s"}, freezeStatus)
	s5Own(freeze, run)
	freezePod := s5Object("Pod", "pod-freeze", "uid-pod-freeze", map[string]any{"serviceAccountName": "default", "automountServiceAccountToken": true,
		"volumes": []any{map[string]any{"name": "program", "configMap": map[string]any{"name": "bifrost-release-freeze-check"}}}}, map[string]any{})
	s5Own(freezePod, freeze)
	objects = append(objects, claim, freeze, freezePod)
	// Round-trip fixtures to the same JSON types as a dynamic Kubernetes client.
	for i, o := range objects {
		b, _ := json.Marshal(o.Object)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		objects[i] = &unstructured.Unstructured{Object: v}
	}
	return c, id, objects
}
func s5TestClient(objects ...*unstructured.Unstructured) (*Client, *dynfake.FakeDynamicClient) {
	objs := []runtime.Object{}
	for _, o := range objects {
		objs = append(objs, o)
	}
	d := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, objs...)
	return &Client{Dynamic: func() (dynamic.Interface, error) { return d, nil }}, d
}
func s5ReadFixture(t *testing.T, k S5CIKey, c s5Contract, id S5RunIdentity, objects []*unstructured.Unstructured) S5Evidence {
	t.Helper()
	client, _ := s5TestClient(objects...)
	got, err := client.readS5(context.Background(), k, id, c)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func TestS5ReadRejectsUntrustedEvidence(t *testing.T) {
	cases := map[string]func(*testing.T, []*unstructured.Unstructured){
		"inline pipeline": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[0], map[string]any{}, "spec", "pipelineSpec")
		},
		"forged labels": func(t *testing.T, o []*unstructured.Unstructured) {
			o[0].SetLabels(map[string]string{"tekton.dev/pipeline": "bifrost-ci-s5", "bifrost.io/repo": "bifrost-platform"})
			setField(t, o[0], "other", "spec", "pipelineRef", "name")
		},
		"recreated UID": func(t *testing.T, o []*unstructured.Unstructured) { o[0].SetUID("different") },
		"removed mandatory task": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[0], []any{}, "status", "pipelineSpec", "tasks")
		},
		"modified resolved pipeline": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[0], []any{}, "status", "pipelineSpec", "results")
		},
		"missing resolved pipeline": func(t *testing.T, o []*unstructured.Unstructured) {
			unstructured.RemoveNestedField(o[0].Object, "status", "pipelineSpec")
		},
		"missing mandatory child": func(t *testing.T, o []*unstructured.Unstructured) {
			a, _, _ := unstructured.NestedSlice(o[0].Object, "status", "childReferences")
			setField(t, o[0], a[:1], "status", "childReferences")
		},
		"skipped task": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[0], []any{map[string]any{"name": "s5-check"}}, "status", "skippedTasks")
		},
		"inline task": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[3], map[string]any{}, "spec", "taskSpec")
		},
		"task not completed": func(t *testing.T, o []*unstructured.Unstructured) {
			unstructured.RemoveNestedField(o[3].Object, "status", "completionTime")
		},
		"task failed": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[3], []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		},
		"task retry": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[3], []any{map[string]any{}}, "status", "retriesStatus")
		},
		"task owner": func(t *testing.T, o []*unstructured.Unstructured) { o[3].SetOwnerReferences(nil) },
		"task from R": func(t *testing.T, o []*unstructured.Unstructured) {
			steps, _, _ := unstructured.NestedSlice(o[3].Object, "status", "taskSpec", "steps")
			steps[0].(map[string]any)["command"] = []any{"/workspace/trees/checker"}
			setField(t, o[3], steps, "status", "taskSpec", "steps")
		},
		"wrong imageID": func(t *testing.T, o []*unstructured.Unstructured) {
			steps, _, _ := unstructured.NestedSlice(o[3].Object, "status", "steps")
			steps[0].(map[string]any)["imageID"] = "sha256:" + strings.Repeat("b", 64)
			setField(t, o[3], steps, "status", "steps")
		},
		"image tag": func(t *testing.T, o []*unstructured.Unstructured) {
			steps, _, _ := unstructured.NestedSlice(o[3].Object, "status", "taskSpec", "steps")
			steps[0].(map[string]any)["image"] = "checker:latest"
			setField(t, o[3], steps, "status", "taskSpec", "steps")
		},
		"wrong task base": func(t *testing.T, o []*unstructured.Unstructured) {
			changeNamed(t, o[3], "spec", "params", "base", s5TestKey().Base)
		},
		"missing base result": func(t *testing.T, o []*unstructured.Unstructured) {
			r, _ := namedValues(*o[0], "status", "results")
			delete(r, "base-commit")
			setField(t, o[0], s5Named(r), "status", "results")
		},
		"legacy results": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[0], s5Named(map[string]string{"platform-commit": s5TestKey().R}), "status", "results")
		},
		"duplicate result": func(t *testing.T, o []*unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o[0].Object, "status", "results")
			setField(t, o[0], append(r, r[0]), "status", "results")
		},
		"mutable UI": func(t *testing.T, o []*unstructured.Unstructured) {
			changeNamed(t, o[0], "spec", "params", "uiRevision", "main")
		},
		"pod secret": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[4], []any{map[string]any{"name": "prod", "secret": map[string]any{"secretName": "prod-token"}}}, "spec", "volumes")
		},
		"pod host mount": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[4], []any{map[string]any{"name": "host", "hostPath": map[string]any{"path": "/"}}}, "spec", "volumes")
		},
		"pod token": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[4], true, "spec", "automountServiceAccountToken")
		},
		"writable tree": func(t *testing.T, o []*unstructured.Unstructured) {
			cs, _, _ := unstructured.NestedSlice(o[4].Object, "spec", "containers")
			cs[0].(map[string]any)["volumeMounts"] = []any{}
			setField(t, o[4], cs, "spec", "containers")
		},
		"extra environment": func(t *testing.T, o []*unstructured.Unstructured) {
			cs, _, _ := unstructured.NestedSlice(o[4].Object, "spec", "containers")
			co := cs[0].(map[string]any)
			co["env"] = append(co["env"].([]any), map[string]any{"name": "LD_PRELOAD", "value": "/tmp/lib.so"})
			setField(t, o[4], cs, "spec", "containers")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			k := s5TestKey()
			c, id, objs := s5Fixture(t, k)
			if got := s5ReadFixture(t, k, c, id, objs); got.Status != "Succeeded" || len(got.Content) == 0 {
				t.Fatalf("positive control: %+v", got)
			}
			mutate(t, objs)
			if got := s5ReadFixture(t, k, c, id, objs); got.Status == "Succeeded" || got.Reason == "" {
				t.Fatalf("accepted mutation: %+v", got)
			}
		})
	}
}
func TestS5ResultBindings(t *testing.T) {
	for _, field := range []string{"M0", "R", "base", "A", "baselineTree", "resultTree", "ruleDigest", "policyDigest", "toolchainDigest", "callersDigest", "typesDigest", "testsDigest", "uiSHA", "ciUID", "keyDigest", "generation", "reviewBindingDigest", "patchEvidenceDigest", "passed", "findings"} {
		t.Run(field, func(t *testing.T) {
			k := s5TestKey()
			values := s5TestResults(k, "uid")
			if _, r := s5CheckedResults(values, k, "uid"); r != "" {
				t.Fatal("control", r)
			}
			var e map[string]any
			_ = decodeS5([]byte(values["evidence"]), &e)
			e[field] = "forged"
			b, _ := canonicalS5(e)
			values["evidence"] = string(b)
			values["evidence-digest"] = s5Hash(b)
			if _, r := s5CheckedResults(values, k, "uid"); r == "" {
				t.Fatal("unbound", field)
			}
		})
	}
	k := s5TestKey()
	v := s5TestResults(k, "uid")
	v["evidence-digest"] = strings.Repeat("b", 64)
	if _, r := s5CheckedResults(v, k, "uid"); r != "S5_BINDING" {
		t.Fatal(r)
	}
}

// The bound run is read by name and UID only: no other run (an older
// success, an inline or forged run, another generation, a later page)
// is listed, selected or allowed to rescue it.
func TestS5LatestNeverFallsBack(t *testing.T) {
	for _, variant := range []string{"new failure", "inline collision", "new generation", "same key digest"} {
		t.Run(variant, func(t *testing.T) {
			k := s5TestKey()
			c, id, objs := s5Fixture(t, k)
			old := objs[0].DeepCopy()
			old.SetName("old-success")
			old.SetUID("old-uid")
			switch variant {
			case "new failure":
				setField(t, objs[0], []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
			case "inline collision":
				setField(t, old, map[string]any{}, "spec", "pipelineSpec")
				unstructured.RemoveNestedField(old.Object, "spec", "pipelineRef")
			case "new generation":
				newKey := k
				newKey.Generation++
				setField(t, old, s5Named(s5Params(newKey)), "spec", "params")
			}
			client, d := s5TestClient(append(objs, old)...)
			d.PrependReactor("list", "*", func(a ktesting.Action) (bool, runtime.Object, error) {
				t.Fatal("S5 read listed", a.GetResource().Resource)
				return true, nil, nil
			})
			got, err := client.readS5(context.Background(), k, id, c)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "new failure" {
				if got.Status == "Succeeded" {
					t.Fatal("older success revived", got)
				}
				return
			}
			// Other runs neither rescue nor poison the bound run.
			if got.Status != "Succeeded" {
				t.Fatal(got)
			}
			// Reading the other run's identity is never this key's evidence.
			other := id
			other.UID = "old-uid"
			if got, _ := client.readS5(context.Background(), k, other, c); got.Status == "Succeeded" || got.Reason != "S5_CI_IDENTITY" {
				t.Fatal("other run adopted", got)
			}
		})
	}
}
func TestS5EnsureIdentityAndRecovery(t *testing.T) {
	for _, mode := range []string{"created", "lost response", "already exists", "not created"} {
		t.Run(mode, func(t *testing.T) {
			k := s5TestKey()
			c := s5TestContract(t)
			client, d := s5TestClient()
			creates := 0
			d.PrependReactor("create", "pipelineruns", func(a ktesting.Action) (bool, runtime.Object, error) {
				creates++
				o := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
				o.SetUID(types.UID("server-uid"))
				if mode != "not created" {
					if err := d.Tracker().Create(pipelineRunGVR, o, "cicd"); err != nil {
						t.Fatal(err)
					}
				}
				switch mode {
				case "lost response", "not created":
					return true, nil, errors.New("lost")
				case "already exists":
					return true, nil, apierrors.NewAlreadyExists(pipelineRunGVR.GroupResource(), o.GetName())
				}
				return true, o, nil
			})
			id, err := client.ensureS5(context.Background(), k, c)
			if mode == "not created" {
				if err == nil {
					t.Fatal("accepted nonexistent run")
				}
				return
			}
			if err != nil || id.UID != "server-uid" {
				t.Fatal(id, err)
			}
			again, err := client.ensureS5(context.Background(), k, c)
			if err != nil || again != id || creates != 1 {
				t.Fatal(again, err, creates)
			}
			run, _ := d.Resource(pipelineRunGVR).Namespace("cicd").Get(context.Background(), id.Run, metav1.GetOptions{})
			p, _ := namedValues(*run, "spec", "params")
			if p["base"] != k.M0 || p["revision"] != k.R || p["keyDigest"] != id.KeyDigest || p["generation"] != "1" {
				t.Fatal(p)
			}
			changeNamed(t, run, "spec", "params", "keyDigest", strings.Repeat("b", 64))
			_, _ = d.Resource(pipelineRunGVR).Namespace("cicd").Update(context.Background(), run, metav1.UpdateOptions{})
			if _, err := client.ensureS5(context.Background(), k, c); err == nil {
				t.Fatal("collision accepted")
			}
		})
	}
}
func TestS5KeyAndDisabledDeployment(t *testing.T) {
	k := s5TestKey()
	// The shared key encoder (canon, the Python form) is exact to int64; the
	// largest generation is MaxInt64 and anything above it is refused rather
	// than encoded differently by the store and the transport.
	k.Generation = math.MaxInt64
	name, err := k.RunName()
	if err != nil || len(name) > 58 || !strings.HasSuffix(name, "-g1y2p0ij32e8e7") {
		t.Fatal(name, err)
	}
	b, _ := k.Canonical()
	if !strings.Contains(string(b), "9223372036854775807") {
		t.Fatal(string(b))
	}
	var decoded S5CIKey
	if decodeS5(b, &decoded) != nil || decoded != k {
		t.Fatal("lossy key")
	}
	for _, g := range []uint64{math.MaxInt64 + 1, math.MaxUint64} {
		over := k
		over.Generation = g
		if _, err := over.RunName(); err == nil {
			t.Fatal("generation overflow accepted", g)
		}
		if err := s5TestContract(t).validate(over); err == nil {
			t.Fatal("transport accepted overflow", g)
		}
	}
	client, _ := s5TestClient()
	if _, err := client.EnsureLandingS5CI(context.Background(), k); err == nil {
		t.Fatal("unbuilt deployment enabled")
	}
	if _, err := client.LandingS5CI(context.Background(), k, "uid"); err == nil {
		t.Fatal("unbuilt deployment read")
	}
	for _, field := range []string{"generation", "ruleVersion", "R", "image", "digest", "schema", "env", "task", "op"} {
		bad := k
		switch field {
		case "env":
			bad.Env = "dev"
		case "task":
			bad.Task = "task-1"
		case "op":
			bad.Op = "op-1"
		case "generation":
			bad.Generation = 0
		case "ruleVersion":
			bad.RuleVersion = 0
		case "R":
			bad.R = "main"
		case "image":
			bad.CheckerImageDigest = "sha256:" + strings.Repeat("0", 64)
		case "digest":
			bad.ProfileDigest = ""
		case "schema":
			bad.Schema = 1
		}
		if _, err := bad.RunName(); err == nil {
			t.Fatal(field)
		}
	}
	c, id, objs := s5Fixture(t, s5TestKey())
	id.UID = ""
	if got := s5ReadFixture(t, s5TestKey(), c, id, objs); got.Status == "Succeeded" {
		t.Fatal("missing persisted UID")
	}
}

func TestS5CanonicalGolden(t *testing.T) {
	// Independently produced with Python hashlib/json, including field case.
	type golden struct {
		Key                     S5CIKey
		Canonical, Digest, Name string
	}
	read := func(path string) golden {
		var fixture golden
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, &fixture); err != nil {
			t.Fatal(err)
		}
		return fixture
	}
	// The original transport golden (shared with the infra runner's Python
	// test) still encodes byte-for-byte with the shared key encoder, but its
	// operation identity is not one the landing store can persist.
	legacy := read("testdata/s5-key.golden.json")
	if d, err := landing.S5Digest(legacy.Key); err != nil || d != legacy.Digest || "s5-"+d[:40]+"-g1" != legacy.Name {
		t.Fatal("shared encoder diverged from the Python golden", d, err)
	}
	if _, err := legacy.Key.Digest(); err == nil {
		t.Fatal("non-landing operation identity accepted")
	}
	fixture := read("testdata/s5-key-landing.golden.json")
	k := s5TestKey()
	raw, _ := k.Canonical()
	digest, _ := k.Digest()
	name, _ := k.RunName()
	if fixture.Key != k || string(raw) != fixture.Canonical || digest != fixture.Digest || name != fixture.Name {
		t.Fatal("cross-language canonical key mismatch")
	}
	if s5Params(k)["key"] != fixture.Canonical || s5Params(k)["keyDigest"] != fixture.Digest || s5Hash([]byte(s5Params(k)["key"])) != digest {
		t.Fatal("transport parameter is not the persisted key encoding")
	}
	var values map[string]any
	_ = decodeS5(raw, &values)
	for field := range values {
		changed := map[string]any{}
		for n, v := range values {
			changed[n] = v
		}
		changed[field] = "different"
		other, _ := canonicalS5(changed)
		if s5Hash(other) == digest {
			t.Fatal("unbound key field", field)
		}
	}
}

func TestS5DeletedRuntimeEvidenceAndForeignPVC(t *testing.T) {
	k := s5TestKey()
	for _, index := range []int{1, 2, 3, 4, 5, 6, 7} {
		c, id, objects := s5Fixture(t, k)
		objects = append(objects[:index], objects[index+1:]...)
		if got := s5ReadFixture(t, k, c, id, objects); got.Status == "Succeeded" || got.Reason != "S5_EVIDENCE_MISSING" {
			t.Fatal(index, got)
		}
	}
	c, id, objects := s5Fixture(t, k)
	objects[5].SetOwnerReferences(nil)
	if got := s5ReadFixture(t, k, c, id, objects); got.Status == "Succeeded" || got.Reason != "S5_CI_IDENTITY" {
		t.Fatal(got)
	}
}
