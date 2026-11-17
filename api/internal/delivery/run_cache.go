package delivery

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The display reads of runs read the bus cluster gate's run cache (bus step
// 2, batch 2; api/internal/bus/RULES.md): the Console's pipeline runs, supply
// chain and release context pages listed every run of the namespace on each
// poll (one of them with no label selector at all). Only display reads: what
// gates a release, a CI verdict or a promotion (releases, head-release,
// FullCI, landingci, lastDeliverRun / lastDeliverSuccess,
// ProdDeliverArtifactsReady) keeps its own list until the side-by-side with
// the CI line (RULES.md §10 R3).
//
// Side by side first: while a route has fewer than N clean comparisons in a
// row (BUS_READERS_COMPARE_N, 50), each call still lists, answers from the
// list, and compares the list's answer with the cache's (read before and
// after the list, so a run that changed while the list ran is not a
// difference). A difference is logged by run name, counted, and starts the
// count again. After N in a row the route answers from the cache and lists no
// more. Without the cache, or while it is not synced, the route lists as it
// always did.
//
// Trust used to live only in memory, so every release (a restart) put the
// routes back on their lists until N more pages were viewed; at night, with
// nobody viewing, they never got there. A route whose readings show it
// earned trust (the caller reads them: N clean comparisons and no difference
// over the last days) starts trusted, and a trusted route still compares one
// read in sampleEvery: a difference takes its trust away and it earns it
// again as before.

// RunCache is the cluster gate's read cache (gatecluster.Gate).
type RunCache interface {
	// Synced says the cache answers; false falls back to the list.
	Synced() bool
	// Objects is the runs of a pipeline (its tekton.dev/pipeline label), or
	// every run when pipeline is "", by name, holding only what display
	// reads use.
	Objects(pipeline string) []unstructured.Unstructured
}

// The routes that read the cache, as the metrics name them.
const (
	routePipelineRuns   = "pipeline_runs"
	routeSupplyChain    = "supply_chain"
	routeReleaseContext = "release_context"
)

var cacheRoutes = []string{routePipelineRuns, routeReleaseContext, routeSupplyChain}

// EnvReadersCompareN is how many clean comparisons in a row a route needs
// before it answers from the cache.
const EnvReadersCompareN = "BUS_READERS_COMPARE_N"

const defaultReadersCompareN = 50

// sampleEvery: a trusted route compares one read in this many.
const sampleEvery = 100

func readersCompareN() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvReadersCompareN))); err == nil && n > 0 {
		return n
	}
	return defaultReadersCompareN
}

// cacheReaders is a service's side-by-side state, per route.
type cacheReaders struct {
	cache RunCache
	n     int
	logf  func(format string, args ...any)
	mu    sync.Mutex
	clean map[string]int  // clean comparisons in a row
	trust map[string]bool // N in a row reached (or earned): reads come from the cache
	since map[string]int  // reads served from the cache since the last sample
}

// SetRunCache hands the service the cluster gate's run cache; nil (or never
// calling it) keeps every route on its list. earned are the routes that start
// trusted: the caller computes them from the routes' own side-by-side
// readings (bifrost_bus_readers_compared/mismatch over the last days), not a
// hand-kept list (the Owner, 2026-11-17); nil starts every route comparing.
func (s *Service) SetRunCache(c RunCache, earned map[string]bool) {
	if c == nil {
		s.readers.Store(nil)
		return
	}
	trust := map[string]bool{}
	for route, ok := range earned {
		if ok {
			trust[route] = true
		}
	}
	s.readers.Store(&cacheReaders{cache: c, n: readersCompareN(), clean: map[string]int{}, trust: trust, since: map[string]int{},
		logf: func(format string, args ...any) { slog.Info(fmt.Sprintf(format, args...)) }})
}

// SetRunCache hands the service the cluster gate's run cache (wiring).
func (h *Handler) SetRunCache(c RunCache, earned map[string]bool) { h.svc.SetRunCache(c, earned) }

// CacheRoutes are the display routes that read the run cache, as the
// readings name them.
func CacheRoutes() []string { return append([]string(nil), cacheRoutes...) }

