package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func exposition(m *httpMetrics) string {
	var b strings.Builder
	m.write(&b)
	return b.String()
}

func mustContain(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
}

func mustNotContain(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(out, u) {
			t.Fatalf("unexpected %q in:\n%s", u, out)
		}
	}
}

// TD-195 ratchet: platform-api must keep exporting the series the cluster's API
// rules read, under the real router, with the route pattern as the handler.
func TestRouterExportsHTTPRequestMetrics(t *testing.T) {
	srv, err := New(newTestConfig(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()
	hit := func(method, path string) int {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}
	hit(http.MethodGet, "/health")
	hit(http.MethodGet, "/health")
	hit(http.MethodGet, "/api/v1/environments")
	if code := hit(http.MethodGet, "/api/v1/topology?env=nope"); code != http.StatusNotFound {
		t.Fatalf("topology for an unknown env = %d, want the handler's 404", code)
	}
	if code := hit(http.MethodGet, "/api/v1/does-not-exist/42"); code != http.StatusNotFound {
		t.Fatalf("unknown route = %d, want 404", code)
	}
	hit(http.MethodPost, "/health") // 405 from the router
	hit("PROPFIND", "/health")      // a method chi does not know

	out := exposition(srv.httpMetrics)
	mustContain(t, out,
		"# TYPE http_requests_total counter\n",
		"# TYPE http_request_duration_seconds histogram\n",
		`http_requests_total{handler="/health",method="GET",status="2xx"} 2`,
		`http_requests_total{handler="/api/v1/environments",method="GET",status="2xx"} 1`,
		// A matched route answering 404 is real traffic and keeps its pattern.
		`http_requests_total{handler="/api/v1/topology",method="GET",status="4xx"} 1`,
		`http_request_duration_seconds_count{handler="/api/v1/environments",method="GET"} 1`,
		`http_request_duration_seconds_bucket{handler="/api/v1/environments",method="GET",le="2.5"} 1`,
	)
	mustNotContain(t, out,
		"does-not-exist",      // client-chosen paths never become labels
		`handler="/api/v1/*"`, // nor does the mount an unmatched path fell into
		`method="POST"`,       // 405
		`method="PROPFIND"`,   // unknown method
		`handler="/metrics"`,  // scrapes are not traffic
		`http_request_duration_seconds_count{handler="/health"`, // counted, not timed
	)

	// /metrics itself serves them (and is still not counted after the scrape).
	// A cancelled context makes the four plugin probes give up at once instead
	// of each waiting out its 8 s budget against nothing.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx))
	mustContain(t, rec.Body.String(),
		`http_requests_total{handler="/health",method="GET",status="2xx"} 2`)
	mustNotContain(t, exposition(srv.httpMetrics), `handler="/metrics"`)
}

func TestHTTPMetricsStatusClassesPanicsAndStreams(t *testing.T) {
	m := newHTTPMetrics()
	r := chi.NewRouter()
	r.Use(m.middleware(r))
	r.Use(middleware.Recoverer)
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	r.Get("/fail/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	r.Get("/silent", func(http.ResponseWriter, *http.Request) {})
	r.Get("/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: x\n\n"))
	})
	for _, p := range []string{"/boom", "/fail/1", "/fail/2", "/silent", "/events"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}

	out := exposition(m)
	mustContain(t, out,
		`http_requests_total{handler="/boom",method="GET",status="5xx"} 1`,
		`http_requests_total{handler="/fail/{id}",method="GET",status="5xx"} 2`,
		`http_requests_total{handler="/silent",method="GET",status="2xx"} 1`,
		`http_requests_total{handler="/events",method="GET",status="2xx"} 1`,
	)
	// An SSE response lasts as long as the client stays; it is no latency sample.
	mustNotContain(t, out, `http_request_duration_seconds_count{handler="/events"`)
}

func TestHTTPDurationBucketsAreCumulativeAndReachPastTheAlert(t *testing.T) {
	// BifrostAPIHighLatency fires on p99 > 2 s. A histogram whose largest finite
	// bucket is at or below that can never satisfy it (TD-194).
	if top := httpDurationBuckets[len(httpDurationBuckets)-1]; top <= 2 {
		t.Fatalf("largest finite bucket %g s cannot express a p99 above the 2 s alert", top)
	}
	m := newHTTPMetrics()
	m.record("/x", "GET", 200, 50*time.Millisecond, false)
	m.record("/x", "GET", 200, time.Second, false) // on the boundary: le="1" includes it
	m.record("/x", "GET", 200, 3*time.Second, false)
	m.record("/x", "GET", 200, time.Minute, false)
	out := exposition(m)
	mustContain(t, out,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="0.1"} 1`,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="0.5"} 1`,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="1"} 2`,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="2.5"} 2`,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="5"} 3`,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="10"} 3`,
		`http_request_duration_seconds_bucket{handler="/x",method="GET",le="+Inf"} 4`,
		`http_request_duration_seconds_count{handler="/x",method="GET"} 4`,
		`http_request_duration_seconds_sum{handler="/x",method="GET"} 64.05`,
	)
}

func TestHTTPMetricsConcurrentRecordAndScrape(t *testing.T) {
	m := newHTTPMetrics()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				m.record("/x", "GET", 200, time.Millisecond, false)
				if i%100 == 0 {
					_ = exposition(m)
				}
			}
		}()
	}
	wg.Wait()
	mustContain(t, exposition(m),
		`http_requests_total{handler="/x",method="GET",status="2xx"} 4000`,
		`http_request_duration_seconds_count{handler="/x",method="GET"} 4000`)
	if first, again := exposition(m), exposition(m); first != again {
		t.Fatal("exposition is not stable across scrapes")
	}
}
