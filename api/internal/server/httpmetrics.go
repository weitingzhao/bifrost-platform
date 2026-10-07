package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Request count and latency in the names and labels the Trade, research and
// plugin APIs export, so the cluster's BifrostAPIHighErrorRate and
// BifrostAPIHighLatency rules read platform-api too (TD-195). Until then it was
// the one target BifrostAPIWithoutHttpMetrics exempted: a platform-api answering
// 5xx raised no API alert.
//
// Hand-written like the rest of /metrics: the module has no Prometheus client,
// and two families do not justify adding one.
//
//	http_requests_total{handler,method,status}             counter, status is the class ("5xx")
//	http_request_duration_seconds{handler,method}          histogram, buckets below
//
// handler is the chi route pattern ("/api/v1/environments/{id}"), so the label
// set is bounded by the routes in the code. A request no route matches (404 from
// the router, 405, unknown methods, CORS preflights answered before routing) is
// not recorded at all: its path is client-chosen.

// Buckets reach past the 2 s the latency rule alerts on. The Trade histograms
// stop at 1 s, which is why that rule cannot fire for them (TD-194).
var httpDurationBuckets = [...]float64{0.1, 0.5, 1, 2.5, 5, 10}

// Not recorded: scraping /metrics is not traffic, and it probes four plugins for
// up to 8 s each scrape, which would read as the API being slow.
const metricsRoute = "/metrics"

// Counted (kubelet probes it every few seconds, so a healthy pod has series at
// once) but kept out of latency, where its volume would drown the real routes.
const healthRoute = "/health"

var statusClasses = [...]string{"", "1xx", "2xx", "3xx", "4xx", "5xx"}

type httpRequestKey struct{ handler, method, status string }

type httpLatencyKey struct{ handler, method string }

// One slot per bucket plus +Inf, each counting only its own range; the render
// cumulates them, so _count and the +Inf bucket are the same number by
// construction even while requests land mid-scrape.
type httpLatency struct {
	slots    [len(httpDurationBuckets) + 1]atomic.Uint64
	sumNanos atomic.Int64
}

func (l *httpLatency) observe(d time.Duration) {
	s := d.Seconds()
	i := sort.SearchFloat64s(httpDurationBuckets[:], s) // first bucket with le >= s
	l.slots[i].Add(1)
	l.sumNanos.Add(int64(d))
}

type httpMetrics struct {
	mu       sync.RWMutex
	requests map[httpRequestKey]*atomic.Uint64
	latency  map[httpLatencyKey]*httpLatency
}

func newHTTPMetrics() *httpMetrics {
	return &httpMetrics{
		requests: map[httpRequestKey]*atomic.Uint64{},
		latency:  map[httpLatencyKey]*httpLatency{},
	}
}

func (m *httpMetrics) counter(k httpRequestKey) *atomic.Uint64 {
	m.mu.RLock()
	c := m.requests[k]
	m.mu.RUnlock()
	if c != nil {
		return c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c = m.requests[k]; c == nil {
		c = new(atomic.Uint64)
		m.requests[k] = c
	}
	return c
}

func (m *httpMetrics) histogram(k httpLatencyKey) *httpLatency {
	m.mu.RLock()
	h := m.latency[k]
	m.mu.RUnlock()
	if h != nil {
		return h
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if h = m.latency[k]; h == nil {
		h = new(httpLatency)
		m.latency[k] = h
	}
	return h
}

// record files one finished request. Exported separately from the middleware so
// the label rules can be tested without a router.
func (m *httpMetrics) record(handler, method string, status int, d time.Duration, streamed bool) {
	if handler == "" || handler == metricsRoute {
		return
	}
	class := status / 100
	if class < 1 || class >= len(statusClasses) {
		return
	}
	m.counter(httpRequestKey{handler, method, statusClasses[class]}).Add(1)
	// Server-sent events and websockets last as long as the client stays: their
	// duration is a session length, not a response time.
	if handler == healthRoute || streamed {
		return
	}
	m.histogram(httpLatencyKey{handler, method}).observe(d)
}

// middleware wraps the whole router. mux is the router itself, consulted only
// for 404 / 405 to tell "no such route" from a matched handler that answered 404.
func (m *httpMetrics) middleware(mux *chi.Mux) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Hijack, Flush and Push survive the wrap (websockets and SSE rely on them).
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			elapsed := time.Since(start)

			rctx := chi.RouteContext(r.Context())
			if rctx == nil {
				return
			}
			status := ww.Status()
			if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
				!mux.Match(chi.NewRouteContext(), r.Method, r.URL.Path) {
				return
			}
			upgraded := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
			if status == 0 {
				// Nothing went through WriteHeader: a hijacked websocket answered
				// 101 on the raw connection, anything else got net/http's implicit 200.
				status = http.StatusOK
				if upgraded {
					status = http.StatusSwitchingProtocols
				}
			}
			streamed := upgraded || strings.HasPrefix(ww.Header().Get("Content-Type"), "text/event-stream")
			m.record(rctx.RoutePattern(), r.Method, status, elapsed, streamed)
		})
	}
}

