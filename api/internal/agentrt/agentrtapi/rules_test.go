package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The rule routes: check writes nothing; the Owner's signed text is taken
// only with the compiled-in key (403 unsigned otherwise) and only from a
// writer; anyone may tighten, nobody may loosen unsigned; the rule in force
// reads back; a proposal says it needs his signature.
func TestRuleRoutes(t *testing.T) {
	pool, _ := world(t)
	key := rptest.NewKey(t)
	anchor = key.Fingerprint
	defer func() { anchor = "" }()
	s := server(t, pool)
	v1 := "rule_id: routing:dev\nrule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2030-01-01T00:00:00Z\n" +
		"tenant: dev\norder: [capability, quota]\nquota_stop_percent: 90\n"
	next := func(version, direction, percent string) string {
		return strings.NewReplacer("rule_version: 1", "rule_version: "+version, "direction: initial", "direction: "+direction,
			"quota_stop_percent: 90", "quota_stop_percent: "+percent).Replace(v1)
	}
	signed := func(text string) map[string]any {
		return map[string]any{"text": text, "signature": key.Sign(t, []byte(text), rules.Namespace)}
	}
	if code, out := call(t, s, "viewer", "GET", "rules/routing:dev", nil); code != http.StatusOK || out["in_force"] != nil {
		t.Fatalf("before any: %d %v", code, out)
	}
	if code, out := call(t, s, "viewer", "POST", "rules/routing:dev/check", map[string]any{"text": v1}); code != http.StatusOK || out["needs_signature"] != true {
		t.Fatalf("check: %d %v", code, out)
	}
	if code, _ := call(t, s, "viewer", "POST", "rules/routing:dev/sign", signed(v1)); code != http.StatusUnauthorized {
		t.Fatalf("a viewer handed in a signed rule: %d", code)
	}
	other := rptest.NewKey(t)
	if code, out := call(t, s, "admin", "POST", "rules/routing:dev/sign", map[string]any{"text": v1,
		"signature": other.Sign(t, []byte(v1), rules.Namespace)}); code != http.StatusForbidden || out["error"] != "unsigned" {
		t.Fatalf("signed by another key: %d %v", code, out)
	}
	if code, out := call(t, s, "admin", "POST", "rules/routing:dev/sign", signed(v1)); code != http.StatusOK || out["signed"] != true {
		t.Fatalf("sign: %d %v", code, out)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "rules/routing:dev/tighten", map[string]any{"text": next("2", "tighten", "80")}); code != http.StatusOK || out["signed"] != false {
		t.Fatalf("agentd tightens: %d %v", code, out)
	}
	if code, out := call(t, s, "viewer", "POST", "rules/routing:dev/tighten", map[string]any{"text": next("3", "tighten", "95")}, session, "s1"); code != http.StatusForbidden || out["error"] != "unsigned" {
		t.Fatalf("loosening unsigned: %d %v", code, out)
	}
	if code, out := call(t, s, "viewer", "GET", "rules/routing:dev", nil); code != http.StatusOK || out["in_force"].(map[string]any)["quota_stop_percent"] != float64(80) {
		t.Fatalf("in force: %d %v", code, out)
	}
	code, out := call(t, s, "operator", "POST", "rules/routing:dev/proposals",
		map[string]any{"idempotency_key": "p1", "text": next("3", "loosen", "95"), "reason": "let pools run fuller again"}, session, "s1")
	if code != http.StatusOK || out["needs_signature"] != true {
		t.Fatalf("proposal: %d %v", code, out)
	}
	if code, out := call(t, s, "admin", "POST", "rules/routing:dev/sign", signed(next("3", "loosen", "95")+"note: x\n")); code != http.StatusUnprocessableEntity || out["error"] != "invalid" {
		t.Fatalf("an unknown field: %d %v", code, out)
	}
}
