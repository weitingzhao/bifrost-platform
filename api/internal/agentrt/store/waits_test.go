package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

type waitRow struct {
	stream, actor, mission, task, attempt string
	body                                  map[string]any
}

// waitsRecorded are the wait_limit_passed events, oldest first.
func waitsRecorded(t *testing.T, w *world) []waitRow {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT event_stream_id, actor_id, COALESCE(mission_id, ''), COALESCE(task_id, ''),
	    COALESCE(attempt_id, ''), body_json::text
	  FROM agentrt.event WHERE event_type = 'wait_limit_passed' ORDER BY event_id`)
	ok(t, err)
	defer rows.Close()
	var out []waitRow
	for rows.Next() {
		var r waitRow
		var doc string
		ok(t, rows.Scan(&r.stream, &r.actor, &r.mission, &r.task, &r.attempt, &doc))
		ok(t, json.Unmarshal([]byte(doc), &r.body))
		out = append(out, r)
	}
	ok(t, rows.Err())
	return out
}

func waitsAt(t *testing.T, w *world, now time.Time) int {
	t.Helper()
	n, err := store.WaitLimits(ctx, w.rt, now)
	ok(t, err)
	return n
}

// at reads an instant of the database as the record writes it.
func (w *world) at(t *testing.T, q string, args ...any) string {
	t.Helper()
	var at time.Time
	ok(t, w.rt.QueryRow(ctx, q, args...).Scan(&at))
	return at.UTC().Format(time.RFC3339Nano)
}

func num(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

// RULE-waits classes 1 and 7 (W-31 2.3, 总则 4: the platform keeps the clock):
// an implementation running past 30 minutes and a ready task no host took
// for 30 minutes are recorded once each, with when they began, the limit and
// what it came from, and whom they call (the thread that proposed the work);
// before the limit nothing is, and a later pass records nothing again.
func TestWaitsPastTheirLimit(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-1",
		[]store.TaskProposal{proposal("first", w.spec), proposal("second", w.spec)})
	ok(t, err)
	ids := taskIDs(out)
	_, err = store.Dispatch(ctx, w.rt)
	ok(t, err)
	c := w.claim(t, "h1", time.Hour, codex)
	if c == nil {
		t.Fatal("nothing to claim")
	}
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	busy, idle := c.TaskID, ids["first"]
	if idle == busy {
		idle = ids["second"]
	}
	started := w.at(t, `SELECT started_at FROM agentrt.attempt WHERE attempt_id = $1`, c.AttemptID)
	readySince := w.at(t, `SELECT max(at) FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'ready'`, idle)

	now := time.Now()
	if n := waitsAt(t, w, now.Add(29*time.Minute)); n != 0 {
		t.Fatalf("%d waits recorded before the limit", n)
	}
	if n := waitsAt(t, w, now.Add(31*time.Minute)); n != 2 {
		t.Fatalf("%d waits recorded past the limit, want the attempt and the ready task", n)
	}
	got := map[string]waitRow{}
	for _, r := range waitsRecorded(t, w) {
		got[r.body["wait"].(string)] = r
	}
	a, r := got[store.WaitAttemptRunning], got[store.WaitTaskReady]
	if a.stream != store.WaitsStream || a.actor != "controller" || a.mission != "M-1" || a.task != busy || a.attempt != c.AttemptID ||
		a.body["ref"] != c.AttemptID || a.body["since"] != started || num(a.body["limit_s"]) != 1800 || a.body["basis"] != "floor" ||
		a.body["calls"] != "thread:s1" || num(a.body["waited_s"]) < 1800 || num(a.body["waited_s"]) > 1900 || a.body["usual_s"] != nil {
		t.Fatalf("the attempt's wait %+v (started %s)", a, started)
	}
	if r.stream != store.WaitsStream || r.task != idle || r.attempt != "" || r.body["ref"] != idle || r.body["since"] != readySince ||
		num(r.body["limit_s"]) != 1800 || r.body["basis"] != "floor" || r.body["calls"] != "thread:s1" {
		t.Fatalf("the ready task's wait %+v (ready since %s)", r, readySince)
	}
	// A wait is recorded once, however long it goes on.
	for _, later := range []time.Duration{31 * time.Minute, 3 * time.Hour} {
		if n := waitsAt(t, w, now.Add(later)); n != 0 {
			t.Fatalf("%s: %d recorded again", later, n)
		}
	}
	if n := len(waitsRecorded(t, w)); n != 2 {
		t.Fatalf("%d records, want 2", n)
	}
	// Nothing else moved: the task is still ready, the attempt still running.
	if w.state(t, "task", idle) != "ready" || w.state(t, "attempt", c.AttemptID) != "running" {
		t.Fatalf("a wait moved state: task %s, attempt %s", w.state(t, "task", idle), w.state(t, "attempt", c.AttemptID))
	}
	// A task ready again later waits anew, and that wait is its own record.
	for _, to := range []string{"blocked", "ready"} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", idle, to) }))
	}
	again := w.at(t, `SELECT max(at) FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'ready'`, idle)
	if n := waitsAt(t, w, time.Now().Add(20*time.Minute)); n != 0 {
		t.Fatalf("the new wait was recorded after 20 minutes (%d)", n)
	}
	if n := waitsAt(t, w, time.Now().Add(31*time.Minute)); n != 1 {
		t.Fatalf("the new wait: %d recorded, want 1", n)
	}
	ws := waitsRecorded(t, w)
	if last := ws[len(ws)-1]; len(ws) != 3 || last.body["ref"] != idle || last.body["since"] != again || again == readySince {
		t.Fatalf("records %+v; want a third for %s since %s", ws, idle, again)
	}
}

// The limit is twice the usual time when that is above the floor: the
// task's own (task_usual), else its role and class's accepted attempts
// (history_usual, as Progress reads it); a check's floor is 20 minutes.
func TestAWaitsLimitIsTwiceTheUsualTime(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for range store.UsualMinSamples {
		w.ended(t, "done")
	}
	// Three accepted implement/go attempts of about 1100 s each.
	w.legacy(t, `UPDATE agentrt.attempt SET started_at = started_at - interval '1000 seconds' WHERE state = 'finished'`)
	hist := w.ready(t, store.Task{})
	own := w.ready(t, store.Task{})
	w.legacy(t, fmt.Sprintf(`UPDATE agentrt.task SET usual_s = 3000 WHERE task_id = '%s'`, own))
	now := time.Now()
	if n := waitsAt(t, w, now.Add(31*time.Minute)); n != 0 {
		t.Fatalf("%d recorded past the floor but within twice the usual time", n)
	}
	if n := waitsAt(t, w, now.Add(40*time.Minute)); n != 1 {
		t.Fatalf("%d recorded at 40 minutes, want the task of the history's usual time", n)
	}
	h := waitsRecorded(t, w)[0]
	if us, ls := num(h.body["usual_s"]), num(h.body["limit_s"]); h.body["ref"] != hist || h.body["basis"] != "history_usual" ||
		us < 1095 || us > 1105 || ls < 2*us-1 || ls > 2*us+1 {
		t.Fatalf("the history's wait %+v", h.body)
	}
	if n := waitsAt(t, w, now.Add(99*time.Minute)); n != 0 {
		t.Fatalf("the task's own usual time (3000 s) was passed at 99 minutes (%d)", n)
	}
	if n := waitsAt(t, w, now.Add(101*time.Minute)); n != 1 {
		t.Fatalf("%d recorded at 101 minutes, want the task of its own usual time", n)
	}
	o := waitsRecorded(t, w)[1]
	if o.body["ref"] != own || o.body["basis"] != "task_usual" || num(o.body["usual_s"]) != 3000 || num(o.body["limit_s"]) != 6000 {
		t.Fatalf("the task's own wait %+v", o.body)
	}

	// A verification running: its floor is 20 minutes.
	w2 := newKernelWorld(t)
	impl := w2.ready(t, store.Task{Repo: "bifrost-platform"})
	w2.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w2.rt, impl)
	ok(t, err)
	vc := w2.claim(t, "h2", time.Hour, deterministic)
	if vc == nil || vc.TaskID != v {
		t.Fatalf("the verification's claim %+v", vc)
	}
	ok(t, store.Start(ctx, w2.rt, fence(vc, "h2"), "", ""))
	now = time.Now()
	if n := waitsAt(t, w2, now.Add(19*time.Minute)); n != 0 {
		t.Fatalf("%d recorded before a check's floor", n)
	}
	if n := waitsAt(t, w2, now.Add(21*time.Minute)); n != 1 {
		t.Fatalf("%d recorded past a check's floor", n)
	}
	if c := waitsRecorded(t, w2)[0]; c.body["wait"] != store.WaitAttemptRunning || c.body["ref"] != vc.AttemptID ||
		num(c.body["limit_s"]) != 1200 || c.body["basis"] != "floor" || c.task != v {
		t.Fatalf("the check's wait %+v", c)
	}
}

// A ready task held by a stop or a pause (global, its mission's, its own or
// its subject's), or of a mission not active, waits for nobody's clock: it
// is not timed; lifted, it is, from when it became ready.
func TestAHeldTaskIsNotTimed(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	held := w.ready(t, store.Task{})
	readySince := w.at(t, `SELECT max(at) FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'ready'`, held)
	other := w.ready(t, store.Task{MissionID: "M-2", OffPlanReason: "test"}) // M-2 is proposed, not active
	for i, scope := range []struct{ scope, ref, kind string }{{"global", "", "stop"}, {"mission", "M-1", "pause"}, {"task", held, "stop"}} {
		id := fmt.Sprintf("c%d", i)
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			return store.SetControl(ctx, tx, "owner", id, scope.scope, scope.ref, scope.kind, "test: hold")
		}))
		if n := waitsAt(t, w, time.Now().Add(31*time.Minute)); n != 0 {
			t.Fatalf("%s %s: %d recorded while held", scope.scope, scope.kind, n)
		}
		_, err := store.LiftControl(ctx, w.rt, "owner", id)
		ok(t, err)
	}
	if n := waitsAt(t, w, time.Now().Add(31*time.Minute)); n != 1 {
		t.Fatalf("%d recorded once lifted, want the held task", n)
	}
	if r := waitsRecorded(t, w)[0]; r.body["ref"] != held || r.body["since"] != readySince {
		t.Fatalf("the lifted task's wait %+v; want since %s", r.body, readySince)
	}
	if w.state(t, "task", other) != "ready" {
		t.Fatalf("the task of the mission not active is %s, not ready: the case proves nothing", w.state(t, "task", other))
	}
}

// RULE-waits class 3: a message to a thread it has not acknowledged in 10
// minutes calls that thread, once; an acknowledged one, mail to anyone but a
// thread, and mail older than the horizon are not timed.
func TestUnreadMailToAThread(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	// The thread writes first, so it is an actor mail can go to.
	_, err := store.SendMessage(ctx, w.rt, wr("thread:s1"), "owner", "note", []byte("to the Owner"))
	ok(t, err)
	unread, err := store.SendMessage(ctx, w.rt, owner, "thread:s1", "note", []byte("read me"))
	ok(t, err)
	read, err := store.SendMessage(ctx, w.rt, owner, "thread:s1", "note", []byte("and me"))
	ok(t, err)
	_, err = store.AckMessages(ctx, w.rt, wr("thread:s1"), []string{read})
	ok(t, err)
	sent := w.at(t, `SELECT created_at FROM agentrt.message WHERE message_id = $1`, unread)
	now := time.Now()
	if n := waitsAt(t, w, now.Add(9*time.Minute)); n != 0 {
		t.Fatalf("%d recorded before 10 minutes", n)
	}
	if n := waitsAt(t, w, now.Add(11*time.Minute)); n != 1 {
		t.Fatalf("%d recorded after 10 minutes, want the unread message", n)
	}
	if n := waitsAt(t, w, now.Add(time.Hour)); n != 0 {
		t.Fatalf("recorded again (%d)", n)
	}
	m := waitsRecorded(t, w)[0]
	if m.stream != store.WaitsStream || m.body["wait"] != store.WaitMessageUnread || m.body["ref"] != unread ||
		m.body["since"] != sent || num(m.body["limit_s"]) != 600 || m.body["basis"] != "floor" || m.body["calls"] != "thread:s1" ||
		m.task != "" || m.attempt != "" {
		t.Fatalf("the message's wait %+v", m)
	}
	// Beyond the horizon a week-old message is not a wait anyone is in.
	w2 := newKernelWorld(t)
	_, err = store.SendMessage(ctx, w2.rt, wr("thread:s2"), "owner", "note", []byte("hello"))
	ok(t, err)
	_, err = store.SendMessage(ctx, w2.rt, owner, "thread:s2", "note", []byte("old news"))
	ok(t, err)
	if n := waitsAt(t, w2, time.Now().Add(store.WaitMessageHorizon+time.Minute)); n != 0 {
		t.Fatalf("%d recorded past the horizon", n)
	}
}

// Two passes at once record a wait once (the stream's lock).
func TestTwoPassesRecordAWaitOnce(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for range 3 {
		w.ready(t, store.Task{})
	}
	at := time.Now().Add(31 * time.Minute)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		conn := w.db.Connect(t, w.db.Runtime)
		go func() {
			defer wg.Done()
			_, errs[i] = store.WaitLimits(ctx, conn, at)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		ok(t, err)
	}
	if n := len(waitsRecorded(t, w)); n != 3 {
		t.Fatalf("%d records of 3 waits", n)
	}
}

// A record's references are checked at its commit, which waits for a
// transaction holding the attempt's row; that transaction appends to the
// attempt's mission's stream (as a start, a checkpoint or a submit does after
// lockLive), and the record holds no lock it waits for: both commit, no
// deadlock (the records are on their own stream).
func TestARecordWaitsForNoMissionsStream(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	id := w.ready(t, store.Task{})
	c := w.claimed(t, id, "h1", codex, true)
	holder := w.db.Connect(t, w.db.Runtime)
	tx, err := holder.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE`, c.AttemptID)
	ok(t, err)
	pass := w.db.Connect(t, w.db.Runtime)
	done := make(chan error, 1)
	go func() {
		_, werr := store.WaitLimits(ctx, pass, time.Now().Add(31*time.Minute))
		done <- werr
	}()
	// The pass waits at its commit for the attempt's row.
	w.waitFor(t, "commit", time.Now().Add(10*time.Second))
	_, err = store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "note", Actor: "controller", MissionID: "M-1",
		TaskID: id, AttemptID: c.AttemptID, Body: map[string]any{"text": "test: as a submit appends"}})
	ok(t, err)
	ok(t, tx.Commit(ctx))
	ok(t, <-done)
	if ws := waitsRecorded(t, w); len(ws) != 1 || ws[0].attempt != c.AttemptID {
		t.Fatalf("records %+v", ws)
	}
}

