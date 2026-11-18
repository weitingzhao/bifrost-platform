// Package gatecluster is gate-cluster, the bus's gate for the cluster
// (api/internal/bus/RULES.md §1 E3, §7): it watches the pipelines
// namespace's PipelineRuns and the release window's ConfigMap and writes
// what changed into the ledger: ci.run.started when a run is created,
// ci.run.finished once when its Succeeded condition turns True or False,
// release.window.changed when the window's holder, end or queue changes. The
// relay takes the events to the bus from there. It only publishes: no reader
// of the window reads it yet. Its run map is also a read cache for the
// display reads of runs (cache.go); the api process runs a cache-only gate
// for them (CacheOnly), which writes nothing.
//
// It imports no domain package: the ledger, the dynamic client and the
// namespace are handed in by the server's wiring.
package gatecluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Gate is gate-cluster.
type Gate struct {
	Ledger    Ledger
	Dynamic   func() (dynamic.Interface, error)
	Namespace string
	// Env is this ledger's environment: a run's cause label is taken only
	// when its LabelCauseEnv names it (PROD and STG watch the same runs, and
	// an event number belongs to one ledger).
	Env  string
	Logf func(format string, args ...any)
	// Now is the clock (tests set it).
	Now func() time.Time
	// ResyncEvery lists the runs again although the watch holds (30 min):
	// what a watch missed without saying so is made up then (the register's
	// reconcile: "gate-cluster relist of PipelineRuns"). WindowEvery is how
	// often the window is looked at without a write (30 s): a window's TTL
	// passes without one. Backoff is the wait after a failed list or watch.
	ResyncEvery, WindowEvery, Backoff time.Duration
	// OnFinished, when set, is told each run whose ci.run.finished the ledger
	// now holds (written by this call, or already there), in this process
	// and at once: the workers' pipeline records are woken by it (bus step
	// 2). It is called from the runs loop and must return at once; nil tells
	// no one.
	OnFinished func(run, pipeline string)
	// CacheOnly makes the gate a read cache and nothing else (the api
	// process, bus step 2 batch 2): it lists and watches the runs as the
	// workers' gate does, writes nothing to the ledger, tells no one, and
	// does not watch the window. Appending stays in the workers.
	CacheOnly bool
	// WatchTasks also keeps the namespace's TaskRuns for the display reads
	// (taskruns.go). Only the process that serves those reads sets it.
	WatchTasks bool
	// UnitTaskLabel, when set, also keeps whole the TaskRuns carrying that
	// label: the unit TaskRuns (unit_taskruns.go).
	UnitTaskLabel string

	// apps is the last state said of each Argo application (gitops.go).
	appMu sync.Mutex
	apps  map[string]AppState

	// The TaskRun cache: each kept TaskRun by its PipelineRun and name, and
	// the PipelineRun of each name (taskruns.go).
	taskMu      sync.RWMutex
	tasksByRun  map[string]map[string]TaskRun
	taskRunOf   map[string]string
	tasksSynced atomic.Bool
	unitMu      sync.RWMutex
	units       map[string]*unstructured.Unstructured
	unitsSynced atomic.Bool

	// runs is what the runs loop knows of each run; only that loop writes
	// it, under mu; the cache's readers read it under mu (cache.go).
	mu   sync.RWMutex
	runs map[string]runFacts
	// synced: the runs were listed in full and the last list or watch did
	// not fail (Synced).
	synced atomic.Bool
	// winDigest is the window's digest the ledger last agreed with; only the
	// window loop touches it.
	winDigest string
	// watching is closed once both watches are first in place (tests wait on it).
	watching      chan struct{}
	runsUp, winUp atomic.Bool
	upOnce        sync.Once
	initOnce      sync.Once
}

func (g *Gate) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
	}
}

func (g *Gate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Gate) backoff() time.Duration {
	if g.Backoff > 0 {
		return g.Backoff
	}
	return 15 * time.Second
}