// ReadersCompareN is how many clean comparisons a route needs to be trusted.
func ReadersCompareN() int { return readersCompareN() }

// syncedCache is the cache when it answers now, else nil.
func (s *Service) syncedCache() *cacheReaders {
	r := s.readers.Load()
	if r == nil || r.cache == nil || !r.cache.Synced() {
		return nil
	}
	return r
}

// serve says whether this read answers from the cache alone: the route is
// trusted and this is not its sampled read.
func (r *cacheReaders) serve(route string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.trust[route] {
		return false
	}
	r.since[route]++
	if r.since[route] >= sampleEvery {
		r.since[route] = 0
		return false
	}
	return true
}

// record counts one comparison; diff is "" when it was clean.
func (r *cacheReaders) record(route, diff string) {
	readerMetric(&metricReadersCompared, route).Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if diff != "" {
		readerMetric(&metricReadersMismatch, route).Add(1)
		r.clean[route] = 0
		if r.trust[route] {
			r.trust[route] = false
			r.logf("bus readers: %s: a sampled read differs; it lists again until %d clean comparisons in a row", route, r.n)
		}
		r.logf("bus readers: %s: the run cache and the list differ (the list answered): %s", route, diff)
		return
	}
	r.clean[route]++
	if r.clean[route] >= r.n && !r.trust[route] {
		r.trust[route] = true
		r.logf("bus readers: %s: %d clean comparisons in a row; it answers from the run cache and lists no more", route, r.n)
	}
}

// readThrough answers a display read: from listed while there is no synced
// cache; from listed, compared with cached, while the route is not trusted;
// from cached once it is. keys is what is compared, in order: one string per
// run, "<name>\x00<what is shown of it>".
func readThrough[T any](s *Service, route string, cached func(RunCache) T, listed func() (T, error), keys func(T) []string) (T, error) {
	r := s.syncedCache()
	if r == nil {
		return listed()
	}
	if r.serve(route) {
		readerMetric(&metricReadersServed, route).Add(1)
		return cached(r.cache), nil
	}
	before := keys(cached(r.cache))
	v, err := listed()
	if err != nil {
		return v, err
	}
	want := keys(v)
	diff := diffKeys(want, before)
	if diff != "" {
		diff = diffKeys(want, keys(cached(r.cache)))
	}
	r.record(route, diff)
	return v, nil
}

// diffKeys names the runs whose keys differ between the list's answer and
// the cache's, or says the order differs; "" when they are the same.
func diffKeys(list, cache []string) string {
	if strings.Join(list, "\x01") == strings.Join(cache, "\x01") {
		return ""
	}
	split := func(keys []string) (map[string]string, []string) {
		m := map[string]string{}
		var order []string
		for _, k := range keys {
			name, rest, _ := strings.Cut(k, "\x00")
			m[name] = rest
			order = append(order, name)
		}
		return m, order
	}
	lm, lo := split(list)
	cm, co := split(cache)
	var onlyList, onlyCache, changed []string
	for name, v := range lm {
		cv, ok := cm[name]
		switch {
		case !ok:
			onlyList = append(onlyList, name)
		case cv != v:
			changed = append(changed, fmt.Sprintf("%s (list %q, cache %q)", name, strings.ReplaceAll(v, "\x00", "|"), strings.ReplaceAll(cv, "\x00", "|")))
		}
	}
	for name := range cm {
		if _, ok := lm[name]; !ok {
			onlyCache = append(onlyCache, name)
		}
	}
	sort.Strings(onlyList)
	sort.Strings(onlyCache)
	sort.Strings(changed)
	var parts []string
	if len(onlyList) > 0 {
		parts = append(parts, "only listed: "+strings.Join(onlyList, ", "))
	}
	if len(onlyCache) > 0 {
		parts = append(parts, "only cached: "+strings.Join(onlyCache, ", "))
	}
	if len(changed) > 0 {
		parts = append(parts, "differ: "+strings.Join(changed, "; "))
	}
	if len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("order differs: list %s, cache %s", strings.Join(lo, ","), strings.Join(co, ",")))
	}
	return strings.Join(parts, "; ")
}

