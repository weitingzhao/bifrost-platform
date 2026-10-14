package store_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Regression tests for review 154.

const (
	claudeImpl = "implementer/exec:claude/anthropic/*"
	cursorImpl = "implementer/exec:cursor/cursor-mix/*"
	codexImpl  = "implementer/exec:codex/openai/*"
	swap       = `UPDATE agentrt.job_candidate SET status = CASE status WHEN 'active' THEN 'probation' ELSE 'active' END
	  WHERE job_id = 'implementer'`
)

// capabilityWrite changes candidates or capability facts as the Owner's rule
// would: the capability lock first, then the event, then the rows.
func (w *world) capabilityWrite(source string, sql ...string) error {
	return capabilityWriteOn(w.rt, source, sql...)
}

func capabilityWriteOn(db store.DB, source string, sql ...string) error {
	return store.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: " + source}}); err != nil {
			return err
		}
		for _, q := range sql {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
}

func (w *world) reroute(t *testing.T) int {
	t.Helper()
	n, err := store.Reroute(ctx, w.rt)
	ok(t, err)
	return n
}

func (w *world) routes(t *testing.T, task string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'route_decided' AND task_id = $1`, task).Scan(&n))
	return n
}

// A1: a demotion, a new model pattern or a new restriction written while a
// claim is choosing waits for the claim, and lands after its lease.
func TestCapabilityWritesWaitForTheClaim(t *testing.T) {
	for _, write := range []struct{ name, sql string }{
		{"demote", `UPDATE agentrt.job_candidate SET status = 'demoted' WHERE job_candidate_id = '` + claudeImpl + `'`},
		{"pattern", `UPDATE agentrt.job_candidate SET model_pattern = 'sonnet-*' WHERE job_candidate_id = '` + claudeImpl + `'`},
		{"restrict", `INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, fact_kind, subject, source_kind, source)
		  VALUES ('claude-cannot-ui', 'exec:claude', 'cannot', 'ui', 'measured', 'test')`},
	} {
		t.Run(write.name, func(t *testing.T) {
			w := newRoutedWorld(t)
			pool := w.db.Pool(t, w.db.Runtime)
			w.jobs(t, "active", "*", claude)
			task := w.ready(t, store.Task{Needs: []string{"ui"}})
			done := make(chan error, 1)
			started := false
			unhook := store.SetHook(func(point, arg string) {
				if point != "picked" || arg != task || started {
					return
				}
				started = true
				go func() { done <- capabilityWriteOn(pool, write.name, write.sql) }()
				w.waitFor(t, "pg_advisory_xact_lock($1)", time.Now().Add(5*time.Second))
			})
			c, err := store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{claude}})
			unhook()
			ok(t, err)
			if c == nil || c.TaskID != task || c.Tool != claude {
				t.Fatalf("claim = %+v", c)
			}
			select {
			case err := <-done:
				ok(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("the write never finished")
			}
			var after bool
			ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT max(event_id) FROM agentrt.event WHERE event_type = 'legacy_imported')
			  > (SELECT event_id FROM agentrt.event WHERE event_type = 'attempt_leased' AND task_id = $1)`, task).Scan(&after))
			if !after {
				t.Fatal("the write landed before the lease it raced")
			}
		})
	}
}

// A1: a route decided again between the claim's pick and its lock is the
// route the claim follows.
func TestClaimFollowsTheRouteUnderItsLock(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", cursor)
	w.jobs(t, "probation", "*", claude)
	task := w.ready(t, store.Task{})
	ok(t, w.capabilityWrite("swap", swap))
	rerouted := -1
	unhook := store.SetHook(func(point, arg string) {
		if point != "picked" || arg != task || rerouted >= 0 {
			return
		}
		rerouted = 0
		rerouted = w.reroute(t)
	})
	c, err := store.ClaimNext(ctx, w.db.Pool(t, w.db.Runtime), store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{cursor, claude}})
	unhook()
	ok(t, err)
	if rerouted != 1 || c == nil || c.TaskID != task || c.Tool != claude {
		t.Fatalf("rerouted %d, claim %+v: the claim followed the route it read before its lock", rerouted, c)
	}
}

