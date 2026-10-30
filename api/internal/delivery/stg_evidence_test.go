package delivery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

var stgAPI = "sha256:" + strings.Repeat("d", 64)
var stgConsole = "sha256:" + strings.Repeat("e", 64)

func stgExpected() STGContent { in := stgInput(); return STGContent{P: in.P, U: in.U, Infra: in.Infra} }
func stgRunWorld(t *testing.T) (*unstructured.Unstructured, map[string]*unstructured.Unstructured) {
	t.Helper()
	run := namedObject(t)
	successful(run)
	defs, _, _ := unstructured.NestedSlice(stgPipe(t).Object, "spec", "tasks")
	stgSet(t, run, map[string]any{"tasks": defs}, "status", "pipelineSpec")
	refs := []any{}
	children := map[string]*unstructured.Unstructured{}
	commits := map[string]string{"clone-platform": stgInput().P, "clone-ui": stgInput().U, "clone-infra": stgInput().Infra}
	repos := map[string]string{"clone-platform": "bifrost-platform", "clone-ui": "bifrost-ui", "clone-infra": "bifrost-trade-infra"}
	ctl := true
	for _, entry := range defs {
		def := entry.(map[string]any)
		task := def["name"].(string)
		ref, ok := def["taskRef"].(map[string]any)
		if !ok {
			continue
		}
		child := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "spec": map[string]any{"taskRef": ref}}}
		child.SetName("run-" + task)
		child.SetNamespace(STGReleaseNamespace)
		child.SetUID(types.UID("uid-" + task))
		child.SetResourceVersion("1")
		child.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "tekton.dev/v1", Kind: "PipelineRun", Name: run.GetName(), UID: run.GetUID(), Controller: &ctl}})
		successful(child)
		params, results := []any{}, []any{}
		if commit := commits[task]; commit != "" {
			params = append(params, map[string]any{"name": "url", "value": "http://gitea.cicd.svc.cluster.local:3000/bifrost/" + repos[task] + ".git"})
			results = append(results, map[string]any{"name": "commit", "value": commit})
		}
		if task == "build-platform-api" || task == "build-platform-console" {
			digest := stgAPI
			if task == "build-platform-console" {
				digest = stgConsole
			}
			results = append(results, map[string]any{"name": "digest", "value": digest})
		}
		if task == "gitops-sync" {
			for name, value := range map[string]string{"application": stgNamespace, "apiDigest": stgAPI, "consoleDigest": stgConsole, "deliverRun": run.GetName()} {
				params = append(params, map[string]any{"name": name, "value": value})
			}
		}
		stgSet(t, child, params, "spec", "params")
		stgSet(t, child, results, "status", "results")
		children[task] = child
		refs = append(refs, map[string]any{"apiVersion": "tekton.dev/v1", "kind": "TaskRun", "name": child.GetName(), "pipelineTaskName": task})
	}
	stgSet(t, run, refs, "status", "childReferences")
	return run, children
}
func readSTGWorld(t *testing.T, run *unstructured.Unstructured, children map[string]*unstructured.Unstructured) (STGRunObservation, error) {
	t.Helper()
	objects := []runtime.Object{run}
	for _, child := range children {
		objects = append(objects, child)
	}
	a, _, _ := stgAdapter(t, objects...)
	return a.ObserveRun(context.Background(), STGRunIdentity{STGReleaseNamespace, run.GetName(), run.GetUID()})
}
func TestSTGRunEvidenceRefusals(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*testing.T, *unstructured.Unstructured, map[string]*unstructured.Unstructured){
		"wrong owner UID": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			child := c["clone-platform"]
			owner := child.GetOwnerReferences()
			owner[0].UID = "other"
			child.SetOwnerReferences(owner)
		},
		"duplicate child task": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			refs, _, _ := unstructured.NestedSlice(r.Object, "status", "childReferences")
			stgSet(t, r, append(refs, refs[0]), "status", "childReferences")
		},
		"same name different child UID": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			refs, _, _ := unstructured.NestedSlice(r.Object, "status", "childReferences")
			refs[0].(map[string]any)["uid"] = "different"
			stgSet(t, r, refs, "status", "childReferences")
		},
		"missing clone": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			delete(c, "clone-infra")
		},
		"wrong taskRef": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["clone-platform"], "other", "spec", "taskRef", "name")
		},
		"wrong definition": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			defs, _, _ := unstructured.NestedSlice(r.Object, "status", "pipelineSpec", "tasks")
			for _, v := range defs {
				m := v.(map[string]any)
				if m["name"] == "clone-platform" {
					m["taskRef"] = map[string]any{"name": "other"}
				}
			}
			stgSet(t, r, defs, "status", "pipelineSpec", "tasks")
		},
		"wrong repository": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["clone-ui"], []any{map[string]any{"name": "url", "value": "https://evil/bifrost-ui.git"}}, "spec", "params")
		},
		"duplicate clone result": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			v, _, _ := unstructured.NestedSlice(c["clone-ui"].Object, "status", "results")
			stgSet(t, c["clone-ui"], append(v, v[0]), "status", "results")
		},
		"duplicate build result": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			v, _, _ := unstructured.NestedSlice(c["build-platform-api"].Object, "status", "results")
			stgSet(t, c["build-platform-api"], append(v, v[0]), "status", "results")
		},
		"wrong infra": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["clone-infra"], []any{map[string]any{"name": "commit", "value": strings.Repeat("f", 40)}}, "status", "results")
		},
		"wrong U": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["clone-ui"], []any{map[string]any{"name": "commit", "value": strings.Repeat("f", 40)}}, "status", "results")
		},
		"invalid digest": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["build-platform-api"], []any{map[string]any{"name": "digest", "value": "sha256:short"}}, "status", "results")
		},
		"pin failed": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["gitops-sync"], []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		},
		"pin wrong inputs": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, c["gitops-sync"], []any{}, "spec", "params")
		},
		"run running": func(t *testing.T, r *unstructured.Unstructured, c map[string]*unstructured.Unstructured) {
			stgSet(t, r, []any{map[string]any{"type": "Succeeded", "status": "Unknown"}}, "status", "conditions")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			run, children := stgRunWorld(t)
			obs, err := readSTGWorld(t, run, children)
			if err != nil {
				t.Fatal("control read", err)
			}
			content, err := obs.Content(stgExpected())
			if err != nil || content.APIDigest != stgAPI || content.ConsoleDigest != stgConsole {
				t.Fatal("control content", content, err)
			}
			raw := obs.Raw["child_refs"].([]any)
			if len(raw) != len(children) || raw[0].(map[string]any)["task_run"] == nil {
				t.Fatal("archive omitted children")
			}
			mutate(t, run, children)
			obs, err = readSTGWorld(t, run, children)
			if err == nil {
				_, err = obs.Content(stgExpected())
			}
			if err == nil {
				t.Fatal("bad evidence accepted")
			}
		})
	}
}
func TestSTGObserveUIDAndVersionFence(t *testing.T) {
	t.Parallel()
	run, children := stgRunWorld(t)
	objects := []runtime.Object{run}
	for _, c := range children {
		objects = append(objects, c)
	}
	a, dyn, _ := stgAdapter(t, objects...)
	id := STGRunIdentity{STGReleaseNamespace, run.GetName(), run.GetUID()}
	if _, err := a.ObserveRun(context.Background(), id); err != nil {
		t.Fatal("control", err)
	}
	id.UID = "new-uid"
	if _, err := a.ObserveRun(context.Background(), id); !errors.Is(err, ErrSTGCollision) {
		t.Fatal(err)
	}
	id.UID = run.GetUID()
	calls := 0
	dyn.PrependReactor("get", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
		calls++
		copy := run.DeepCopy()
		if calls > 1 {
			copy.SetResourceVersion("2")
		}
		return true, copy, nil
	})
	if _, err := a.ObserveRun(context.Background(), id); !errors.Is(err, ErrSTGUnknown) {
		t.Fatal(err)
	}
}
func stgWorkloadWorld() []runtime.Object {
	objects := []runtime.Object{}
	one := int32(1)
	ctl := true
	for _, name := range stgDeployments {
		container, digest := "platform-api", stgAPI
		if name == "platform-console" {
			container, digest = "platform-console", stgConsole
		}
		labels := map[string]string{"app": name}
		dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: stgNamespace, UID: types.UID(name + "-uid"), Generation: 2, Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}}, Spec: appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: container, Image: "registry/" + container + "@" + digest}}}}}, Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}}
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name + "-rs", Namespace: stgNamespace, UID: types.UID(name + "-rs-uid"), Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: dep.UID, Controller: &ctl}}}}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-pod", Namespace: stgNamespace, Labels: labels, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &ctl}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: container}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: container, Ready: true, ImageID: "docker-pullable://registry/" + container + "@" + digest, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
		objects = append(objects, dep, rs, pod)
	}
	return objects
}
func TestSTGWorkloadStrictConditionsAndTemplate(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"workers zero", "no pods", "missing pod", "old generation", "old replicaset", "wrong owner", "template changed", "not ready", "invalid imageID", "old terminating pod", "incomplete list", "generation race", "uid race"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			core := kfake.NewSimpleClientset(stgWorkloadWorld()...)
			a := &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }}
			if got, err := a.ReadWorkloads(context.Background(), stgAPI, stgConsole); err != nil || len(got) != 3 {
				t.Fatal("control", got, err)
			}
			ctx := context.Background()
			dep, _ := core.AppsV1().Deployments(stgNamespace).Get(ctx, "platform-workers", metav1.GetOptions{})
			pod, _ := core.CoreV1().Pods(stgNamespace).Get(ctx, "platform-workers-pod", metav1.GetOptions{})
			rs, _ := core.AppsV1().ReplicaSets(stgNamespace).Get(ctx, "platform-workers-rs", metav1.GetOptions{})
			switch mode {
			case "workers zero":
				zero := int32(0)
				dep.Spec.Replicas = &zero
			case "no pods", "missing pod":
				if err := core.CoreV1().Pods(stgNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			case "old generation":
				dep.Status.ObservedGeneration = 1
			case "old replicaset":
				rs.Annotations["deployment.kubernetes.io/revision"] = "1"
			case "wrong owner":
				rs.OwnerReferences[0].UID = "other"
			case "template changed":
				dep.Spec.Template.Spec.Containers[0].Image = "registry/api@" + stgConsole
			case "not ready":
				pod.Status.ContainerStatuses[0].Ready = false
			case "invalid imageID":
				pod.Status.ContainerStatuses[0].ImageID = "containerd://not-a-digest"
			case "old terminating pod":
				old := pod.DeepCopy()
				old.Name = "old-pod"
				now := metav1.Now()
				old.DeletionTimestamp = &now
				old.Status.ContainerStatuses[0].ImageID = "registry/api@" + stgConsole
				if _, err := core.CoreV1().Pods(stgNamespace).Create(ctx, old, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "incomplete list":
				core.PrependReactor("list", "pods", func(kt.Action) (bool, runtime.Object, error) {
					return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "next"}}, nil
				})
			case "generation race", "uid race":
				counts := map[string]int{}
				core.PrependReactor("get", "deployments", func(action kt.Action) (bool, runtime.Object, error) {
					name := action.(kt.GetAction).GetName()
					counts[name]++
					if name == dep.Name && counts[name] >= 3 {
						copy := dep.DeepCopy()
						if mode == "generation race" {
							copy.Generation++
						} else {
							copy.UID = "replacement"
						}
						return true, copy, nil
					}
					return false, nil, nil
				})
			}
			if _, err := core.AppsV1().Deployments(stgNamespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := core.AppsV1().ReplicaSets(stgNamespace).Update(ctx, rs, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if mode != "missing pod" && mode != "no pods" {
				if _, err := core.CoreV1().Pods(stgNamespace).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := a.ReadWorkloads(ctx, stgAPI, stgConsole); err == nil {
				t.Fatal("accepted", got)
			}
		})
	}
}
func TestSTGPinUsesCurrentDesiredImages(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"control", "wrong digest", "duplicate", "tag", "wrong repo", "multiple sources", "no uid"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			images := []any{"registry.cicd.svc.cluster.local:5000/bifrost-platform-api=node:30500/bifrost-platform-api@" + stgAPI, "registry.cicd.svc.cluster.local:5000/bifrost-platform-console=node:30500/bifrost-platform-console@" + stgConsole}
			app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": stgNamespace, "namespace": STGReleaseNamespace, "uid": "app-uid", "resourceVersion": "1"}, "spec": map[string]any{"source": map[string]any{"kustomize": map[string]any{"images": images}}}}}
			a, _, _ := stgAdapter(t, app)
			if _, err := a.ReadPin(context.Background(), stgAPI, stgConsole); err != nil {
				t.Fatal("control", err)
			}
			switch mode {
			case "wrong digest":
				images[0] = strings.ReplaceAll(images[0].(string), stgAPI, stgConsole)
			case "duplicate":
				images[1] = images[0]
			case "tag":
				images[0] = "registry.cicd.svc.cluster.local:5000/bifrost-platform-api=node:30500/bifrost-platform-api:stg"
			case "wrong repo":
				images[0] = "registry.cicd.svc.cluster.local:5000/bifrost-platform-api=node:30500/other@" + stgAPI
			case "multiple sources":
				stgSet(t, app, []any{}, "spec", "sources")
			case "no uid":
				app.SetUID("")
			}
			stgSet(t, app, images, "spec", "source", "kustomize", "images")
			a, _, _ = stgAdapter(t, app)
			_, err := a.ReadPin(context.Background(), stgAPI, stgConsole)
			if (err == nil) != (mode == "control") {
				t.Fatal(mode, err)
			}
		})
	}
}
func TestSTGHealthClassifiesDefiniteAndUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, media, want string
		status                  int
		err                     error
	}{
		{"ok", `{"status":"ok","service":"bifrost-platform-api"}`, "application/json", "passed", 200, nil},
		{"html", "<html>ok</html>", "text/html", "failed", 200, nil},
		{"bad json", "{", "application/json", "failed", 200, nil},
		{"status", `{"status":"error","service":"bifrost-platform-api"}`, "application/json", "failed", 200, nil},
		{"service", `{"status":"ok","service":"trade"}`, "application/json", "failed", 200, nil},
		{"duplicate", `{"status":"no","status":"ok","service":"bifrost-platform-api"}`, "application/json", "failed", 200, nil},
		{"http", "{}", "application/json", "failed", 503, nil},
		{"redirect", "{}", "application/json", "failed", 302, nil},
		{"timeout", "", "", "unknown", 0, context.DeadlineExceeded},
		{"network", "", "", "unknown", 0, errors.New("connection")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			a := &STGReleaseAdapter{HTTP: &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != stgHealthURL || r.Method != "GET" {
					t.Fatal(r.URL, r.Method)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.media}, "Location": []string{"http://other/health"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, tc.err
			})}}
			if got := a.ReadHealth(context.Background()); got.Outcome != tc.want || calls != 1 {
				t.Fatal(got, calls)
			}
		})
	}
}

