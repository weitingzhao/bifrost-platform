//bifrost:owner plan

package server

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/runtimedb"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/schemawatch"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

const agentRuntimeAuthYAML = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-agentrt
  - name: agentd:mac-dev
    role: agentd
    token: fixture-agentd-agentrt
`

// Without AGENTRT_DSN the agent runtime's routes are mounted but answer 503,
// after authentication: an anonymous write is still refused as such.
func TestAgentRuntimeRoutesWithoutDatabase(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	t.Setenv("PLATFORM_AGENT_THREAD_WATCH", "off")
	t.Setenv("AGENTRT_DSN", "")
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(agentRuntimeAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, cfg)
	do := func(method, path, token, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do(http.MethodGet, "/api/v1/agentrt/progress", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous read: %d, want 401", code)
	}
	if code := do(http.MethodGet, "/api/v1/agentrt/whoami", "fixture-viewer-agentrt", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("whoami without a database: %d, want 503", code)
	}
}

// Review 528 A1b: the routes open with the moment they did already recorded,
// and /health says agentrt_ready only then: no reader sees the routes ready
// without the moment the API's silence counting starts from.
func TestTheRoutesOpenWithTheirMoment(t *testing.T) {
	db := agentrttest.Migrated(t)
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	srv := newTestServer(t, newTestConfig(t))
	late := &runtimedb.Late{}
	srv.agentRuntime = &agentRuntime{schema: &schemawatch.Watch{}, late: late}
	ready := func() any {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		var body map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("health %d %s", rec.Code, rec.Body.String())
		}
		return body["agentrt_ready"]
	}
	if late.Ready() || !late.Back().IsZero() || ready() != false {
		t.Fatalf("before connecting: ready %v back %v health %v", late.Ready(), late.Back(), ready())
	}
	// Whatever moment a reader catches between the two stores, it never sees
	// the routes ready with no moment recorded.
	at := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !late.Ready() {
		}
		if late.Back().IsZero() {
			t.Error("the routes were ready before their moment was recorded")
		}
	}()
	late.Open(pool, at)
	<-done
	if !late.Ready() || !late.Back().Equal(at) || ready() != true {
		t.Fatalf("after connecting: ready %v back %v health %v", late.Ready(), late.Back(), ready())
	}
}