// A2: the controller decides again when a candidate changes under the same
// id (status, model pattern), and not again when nothing changed.
func TestRerouteSeesChangesUnderTheSameID(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", cursor)
	w.jobs(t, "probation", "*", claude)
	task := w.ready(t, store.Task{})
	route := func() string { t.Helper(); return fmt.Sprint(w.lastRoute(t, task)["candidates"]) }
	if r := route(); r != "["+cursorImpl+" "+claudeImpl+"]" {
		t.Fatalf("route = %s", r)
	}
	ok(t, w.capabilityWrite("swap", swap))
	if n := w.reroute(t); n != 1 {
		t.Fatalf("a status swap rerouted %d", n)
	}
	if r := route(); r != "["+claudeImpl+" "+cursorImpl+"]" {
		t.Fatalf("after the swap, route = %s", r)
	}
	if n := w.reroute(t); n != 0 {
		t.Fatalf("nothing changed and %d rerouted", n)
	}
	ok(t, w.capabilityWrite("pattern", `UPDATE agentrt.job_candidate SET model_pattern = 'sonnet-*' WHERE job_candidate_id = '`+claudeImpl+`'`))
	if n := w.reroute(t); n != 1 {
		t.Fatalf("a pattern change rerouted %d", n)
	}
	in := w.lastRoute(t, task)["inputs"].(map[string]any)
	first := in["candidates"].([]any)[0].(map[string]any)
	if first["id"] != claudeImpl || first["pattern"] != "sonnet-*" || first["status"] != "active" {
		t.Fatalf("recorded candidate = %v", first)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("model opus under sonnet-* took %s", c.TaskID)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, claude, cursor); c == nil || c.Tool != cursor {
		t.Fatalf("claim = %+v", c)
	}
}

// B1: two controllers, or a controller and anything else that decides the
// route in between, record one decision per change. A controller that finds
// the task held by another passes over it.
func TestRerouteDecidesOnce(t *testing.T) {
	w := newRoutedWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	outer := func() int { t.Helper(); n, err := store.Reroute(ctx, pool); ok(t, err); return n }
	task := w.ready(t, store.Task{})
	w.jobs(t, "active", "*", claude)
	other := -1
	unhook := store.SetHook(func(point, arg string) {
		if point != "rerouting" || arg != task || other >= 0 {
			return
		}
		other = 0
		other = w.reroute(t)
	})
	n := outer()
	unhook()
	if other != 1 || n != 0 || w.routes(t, task) != 2 {
		t.Fatalf("other controller %d, this one %d, %d decisions", other, n, w.routes(t, task))
	}

	w.jobs(t, "probation", "*", codex)
	other = -1
	unhook = store.SetHook(func(point, arg string) {
		if point != "reroute" || arg != task || other >= 0 {
			return
		}
		other = 0
		other = w.reroute(t)
	})
	n = outer()
	unhook()
	if other != 0 || n != 1 || w.routes(t, task) != 3 {
		t.Fatalf("while held: other controller %d, this one %d, %d decisions", other, n, w.routes(t, task))
	}
}