// write appends both families in exposition format, sorted so two scrapes of
// the same state are byte-identical.
func (m *httpMetrics) write(b *strings.Builder) {
	m.mu.RLock()
	reqKeys := make([]httpRequestKey, 0, len(m.requests))
	for k := range m.requests {
		reqKeys = append(reqKeys, k)
	}
	latKeys := make([]httpLatencyKey, 0, len(m.latency))
	for k := range m.latency {
		latKeys = append(latKeys, k)
	}
	requests := make([]*atomic.Uint64, len(reqKeys))
	for i, k := range reqKeys {
		requests[i] = m.requests[k]
	}
	latency := make([]*httpLatency, len(latKeys))
	for i, k := range latKeys {
		latency[i] = m.latency[k]
	}
	m.mu.RUnlock()

	reqOrder := make([]int, len(reqKeys))
	for i := range reqOrder {
		reqOrder[i] = i
	}
	sort.Slice(reqOrder, func(a, c int) bool {
		x, y := reqKeys[reqOrder[a]], reqKeys[reqOrder[c]]
		if x.handler != y.handler {
			return x.handler < y.handler
		}
		if x.method != y.method {
			return x.method < y.method
		}
		return x.status < y.status
	})
	b.WriteString("# HELP http_requests_total Total number of requests by method, status and handler.\n")
	b.WriteString("# TYPE http_requests_total counter\n")
	for _, i := range reqOrder {
		k := reqKeys[i]
		fmt.Fprintf(b, "http_requests_total{handler=%q,method=%q,status=%q} %d\n",
			k.handler, k.method, k.status, requests[i].Load())
	}

	latOrder := make([]int, len(latKeys))
	for i := range latOrder {
		latOrder[i] = i
	}
	sort.Slice(latOrder, func(a, c int) bool {
		x, y := latKeys[latOrder[a]], latKeys[latOrder[c]]
		if x.handler != y.handler {
			return x.handler < y.handler
		}
		return x.method < y.method
	})
	b.WriteString("# HELP http_request_duration_seconds Latency by handler, excluding /health and streaming responses.\n")
	b.WriteString("# TYPE http_request_duration_seconds histogram\n")
	for _, i := range latOrder {
		k, h := latKeys[i], latency[i]
		var cum uint64
		for j, le := range httpDurationBuckets {
			cum += h.slots[j].Load()
			fmt.Fprintf(b, "http_request_duration_seconds_bucket{handler=%q,method=%q,le=%q} %d\n",
				k.handler, k.method, strconv.FormatFloat(le, 'g', -1, 64), cum)
		}
		cum += h.slots[len(httpDurationBuckets)].Load()
		fmt.Fprintf(b, "http_request_duration_seconds_bucket{handler=%q,method=%q,le=\"+Inf\"} %d\n",
			k.handler, k.method, cum)
		fmt.Fprintf(b, "http_request_duration_seconds_sum{handler=%q,method=%q} %s\n",
			k.handler, k.method, strconv.FormatFloat(time.Duration(h.sumNanos.Load()).Seconds(), 'g', -1, 64))
		fmt.Fprintf(b, "http_request_duration_seconds_count{handler=%q,method=%q} %d\n",
			k.handler, k.method, cum)
	}
}
