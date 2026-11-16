package gatecluster

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// displayRun is run() with what the display reads use: a creation time, a
// pipelineRef, the repo label and params, start and completion times, and
// params and labels the cache must leave behind.
func displayRun(name, pipeline, commit, status, reason string, created time.Time) *unstructured.Unstructured {
	r := run(name, pipeline, commit, status, reason)
	r.SetCreationTimestamp(metav1.NewTime(created))
	r.SetLabels(map[string]string{labelPipeline: pipeline, labelRepo: "bifrost-platform", "app.kubernetes.io/managed-by": "tekton"})
	_ = unstructured.SetNestedField(r.Object, pipeline, "spec", "pipelineRef", "name")
	_ = unstructured.SetNestedSlice(r.Object, []any{
		map[string]any{"name": "revision", "value": commit},
		map[string]any{"name": "repo", "value": "bifrost-platform"},
		map[string]any{"name": "repo-url", "value": "http://gitea/bifrost-platform.git"},
		map[string]any{"name": "uiRevision", "value": "main"},
		map[string]any{"name": "token", "value": "not-kept"}}, "spec", "params")
	_ = unstructured.SetNestedField(r.Object, created.Add(time.Second).UTC().Format(time.RFC3339), "status", "startTime")
	if status == "True" || status == "False" {
		_ = unstructured.SetNestedField(r.Object, created.Add(time.Minute).UTC().Format(time.RFC3339), "status", "completionTime")
	}
	return r
}

// startCache runs a cache-only gate over the client until the test ends and
// waits until it is synced.
func startCache(t *testing.T, client dynamic.Interface, l Ledger, onFinished func(string, string)) *Gate {
	t.Helper()
	g := &Gate{CacheOnly: true, Ledger: l, OnFinished: onFinished, Namespace: ns,
		Dynamic: func() (dynamic.Interface, error) { return client, nil }, Backoff: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safego.Go("test-run-cache", func() { defer close(done); g.Run(ctx) })
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-g.Watching():
	case <-time.After(5 * time.Second):
		t.Fatal("the cache-only gate did not start watching (it must not wait for the window)")
	}
	eventually(t, "synced", g.Synced)
	return g
}

