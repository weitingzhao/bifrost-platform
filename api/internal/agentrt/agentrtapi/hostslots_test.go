package agentrtapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// claimGranted posts a claim as agentd:h1 and returns the status, the body
// and the granted slots header.
func claimGranted(t *testing.T, s *httptest.Server, body map[string]any) (int, map[string]any, string) {
	t.Helper()
	doc, err := json.Marshal(body)
	must(t, err)
	req, err := http.NewRequest("POST", s.URL+"/api/v1/agentrt/claim", bytes.NewReader(doc))
	must(t, err)
	req.Header.Set("Authorization", "Bearer "+tokens["agentd:h1"])
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp.Header.Get(agentrtapi.GrantedSlotsHeader)
}

// Elastic slots (the Owner 2026-11-06 Q4): a claim may carry the host's
// headroom, each field optional; the answer says how many slots the host is
// granted, in the body of a lease and in a header on a 204 alike; headroom
// that cannot be is refused; GET /hosts shows the headroom, the recorded
// count, the moves and the rule's slots configuration.
func TestAClaimCarriesHeadroomAndAnswersItsGrantedSlots(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claim := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	for name, bad := range map[string]map[string]any{
		"negative load":       {"load1": -1.0, "cpus": 2},
		"no cpus":             {"load1": 1.0, "cpus": 0},
		"more available":      {"mem_avail_bytes": 2, "mem_total_bytes": 1},
		"available, no total": {"mem_avail_bytes": 2},
		"cpus as a string":    {"load1": 1.0, "cpus": "8"},
	} {
		if code, out, _ := claimGranted(t, s, claim(bad)); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	room := map[string]any{"load1": 1.5, "cpus": 8, "mem_avail_bytes": 4 << 30, "mem_total_bytes": 16 << 30}
	code, out, header := claimGranted(t, s, claim(room))
	if code != http.StatusOK || out["task_id"] != task || out["granted_slots"] != 1.0 || header != "1" {
		t.Fatalf("lease: %d %v %q", code, out, header)
	}
	if code, out, header = claimGranted(t, s, claim(map[string]any{"load1": 0.0, "cpus": 4})); code != http.StatusNoContent || header != "1" {
		t.Fatalf("no lease: %d %v %q", code, out, header)
	}
	code, out = call(t, s, "viewer", "GET", "hosts", nil)
	if code != http.StatusOK {
		t.Fatalf("hosts: %d %v", code, out)
	}
	h := out["hosts"].([]any)[0].(map[string]any)
	sig, _ := h["signals_json"].(map[string]any)
	cfg, _ := out["slots"].(map[string]any)
	if sig["cpus"] != 4.0 || sig["load1"] != 0.0 || sig["mem_total_bytes"] != nil || sig["at"] == nil || h["effective_slots"] != nil || len(h["slot_moves"].([]any)) != 0 ||
		cfg["policy"] != "fixed" || cfg["elastic"] != false || cfg["rule_version"] != 1.0 || cfg["ceiling"] != float64(rules.PerHostCeiling) {
		t.Fatalf("hosts: %v", out)
	}
}