// sinceOf is when a ready task last became ready, or an attempt started, as
// the database has it.
func (w *world) readySince(t *testing.T, task string) time.Time {
	t.Helper()
	var at time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT max(at) FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'ready'`, task).Scan(&at))
	return at
}

// Review 474 B4: the limit at its edge (a wait exactly at its limit is not
// past it); a task's own usual time below the floor gives the floor and is
// still said; a role and class with fewer than three accepted attempts has
// no usual time; a clock in another zone reads the same instants.
func TestAWaitAtItsEdge(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ended(t, "done")
	w.ended(t, "done") // two samples: no usual time
	w.legacy(t, `UPDATE agentrt.attempt SET started_at = started_at - interval '5000 seconds' WHERE state = 'finished'`)
	own := w.ready(t, store.Task{})
	w.legacy(t, fmt.Sprintf(`UPDATE agentrt.task SET usual_s = 300 WHERE task_id = '%s'`, own))
	since := w.readySince(t, own)
	east := time.FixedZone("UTC+5", 5*3600)
	if n := waitsAt(t, w, since.Add(1800*time.Second).In(east)); n != 0 {
		t.Fatalf("%d recorded at exactly the limit", n)
	}
	if n := waitsAt(t, w, since.Add(1801*time.Second).In(east)); n != 1 {
		t.Fatalf("%d recorded a second past the limit", n)
	}
	for range 2 {
		if n := waitsAt(t, w, since.Add(time.Hour)); n != 0 {
			t.Fatalf("recorded again (%d)", n)
		}
	}
	r := waitsRecorded(t, w)[0]
	if r.body["ref"] != own || r.body["basis"] != "floor" || num(r.body["limit_s"]) != 1800 || num(r.body["usual_s"]) != 300 ||
		num(r.body["waited_s"]) != 1801 {
		t.Fatalf("the wait %+v", r.body)
	}
	// Two samples of the role and class: a task of it has no usual time.
	other := w.ready(t, store.Task{})
	if n := waitsAt(t, w, w.readySince(t, other).Add(1801*time.Second)); n != 1 {
		t.Fatalf("%d recorded for the task of two samples", n)
	}
	if o := waitsRecorded(t, w)[1]; o.body["ref"] != other || o.body["basis"] != "floor" || o.body["usual_s"] != nil {
		t.Fatalf("the task of two samples %+v", o.body)
	}

	// A running attempt of a task with its own usual time below the floor,
	// started at an instant with microseconds (review 475 C2).
	w3 := newKernelWorld(t)
	run := w3.ready(t, store.Task{})
	w3.legacy(t, fmt.Sprintf(`UPDATE agentrt.task SET usual_s = 300 WHERE task_id = '%s'`, run))
	c := w3.claimed(t, run, "h1", codex, true)
	w3.legacy(t, fmt.Sprintf(`UPDATE agentrt.attempt SET started_at = date_trunc('second', started_at) + interval '0.123457 seconds'
	  WHERE attempt_id = '%s'`, c.AttemptID))
	var started time.Time
	ok(t, w3.rt.QueryRow(ctx, `SELECT started_at FROM agentrt.attempt WHERE attempt_id = $1`, c.AttemptID).Scan(&started))
	if started.Nanosecond() != 123457000 {
		t.Fatalf("started at %s: the case needs microseconds", started.Format(time.RFC3339Nano))
	}
	if n := waitsAt(t, w3, started.Add(1800*time.Second).In(east)); n != 0 {
		t.Fatalf("%d recorded for the attempt at exactly its limit", n)
	}
	for i, want := range []int{1, 0} {
		if n := waitsAt(t, w3, started.Add(1801*time.Second).In(east)); n != want {
			t.Fatalf("pass %d recorded %d for the attempt a second past its limit, want %d", i+1, n, want)
		}
	}
	ws := waitsRecorded(t, w3)
	if len(ws) != 1 || ws[0].body["wait"] != store.WaitAttemptRunning || ws[0].body["ref"] != c.AttemptID || ws[0].body["basis"] != "floor" ||
		num(ws[0].body["limit_s"]) != 1800 || num(ws[0].body["usual_s"]) != 300 ||
		ws[0].body["since"] != started.UTC().Format(time.RFC3339Nano) || !strings.HasSuffix(ws[0].body["since"].(string), ".123457Z") {
		t.Fatalf("the running attempt's waits %+v", ws)
	}
}

// Review 474 B4: a check of a thread's work calls that thread, ready and
// running; a pause on the work it checks holds its ready wait; a paused
// mission holds a ready task, a control on a host does not.
func TestAChecksWaitCallsTheWorksProposer(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	p := proposal("impl", w.spec)
	p.BaseCommit = "" // the run pushes commitA: a change only against no base
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-1", []store.TaskProposal{p})
	ok(t, err)
	impl := taskIDs(out)["impl"]
	_, err = store.Dispatch(ctx, w.rt)
	ok(t, err)
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.control(t, "c1", "task", impl, "pause")
	since := w.readySince(t, v)
	if n := waitsAt(t, w, since.Add(31*time.Minute)); n != 0 {
		t.Fatalf("%d recorded while the work it checks is paused", n)
	}
	w.clear(t, "c1")
	if n := waitsAt(t, w, since.Add(31*time.Minute)); n != 1 {
		t.Fatalf("%d recorded once lifted", n)
	}
	if r := waitsRecorded(t, w)[0]; r.body["ref"] != v || r.body["calls"] != "thread:s1" {
		t.Fatalf("the ready check %+v", r.body)
	}
	vc := w.claim(t, "h2", time.Hour, deterministic)
	if vc == nil || vc.TaskID != v {
		t.Fatalf("the check's claim %+v", vc)
	}
	ok(t, store.Start(ctx, w.rt, fence(vc, "h2"), "", ""))
	if n := waitsAt(t, w, time.Now().Add(21*time.Minute)); n != 1 {
		t.Fatalf("%d recorded for the running check", n)
	}
	if r := waitsRecorded(t, w)[1]; r.body["ref"] != vc.AttemptID || r.body["calls"] != "thread:s1" {
		t.Fatalf("the running check %+v", r.body)
	}

	w2 := newKernelWorld(t)
	ready := w2.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w2.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "test") }))
	at := w2.readySince(t, ready).Add(31 * time.Minute)
	if n := waitsAt(t, w2, at); n != 0 {
		t.Fatalf("%d recorded in a paused mission", n)
	}
	ok(t, store.InTx(ctx, w2.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	w2.control(t, "c2", "host", "h1", "pause")
	w2.control(t, "c3", "host", "h2", "drain")
	if n := waitsAt(t, w2, at); n != 1 {
		t.Fatalf("%d recorded with only hosts held, want the ready task", n)
	}
}

// Review 474 B4: a pass records at most twenty of a wait, the oldest first,
// and the next passes the rest: what is on record leaves room for the next.
func TestAPassIsBounded(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var ids []string
	for range 41 {
		ids = append(ids, w.ready(t, store.Task{}))
	}
	at := time.Now().Add(31 * time.Minute)
	for i, want := range []int{20, 20, 1, 0} {
		if n := waitsAt(t, w, at); n != want {
			t.Fatalf("pass %d recorded %d, want %d", i+1, n, want)
		}
	}
	ws := waitsRecorded(t, w)
	for i, r := range ws {
		if r.body["ref"] != ids[i] {
			t.Fatalf("record %d is of %v, want %s: the oldest first", i, r.body["ref"], ids[i])
		}
	}
	// So with mail to a thread.
	w2 := newKernelWorld(t)
	_, err := store.SendMessage(ctx, w2.rt, wr("thread:s1"), "owner", "note", []byte("hello"))
	ok(t, err)
	for i := range 21 {
		_, err = store.SendMessage(ctx, w2.rt, owner, "thread:s1", "note", []byte(fmt.Sprint("note ", i)))
		ok(t, err)
	}
	at = time.Now().Add(11 * time.Minute)
	for i, want := range []int{20, 1, 0} {
		if n := waitsAt(t, w2, at); n != want {
			t.Fatalf("mail pass %d recorded %d, want %d", i+1, n, want)
		}
	}
}

// Review 474 B2, B3: a pass waiting for a row another transaction holds
// gives up within its bound and says so, and the record whose commit failed
// (its references are checked at the commit, which waited for the row) is
// not counted; the next pass records the wait.
func TestAPassThatCannotRecordSaysSo(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	id := w.ready(t, store.Task{})
	c := w.claimed(t, id, "h1", codex, true)
	at := time.Now().Add(31 * time.Minute)
	holder := w.db.Connect(t, w.db.Runtime)
	tx, err := holder.Begin(ctx)
	ok(t, err)
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE`, c.AttemptID)
	ok(t, err)
	start := time.Now()
	n, err := store.WaitLimits(ctx, w.rt, at)
	if n != 0 || err == nil || time.Since(start) > 20*time.Second {
		t.Fatalf("held: %d recorded, %v, after %s", n, err, time.Since(start))
	}
	ok(t, tx.Rollback(ctx))
	if n = waitsAt(t, w, at); n != 1 {
		t.Fatalf("%d recorded once the row was let go", n)
	}
}
