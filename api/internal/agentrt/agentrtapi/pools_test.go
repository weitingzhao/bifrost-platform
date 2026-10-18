package agentrtapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Vendor pools over HTTP (W-31 2.4 slice ①): computed carries usage and pool
// readings, no other route does; anyone reads the pools; only the Owner reads
// one by hand.
func TestPools(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	call2 := func(caller, method, path string, body any) (int, map[string]any) {
		return call(t, s, caller, method, path, body)
	}
	attempt, fence := claimOne(t, s, call2)
	usage := map[string]any{"pool": "cursor:owner:monthly", "tokens": 77}
	refusal := []any{map[string]any{"pool": "cursor:owner:monthly", "limited": true, "text": "You've hit your usage limit"}}
	if code, out := call2("agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(map[string]any{"usage": usage})); code != http.StatusBadRequest {
		t.Fatalf("a heartbeat with usage: %d %v", code, out)
	}
	code, out := call2("agentd:h1", "POST", "attempts/"+attempt+"/computed",
		fence(map[string]any{"exit_code": 1, "end_how": "exit", "conclusion": "failed", "usage": usage, "pools": refusal}))
	if code != http.StatusOK || out["computed"] != true || out["usage_refused"] != nil {
		t.Fatalf("computed: %d %v", code, out)
	}
	code, out = call2("viewer", "GET", "pools", nil)
	pools, _ := out["pools"].([]any)
	if code != http.StatusOK || len(pools) != 1 {
		t.Fatalf("pools: %d %v", code, out)
	}
	p := pools[0].(map[string]any)
	if p["vendor_pool_id"] != "cursor:owner:monthly" || p["cooling"] != true || p["measure_source"] != "exit_text" ||
		p["used"].(map[string]any)["tokens"] != float64(77) {
		t.Fatalf("pool = %v", p)
	}
	reading := map[string]any{"idempotency_key": "r-1", "used_fraction": 0.2, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)}
	for _, who := range []string{"viewer", "operator", "agentd:h1"} {
		if code, out := call2(who, "POST", "pools/cursor:owner:monthly/reading", reading); code != http.StatusForbidden && code != http.StatusUnauthorized {
			t.Fatalf("%s read a pool by hand: %d %v", who, code, out)
		}
	}
	if code, out := call2("admin", "POST", "pools/cursor:owner:monthly/reading", reading); code != http.StatusOK || out["recorded"] != true {
		t.Fatalf("the Owner's reading: %d %v", code, out)
	}
	// As the Console sends it: the pool escaped (review 281 A1).
	reading["idempotency_key"] = "r-escaped"
	if code, out := call2("admin", "POST", "pools/cursor%3Aowner%3Amonthly/reading", reading); code != http.StatusOK || out["vendor_pool_id"] != "cursor:owner:monthly" {
		t.Fatalf("an escaped pool: %d %v", code, out)
	}
	_, out = call2("viewer", "GET", "pools", nil)
	if p := out["pools"].([]any)[0].(map[string]any); p["cooling"] != false || p["measure_source"] != "owner" || p["used_fraction"] != 0.2 {
		t.Fatalf("after the Owner's reading: %v", p)
	}
	if code, out := call2("admin", "POST", "pools/not-a-pool/reading", map[string]any{"idempotency_key": "r-2", "used_fraction": 0.2}); code != http.StatusUnprocessableEntity ||
		!strings.Contains(out["message"].(string), "not a pool") {
		t.Fatalf("a reading of no pool: %d %v", code, out)
	}
}

// Review 274 A2: a report that does not even parse (a number past int64, a
// field it does not know, a time that is not one) is dropped; the ending is
// not refused.
func TestComputedWithAReportThatDoesNotParse(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"tokens past int64":  {"usage": json.RawMessage(`{"pool":"cursor:owner:monthly","tokens":9223372036854775808}`)},
		"an unknown field":   {"usage": json.RawMessage(`{"pool":"cursor:owner:monthly","seconds":3}`)},
		"a time that is not": {"pools": json.RawMessage(`[{"pool":"cursor:owner:monthly","resets_at":"soon"}]`)},
	} {
		pool, _ := world(t)
		s := server(t, pool)
		call2 := func(caller, method, path string, body any) (int, map[string]any) {
			return call(t, s, caller, method, path, body)
		}
		attempt, fence := claimOne(t, s, call2)
		body := fence(map[string]any{"exit_code": 0, "end_how": "exit", "conclusion": "done"})
		for k, v := range extra {
			body[k] = v
		}
		code, out := call2("agentd:h1", "POST", "attempts/"+attempt+"/computed", body)
		if code != http.StatusOK || out["computed"] != true || out["usage_refused"] == nil {
			t.Fatalf("%s: %d %v", name, code, out)
		}
	}
}
