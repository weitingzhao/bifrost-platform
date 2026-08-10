package patrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStubDispatcher(t *testing.T) {
	out := stubDispatcher{}.Dispatch(context.Background(), PatrolSkill{ID: "fleet-drift-scan", TrustLevel: TrustL0}, TriggerManual, "prompt", nil)
	if out.Result != ResultSuccess || out.Status != StatusCompleted {
		t.Fatalf("%+v", out)
	}
	if out.Evidence == "" {
		t.Fatal("expected evidence")
	}
}

func TestCursorDispatcherMissingKey(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "")
	d := &cursorDispatcher{client: http.DefaultClient}
	out := d.Dispatch(context.Background(), PatrolSkill{ID: "x", TimeoutSeconds: 5}, TriggerManual, "hi", nil)
	if out.Result != ResultSkipped || out.Error == "" {
		t.Fatalf("want skipped missing key, got %+v", out)
	}
}

func TestCursorDispatcherHTTPSuccess(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent": map[string]any{"id": "bc-test", "latestRunId": "run-1"},
				"run":   map[string]any{"id": "run-1", "status": "CREATING"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/bc-test/runs/run-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "run-1", "status": "FINISHED", "result": "fleet ok",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CURSOR_API_KEY", "test-key")
	t.Setenv("CURSOR_AGENT_API_BASE", srv.URL)
	d := &cursorDispatcher{client: srv.Client()}
	out := d.Dispatch(context.Background(), PatrolSkill{ID: "fleet-drift-scan", TimeoutSeconds: 10}, TriggerManual, "scan", nil)
	if !created {
		t.Fatal("expected create POST")
	}
	if out.Result != ResultSuccess || out.Evidence != "fleet ok" {
		t.Fatalf("%+v", out)
	}
}

func TestCursorDispatcherHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CURSOR_API_KEY", "test-key")
	t.Setenv("CURSOR_AGENT_API_BASE", srv.URL)
	d := &cursorDispatcher{client: srv.Client()}
	out := d.Dispatch(context.Background(), PatrolSkill{ID: "x", TimeoutSeconds: 5}, TriggerManual, "x", nil)
	if out.Result != ResultFailure {
		t.Fatalf("%+v", out)
	}
}

func TestClassifyPatrolEvidence(t *testing.T) {
	cases := []struct {
		in   string
		want RunResult
	}{
		{"", ResultSuccess},
		{"NOMINAL — fleet matches matrix", ResultSuccess},
		{"PROBE_DRIFT on stg nginx upstream", ResultSuccess},
		{"DATA_LAYER stale redis-ib", ResultSuccess},
		{"### Verdict\n**HTTP_FAIL** · 4 tools ok · 0 failed", ResultSuccess},
		{"## 🚨 Bifrost Patrol L0 - 漂移扫描失败\n**状态**: **HTTP_FAIL** (工具层不可用)", ResultFailure},
		{"status: HTTP_FAIL tool layer unavailable", ResultFailure},
		{"Drift scan failed: GetMcpTools not found", ResultFailure},
		{"扫描失败 after two MCP lookups", ResultFailure},
	}
	for _, tc := range cases {
		got, errMsg := classifyPatrolEvidence(tc.in)
		if got != tc.want {
			t.Fatalf("classify %q → %s want %s (%s)", tc.in, got, tc.want, errMsg)
		}
		if tc.want == ResultFailure && errMsg == "" {
			t.Fatalf("classify %q: expected error note", tc.in)
		}
		if tc.want == ResultSuccess && errMsg != "" {
			t.Fatalf("classify %q: unexpected error %q", tc.in, errMsg)
		}
	}
}

func TestCursorDispatcherFinishedHTTPFailIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent": map[string]any{"id": "bc-test", "latestRunId": "run-1"},
				"run":   map[string]any{"id": "run-1", "status": "CREATING"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/bc-test/runs/run-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "run-1", "status": "FINISHED",
				"result": "## 漂移扫描失败\n**状态**: **HTTP_FAIL** (工具层不可用)",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CURSOR_API_KEY", "test-key")
	t.Setenv("CURSOR_AGENT_API_BASE", srv.URL)
	d := &cursorDispatcher{client: srv.Client()}
	out := d.Dispatch(context.Background(), PatrolSkill{ID: "fleet-drift-scan", TimeoutSeconds: 10}, TriggerManual, "scan", nil)
	if out.Result != ResultFailure {
		t.Fatalf("want failure for HTTP_FAIL report, got %+v", out)
	}
	if out.Evidence == "" || out.Error == "" {
		t.Fatalf("expected evidence + classified error, got %+v", out)
	}
}

func TestBuildPromptIncludesContext(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	p := buildPrompt(PatrolSkill{
		ID:             "fleet-drift-scan",
		TrustLevel:     TrustL0,
		Scope:          "fleet",
		PromptTemplate: "Scan drift.",
		MCPTools:       []string{"verify_mission_snapshot"},
	}, TriggerCron, now)
	for _, want := range []string{"Scan drift.", "trust_level: L0", "trigger: cron", "scope: fleet", "2026-08-09"} {
		if !contains(p, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
