package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// A claim reports the agentd build its host runs; GET /hosts says the last
// one, recorded once while it does not change. A build that is not a commit
// and a sha256 is refused, and the claim with it.
func TestAClaimsBuildIsWhatTheHostsSay(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	commit, sum := strings.Repeat("0a", 20), strings.Repeat("b1", 32)
	claim := func(build map[string]any, slots int) map[string]any {
		return map[string]any{"host_id": "h1", "free_slots": slots, "tools": []any{cursor}, "build": build}
	}
	for i, bad := range []map[string]any{{"commit": "main", "sha256": sum, "modified": false}, {"commit": commit, "sha256": "ab", "modified": false},
		{"commit": commit, "sha256": sum, "modified": false, "extra": 1}} {
		if code, out := call(t, s, "agentd:h1", "POST", "claim", claim(bad, 1)); code != http.StatusBadRequest {
			t.Fatalf("bad build %d: %d %v", i, code, out)
		}
	}
	build := map[string]any{"commit": commit, "sha256": sum, "modified": true}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", claim(build, 0)); code != http.StatusNoContent {
		t.Fatalf("a claim with no slot: %d %v", code, out)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", claim(build, 1)); code != http.StatusOK || out["task_id"] != task {
		t.Fatalf("a claim with the build: %d %v", code, out)
	}
	_, hosts := call(t, s, "viewer", "GET", "hosts", nil)
	h1 := hostOf(t, hosts, "h1")
	if b, _ := h1["build"].(map[string]any); b["commit"] != commit || b["sha256"] != sum || b["modified"] != true || h1["build_at"] == nil {
		t.Fatalf("h1: %v", h1)
	}
	var notes int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'build:host:h1'`).Scan(&notes))
	if notes != 1 {
		t.Fatalf("%d records of h1's build", notes)
	}
}
