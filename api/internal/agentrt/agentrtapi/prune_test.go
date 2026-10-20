package agentrtapi_test

import (
	"net/http"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-383: agentd asks which attempt branches its host may delete and says
// what it did; only its own host's token may; a refusal is recorded and
// pauses the handing out (the rule itself is the store's, prune_test.go
// there).
func TestPruneRoutes(t *testing.T) {
	pool, _ := world(t)
	agentHeads = map[string][]store.AgentHead{}
	defer func() { agentHeads = nil }()
	s := server(t, pool)
	task := readyTask(t, pool)
	ref := store.AttemptRef(task, 1)
	agentHeads["bifrost-platform"] = []store.AgentHead{{Repo: "bifrost-platform", Ref: ref, Commit: commitA}}
	// The host is registered by its first claim.
	if code, _ := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 0, "tools": []any{}}); code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("claim: %d", code)
	}
	for who, want := range map[string]int{"viewer": http.StatusForbidden, "operator": http.StatusForbidden, "agentd:h2": http.StatusForbidden} {
		if code, _ := call(t, s, who, "GET", "prunes?host_id=h1", nil); code != want && code != http.StatusUnauthorized {
			t.Errorf("%s asked for h1's prunes: %d", who, code)
		}
	}
	code, out := call(t, s, "agentd:h1", "GET", "prunes?host_id=h1", nil)
	if list, _ := out["prunes"].([]any); code != http.StatusOK || list == nil || len(list) != 0 || out["paused_until"] != nil {
		t.Fatalf("prunes: %d %v; want an empty list (nothing is seven days old)", code, out)
	}
	report := func(results ...map[string]any) (int, map[string]any) {
		return call(t, s, "agentd:h1", "POST", "prunes", map[string]any{"host_id": "h1", "results": results})
	}
	if code, out = report(map[string]any{"repo": "bifrost-platform", "ref": "refs/heads/main", "commit": commitA, "outcome": "deleted"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("a report of main: %d %v", code, out)
	}
	if code, _ = call(t, s, "agentd:h1", "POST", "prunes", map[string]any{"host_id": "h2", "results": []any{}}); code != http.StatusForbidden {
		t.Fatalf("a report for another host: %d", code)
	}
	if code, out = report(map[string]any{"repo": "bifrost-platform", "ref": ref, "commit": commitA, "outcome": "deleted"},
		map[string]any{"repo": "bifrost-platform", "ref": store.AttemptRef(task, 2), "commit": commitA, "outcome": "refused",
			"detail": "remote: Gitea: branch is protected"}); code != http.StatusOK || out["recorded"] != float64(2) {
		t.Fatalf("a report: %d %v", code, out)
	}
	var pruned string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'pruned' FROM agentrt.event WHERE event_type = 'note' AND task_id = $1`, task).Scan(&pruned))
	if pruned != ref {
		t.Fatalf("pruned %q", pruned)
	}
	if code, out = call(t, s, "agentd:h1", "GET", "prunes?host_id=h1", nil); code != http.StatusOK || out["paused_until"] == nil {
		t.Fatalf("after a refusal: %d %v; want the handing out paused", code, out)
	}
}
