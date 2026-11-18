package gatecluster

import (
	"context"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

func taskRun(name, run string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd", "creationTimestamp": "2026-11-18T01:00:00Z",
			"labels": map[string]any{labelTaskRunOf: run, labelTask: "tests", "other": "dropped"}},
		"status": map[string]any{"startTime": "2026-11-18T01:00:01Z", "completionTime": "2026-11-18T01:00:09Z",
			"taskSpec":   map[string]any{"steps": []any{"a big copy"}},
			"conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}},
			"results":    []any{map[string]any{"name": "modules", "value": "api"}},
			"steps": []any{map[string]any{"name": "go", "terminated": map[string]any{
				"startedAt": "2026-11-18T01:00:02Z", "finishedAt": "2026-11-18T01:00:08Z", "exitCode": int64(0)}}}},
	}}
}

// Origin: 2026-11-18, the source switch's readers "a run's steps" and
// "pipeline timings": a page poll listed TaskRuns. The cache must hand them
// the fields they show, in the shape of a listed TaskRun, without the Task
// copy that is most of its size.
func TestATaskRunKeepsWhatTheDisplaysRead(t *testing.T) {
	o := TaskRunOf(taskRun("run-1-tests", "run-1")).Object()
	if o.GetLabels()[labelTaskRunOf] != "run-1" || o.GetLabels()[labelTask] != "tests" || o.GetLabels()["other"] != "" {
		t.Fatalf("labels: %v", o.GetLabels())
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(o.Object, "status", "taskSpec"); found {
		t.Fatal("the Task's copy was kept")
	}
	steps, _, _ := unstructured.NestedSlice(o.Object, "status", "steps")
	want := []any{map[string]any{"name": "go", "terminated": map[string]any{
		"startedAt": "2026-11-18T01:00:02Z", "finishedAt": "2026-11-18T01:00:08Z", "exitCode": int64(0)}}}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps: %v", steps)
	}
	if v, _, _ := unstructured.NestedString(o.Object, "status", "completionTime"); v != "2026-11-18T01:00:09Z" {
		t.Fatalf("completion: %q", v)
	}
}

// Same origin: the cache follows the cluster: it answers only once listed,
// takes what the watch says, and forgets what is deleted.
func TestTheTaskCacheFollowsTheCluster(t *testing.T) {
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{TaskRunGVR: "TaskRunList"}, taskRun("a-tests", "a"))
	g := &Gate{CacheOnly: true, WatchTasks: true, Namespace: "cicd", Dynamic: func() (dynamic.Interface, error) { return dyn, nil }, Logf: func(string, ...any) {}}
	g.init()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if g.TasksSynced() {
		t.Fatal("synced before any list")
	}
	go g.runTasks(ctx)
	wait := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatal("timed out: " + what)
	}
	wait("first list", func() bool { return g.TasksSynced() && len(g.TaskObjects("a")) == 1 })
	res := dyn.Resource(TaskRunGVR).Namespace("cicd")
	// The fake's watch starts asynchronously: create until the cache hears it.
	wait("created", func() bool {
		_, _ = res.Create(ctx, taskRun("b-tests", "b"), metav1.CreateOptions{})
		return len(g.TaskObjects("b")) == 1
	})
	wait("deleted", func() bool {
		_ = res.Delete(ctx, "a-tests", metav1.DeleteOptions{})
		return len(g.TaskObjects("a")) == 0
	})
	_ = watch.Added
}
