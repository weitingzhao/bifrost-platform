package agentrtapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-383: agentd asks which attempt branches its host may delete and says
// what it did; only its own host's token may; a branch is handed out once
// the rule lets it go, a repository that cannot be read is passed over and
// named (review 436 B3); a report of a branch the runtime did not push, or
// of a task still open, is refused (B2); a refusal is recorded and pauses the
// handing out (the rule itself is the store's, prune_test.go there).
func TestPruneRoutes(t *testing.T) {
	pool, _ := world(t)
	agentHeads = map[string][]store.AgentHead{}
	defer func() { agentHeads, pruneNow = nil, nil }()
	s := server(t, pool)
	task, open := readyTask(t, pool), readyTask(t, pool)
	ref := store.AttemptRef(task, 1)
	// The task's first attempt pushed its branch; the task is cancelled. A
	// task of another repository, whose branches cannot be read now.
	ui := store.NewTaskID()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# ui\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: ui, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "ui", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-ui", BaseCommit: commitA}); err != nil {
			return err
		}
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, id := range []string{task, ui} {
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
			    lease_expires_at, heartbeat_at, state, conclusion, pushed_ref, pushed_commit)
			  VALUES ($1, $2, 1, 'controller', 1, now(), now(), 'failed', 'failed', $3, $4)`,
				store.NewAttemptID(), id, store.AttemptRef(id, 1), commitA); err != nil {
				return err
			}
		}
		return nil
	}))
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", task, "cancelled", "test") }))
	agentHeads["bifrost-platform"] = []store.AgentHead{{Repo: "bifrost-platform", Ref: ref, Commit: commitA}}
	agentHeads["bifrost-ui"] = nil // cannot be read
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
	pruneNow = func() time.Time { return time.Now().Add(store.PruneAfter + time.Hour) }
	code, out = call(t, s, "agentd:h1", "GET", "prunes?host_id=h1", nil)
	list, _ := out["prunes"].([]any)
	if code != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["ref"] != ref || list[0].(map[string]any)["commit"] != commitA {
		t.Fatalf("prunes a week on: %d %v; want the cancelled task's branch", code, out)
	}
	if unread, _ := out["unread"].([]any); len(unread) != 1 || unread[0] != "bifrost-ui" {
		t.Fatalf("unread %v; want bifrost-ui, passed over", out["unread"])
	}
	report := func(results ...map[string]any) (int, map[string]any) {
		return call(t, s, "agentd:h1", "POST", "prunes", map[string]any{"host_id": "h1", "results": results})
	}
	for name, bad := range map[string]map[string]any{
		"main":               {"repo": "bifrost-platform", "ref": "refs/heads/main", "commit": commitA, "outcome": "deleted"},
		"an open task":       {"repo": "bifrost-platform", "ref": store.AttemptRef(open, 1), "commit": commitA, "outcome": "deleted"},
		"no such attempt":    {"repo": "bifrost-platform", "ref": store.AttemptRef(task, 2), "commit": commitA, "outcome": "refused"},
		"another repository": {"repo": "bifrost-ui", "ref": ref, "commit": commitA, "outcome": "deleted"},
	} {
		if code, out = report(bad); code != http.StatusUnprocessableEntity {
			t.Errorf("a report of %s: %d %v", name, code, out)
		}
	}
	if code, _ = call(t, s, "agentd:h1", "POST", "prunes", map[string]any{"host_id": "h2", "results": []any{}}); code != http.StatusForbidden {
		t.Fatalf("a report for another host: %d", code)
	}
	if code, out = report(map[string]any{"repo": "bifrost-platform", "ref": ref, "commit": commitA, "outcome": "deleted"},
		map[string]any{"repo": "bifrost-platform", "ref": ref, "commit": commitA, "outcome": "refused",
			"detail": "remote: Gitea: branch is protected"}); code != http.StatusOK || out["recorded"] != float64(2) {
		t.Fatalf("a report: %d %v", code, out)
	}
	var pruned string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'pruned' FROM agentrt.event WHERE event_type = 'note' AND task_id = $1
	  AND body_json ? 'pruned'`, task).Scan(&pruned))
	if pruned != ref {
		t.Fatalf("pruned %q", pruned)
	}
	pruneNow = nil
	if code, out = call(t, s, "agentd:h1", "GET", "prunes?host_id=h1", nil); code != http.StatusOK || out["paused_until"] == nil {
		t.Fatalf("after a refusal: %d %v; want the handing out paused", code, out)
	}
}