// viewKey is what a page shows of a run view.
func viewKey(v PipelineRunView) string {
	return strings.Join([]string{v.Name, v.Pipeline, v.Revision, v.Status, v.Reason, v.StartTime, v.CompletionTime}, "\x00")
}

func viewKeys(views []PipelineRunView) []string {
	out := make([]string, len(views))
	for i, v := range views {
		out[i] = viewKey(v)
	}
	return out
}

// objectKey is what the release context reads of a listed run: its
// pipeline, Succeeded condition, times, and the labels and params it matches
// a repository by.
func objectKey(run *unstructured.Unstructured) string {
	status, reason := "", ""
	conds, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, c := range conds {
		if m, ok := c.(map[string]any); ok && m["type"] == "Succeeded" {
			status, _ = m["status"].(string)
			reason, _ = m["reason"].(string)
		}
	}
	params := map[string]string{}
	ps, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, p := range ps {
		m, _ := p.(map[string]any)
		name, _ := m["name"].(string)
		if v, ok := m["value"].(string); ok {
			params[name] = v
		}
	}
	ref, _, _ := unstructured.NestedString(run.Object, "spec", "pipelineRef", "name")
	start, _, _ := unstructured.NestedString(run.Object, "status", "startTime")
	done, _, _ := unstructured.NestedString(run.Object, "status", "completionTime")
	l := run.GetLabels()
	return strings.Join([]string{run.GetName(), l["tekton.dev/pipeline"], ref, status, reason, start, done,
		run.GetCreationTimestamp().UTC().Format("2006-01-02T15:04:05Z"),
		l["bifrost.io/repo"], l["bifrost.io/revision"], params["revision"], params["repo"], params["repo-url"], params["url"]}, "\x00")
}

func objectKeys(items []unstructured.Unstructured) []string {
	out := make([]string, len(items))
	for i := range items {
		out[i] = objectKey(&items[i])
	}
	return out
}

var metricReadersCompared, metricReadersMismatch, metricReadersServed sync.Map // route -> *atomic.Int64

func readerMetric(m *sync.Map, route string) *atomic.Int64 {
	v, _ := m.LoadOrStore(route, new(atomic.Int64))
	return v.(*atomic.Int64)
}

// WriteBusReaderMetrics writes the side-by-side readings of the routes that
// read the run cache; a process without the cache writes zeros.
func WriteBusReaderMetrics(b *strings.Builder) {
	for _, m := range []struct {
		name, help string
		v          *sync.Map
	}{
		{"bifrost_bus_readers_compared_total", "Display reads of runs answered by the list and compared with the bus run cache", &metricReadersCompared},
		{"bifrost_bus_readers_mismatch_total", "Comparisons in which the list and the bus run cache differed (logged by run name)", &metricReadersMismatch},
		{"bifrost_bus_readers_cache_served_total", "Display reads of runs answered from the bus run cache without a list", &metricReadersServed},
	} {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n", m.name, m.help, m.name)
		for _, route := range cacheRoutes {
			fmt.Fprintf(b, "%s{route=%q} %d\n", m.name, route, readerMetric(m.v, route).Load())
		}
	}
}

// CachedRunsOf is a pipeline's runs from the bus run cache when it is synced
// (ok false otherwise): what head-release reads each pass instead of listing
// every run of its pipeline (2026-11-17, ~470 LISTs in 10 minutes). The
// start itself still reads the cluster (StartDeliveryIfFree).
func (s *Service) CachedRunsOf(pipeline string) ([]unstructured.Unstructured, bool) {
	r := s.syncedCache()
	if r == nil {
		return nil, false
	}
	return r.cache.Objects(pipeline), true
}

// CachedRunsOf is Service.CachedRunsOf for the server's wiring.
func (h *Handler) CachedRunsOf(pipeline string) ([]unstructured.Unstructured, bool) {
	return h.svc.CachedRunsOf(pipeline)
}
