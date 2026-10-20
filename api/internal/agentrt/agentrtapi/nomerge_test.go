package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-383 (Owner 10-20 「不合 A」): a mission, or a goal, proposed with
// no_merge keeps why in its scope, for the coordinator's merge to pass it
// over; the Owner's card says so; one longer than a sentence is refused, and
// a proposal without it stays as it was.
func TestNoMergeIsKeptInTheScope(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	mains = nil
	scope := func(mission any) string {
		t.Helper()
		var v string
		must(t, pool.QueryRow(ctx, `SELECT COALESCE(scope_json->>'no_merge', '') FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&v))
		return v
	}
	cards = nil
	body := map[string]any{"idempotency_key": "nm-1", "title": "m", "goal": "g", "success": []string{"it works"},
		"no_merge": "  a benchmark replay: the fixes are on main already  "}
	code, out := call(t, s, "operator", "POST", "missions", body, session, "s1")
	if code != http.StatusOK || scope(out["mission_id"]) != "a benchmark replay: the fixes are on main already" {
		t.Fatalf("a mission with no_merge: %d %v, scope %q", code, out, scope(out["mission_id"]))
	}
	if len(cards) != 1 || !strings.Contains(cards[0].Body, "Not to be merged into main: a benchmark replay: the fixes are on main already") {
		t.Fatalf("the Owner's card: %+v", cards)
	}
	plain := map[string]any{"idempotency_key": "nm-2", "title": "m", "goal": "g", "success": []string{"it works"}}
	if code, out = call(t, s, "operator", "POST", "missions", plain, session, "s1"); code != http.StatusOK || scope(out["mission_id"]) != "" {
		t.Fatalf("a mission without it: %d %v", code, out)
	}
	long := map[string]any{"idempotency_key": "nm-3", "title": "m", "goal": "g", "success": []string{"it works"},
		"no_merge": strings.Repeat("x", store.MaxNoMergeBytes+1)}
	if code, out = call(t, s, "operator", "POST", "missions", long, session, "s1"); code != http.StatusUnprocessableEntity {
		t.Fatalf("a long no_merge: %d %v", code, out)
	}
	goal := map[string]any{"idempotency_key": "nm-g", "goal": "Replay benchmark question Q04", "repo": "bifrost-platform",
		"no_merge": "a benchmark replay"}
	if code, out = call(t, s, "admin", "POST", "goals", goal); code != http.StatusOK || scope(out["mission_id"]) != "a benchmark replay" {
		t.Fatalf("a goal with no_merge: %d %v", code, out)
	}
	goal["idempotency_key"], goal["no_merge"] = "nm-g2", strings.Repeat("目", store.MaxNoMergeBytes/3+1)
	if code, out = call(t, s, "admin", "POST", "goals", goal); code != http.StatusUnprocessableEntity {
		t.Fatalf("a goal with a long no_merge: %d %v", code, out)
	}
}
