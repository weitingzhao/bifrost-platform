package releasefreeze

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
)

func testREST(t *testing.T, host string) rest.Interface {
	t.Helper()
	return testRESTFull(t, host, nil, "")
}

func testRESTFull(t *testing.T, host string, ca []byte, tokenFile string) rest.Interface {
	t.Helper()
	cfg := &rest.Config{
		Host:        host,
		RateLimiter: flowcontrol.NewFakeAlwaysRateLimiter(),
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Group: "", Version: "v1"},
			NegotiatedSerializer: serializer.NewCodecFactory(scheme.Scheme).WithoutConversion(),
		},
		BearerTokenFile: tokenFile,
	}
	if len(ca) > 0 {
		cfg.CAData = ca
	}
	client, err := rest.RESTClientFor(cfg)
	if err != nil {
		t.Fatalf("rest client: %v", err)
	}
	return client
}

func TestReadPathHasAnEmptyQueryAndDoesNotRetry(t *testing.T) {
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Retry-After", "30")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"later"}`))
	}))
	defer srv.Close()

	got := Gate{}.Read(context.Background(), testREST(t, srv.URL))
	if len(hits) != 1 {
		t.Fatalf("requests %d: %v", len(hits), hits)
	}
	if hits[0] != ListPath+"?" {
		t.Fatalf("request %q", hits[0])
	}
	if got.Verdict != VerdictUnreadable || got.Reason != "HTTP 503" {
		t.Fatalf("decision %+v", got)
	}
}

func TestReadPlainTextErrorKeepsTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	got := Gate{}.Read(context.Background(), testREST(t, srv.URL))
	if got.Verdict != VerdictUnreadable || got.Reason != "HTTP 500" {
		t.Fatalf("decision %+v", got)
	}
}

func TestReadTimeoutAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := testREST(t, srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	timed := Gate{}.Read(ctx, client)
	if timed.Reason != "timeout" {
		t.Fatalf("timeout reason %q", timed.Reason)
	}

	dead, stop := context.WithCancel(context.Background())
	stop()
	cancelled := Gate{}.Read(dead, client)
	if cancelled.Reason != "context canceled" {
		t.Fatalf("cancel reason %q", cancelled.Reason)
	}

	closed := Gate{}.Read(context.Background(), testREST(t, "http://127.0.0.1:1"))
	if closed.Reason != "connection failed" {
		t.Fatalf("connection reason %q", closed.Reason)
	}
}

func TestUnfreezeHandlerIs410(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-policy/unfreeze", strings.NewReader(`{"text":"unfreeze","sig":"abc"}`))
	rr := httptest.NewRecorder()
	UnfreezeHandler(rr, req)
	if rr.Code != http.StatusGone {
		t.Fatalf("status %d", rr.Code)
	}
	if rr.Body.String() != UnfreezeGoneBody {
		t.Fatalf("body %s", rr.Body.String())
	}
}

func TestImportGraph(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Dir = strings.TrimSuffix(file, "/read_test.go")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	forbidden := []string{
		"k8s.io/client-go/tools/cache",
		"k8s.io/client-go/tools/pager",
		"k8s.io/client-go/informers",
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		for _, bad := range forbidden {
			if line == bad || strings.HasPrefix(line, bad+"/") {
				t.Fatalf("import graph contains %s", line)
			}
		}
	}
}
