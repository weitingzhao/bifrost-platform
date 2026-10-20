package agentrtapi_test

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The checker catches what it is there to catch. Reviews 449 and 451 found
// each of these passing an earlier checker (a regression case): a step moved
// out of dropped; a wrong move inside a round trip that ends where it began;
// a move whose only other event in its transaction is about something else,
// or is about the task but is not what causes that move; a refused request
// that changed a row's other columns; a plan node no event of the request
// creates beside an import head on its tree; an attempt that submits without
// starting. Two retained cases held before too: a move with no reason and
// nothing else in its transaction, and events that end elsewhere than the row.
func TestStateEventCheckerCatches(t *testing.T) {
	// ev is a move; each sibling is "<id> <type>".
	ev := func(id int64, typ, from, to string, reason bool, siblings ...string) fuzzEvent {
		body := map[string]any{"from": from, "to": to}
		if reason {
			body["reason"] = "test"
		}
		e := fuzzEvent{id: id, typ: typ, body: body}
		for _, x := range siblings {
			n, kind, _ := strings.Cut(x, " ")
			sid, err := strconv.ParseInt(n, 10, 64)
			must(t, err)
			e.siblings = append(e.siblings, fuzzSibling{sid, kind})
		}
		return e
	}
	for name, c := range map[string]struct {
		table     string
		evs       []fuzzEvent
		from, now string
		want      string
	}{
		"regression: a step out of dropped": {"plan_node", []fuzzEvent{ev(1, "plan_node_state_changed", "dropped", "active", true)}, "dropped", "active", "no transition table"},
		"regression: a round trip with a wrong second move": {"task", []fuzzEvent{ev(1, "task_state_changed", "ready", "paused", true),
			ev(2, "task_state_changed", "blocked", "ready", true)}, "ready", "ready", `moves from "blocked"`},
		"regression: paused, its only sibling a checkpoint of the task": {"task", []fuzzEvent{ev(1, "task_state_changed", "ready", "paused", false, "2 attempt_checkpointed")},
			"ready", "paused", "no reason"},
		"regression: running, its only sibling the route": {"task", []fuzzEvent{ev(1, "task_state_changed", "leased", "running", false, "2 route_decided")},
			"leased", "running", "no reason"},
		"regression: ready again, the route an earlier one's": {"task", []fuzzEvent{
			ev(2, "task_state_changed", "ready", "paused", true, "1 route_decided", "3 task_state_changed"),
			ev(3, "task_state_changed", "paused", "ready", false, "1 route_decided", "2 task_state_changed")}, "ready", "ready", "no reason"},
		"regression: running, started before the task's move before it": {"task", []fuzzEvent{
			ev(2, "task_state_changed", "running", "ready", true, "1 attempt_started", "3 task_state_changed", "4 task_state_changed"),
			ev(3, "task_state_changed", "ready", "leased", true, "1 attempt_started", "2 task_state_changed", "4 task_state_changed"),
			ev(4, "task_state_changed", "leased", "running", false, "1 attempt_started", "2 task_state_changed", "3 task_state_changed")},
			"running", "running", "no reason"},
		"retained: no reason, nothing else about the row": {"task", []fuzzEvent{ev(1, "task_state_changed", "leased", "running", false)}, "leased", "running", "no reason"},
		"retained: events that end elsewhere":             {"mission", []fuzzEvent{ev(1, "mission_state_changed", "active", "paused", true)}, "active", "stopped", "end at"},
	} {
		if _, err := replayChain(c.table, c.evs, c.from, c.now); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", name, err, c.want)
		}
	}
	for name, e := range map[string]fuzzEvent{
		"running, its attempt started":   ev(2, "task_state_changed", "leased", "running", false, "1 attempt_started"),
		"submitted, its attempt was":     ev(2, "task_state_changed", "running", "submitted", false, "1 attempt_submitted"),
		"ready, with its route":          ev(1, "task_state_changed", "proposed", "ready", false, "2 route_decided"),
		"paused, with its reason":        ev(1, "task_state_changed", "ready", "paused", true),
		"a move with its cause_event_id": {id: 1, typ: "task_state_changed", cause: true, body: map[string]any{"from": "ready", "to": "paused"}},
	} {
		if !hasCause("task", e) {
			t.Errorf("%s: refused", name)
		}
	}
	if err := replayAttempt([]fuzzEvent{{id: 1, typ: "attempt_submitted"}}, "leased", "submitted", true); err == nil {
		t.Error("regression: a submit of an attempt that never started passed")
	}

	f := newStateFuzz(t, 1, 1, map[string]*routeTally{}, map[string]int{})
	move := func(tx pgx.Tx, from, to string, reason bool) error {
		body := map[string]any{"from": from, "to": to}
		if reason {
			body["reason"] = "test"
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-1", PlanNode: "R2:1-1", TaskID: f.task, Body: body}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = $2, version = version + 1 WHERE task_id = $1`, f.task, to)
		return err
	}
	judge := func(code int, fn func(tx pgx.Tx) error) error {
		t.Helper()
		before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
		must(t, store.InTx(ctx, f.pool, fn))
		return f.judge(t, "test", false, code, before, digest, since)
	}
	// A move whose transaction's other event (an upload) is not about the task.
	err := judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte("unrelated")); err != nil {
			return err
		}
		return move(tx, "ready", "paused", false)
	})
	if err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Errorf("regression: a move caused by an unrelated upload: %v", err)
	}
	// A move whose transaction's other event is about the task but is a note.
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, nerr := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "note", Actor: "controller",
			MissionID: "M-1", TaskID: f.task, Body: map[string]any{"text": "unrelated"}}); nerr != nil {
			return nerr
		}
		return move(tx, "paused", "ready", false)
	})
	if err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Errorf("regression: a move caused by a note about the task: %v", err)
	}
	// Ready again, the only route in the transaction one decided before the
	// task was paused (review 452 A-4).
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, rerr := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "route_decided", Actor: "controller",
			MissionID: "M-1", PlanNode: "R2:1-1", TaskID: f.task, Body: map[string]any{"job": "implementer", "candidates": []any{},
				"excluded": []any{}, "inputs": map[string]any{}, "fingerprint": "sha256:" + strings.Repeat("ab", 32)}}); rerr != nil {
			return rerr
		}
		if merr := move(tx, "ready", "paused", true); merr != nil {
			return merr
		}
		return move(tx, "paused", "ready", false)
	})
	if err == nil || !strings.Contains(err.Error(), "no reason") {
		t.Errorf("regression: ready again with an earlier route: %v", err)
	}
	must(t, store.InTx(ctx, f.pool, func(tx pgx.Tx) error { return move(tx, "ready", "paused", true) }))
	// A round trip back to where the task began (paused), its second move
	// recorded wrong: the state is the same before and after.
	if st := f.snapshot(t)[rowKey{"task", f.task}]; st != "paused" {
		t.Fatalf("the task is %s", st)
	}
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if merr := move(tx, "paused", "ready", true); merr != nil {
			return merr
		}
		return move(tx, "blocked", "paused", true)
	})
	if err == nil || !strings.Contains(err.Error(), `moves from "blocked"`) {
		t.Errorf("regression: a wrong move inside a round trip: %v", err)
	}
	// A node written beside an import head on its tree, with no event of its own.
	must(t, store.InTx(ctx, f.pool, func(tx pgx.Tx) error {
		_, cerr := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "A", Title: "architecture", Root: "ARCH"})
		return cerr
	}))
	err = judge(http.StatusOK, func(tx pgx.Tx) error {
		if _, ierr := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: an import head"}}); ierr != nil {
			return ierr
		}
		_, ierr := tx.Exec(ctx, `INSERT INTO agentrt.plan_node (plan_node_id, parent_plan_node_id, node_kind, ordinal, label, title, created_by_actor_id)
		  VALUES ('ARCH:extra', 'ARCH', 'arch_row', 9, 'extra', 'not in the document', 'controller')`)
		return ierr
	})
	if err == nil || !strings.Contains(err.Error(), "no event of the request creates") {
		t.Errorf("regression: a node beside an import head: %v", err)
	}
	// A refused request that changed a column other than the state.
	var db string
	must(t, f.pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db))
	admin, err := pgx.Connect(ctx, os.Getenv("AGENTRT_TEST_PG")+" dbname="+db)
	must(t, err)
	t.Cleanup(func() { _ = admin.Close(ctx) })
	before, digest, since := f.snapshot(t), f.digest(t), f.lastEvent(t)
	for _, q := range []string{`SET session_replication_role = replica`, `UPDATE agentrt.task SET title = 'edited' WHERE task_id = '` + f.task + `'`} {
		_, err = admin.Exec(ctx, q)
		must(t, err)
	}
	if err = f.judge(t, "test", false, http.StatusConflict, before, digest, since); err == nil || !strings.Contains(err.Error(), "agentrt.task changed") {
		t.Errorf("regression: a refused request that edited a title: %v", err)
	}
	// A refused request while another transaction writes a host's poll, a
	// liveness column (ci-platform-tj5wx): nothing the request did.
	before, digest, since = f.snapshot(t), f.digest(t), f.lastEvent(t)
	_, err = f.pool.Exec(ctx, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() WHERE host_id = 'h1'`)
	must(t, err)
	if err = f.judge(t, "test", false, http.StatusUnauthorized, before, digest, since); err != nil {
		t.Errorf("regression: a refused request beside a host's poll: %v", err)
	}
	// Another column of the same row still counts.
	before, digest, since = f.snapshot(t), f.digest(t), f.lastEvent(t)
	_, err = admin.Exec(ctx, `UPDATE agentrt.host SET capacity_json = '{"slots": 9}' WHERE host_id = 'h1'`)
	must(t, err)
	if err = f.judge(t, "test", false, http.StatusUnauthorized, before, digest, since); err == nil || !strings.Contains(err.Error(), "agentrt.host changed") {
		t.Errorf("a refused request that changed a host's capacity: %v", err)
	}
}