// A failed release can leave API and workers on different, individually
// settled revisions. Capture both facts; strict successful delivery still
// requires the same API digest on both workloads.
func TestSTGObservePartialRolloutPreservesEachDigest(t *testing.T) {
	t.Parallel()
	core := kfake.NewSimpleClientset(stgWorkloadWorld()...)
	a := &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }}
	ctx := context.Background()
	dep, err := core.AppsV1().Deployments(stgNamespace).Get(ctx, "platform-workers", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod, err := core.CoreV1().Pods(stgNamespace).Get(ctx, "platform-workers-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dep.Spec.Template.Spec.Containers[0].Image = "registry/api@" + stgConsole
	pod.Status.ContainerStatuses[0].ImageID = "docker-pullable://registry/api@" + stgConsole
	if _, err = core.AppsV1().Deployments(stgNamespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = core.CoreV1().Pods(stgNamespace).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	observed, err := a.ObserveWorkloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if observed["platform-api"].(map[string]any)["digest"] != stgAPI || observed["platform-workers"].(map[string]any)["digest"] != stgConsole {
		t.Fatal("partial rollout was collapsed", observed)
	}
	if _, err = a.ReadWorkloads(ctx, stgAPI, stgConsole); err == nil {
		t.Fatal("partial rollout passed successful-delivery predicate")
	}
}
