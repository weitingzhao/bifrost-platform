package store_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// newRoutedWorld is a kernel world without jobs: the tests write their own.
func newRoutedWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	for i := 1; i <= 4; i++ {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", fmt.Sprintf("h%d", i), "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.OneAttemptPerHost)
	ok(t, err)
	return w
}

func (w *world) lastRoute(t *testing.T, task string) map[string]any {
	t.Helper()
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type = 'route_decided' AND task_id = $1
	  ORDER BY event_id DESC LIMIT 1`, task).Scan(&body))
	return body
}

func (w *world) retire(t *testing.T, candidate string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: demote"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.job_candidate SET status = 'demoted' WHERE job_candidate_id = $1`, candidate)
		return err
	}))
}

// ① With no candidates for its job a task goes to nobody, however able the
// tools; once candidates are written, the controller routes it again, once.
func TestRouteNeedsCandidates(t *testing.T) {
	w := newRoutedWorld(t)
	task := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", store.LeaseTTL, claude, codex, cursor); c != nil {
		t.Fatalf("claimed %s with no candidates", c.TaskID)
	}
	if r := w.lastRoute(t, task); r["job"] != "implementer" || len(r["candidates"].([]any)) != 0 {
		t.Fatalf("route = %v", r)
	}
	w.jobs(t, "active", "*", claude)
	n, err := store.Reroute(ctx, w.rt)
	ok(t, err)
	if n != 1 {
		t.Fatalf("rerouted %d", n)
	}
	if n, err = store.Reroute(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a second reroute decided %d (%v)", n, err)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, codex, claude); c == nil || c.TaskID != task || c.Tool != claude {
		t.Fatalf("claim = %+v", c)
	}

	other := w.ready(t, store.Task{Role: "other", Effect: "none", OffPlanReason: "outside the plan"})
	if r := w.lastRoute(t, other); r["job"] != "none" {
		t.Fatalf("route of role other = %v", r)
	}
	if c := w.claim(t, "h2", store.LeaseTTL, claude); c != nil {
		t.Fatalf("a task with no job went to %s", c.Tool.Executor)
	}
}

// T08, routing half: the route says who was dropped and why; the claim
// follows the route's order, active before probation, not the host's.
func TestRouteRecordsWhyAndOrder(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", cursor, codex)
	w.jobs(t, "probation", "*", claude)
	impl := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", store.LeaseTTL, claude, cursor); c == nil || c.Tool != cursor {
		t.Fatalf("the active candidate did not come first: %+v", c)
	}
	review := w.ready(t, store.Task{Role: "review", Subject: impl, Effect: "none"})
	r := w.lastRoute(t, review)
	excluded := map[string]string{}
	for _, x := range r["excluded"].([]any) {
		e := x.(map[string]any)
		excluded[e["candidate"].(string)] = e["reason"].(string)
	}
	if excluded["reviewer/exec:cursor/cursor-mix/*"] != "not independent of the subject's attempts" {
		t.Fatalf("route_decided = %v", r)
	}
	route := fmt.Sprint(r["candidates"])
	if route != "[reviewer/exec:codex/openai/* reviewer/exec:claude/anthropic/*]" {
		t.Fatalf("route = %s", route)
	}
	if c := w.claim(t, "h2", store.LeaseTTL, cursor, claude, codex); c == nil || c.TaskID != review || c.Tool != codex {
		t.Fatalf("review claim = %+v", c)
	}
}

// A candidate demoted after the route was decided no longer takes the task;
// a model pattern admits only the models it matches.
func TestRouteChecksCandidatesAtClaim(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "gpt-*", codex)
	w.jobs(t, "active", "*", claude)
	task := w.ready(t, store.Task{})
	o3 := store.Tool{Executor: "exec:codex", Provider: "openai", Model: "o3"}
	if c := w.claim(t, "h1", store.LeaseTTL, o3); c != nil {
		t.Fatalf("model o3 matched gpt-*: %+v", c)
	}
	w.retire(t, "implementer/exec:claude/anthropic/*")
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("a demoted candidate took %s", c.TaskID)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c == nil || c.TaskID != task {
		t.Fatalf("gpt-x under gpt-*: %+v", c)
	}
}
