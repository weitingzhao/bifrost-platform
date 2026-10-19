package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Benchmark runs over HTTP (W-31 2.4 slice ⑤): only an agentd token, for the
// host it is bound to; a run handed in twice is recorded once.
func TestBenchmark(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	run := func(host string) map[string]any {
		return map[string]any{"host_id": host, "run_id": "run-0001-codex", "executor": "exec:codex", "provider": "openai",
			"model": "gpt-x", "questions_sha256": strings.Repeat("ab", 32), "started_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"results": []any{map[string]any{"question": "Q01", "class": "go", "outcome": "pass", "wall_s": 300},
				map[string]any{"question": "Q12", "class": "ts", "outcome": "unrunnable", "wall_s": 40, "reason": "npm ci failed"}}}
	}
	for _, who := range []string{"operator", "admin", "viewer"} {
		if code, out := call(t, s, who, "POST", "benchmark", run("h1")); code != http.StatusForbidden && code != http.StatusUnauthorized {
			t.Fatalf("%s handed in a run: %d %v", who, code, out)
		}
	}
	if code, out := call(t, s, "agentd:h1", "POST", "benchmark", run("h2")); code != http.StatusForbidden {
		t.Fatalf("a run for another host: %d %v", code, out)
	}
	bad := run("h1")
	bad["executor"] = "exec:deterministic"
	if code, out := call(t, s, "agentd:h1", "POST", "benchmark", bad); code != http.StatusUnprocessableEntity {
		t.Fatalf("a run of the deterministic executor: %d %v", code, out)
	}
	code, out := call(t, s, "agentd:h1", "POST", "benchmark", run("h1"))
	if code != http.StatusOK || out["duplicate"] != false || out["rows"] != float64(1) {
		t.Fatalf("a run: %d %v", code, out)
	}
	again, out2 := call(t, s, "agentd:h1", "POST", "benchmark", run("h1"))
	if again != http.StatusOK || out2["duplicate"] != true || out2["event_id"] != out["event_id"] {
		t.Fatalf("the same run again: %d %v", again, out2)
	}
}