// B3: a restriction added takes a candidate off the route; retired, it puts
// it back.
func TestRerouteFollowsRestrictions(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", claude, codex)
	task := w.ready(t, store.Task{Needs: []string{"gpu"}})
	ok(t, w.capabilityWrite("restrict", `INSERT INTO agentrt.capability_fact
	  (capability_fact_id, executor_actor_id, model_provider, fact_kind, subject, source_kind, source)
	  VALUES ('claude-cannot-gpu', 'exec:claude', 'anthropic', 'cannot', 'gpu', 'measured', 'test')`))
	if n := w.reroute(t); n != 1 {
		t.Fatalf("a new restriction rerouted %d", n)
	}
	r := w.lastRoute(t, task)
	if fmt.Sprint(r["candidates"]) != "["+codexImpl+"]" || fmt.Sprint(r["excluded"]) !=
		"[map[candidate:"+claudeImpl+" reason:cannot or must_not on a need reasons:[cannot or must_not on a need]]]" {
		t.Fatalf("route = %v", r)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("a restricted candidate took %s", c.TaskID)
	}
	ok(t, w.capabilityWrite("retire", `UPDATE agentrt.capability_fact SET retired_at = clock_timestamp() WHERE capability_fact_id = 'claude-cannot-gpu'`))
	if n := w.reroute(t); n != 1 {
		t.Fatalf("a retired restriction rerouted %d", n)
	}
	if r := fmt.Sprint(w.lastRoute(t, task)["candidates"]); r != "["+claudeImpl+" "+codexImpl+"]" {
		t.Fatalf("after retiring, route = %s", r)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c == nil || c.TaskID != task {
		t.Fatalf("claim = %+v", c)
	}
}

// B2: route_decided records what it read, so the decision can be made again
// from the record alone, whatever changed since.
func TestRouteDecisionReplays(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", cursor, codex)
	w.jobs(t, "probation", "*", claude)
	impl := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", store.LeaseTTL, cursor); c == nil || c.TaskID != impl {
		t.Fatalf("claim = %+v", c)
	}
	ok(t, w.capabilityWrite("restrict", `INSERT INTO agentrt.capability_fact
	  (capability_fact_id, executor_actor_id, fact_kind, subject, source_kind, source)
	  VALUES ('codex-cannot-gpu', 'exec:codex', 'cannot', 'gpu', 'measured', 'test')`))
	review := w.ready(t, store.Task{Role: "review", Subject: impl, Effect: "none", Needs: []string{"gpu"}})
	body := w.lastRoute(t, review)
	if fmt.Sprint(body["candidates"]) != "[reviewer/exec:claude/anthropic/*]" {
		t.Fatalf("route = %v", body["candidates"])
	}
	ok(t, w.capabilityWrite("change everything", swap,
		`UPDATE agentrt.job_candidate SET status = 'demoted' WHERE job_candidate_id = 'reviewer/exec:claude/anthropic/*'`,
		`UPDATE agentrt.capability_fact SET retired_at = clock_timestamp() WHERE capability_fact_id = 'codex-cannot-gpu'`))

	in := body["inputs"].(map[string]any)
	if fmt.Sprint(in["needs"]) != "[gpu]" || len(in["facts"].([]any)) != 1 || len(in["attempts"].([]any)) != 1 {
		t.Fatalf("recorded inputs = %v", in)
	}
	route, excluded, fp, err := store.ReplayRoute(in)
	ok(t, err)
	got, err := json.Marshal([]any{route, excluded})
	ok(t, err)
	want, err := json.Marshal([]any{body["candidates"], body["excluded"]})
	ok(t, err)
	if string(got) != string(want) || fp != body["fingerprint"] {
		t.Fatalf("replayed %s (%s), recorded %s (%v)", got, fp, want, body["fingerprint"])
	}
}

// B4: a pass over many ready tasks of which none changed decides nothing and
// opens no transaction per task.
func TestReroutePassScales(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", claude, codex)
	const n = 200
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for i := 0; i < n; i++ {
			id := store.NewTaskID()
			if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: s1, Role: "implement",
				Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "branch"}); err != nil {
				return err
			}
			if err := store.MarkReady(ctx, tx, "controller", id); err != nil {
				return err
			}
		}
		return nil
	}))
	opened := 0
	unhook := store.SetHook(func(point, _ string) {
		if point == "rerouting" {
			opened++
		}
	})
	start := time.Now()
	decided := w.reroute(t)
	took := time.Since(start)
	unhook()
	t.Logf("a pass over %d unchanged ready tasks took %s", n, took)
	if decided != 0 || opened != 0 {
		t.Fatalf("decided %d, opened a transaction for %d tasks", decided, opened)
	}
	if took > 5*time.Second {
		t.Fatalf("a pass over %d ready tasks took %s", n, took)
	}
}

// A claim walks the route: a candidate written after the route was decided
// takes the task only once the controller has routed it again.
func TestClaimTakesOnlyTheRoute(t *testing.T) {
	w := newRoutedWorld(t)
	w.jobs(t, "active", "*", cursor)
	task := w.ready(t, store.Task{})
	w.jobs(t, "active", "*", claude)
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("a candidate not on the route took %s", c.TaskID)
	}
	if n := w.reroute(t); n != 1 {
		t.Fatalf("rerouted %d", n)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c == nil || c.TaskID != task {
		t.Fatalf("claim = %+v", c)
	}
}