func (g *Gate) init() {
	g.initOnce.Do(func() {
		g.runs = map[string]runFacts{}
		g.apps = map[string]AppState{}
		g.tasksByRun = map[string]map[string]TaskRun{}
		g.taskRunOf = map[string]string{}
		g.units = map[string]*unstructured.Unstructured{}
		g.watching = make(chan struct{})
	})
}

// Watching is closed once the runs and the window (the runs alone in a
// cache-only gate) are first watched.
func (g *Gate) Watching() <-chan struct{} {
	g.init()
	return g.watching
}

func (g *Gate) up(which *atomic.Bool) {
	which.Store(true)
	if g.runsUp.Load() && (g.winUp.Load() || g.CacheOnly) {
		g.upOnce.Do(func() { close(g.watching) })
	}
}

// append writes one event and counts it; it says whether the ledger now
// agrees (written, or already there). An error is logged and counted, not
// retried: the next observation or relist says it again.
func (g *Gate) append(ctx context.Context, stream, typ string, cause int64, decide Decide) bool {
	ok, err := g.Ledger.Append(ctx, stream, typ, cause, decide)
	if err != nil {
		metricErrors.Add(1)
		g.logf("bus gate-cluster: %s on %s: %v", typ, stream, err)
		return false
	}
	if ok {
		metricAppended.Add(1)
		metricLastEvent.Store(g.now().Unix())
	}
	return true
}

// Run watches until ctx ends: the runs and the window, each in its own
// goroutine; a cache-only gate the runs alone.
func (g *Gate) Run(ctx context.Context) {
	g.init()
	if g.WatchTasks {
		safego.Go("bus-gate-cluster-tasks", func() { g.runTasks(ctx) })
	}
	if g.UnitTaskLabel != "" {
		safego.Go("bus-gate-cluster-unit-tasks", func() { g.runUnitTasks(ctx) })
	}
	if g.CacheOnly {
		g.runRuns(ctx)
		return
	}
	var wg sync.WaitGroup
	wg.Add(3)
	safego.Go("bus-gate-cluster-apps", func() { defer wg.Done(); g.runApps(ctx) })
	safego.Go("bus-gate-cluster-runs", func() { defer wg.Done(); g.runRuns(ctx) })
	safego.Go("bus-gate-cluster-window", func() { defer wg.Done(); g.runWindow(ctx) })
	wg.Wait()
}

func (g *Gate) sleep(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(g.backoff()):
		return true
	}
}

// watchTimeout is how long one watch request is held before it is opened
// again from its last resourceVersion.
func watchTimeout() *int64 { s := int64(300); return &s }

// runRuns lists the runs once without saying anything (history is not
// replayed), then watches from the list's resourceVersion; a watch that ends
// is opened again from where it was, one the server says is too old (or an
// error) lists again, and so does each ResyncEvery.
func (g *Gate) runRuns(ctx context.Context) {
	resync := g.ResyncEvery
	if resync <= 0 {
		resync = 30 * time.Minute
	}
	first, list := true, true
	var rv string
	var listed time.Time
	for ctx.Err() == nil {
		if list {
			v, err := g.listRuns(ctx, first)
			if err != nil {
				g.synced.Store(false)
				metricErrors.Add(1)
				g.logf("bus gate-cluster: list runs: %v", err)
				if !g.sleep(ctx) {
					return
				}
				continue
			}
			rv, list, first, listed = v, false, false, g.now()
			g.synced.Store(true)
		}
		dyn, err := g.Dynamic()
		var w watch.Interface
		if err == nil {
			w, err = dyn.Resource(PipelineRunGVR).Namespace(g.Namespace).Watch(ctx, metav1.ListOptions{
				ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: watchTimeout()})
		}
		if err != nil {
			g.synced.Store(false)
			metricRestarts.Add(1)
			g.logf("bus gate-cluster: watch runs: %v", err)
			list = true
			if !g.sleep(ctx) {
				return
			}
			continue
		}
		g.up(&g.runsUp)
		rv, err = consume(ctx, w, rv, g.observeRunEvent(ctx))
		if ctx.Err() != nil {
			return
		}
		metricRestarts.Add(1)
		if err != nil {
			if !errors.Is(err, errGone) {
				g.logf("bus gate-cluster: watch runs ended: %v", err)
			}
			list = true
		}
		if g.now().Sub(listed) >= resync {
			list = true
		}
	}
}

