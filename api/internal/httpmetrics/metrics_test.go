package httpmetrics

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func exposition(m *Metrics) string {
	var b strings.Builder
	m.Write(&b)
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

func TestHTTPMetricsStatusClassesPanicsAndStreams(t *testing.T) {
	t.Parallel()
	m := New()
	r := chi.NewRouter()
	r.Use(m.Middleware(r))
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
	t.Parallel()
	// BifrostAPIHighLatency fires on p99 > 2 s. A histogram whose largest finite
	// bucket is at or below that can never satisfy it (TD-194).
	if top := httpDurationBuckets[len(httpDurationBuckets)-1]; top <= 2 {
		t.Fatalf("largest finite bucket %g s cannot express a p99 above the 2 s alert", top)
	}
	m := New()
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
	t.Parallel()
	m := New()
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
