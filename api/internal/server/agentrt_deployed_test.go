package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

func tektonObject(kind, name string, labels map[string]any, spec, status map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": labels}, "spec": spec, "status": status}}
}

// settledWorkload is a deployment of one replica, rolled out, whose pod runs
// container at digest.
func settledWorkload(name, container, digest string) []runtime.Object {
	one, ctl := int32(1), true
	ns := "bifrost-platform-stg"
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: types.UID(name + "-dep"), Generation: 2,
			Annotations: map[string]string{"deployment.kubernetes.io/revision": "5"}},
		Spec:   appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
	}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name + "-r5", Namespace: ns, UID: types.UID(name + "-rs"),
		Annotations:     map[string]string{"deployment.kubernetes.io/revision": "5"},
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: dep.UID, Controller: &ctl}}}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-x", Namespace: ns, Labels: map[string]string{"app": name},
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &ctl}}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: container}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: container, Ready: true,
			ImageID: "registry/" + container + "@" + digest, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}},
	}
	return []runtime.Object{dep, rs, pod}
}

// TD-343 end to end (review 167 A1): a delivery run is recorded with its
// digests, and the runtime reads the deployment as known only while every
// pod runs what the record says.
func TestDeployedFromReleaseRecords(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-rules.yaml"),
		[]byte("namespace: cicd\nrules:\n  - match: { pipeline: deliver-platform }\n    lane: platform\n    env: stg\n    deploys: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("1", 40)
	api, console := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("c", 64)
	done := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	run := tektonObject("PipelineRun", "deliver-platform-7", map[string]any{"tekton.dev/pipeline": "deliver-platform"},
		map[string]any{"params": []any{map[string]any{"name": "revision", "value": "main"}}},
		map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}},
			"startTime": done.Add(-time.Hour).Format(time.RFC3339), "completionTime": done.Format(time.RFC3339)})
	task := func(name string, spec, status map[string]any) *unstructured.Unstructured {
		return tektonObject("TaskRun", "deliver-platform-7-"+name, map[string]any{"tekton.dev/pipelineRun": "deliver-platform-7", "tekton.dev/pipelineTask": name}, spec, status)
	}
	build := func(name, digest string) *unstructured.Unstructured {
		return task(name, map[string]any{}, map[string]any{"taskSpec": map[string]any{"results": []any{map[string]any{"name": "digest"}}},
			"results": []any{map[string]any{"name": "digest", "value": digest}}})
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}: "PipelineRunList",
		{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}:     "TaskRunList",
	}, run,
		task("clone-platform", map[string]any{"params": []any{map[string]any{"name": "url", "value": "http://gitea/org/bifrost-platform.git"}}},
			map[string]any{"results": []any{map[string]any{"name": "commit", "value": sha}}}),
		build("build-platform-api", api), build("build-platform-console", console))
	var objs []runtime.Object
	objs = append(objs, settledWorkload("platform-api", "platform-api", api)...)
	objs = append(objs, settledWorkload("platform-workers", "platform-api", api)...)
	objs = append(objs, settledWorkload("platform-console", "platform-console", console)...)
	core := k8sfake.NewSimpleClientset(objs...)
	svc := releases.NewService(dir, func() (kubernetes.Interface, dynamic.Interface, error) { return core, dyn, nil })
	if r := svc.Tick(ctx); r.Recorded != 1 || len(r.Errors) != 0 {
		t.Fatalf("tick = %+v", r)
	}
	src := agentRuntimeSources(nil, nil, nil, svc)
	commit, ref, ok := src.Deployed(ctx, "stg")
	images, _ := ref["images"].(map[string]any)
	if !ok || commit != sha || ref["delivery_run"] != "deliver-platform-7" || images["platform-api"] != api || images["platform-console"] != console {
		t.Fatalf("deployed = %s %v %v", commit, ref, ok)
	}

	// The workers pulled another image under the same tag: not known.
	pod, err := core.CoreV1().Pods("bifrost-platform-stg").Get(ctx, "platform-workers-x", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.Status.ContainerStatuses[0].ImageID = "registry/platform-api@sha256:" + strings.Repeat("e", 64)
	if _, err = core.CoreV1().Pods("bifrost-platform-stg").UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if commit, _, ok = src.Deployed(ctx, "stg"); ok {
		t.Fatalf("a pod running another digest read as deployed %s", commit)
	}
}