// runWindow reads the window's ConfigMap (by name) and watches it; each
// record it sees, and the last one again every WindowEvery, is said to the
// ledger when it changed.
func (g *Gate) runWindow(ctx context.Context) {
	every := g.WindowEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	byName := fields.OneTermEqualSelector("metadata.name", WindowConfigMap).String()
	var last windowRecord
	for ctx.Err() == nil {
		dyn, err := g.Dynamic()
		var rv string
		if err == nil {
			res := dyn.Resource(ConfigMapGVR).Namespace(g.Namespace)
			var page *unstructured.UnstructuredList
			if page, err = res.List(ctx, metav1.ListOptions{FieldSelector: byName}); err == nil {
				rv = page.GetResourceVersion()
				last = windowRecord{}
				for i := range page.Items {
					if page.Items[i].GetName() == WindowConfigMap {
						last = parseWindow(&page.Items[i])
					}
				}
				g.observeWindow(ctx, last)
			}
		}
		var w watch.Interface
		if err == nil {
			w, err = dyn.Resource(ConfigMapGVR).Namespace(g.Namespace).Watch(ctx, metav1.ListOptions{
				FieldSelector: byName, ResourceVersion: rv, AllowWatchBookmarks: true, TimeoutSeconds: watchTimeout()})
		}
		if err != nil {
			metricErrors.Add(1)
			g.logf("bus gate-cluster: window: %v", err)
			if !g.sleep(ctx) {
				return
			}
			continue
		}
		g.up(&g.winUp)
		if g.consumeWindow(ctx, w, every, &last) != nil && ctx.Err() == nil && !g.sleep(ctx) {
			return
		}
		if ctx.Err() == nil {
			metricRestarts.Add(1)
		}
	}
}

// consumeWindow reads one watch of the window until it ends.
func (g *Gate) consumeWindow(ctx context.Context, w watch.Interface, every time.Duration, last *windowRecord) error {
	defer w.Stop()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			g.observeWindow(ctx, *last)
		case ev, ok := <-w.ResultChan():
			if !ok {
				return nil
			}
			switch ev.Type {
			case watch.Error:
				return fmt.Errorf("window watch: %v", ev.Object)
			case watch.Bookmark:
				continue
			case watch.Deleted:
				*last = windowRecord{}
			default:
				obj, ok := ev.Object.(*unstructured.Unstructured)
				if !ok || obj.GetName() != WindowConfigMap {
					continue
				}
				*last = parseWindow(obj)
			}
			g.observeWindow(ctx, *last)
		}
	}
}

var (
	metricAppended, metricRestarts, metricErrors atomic.Int64
	metricLastEvent                              atomic.Int64
)

// WriteMetrics writes gate-cluster's readings in the exposition format; a
// process that runs no gate writes zeros.
func WriteMetrics(b *strings.Builder) {
	for _, m := range []struct {
		name, help, kind string
		v                int64
	}{
		{"bifrost_bus_gate_cluster_events_appended_total", "Events gate-cluster appended to the ledger (ci.run.started, ci.run.finished, release.window.changed)", "counter", metricAppended.Load()},
		{"bifrost_bus_gate_cluster_watch_restarts_total", "Times a gate-cluster watch ended and was opened again (or listed again)", "counter", metricRestarts.Load()},
		{"bifrost_bus_gate_cluster_errors_total", "gate-cluster lists, watches and ledger writes that failed", "counter", metricErrors.Load()},
		{"bifrost_bus_gate_cluster_last_event_timestamp_seconds", "Unix time of gate-cluster's last appended event (0: none since the process started)", "gauge", metricLastEvent.Load()},
	} {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", m.name, m.help, m.name, m.kind, m.name, m.v)
	}
}
