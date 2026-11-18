//bifrost:owner delivery

package delivery

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/gatecluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

var cacheT0 = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// cacheRun is a retained run as the cluster keeps it, with the heavy fields
// the cache leaves behind.
func cacheRun(name, pipeline, status, reason string, at time.Duration, labelled bool) *unstructured.Unstructured {
	created := cacheT0.Add(at)
	labels := map[string]any{"bifrost.io/repo": "bifrost-platform"}
	if labelled {
		labels["tekton.dev/pipeline"] = pipeline
	}
	r := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": labels,
			"creationTimestamp": created.Format(time.RFC3339),
			"managedFields":     []any{map[string]any{"manager": "tekton"}}},
		"spec": map[string]any{"pipelineRef": map[string]any{"name": pipeline}, "params": []any{
			map[string]any{"name": "revision", "value": strings.Repeat("a", 40)},
			map[string]any{"name": "repo", "value": "bifrost-platform"},
			map[string]any{"name": "uiRevision", "value": "main"}}},
		"status": map[string]any{"pipelineSpec": map[string]any{"tasks": []any{"x"}},
			"startTime": created.Add(time.Second).Format(time.RFC3339)},
	}}
	if status != "" {
		_ = unstructured.SetNestedSlice(r.Object, []any{map[string]any{"type": "Succeeded", "status": status, "reason": reason, "message": "long"}}, "status", "conditions")
	}
	if status == "True" || status == "False" {
		_ = unstructured.SetNestedField(r.Object, created.Add(time.Minute).Format(time.RFC3339), "status", "completionTime")
	}
	return r
}

func cacheRuns() []runtime.Object {
	return []runtime.Object{
		cacheRun("deliver-stg-1", "bifrost-deliver-stg", "True", "Succeeded", 0, true),
		cacheRun("deliver-stg-2", "bifrost-deliver-stg", "False", "Failed", time.Hour, true),
		cacheRun("deliver-stg-3", "bifrost-deliver-stg", "Unknown", "Running", 2*time.Hour, true),
		cacheRun("ci-platform-1", "bifrost-ci-platform", "", "", 3*time.Hour, true),
		// An older run without the pipeline label: found by its pipelineRef.
		cacheRun("legacy-1", "bifrost-legacy", "True", "Succeeded", 4*time.Hour, false),
	}
}

// listCounting is a fake cluster holding objs that counts PipelineRun lists.
func listCounting(objs ...runtime.Object) (*dynfake.FakeDynamicClient, *atomic.Int64) {
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList", gatecluster.ConfigMapGVR: "ConfigMapList",
		// The supply chain also lists Pipelines and Tasks (OWNER_LAW #30, 2026-11-17).
		pipelineGVR: "PipelineList", taskGVR: "TaskList"}, objs...)
	var lists atomic.Int64
	dyn.PrependReactor("list", "pipelineruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		lists.Add(1)
		return false, nil, nil
	})
	return dyn, &lists
}

func cacheService(dyn dynamic.Interface) *Service {
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	s.SetClientsetForTest(k8sfake.NewSimpleClientset())
	return s
}

