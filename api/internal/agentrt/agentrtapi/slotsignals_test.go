package agentrtapi_test

import (
	"net/http"
	"testing"
)

// R5 phase 2 step 3b: a claim carries the host's headroom, which GET /hosts
// shows in the host's slot signals with the pools its tools route to and the
// queue; a headroom no host can report is refused, and signals_since must
// be a time.
func TestAClaimsHeadroomShowsInTheHostsSlotSignals(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claim := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "free_slots": 0, "tools": []any{cursor}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	for i, bad := range []map[string]any{{"load1": -1}, {"cpus": 0}, {"mem_avail_bytes": 1}, {"mem_avail_bytes": 2, "mem_total_bytes": 1}} {
		if code, out := call(t, s, "agentd:h1", "POST", "claim", claim(bad)); code != http.StatusBadRequest {
			t.Fatalf("bad headroom %d (%v): %d %v", i, bad, code, out)
		}
	}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", claim(map[string]any{"load1": 2.5, "cpus": 8,
		"mem_avail_bytes": int64(12) << 30, "mem_total_bytes": int64(32) << 30})); code != http.StatusNoContent {
		t.Fatalf("claim with headroom: %d %v", code, out)
	}
	code, out := call(t, s, "viewer", "GET", "hosts", nil)
	if code != http.StatusOK {
		t.Fatalf("GET hosts: %d %v", code, out)
	}
	var h1 map[string]any
	for _, h := range out["hosts"].([]any) {
		if m := h.(map[string]any); m["host_id"] == "h1" {
			h1 = m
		}
	}
	if h1 == nil || h1["online"] == nil {
		t.Fatalf("hosts %v", out["hosts"])
	}
	sig, _ := h1["slot_signals"].(map[string]any)
	head, _ := sig["headroom"].(map[string]any)
	if head["load1"] != 2.5 || head["cpus"] != float64(8) || head["mem_total_bytes"] != float64(int64(32)<<30) || head["at"] == nil {
		t.Fatalf("h1's slot signals %v", sig)
	}
	pools, _ := sig["pools"].([]any)
	if len(pools) != 1 || pools[0].(map[string]any)["vendor_pool"] != "cursor:owner" || sig["queue_depth"] == nil || sig["durations"] == nil {
		t.Fatalf("h1's slot signals %v", sig)
	}
	if code, out := call(t, s, "viewer", "GET", "hosts?signals_since=yesterday", nil); code != http.StatusBadRequest {
		t.Fatalf("signals_since not a time: %d %v", code, out)
	}
	if code, _ := call(t, s, "viewer", "GET", "hosts?signals_since=2026-11-01T00:00:00Z", nil); code != http.StatusOK {
		t.Fatalf("signals_since a time: %d", code)
	}
}