func sameRun(a, b Run) bool {
	if !a.Created.Equal(b.Created) {
		return false
	}
	a.Created, b.Created = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

// 2026-11-16: bus step 2 batch 2 (display reads of runs move to the gate's
// map): what the cache answers of each run is what a list of the same runs
// says, the queries select as a filter of the list would, the object a
// reader gets back reads as the run it came from, and the heavy fields stay
// behind.
func TestSnapshotAnswersAsAListOfTheSameRuns(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	client := fakeClient(
		displayRun("deliver-1", "bifrost-deliver-stg", shaA, "True", "Succeeded", t0),
		displayRun("deliver-2", "bifrost-deliver-stg", shaB, "False", "Failed", t0.Add(time.Hour)),
		displayRun("deliver-3", "bifrost-deliver-stg", shaB, "Unknown", "Running", t0.Add(2*time.Hour)),
		displayRun("ci-1", "bifrost-ci-platform", shaA, "", "", t0.Add(30*time.Minute)))
	g := startCache(t, client, nil, nil)
	list, err := client.Resource(PipelineRunGVR).Namespace(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snap := g.Snapshot()
	if len(snap) != len(list.Items) {
		t.Fatalf("snapshot %d runs, list %d", len(snap), len(list.Items))
	}
	var running, deliver []string
	for i := range list.Items {
		want := RunOf(&list.Items[i])
		got, ok := g.Get(want.Name)
		if !ok || !sameRun(got, want) {
			t.Errorf("%s: cache %+v, list %+v", want.Name, got, want)
		}
		if !want.Finished() {
			running = append(running, want.Name)
		}
		if list.Items[i].GetLabels()[labelPipeline] == "bifrost-deliver-stg" {
			deliver = append(deliver, want.Name)
		}
		// The object handed to readers reads back as the same run.
		obj := got.Object()
		if back := RunOf(&obj); !sameRun(back, got) {
			t.Errorf("%s: object reads back as %+v, was %+v", got.Name, back, got)
		}
		raw, _ := json.Marshal(obj.Object)
		for _, gone := range []string{"pipelineSpec", "managedFields", "childReferences", "not-kept", "uiRevision", "managed-by", "mmmm"} {
			if strings.Contains(string(raw), gone) {
				t.Errorf("%s: the cached object keeps %s: %s", got.Name, gone, raw)
			}
		}
	}
	names := func(rs []Run) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return out
	}
	if got := names(g.Running()); !reflect.DeepEqual(got, running) {
		t.Errorf("running %v, want %v", got, running)
	}
	if got := names(g.RunsOf("bifrost-deliver-stg")); !reflect.DeepEqual(got, deliver) {
		t.Errorf("runs of deliver-stg %v, want %v", got, deliver)
	}
	if r, ok := g.Latest("bifrost-deliver-stg"); !ok || r.Name != "deliver-3" {
		t.Errorf("latest %v %v", r.Name, ok)
	}
	if len(g.Objects("bifrost-deliver-stg")) != 3 || len(g.Objects("")) != 4 {
		t.Errorf("objects")
	}
	// A run that changes after the list is seen by the watch.
	upd := displayRun("deliver-3", "bifrost-deliver-stg", shaB, "True", "Succeeded", t0.Add(2*time.Hour))
	if _, err := client.Resource(PipelineRunGVR).Namespace(ns).Update(context.Background(), upd, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the update in the cache", func() bool { r, _ := g.Get("deliver-3"); return r.Status == "True" })
	if err := client.Resource(PipelineRunGVR).Namespace(ns).Delete(context.Background(), "ci-1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the delete in the cache", func() bool { _, ok := g.Get("ci-1"); return !ok })
}

// 2026-11-16: bus step 2 batch 2: the api process's watcher is a cache only;
// appending stays in the workers. Runs created and finished while it watches
// write nothing to the ledger and tell no one, and it does not read the
// window.
func TestCacheOnlyGateAppendsNothing(t *testing.T) {
	client := fakeClient(run("old", "bifrost-ci-platform", shaA, "Unknown", "Running"))
	l := &memLedger{}
	g := startCache(t, client, l, func(run, _ string) { t.Errorf("told of %s", run) })
	ctx := context.Background()
	if _, err := client.Resource(PipelineRunGVR).Namespace(ns).Create(ctx, run("new", "bifrost-ci-platform", shaB, "", ""), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resource(PipelineRunGVR).Namespace(ns).Update(ctx, run("old", "bifrost-ci-platform", shaA, "True", "Succeeded"), metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// The positive control: the watcher saw both.
	eventually(t, "the cache sees both", func() bool {
		o, _ := g.Get("old")
		_, ok := g.Get("new")
		return ok && o.Status == "True"
	})
	settle()
	if n := l.count(TypeRunStarted) + l.count(TypeRunFinished) + l.count(TypeWindowChanged); n != 0 {
		t.Fatalf("a cache-only gate appended %d events", n)
	}
	for _, a := range client.Actions() {
		if a.GetResource().Resource == ConfigMapGVR.Resource {
			t.Fatalf("a cache-only gate read the window: %v", a)
		}
	}
}

// 2026-11-16: bus step 2 batch 2: the cache says it is not synced until the
// runs were listed, and readers then fall back to their own list.
func TestNotSyncedWithoutAList(t *testing.T) {
	g := &Gate{CacheOnly: true, Namespace: ns, Backoff: 10 * time.Millisecond,
		Dynamic: func() (dynamic.Interface, error) { return nil, errors.New("no cluster") }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safego.Go("test-run-cache-down", func() { defer close(done); g.Run(ctx) })
	settle()
	if g.Synced() || len(g.Snapshot()) != 0 {
		t.Error("synced without a list")
	}
	cancel()
	<-done
}
