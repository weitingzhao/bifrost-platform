package agentrtapi_test

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// proposeIn proposes a task of M-1 in repo, as readyTask does, and leaves it
// proposed.
func proposeIn(t *testing.T, tx pgx.Tx, id, repo string) {
	t.Helper()
	spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# "+repo+"\n"))
	must(t, err)
	_, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
		Class: "go", Title: repo, SpecArtifact: spec, Effect: "branch", Repo: repo, BaseCommit: commitA})
	must(t, err)
}

// handedOut is the refs of a GET /prunes answer, sorted, and its unread.
func handedOut(t *testing.T, out map[string]any) ([]string, []string) {
	t.Helper()
	var refs, unread []string
	for _, p := range out["prunes"].([]any) {
		refs = append(refs, p.(map[string]any)["ref"].(string))
	}
	sort.Strings(refs)
	for _, u := range asList(out["unread"]) {
		unread = append(unread, u.(map[string]any)["repo"].(string)+" "+u.(map[string]any)["what"].(string))
	}
	return refs, unread
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// TD-383: agentd asks which attempt branches its host may delete and says
// what it did; only its own host's token may. A branch is handed out once the
// rule lets it go; the accepted attempt's only while main, read now, holds
// its result (review 437 A1); a repository whose branches or main cannot be
// read, or are slow, is passed over and named, the others answered (436 B3,
// 437 B3). A report of a branch the runtime did not push, of a task still
// open, is refused (436 B2); a refusal is recorded and pauses the handing
// out (the rule itself is the store's, prune_test.go there).
func TestPruneRoutes(t *testing.T) {
	pool, _ := world(t)
	agentHeads, slowHeads = map[string][]store.AgentHead{}, map[string]bool{"bifrost-api": true}
	defer agentrtapi.SetPruneReadTimeoutForTest(200 * time.Millisecond)()
	defer func() { agentHeads, slowHeads, mainRead, pruneNow = nil, nil, nil, nil }()
	s := server(t, pool)
	task, open := readyTask(t, pool), readyTask(t, pool)
	ref := store.AttemptRef(task, 1)
	// task: cancelled after its first attempt pushed. acc: accepted, its
	// result seen on main. ui, api: tasks of repositories whose branches
	// cannot be read now, or only slowly.
	acc, ui, api := store.NewTaskID(), store.NewTaskID(), store.NewTaskID()
	accRef := store.AttemptRef(acc, 1)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		proposeIn(t, tx, acc, "bifrost-platform")
		proposeIn(t, tx, ui, "bifrost-ui")
		proposeIn(t, tx, api, "bifrost-api")
		evidence, err := store.PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte("pass\n"))
		must(t, err)
		if _, err = store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, id := range []string{task, ui, api} {
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
			    lease_expires_at, heartbeat_at, state, conclusion, pushed_ref, pushed_commit)
			  VALUES ($1, $2, 1, 'controller', 1, now(), now(), 'failed', 'failed', $3, $4)`,
				store.NewAttemptID(), id, store.AttemptRef(id, 1), commitA); err != nil {
				return err
			}
		}
		attempt := store.NewAttemptID()
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
		    lease_expires_at, heartbeat_at, state, conclusion, pushed_ref, pushed_commit, computed_at, persisted_at, report_artifact_id)
		  VALUES ($1, $2, 1, 'controller', 1, now(), now(), 'finished', 'done', $3, $4, now(), now(), $5)`,
			attempt, acc, accRef, commitA, evidence); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		    evidence_artifact_id, accepts_attempt) VALUES ($1, $2, $3, 'review', 'pass', $4, true)`,
			store.NewVerificationID(), acc, attempt, evidence); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.task SET state = 'accepted' WHERE task_id = $1`, acc)
		return err
	}))
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "note", Actor: "controller",
			MissionID: "M-1", TaskID: acc, Body: map[string]any{"text": "on main", "landed": commitA}}); err != nil {
			return err
		}
		return store.MoveTask(ctx, tx, "controller", task, "cancelled", "test")
	}))
	agentHeads["bifrost-platform"] = []store.AgentHead{{Repo: "bifrost-platform", Ref: ref, Commit: commitA},
		{Repo: "bifrost-platform", Ref: accRef, Commit: commitA}}
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
	for name, c := range map[string]struct {
		main   store.MainResults
		refs   []string
		unread []string
	}{
		"main holds the result": {func(context.Context, string) (store.OnMain, bool) {
			return store.OnMain{acc: {commitA: commitA}}, true
		}, []string{accRef, ref}, []string{"bifrost-api branches", "bifrost-ui branches"}},
		"reverted since": {func(context.Context, string) (store.OnMain, bool) { return store.OnMain{}, true },
			[]string{ref}, []string{"bifrost-api branches", "bifrost-ui branches"}},
		"main not read now": {func(context.Context, string) (store.OnMain, bool) { return nil, false },
			[]string{ref}, []string{"bifrost-api branches", "bifrost-ui branches", "bifrost-platform main"}},
	} {
		mainRead = c.main
		code, out = call(t, s, "agentd:h1", "GET", "prunes?host_id=h1", nil)
		refs, unread := handedOut(t, out)
		sort.Strings(c.refs)
		if code != http.StatusOK || !slices.Equal(refs, c.refs) || !slices.Equal(unread, c.unread) {
			t.Errorf("%s: %d, handed out %v, unread %v; want %v, %v", name, code, refs, unread, c.refs, c.unread)
		}
	}
	// Review 438 B3: however long a repository's own reads may take, the
	// answer comes within the bound of all reads, with what was read by then.
	restore := agentrtapi.SetPruneReadTimeoutForTest(10 * time.Second)
	restoreCollect := agentrtapi.SetPruneCollectForTest(600*time.Millisecond, 300*time.Millisecond)
	mainRead = func(ctx context.Context, _ string) (store.OnMain, bool) {
		<-ctx.Done() // main is slow too
		return nil, false
	}
	start := time.Now()
	code, out = call(t, s, "agentd:h1", "GET", "prunes?host_id=h1", nil)
	refs, unread := handedOut(t, out)
	if took := time.Since(start); code != http.StatusOK || took > 5*time.Second || !slices.Equal(refs, []string{ref}) ||
		!slices.Equal(unread, []string{"bifrost-api branches", "bifrost-ui branches", "bifrost-platform main"}) {
		t.Fatalf("slow reads: %d after %v, handed out %v, unread %v", code, took, refs, unread)
	}
	restoreCollect()
	restore()
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
