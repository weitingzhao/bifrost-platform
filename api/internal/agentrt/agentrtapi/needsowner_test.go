package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-390 (Owner 10-20 「STG 用 A」: a walk that tests his buttons marks its
// cards as his): a mission, or a goal, proposed with needs_owner keeps why in
// its scope, so a card-auto-answer rule never answers its cards; the Owner's
// card says so; one longer than a sentence is refused, and a proposal
// without it stays as it was.
func TestNeedsOwnerIsKeptInTheScope(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	mains = nil
	scope := func(mission any) string {
		t.Helper()
		var v string
		must(t, pool.QueryRow(ctx, `SELECT COALESCE(scope_json->>'needs_owner', '') FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&v))
		return v
	}
	cards = nil
	body := map[string]any{"idempotency_key": "no-1", "title": "m", "goal": "g", "success": []string{"it works"},
		"needs_owner": "  the walk tests the Owner's Close mission button  "}
	code, out := call(t, s, "operator", "POST", "missions", body, session, "s1")
	if code != http.StatusOK || scope(out["mission_id"]) != "the walk tests the Owner's Close mission button" {
		t.Fatalf("a mission with needs_owner: %d %v, scope %q", code, out, scope(out["mission_id"]))
	}
	if len(cards) != 1 || !strings.Contains(cards[0].Body, "Its cards wait for you, never answered by a rule: the walk tests the Owner's Close mission button") {
		t.Fatalf("the Owner's card: %+v", cards)
	}
	plain := map[string]any{"idempotency_key": "no-2", "title": "m", "goal": "g", "success": []string{"it works"}}
	if code, out = call(t, s, "operator", "POST", "missions", plain, session, "s1"); code != http.StatusOK || scope(out["mission_id"]) != "" {
		t.Fatalf("a mission without it: %d %v", code, out)
	}
	long := map[string]any{"idempotency_key": "no-3", "title": "m", "goal": "g", "success": []string{"it works"},
		"needs_owner": strings.Repeat("x", store.MaxNeedsOwnerBytes+1)}
	if code, out = call(t, s, "operator", "POST", "missions", long, session, "s1"); code != http.StatusUnprocessableEntity {
		t.Fatalf("a long needs_owner: %d %v", code, out)
	}
	goal := map[string]any{"idempotency_key": "no-g", "goal": "Walk the blocked card", "repo": "bifrost-platform",
		"needs_owner": "the walk tests the Owner's buttons"}
	if code, out = call(t, s, "admin", "POST", "goals", goal); code != http.StatusOK || scope(out["mission_id"]) != "the walk tests the Owner's buttons" {
		t.Fatalf("a goal with needs_owner: %d %v", code, out)
	}
	goal["idempotency_key"], goal["needs_owner"] = "no-g2", strings.Repeat("目", store.MaxNeedsOwnerBytes/3+1)
	if code, out = call(t, s, "admin", "POST", "goals", goal); code != http.StatusUnprocessableEntity {
		t.Fatalf("a goal with a long needs_owner: %d %v", code, out)
	}
}