// liveCache runs the bus cluster gate as a cache over dyn and waits until it is synced.
func liveCache(t *testing.T, dyn dynamic.Interface) *gatecluster.Gate {
	t.Helper()
	g := &gatecluster.Gate{CacheOnly: true, Namespace: "cicd", Backoff: 20 * time.Millisecond,
		Dynamic: func() (dynamic.Interface, error) { return dyn, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safego.Go("test-delivery-run-cache", func() { defer close(done); g.Run(ctx) })
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for !g.Synced() {
		if time.Now().After(deadline) {
			t.Fatal("the run cache did not sync")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return g
}

// capture keeps the side-by-side log lines of s.
func capture(s *Service) func() string {
	var mu sync.Mutex
	var lines []string
	s.readers.Load().logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(lines, "\n")
	}
}

func readerCounts(route string) (compared, mismatch, served int64) {
	return readerMetric(&metricReadersCompared, route).Load(), readerMetric(&metricReadersMismatch, route).Load(),
		readerMetric(&metricReadersServed, route).Load()
}

// 2026-11-16: bus step 2 batch 2 (the Console's run pages listed every
// retained run on each poll, one of them with no selector): over the same
// runs, each switched route compares clean with the bus run cache N times in
// a row (counted, no mismatch), then answers from the cache exactly as the
// list did and lists no more.
func TestDisplayReadsMatchTheListThenStopListing(t *testing.T) {
	t.Setenv(EnvReadersCompareN, "3")
	dyn, lists := listCounting(cacheRuns()...)
	g := liveCache(t, dyn)
	s := cacheService(dyn)
	s.SetRunCache(g, nil)
	logs := capture(s)
	ctx := context.Background()
	read := map[string]func() any{
		routePipelineRuns: func() any {
			return []any{s.PipelineRuns(ctx, "bifrost-deliver-stg").Runs, s.PipelineRuns(ctx, "bifrost-legacy").Runs}
		},
		routeSupplyChain: func() any {
			sc := s.SupplyChain(ctx)
			return []any{sc.LastDeliverRun, sc.LastDeliverSuccess}
		},
		routeReleaseContext: func() any {
			runs, err := runsInFlight(ctx, ReleaseContextSources{Runs: s.cachedRunPage}, "bifrost-platform")
			if err != nil {
				t.Fatal(err)
			}
			return runs
		},
	}
	for _, route := range []string{routePipelineRuns, routeReleaseContext, routeSupplyChain} {
		c0, m0, _ := readerCounts(route)
		// The first read is all from the list (the pipeline runs read makes
		// two calls, so its third comparison comes in its second read).
		listed := read[route]()
		for i := 0; i < 2; i++ {
			read[route]()
		}
		c1, m1, s1 := readerCounts(route)
		if m1 != m0 || c1-c0 != 3 {
			t.Fatalf("%s: compared %d, mismatches %d (logs %s)", route, c1-c0, m1-m0, logs())
		}
		before := lists.Load()
		cached := read[route]()
		if n := lists.Load() - before; n != 0 {
			t.Errorf("%s: %d run lists after %d clean comparisons", route, n, 3)
		}
		if !reflect.DeepEqual(cached, listed) {
			t.Errorf("%s: the cache answers %#v, the list answered %#v", route, cached, listed)
		}
		if _, _, s2 := readerCounts(route); s2 == s1 {
			t.Errorf("%s: not counted as served from the cache", route)
		}
	}
	// The positive control of "the answer is the list's": the reads say something.
	if got := s.PipelineRuns(ctx, "bifrost-legacy").Runs; len(got) != 1 || got[0].Name != "legacy-1" {
		t.Errorf("legacy runs %+v", got)
	}
	if sc := s.SupplyChain(ctx); sc.LastDeliverRun == nil || sc.LastDeliverRun.Name != "deliver-stg-3" || sc.LastDeliverSuccess.Name != "deliver-stg-1" {
		t.Errorf("supply chain %+v %+v", sc.LastDeliverRun, sc.LastDeliverSuccess)
	}
	if !strings.Contains(logs(), "lists no more") {
		t.Errorf("trust not logged: %s", logs())
	}
}

// fakeCache is a run cache that answers what it is given.
type fakeCache struct {
	synced bool
	objs   []unstructured.Unstructured
}

func (f *fakeCache) Synced() bool { return f.synced }
func (f *fakeCache) Objects(pipeline string) []unstructured.Unstructured {
	var out []unstructured.Unstructured
	for _, o := range f.objs {
		if pipeline == "" || o.GetLabels()["tekton.dev/pipeline"] == pipeline {
			out = append(out, o)
		}
	}
	return out
}

// 2026-11-16: bus step 2 batch 2: without a cache, or while it is not
// synced, a route lists as it always did and compares nothing.
func TestDisplayReadsFallBackWhenTheCacheIsNotSynced(t *testing.T) {
	dyn, lists := listCounting(cacheRuns()...)
	s := cacheService(dyn)
	ctx := context.Background()
	want := s.PipelineRuns(ctx, "bifrost-deliver-stg").Runs
	if len(want) != 3 || lists.Load() == 0 {
		t.Fatalf("without a cache: %d runs, %d lists", len(want), lists.Load())
	}
	stale := *cacheRun("ghost", "bifrost-deliver-stg", "True", "Succeeded", 9*time.Hour, true)
	s.SetRunCache(&fakeCache{synced: false, objs: []unstructured.Unstructured{stale}}, nil)
	c0, _, s0 := readerCounts(routePipelineRuns)
	before := lists.Load()
	got := s.PipelineRuns(ctx, "bifrost-deliver-stg").Runs
	if !reflect.DeepEqual(got, want) || lists.Load() == before {
		t.Fatalf("not synced: answered %+v (want the list's %+v), lists %d", got, want, lists.Load()-before)
	}
	if c1, _, s1 := readerCounts(routePipelineRuns); c1 != c0 || s1 != s0 {
		t.Fatalf("not synced yet compared %d, served %d", c1-c0, s1-s0)
	}
	before = lists.Load()
	if _, next, err := s.cachedRunPage(ctx, ""); err != nil || next != "" || lists.Load() == before {
		t.Fatalf("release context not synced: next %q err %v lists %d", next, err, lists.Load()-before)
	}
}

// 2026-11-16: bus step 2 batch 2: a difference between the cache and the
// list is logged by run name and counted, the list's answer stands, and the
// clean count starts again (the route keeps listing).
func TestDisplayReadMismatchIsCountedAndLogged(t *testing.T) {
	t.Setenv(EnvReadersCompareN, "2")
	dyn, lists := listCounting(cacheRuns()...)
	s := cacheService(dyn)
	ctx := context.Background()
	want := s.PipelineRuns(ctx, "bifrost-deliver-stg").Runs
	var good []unstructured.Unstructured
	for _, o := range cacheRuns() {
		u := o.(*unstructured.Unstructured)
		if u.GetLabels()["tekton.dev/pipeline"] == "bifrost-deliver-stg" {
			good = append(good, *u)
		}
	}
	// The cache lags: deliver-stg-3 still running in the list is finished in
	// the cache, and deliver-stg-1 is missing.
	wrong := []unstructured.Unstructured{good[1], *cacheRun("deliver-stg-3", "bifrost-deliver-stg", "True", "Succeeded", 2*time.Hour, true)}
	fc := &fakeCache{synced: true, objs: good}
	s.SetRunCache(fc, nil)
	logs := capture(s)
	_, m0, _ := readerCounts(routePipelineRuns)
	s.PipelineRuns(ctx, "bifrost-deliver-stg") // clean 1
	fc.objs = wrong
	got := s.PipelineRuns(ctx, "bifrost-deliver-stg").Runs
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the cache's answer stood: %+v", got)
	}
	if _, m1, _ := readerCounts(routePipelineRuns); m1-m0 != 1 {
		t.Fatalf("%d mismatches counted, want 1", m1-m0)
	}
	if l := logs(); !strings.Contains(l, "only listed: deliver-stg-1") || !strings.Contains(l, "differ: deliver-stg-3") {
		t.Fatalf("the difference is not named: %s", l)
	}
	// The count started again: one clean comparison after the mismatch is not
	// enough for N=2, the route still lists.
	fc.objs = good
	s.PipelineRuns(ctx, "bifrost-deliver-stg")
	before := lists.Load()
	s.PipelineRuns(ctx, "bifrost-deliver-stg") // clean 2: trusted after this one
	if lists.Load() == before {
		t.Fatal("trusted before N clean comparisons in a row")
	}
	before = lists.Load()
	s.PipelineRuns(ctx, "bifrost-deliver-stg")
	if lists.Load() != before {
		t.Fatal("still listing after N clean comparisons in a row")
	}
}

// 2026-11-16: bus step 2 batch 2: the readings are written per route, zeros
// included.
func TestBusReaderMetricsNameEachRoute(t *testing.T) {
	var b strings.Builder
	WriteBusReaderMetrics(&b)
	for _, route := range cacheRoutes {
		for _, name := range []string{"bifrost_bus_readers_compared_total", "bifrost_bus_readers_mismatch_total", "bifrost_bus_readers_cache_served_total"} {
			if !strings.Contains(b.String(), fmt.Sprintf("%s{route=%q} ", name, route)) {
				t.Errorf("%s{route=%q} missing:\n%s", name, route, b.String())
			}
		}
	}
}

// 2026-11-17: trust lived only in memory, so each PROD release put the three
// display routes back on their lists (02:36: pipeline_runs at 22 of 50 after
// the 01:11 restart, the other two at 0 with nobody viewing at night). An
// earned route starts trusted, still compares one read in sampleEvery, and a
// sampled difference takes its trust away.
func TestEarnedRouteStartsTrustedSamplesAndLosesTrustOnDifference(t *testing.T) {
	dyn, lists := listCounting(cacheRuns()...)
	s := cacheService(dyn)
	ctx := context.Background()
	var good []unstructured.Unstructured
	for _, o := range cacheRuns() {
		u := o.(*unstructured.Unstructured)
		if u.GetLabels()["tekton.dev/pipeline"] == "bifrost-deliver-stg" {
			good = append(good, *u)
		}
	}
	fc := &fakeCache{synced: true, objs: good}
	s.SetRunCache(fc, map[string]bool{routePipelineRuns: true})
	logs := capture(s)

	// Earned: the first read after a restart lists nothing.
	before := lists.Load()
	s.PipelineRuns(ctx, "bifrost-deliver-stg")
	if lists.Load() != before {
		t.Fatal("an earned route listed on its first read after a restart")
	}
	// One read in sampleEvery is compared with the list.
	for i := 1; i < sampleEvery-1; i++ {
		s.PipelineRuns(ctx, "bifrost-deliver-stg")
	}
	if lists.Load() != before {
		t.Fatalf("listed %d times before the sampled read", lists.Load()-before)
	}
	c0, m0, _ := readerCounts(routePipelineRuns)
	fc.objs = good[1:] // the cache lost a run: the sampled read sees it
	s.PipelineRuns(ctx, "bifrost-deliver-stg")
	if lists.Load() == before {
		t.Fatal("the sampled read did not list")
	}
	if c1, m1, _ := readerCounts(routePipelineRuns); c1-c0 != 1 || m1-m0 != 1 {
		t.Fatalf("sampled read: %d compared, %d differences; want 1 and 1", c1-c0, m1-m0)
	}
	if !strings.Contains(logs(), "a sampled read differs") {
		t.Fatalf("losing trust is not logged: %s", logs())
	}
	// Trust is gone: the next read lists again.
	before = lists.Load()
	s.PipelineRuns(ctx, "bifrost-deliver-stg")
	if lists.Load() == before {
		t.Fatal("still answering from the cache after a sampled difference")
	}
}

// 2026-11-18 (bus/RULES.md §10 R3): the promote checks read a delivery
// pipeline's runs like the display routes: the list answers and is compared
// until the route has its clean comparisons, then the cache answers and the
// cluster is not listed.
// Positive control: GateRuns calling listed every time keeps the list count
// rising, and the last check fails.
func TestPromoteReadsEarnTheCache(t *testing.T) {
	t.Setenv(EnvReadersCompareN, "2")
	ctx := context.Background()
	const p = "bifrost-deliver-prod"
	dyn, lists := listCounting(cacheRun("deliver-a", p, "True", "Succeeded", 0, true), cacheRun("deliver-b", p, "False", "Failed", time.Hour, true))
	s := cacheService(dyn)
	s.SetRunCache(liveCache(t, dyn), nil)
	before, mismatchBefore, _ := readerCounts(routeLastDeliver)
	for i := 0; i < 2; i++ {
		if run := s.lastDeliverRun(ctx, p); run == nil || run.Name != "deliver-b" {
			t.Fatalf("last run = %+v", run)
		}
	}
	after, mismatch, _ := readerCounts(routeLastDeliver)
	if after-before != 2 || mismatch != mismatchBefore {
		t.Fatalf("compared %d, mismatches %d", after-before, mismatch-mismatchBefore)
	}
	n := lists.Load()
	if run := s.lastDeliverSuccess(ctx, p); run == nil || run.Name != "deliver-a" {
		t.Fatalf("last success = %+v", run)
	}
	if lists.Load() != n {
		t.Fatalf("a trusted route listed: %d -> %d", n, lists.Load())
	}
}
