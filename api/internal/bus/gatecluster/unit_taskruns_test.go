package gatecluster

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

const testUnitLabel = "example.io/unit-plan"

func unitTask(name, plan string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd", "uid": "u-" + name,
			"annotations": map[string]any{"a": "kept"}, "managedFields": []any{"dropped"},
			"labels": map[string]any{testUnitLabel: plan}},
		"spec":   map[string]any{"params": []any{map[string]any{"name": "p", "value": "v"}}},
		"status": map[string]any{"taskSpec": map[string]any{"steps": []any{"dropped"}}},
	}}
}

// Origin: 2026-11-18, the CI line on moving the unit TaskRuns to a cache: they
// belong to no PipelineRun (the display cache groups by one), every field the
// evidence reads must survive, and a deleted one must leave the cache.
func TestTheUnitCacheFollowsTheLabelledTaskRuns(t *testing.T) {
	other := taskRun("a-tests", "a")
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{TaskRunGVR: "TaskRunList"}, unitTask("u1", "p1"), other)
	g := &Gate{CacheOnly: true, UnitTaskLabel: testUnitLabel, Namespace: "cicd",
		Dynamic: func() (dynamic.Interface, error) { return dyn, nil }, Backoff: time.Millisecond}
	g.init()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.runUnitTasks(ctx)
	wait := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatal("timed out: " + what)
	}
	wait("first list", func() bool { return g.UnitTasksSynced() && len(g.UnitTaskObjects("")) == 1 })
	got := g.UnitTaskObjects("p1")[0]
	if got.GetUID() != "u-u1" || got.GetAnnotations()["a"] != "kept" {
		t.Errorf("uid or annotations lost: %v", got.Object)
	}
	if params, _, _ := unstructured.NestedSlice(got.Object, "spec", "params"); len(params) != 1 {
		t.Errorf("params lost: %v", got.Object)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "status", "taskSpec"); found {
		t.Error("the Task copy was kept")
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "metadata", "managedFields"); found {
		t.Error("managedFields were kept")
	}
	if len(g.UnitTaskObjects("p2")) != 0 {
		t.Error("another plan's read returned p1's run")
	}
	res := dyn.Resource(TaskRunGVR).Namespace("cicd")
	wait("created", func() bool {
		_, _ = res.Create(ctx, unitTask("u2", "p2"), metav1.CreateOptions{})
		return len(g.UnitTaskObjects("p2")) == 1
	})
	wait("deleted", func() bool {
		_ = res.Delete(ctx, "u1", metav1.DeleteOptions{})
		return len(g.UnitTaskObjects("p1")) == 0
	})
}
