package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 145 B7: the acceptance paths its table found without a test of
// their own.

// notes writes n notes on stream, one transaction each.
func notes(t *testing.T, w *world, stream string, n int) {
	t.Helper()
	for i := range n {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": string(rune('a' + i))}})
			return err
		}))
	}
}

// T23b: on a clean stream of its own, deleting the last event alone is found
// (the head names an event that is gone); so is an envelope field edited, one
// field at a time with the body untouched (review 334 A2), and a NULL envelope
// field given a value or a value taken to NULL.
func TestTamperOnACleanStream(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	admin := w.db.Connect(t, w.db.Admin)
	tamper := func(q string) {
		t.Helper()
		for _, s := range []string{`ALTER TABLE agentrt.event DISABLE TRIGGER USER`, q, `ALTER TABLE agentrt.event ENABLE TRIGGER USER`} {
			_, err := admin.Exec(ctx, s)
			ok(t, err)
		}
	}
	found := func(stream string, seq int64) {
		t.Helper()
		_, err := store.VerifyStream(ctx, w.rt, stream)
		var br *store.ChainBreak
		if !errors.As(err, &br) || (seq != 0 && br.Seq != seq) {
			t.Fatalf("%s: verify = %v, want a break at %d", stream, err, seq)
		}
	}
	notes(t, w, "tail", 3)
	w.chainHolds(t, "tail")
	tamper(`DELETE FROM agentrt.event WHERE event_stream_id = 'tail' AND seq = 3`)
	found("tail", 0)

	for name, q := range map[string]string{
		"by":           `UPDATE agentrt.event SET actor_id = 'owner' WHERE event_stream_id = 'by' AND seq = 2`,
		"type":         `UPDATE agentrt.event SET event_type = 'owner_signoff' WHERE event_stream_id = 'type' AND seq = 2`,
		"at":           `UPDATE agentrt.event SET at = at + interval '1 microsecond' WHERE event_stream_id = 'at' AND seq = 2`,
		"null to set":  `UPDATE agentrt.event SET mission_id = 'M-1' WHERE event_stream_id = 'null to set' AND seq = 2`,
		"set to null":  `UPDATE agentrt.event SET mission_id = NULL WHERE event_stream_id = 'set to null' AND seq = 2`,
		"cause edited": `UPDATE agentrt.event SET cause_event_id = (SELECT min(event_id) FROM agentrt.event) WHERE event_stream_id = 'cause edited' AND seq = 2`,
	} {
		if name == "set to null" {
			for i := range 3 {
				ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
					_, err := store.Append(ctx, tx, store.Event{Stream: name, Type: "note", Actor: "controller", MissionID: "M-1",
						Body: map[string]any{"text": string(rune('a' + i))}})
					return err
				}))
			}
		} else {
			notes(t, w, name, 3)
		}
		w.chainHolds(t, name)
		tamper(q)
		found(name, 2)
	}
}

// T20: the writer's connection is killed while its transaction is open, an
// event appended in it: nothing of it lands, the head does not move, and the
// next write on another connection takes the next seq. And a write that
// commits while a stream is being verified, between its two reads, breaks
// nothing (review 145 B3's read in one snapshot).
func TestKilledInsideATransaction(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	admin := w.db.Connect(t, w.db.Admin)
	notes(t, w, "mid", 2)
	conn := w.db.Connect(t, w.db.Runtime)
	var pid int
	ok(t, conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	tx, err := conn.Begin(ctx)
	ok(t, err)
	if _, err = store.Append(ctx, tx, store.Event{Stream: "mid", Type: "note", Actor: "controller", Body: map[string]any{"text": "lost"}}); err != nil {
		t.Fatal(err)
	}
	var killed bool
	ok(t, admin.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&killed))
	if !killed {
		t.Fatal("the writer's backend was not terminated")
	}
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("a commit on a killed connection succeeded")
	}
	var events, lastSeq int64
	ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'mid'),
	  (SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id = 'mid')`).Scan(&events, &lastSeq))
	if events != 2 || lastSeq != 2 {
		t.Fatalf("after the kill: %d events, head at %d; want 2 and 2", events, lastSeq)
	}
	notes(t, w, "mid", 1)
	if n, verr := store.VerifyStream(ctx, w.rt, "mid"); verr != nil || n != 3 {
		t.Fatalf("after the next write: %d %v", n, verr)
	}

	// A write commits between the verification's two reads, its events and
	// its stream's head, three times: each verification still holds and
	// counts what its snapshot saw; the next one sees the write (review 335
	// A1: a barrier, not a race the writers might win).
	pool := w.db.Pool(t, w.db.Runtime)
	notes(t, w, "busy", 1)
	for round := range 3 {
		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		hooked := hookedDB{db: pool, before: func() {
			ok(t, store.InTx(bctx, pool, func(tx pgx.Tx) error {
				_, aerr := store.Append(bctx, tx, store.Event{Stream: "busy", Type: "note", Actor: "controller", Body: map[string]any{"text": "between"}})
				return aerr
			}))
		}}
		n, verr := store.VerifyStream(bctx, hooked, "busy")
		cancel()
		if verr != nil || n != int64(1+round) {
			t.Fatalf("round %d: a write between the reads: %d events, %v; want %d and no break", round, n, verr, 1+round)
		}
	}
	if n, verr := store.VerifyStream(ctx, pool, "busy"); verr != nil || n != 4 {
		t.Fatalf("after the writes: %d %v; want 4", n, verr)
	}
}

// hookedDB begins transactions whose QueryRow first runs before: in
// VerifyStream, after the events are read and before the stream's head is.
type hookedDB struct {
	db interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	}
	before func()
}

func (h hookedDB) BeginTx(c context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := h.db.BeginTx(c, o)
	if err != nil {
		return nil, err
	}
	return hookedTx{Tx: tx, before: h.before}, nil
}

type hookedTx struct {
	pgx.Tx
	before func()
}

func (h hookedTx) QueryRow(c context.Context, sql string, args ...any) pgx.Row {
	h.before()
	return h.Tx.QueryRow(c, sql, args...)
}

// Host liveness: a poll moves the host's last_poll_at forward, with work to
// hand out or none, and appends no event (v5: liveness is a column, not the
// log).
func TestPollMovesLivenessWithoutAnEvent(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	read := func() (time.Time, int) {
		t.Helper()
		var at *time.Time
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT last_poll_at FROM agentrt.host WHERE host_id = 'h1'), (SELECT count(*) FROM agentrt.event)::int`).Scan(&at, &n))
		if at == nil {
			return time.Time{}, n
		}
		return *at, n
	}
	before, events := read()
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c != nil {
		t.Fatalf("an empty queue handed out %+v", c)
	}
	first, after := read()
	if !first.After(before) || after != events {
		t.Fatalf("a poll: last_poll_at %v -> %v, events %d -> %d", before, first, events, after)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c != nil {
		t.Fatalf("an empty queue handed out %+v", c)
	}
	second, again := read()
	if !second.After(first) || again != events {
		t.Fatalf("a second poll: last_poll_at %v -> %v, events %d -> %d", first, second, events, again)
	}
	// A poll that hands out work moves it too (review 334 B2).
	_ = w.ready(t, store.Task{})
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c == nil {
		t.Fatal("a ready task was not handed out")
	}
	if third, _ := read(); !third.After(second) {
		t.Fatalf("a poll with work: last_poll_at %v -> %v", second, third)
	}
	// A poll from a host that is not registered moves nothing.
	if _, err := store.ClaimFor(ctx, w.rt, store.ClaimRequest{HostID: "nobody", FreeSlots: 1, Tools: []store.Tool{codex}}, store.LeaseTTL); !errors.Is(err, store.ErrFenced) {
		t.Fatalf("an unregistered host: %v", err)
	}
}

// T31, the other way round: every event that signals has exactly one outbox
// row, on its topic, naming what the event names (a readied task its task, a
// message its mailbox and message, a control the stop topic), and no outbox
// row hangs on an event that signals nothing.
func TestEveryOutboxRowHasItsEventAndBack(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := w.ready(t, store.Task{})
	_ = w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", a, "cancelled", "test") }))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.PostMessage(ctx, tx, "owner", store.Seat, "ask", []byte("status?"))
		return err
	}))
	w.control(t, store.NewControlID(), "task", a, "stop")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.SetCooldown(ctx, tx, "controller", "codex:owner:weekly", "codex", "weekly", time.Now().Add(time.Hour), "test")
	}))
	notes(t, w, "quiet", 2)
	// Every kind of signal is in the fixture (review 334 A3).
	for kind, q := range map[string]string{
		"a readied task": `SELECT count(*) FROM agentrt.event WHERE event_type = 'task_state_changed' AND body_json->>'to' = 'ready'`,
		"a message":      `SELECT count(*) FROM agentrt.event WHERE event_type = 'message_posted'`,
		"a control":      `SELECT count(*) FROM agentrt.event WHERE event_type = 'control_set'`,
		"a cooldown":     `SELECT count(*) FROM agentrt.event WHERE event_type = 'pool_cooldown_set'`,
	} {
		if w.count(t, q) == 0 {
			t.Fatalf("the fixture has no %s", kind)
		}
	}
	// Each signalling event: one row, its topic, its subject.
	if n := w.count(t, `SELECT count(*) FROM agentrt.event e WHERE
	    ((e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'ready') OR e.event_type IN ('message_posted', 'control_set', 'pool_cooldown_set'))
	  AND (SELECT count(*) FROM agentrt.outbox o WHERE o.event_id = e.event_id AND
	    CASE e.event_type
	      WHEN 'task_state_changed' THEN o.topic = 'wake' AND o.payload_json->>'task_id' = e.task_id
	      WHEN 'message_posted' THEN o.topic = 'mbox:' || (e.body_json->>'to') AND o.payload_json->>'message_id' = e.body_json->>'message_id'
	      WHEN 'control_set' THEN o.topic = 'stop' AND o.payload_json->>'control_id' = e.body_json->>'control_id'
	      WHEN 'pool_cooldown_set' THEN o.topic = 'cooldown' AND o.payload_json->>'vendor_pool_id' = e.body_json->>'vendor_pool_id'
	        AND o.payload_json->>'until' = e.body_json->>'until'
	    END) <> 1`); n != 0 {
		t.Fatalf("%d signalling events without exactly one matching outbox row", n)
	}
	// Each outbox row: an event that signals, on the topic it signals.
	if n := w.count(t, `SELECT count(*) FROM agentrt.outbox o JOIN agentrt.event e ON e.event_id = o.event_id WHERE NOT (
	    (e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'ready' AND o.topic = 'wake')
	    OR (e.event_type = 'message_posted' AND o.topic = 'mbox:' || (e.body_json->>'to'))
	    OR (e.event_type = 'control_set' AND o.topic = 'stop')
	    OR (e.event_type = 'pool_cooldown_set' AND o.topic = 'cooldown'))`); n != 0 {
		t.Fatalf("%d outbox rows on an event that signals nothing, or on another topic", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.outbox o LEFT JOIN agentrt.event e ON e.event_id = o.event_id WHERE e.event_id IS NULL`); n != 0 {
		t.Fatalf("%d outbox rows without their event", n)
	}
}

// blockedOn waits, within a bound, until the backend pid waits on a lock held
// by holder while running a statement that contains stmt: proof that the
// second connection reached the guarded statement and waits there, not a
// silence taken for it (review 334 A4).
func blockedOn(t *testing.T, w *world, pid, holder int, stmt string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var blocked bool
		var query string
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := w.rt.QueryRow(qctx, `SELECT $2 = ANY(pg_blocking_pids($1)), COALESCE((SELECT query FROM pg_stat_activity WHERE pid = $1), '')`,
			pid, holder).Scan(&blocked, &query)
		cancel()
		ok(t, err)
		if blocked && strings.Contains(query, stmt) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend %d did not wait on %d at %q (it runs %q)", pid, holder, stmt, query)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// T32, the reverse migration: a task on a step moved off the plan while a
// review of it is being made on that step, in both orders: the second waits
// for the first (on the step's or the subject's lock, whichever it meets
// first), then fails, so no review sits apart from its subject (review 144
// A2 covered the move onto a step). The subject's own lock is not isolated
// here (review 334 A4).
func TestSubjectLockSerializesMoveOffAndReview(t *testing.T) {
	t.Parallel()
	for _, moveFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "move_first", false: "review_first"}[moveFirst], func(t *testing.T) {
			w := newWorld(t)
			subj, err := w.task(t, "M-1", s2, "", "implement")
			ok(t, err)
			other := w.db.Connect(t, w.db.Runtime)
			var otherPID int
			ok(t, other.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&otherPID))
			move := func(tx pgx.Tx) error {
				if _, aerr := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
					MissionID: "M-1", TaskID: subj, Body: map[string]any{"change": "off the plan"}}); aerr != nil {
					return aerr
				}
				_, xerr := tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = NULL, off_plan = true, off_plan_reason = 'moved off' WHERE task_id = $1`, subj)
				return xerr
			}
			review := func(tx pgx.Tx) error {
				_, perr := store.ProposeTask(ctx, tx, "controller", store.Task{MissionID: "M-1", PlanNode: s2, Subject: subj, Role: "review",
					Class: "go", Title: "r", SpecArtifact: w.spec, Effect: "none"})
				return perr
			}
			first, second, stmt := move, review, "FROM agentrt.plan_node" // the step's lock, met first
			if !moveFirst {
				first, second, stmt = review, move, "UPDATE agentrt.task"
			}
			conn1 := w.db.Connect(t, w.db.Runtime)
			var firstPID int
			ok(t, conn1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&firstPID))
			tx1, err := conn1.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			ok(t, err)
			defer func() { _ = tx1.Rollback(ctx) }()
			ok(t, first(tx1))
			done := make(chan error, 1)
			go func() { done <- store.InTx(ctx, other, second) }()
			blockedOn(t, w, otherPID, firstPID, stmt)
			ok(t, tx1.Commit(ctx))
			wantErr(t, <-done, "sit")
			if n := w.count(t, `SELECT count(*) FROM agentrt.task t JOIN agentrt.task s ON s.task_id = t.subject_task_id
			  WHERE t.plan_node_id IS DISTINCT FROM s.plan_node_id OR t.off_plan <> s.off_plan`); n != 0 {
				t.Fatalf("%d tasks sit apart from their subject", n)
			}
		})
	}
}

// T32, acceptance against the attempt's result on two connections: an
// accepting verification inserted while the attempt's result is being
// persisted waits for that transaction; it is taken once the result commits,
// and refused, as a result not yet persisted, once it rolls back (the guard
// reads the attempt FOR SHARE).
func TestAcceptanceWaitsForTheResult(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{true, false} {
		t.Run(map[bool]string{true: "persist_commits", false: "persist_rolls_back"}[commit], func(t *testing.T) {
			w := newWorld(t)
			task, err := w.task(t, "M-1", s1, "", "implement")
			ok(t, err)
			var attempt string
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				var lerr error
				attempt, _, lerr = store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:codex", "", 1, time.Minute)
				return lerr
			}))
			conn1 := w.db.Connect(t, w.db.Runtime)
			var persistPID int
			ok(t, conn1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&persistPID))
			persist, err := conn1.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			ok(t, err)
			defer func() { _ = persist.Rollback(ctx) }()
			if _, err = store.Append(ctx, persist, store.Event{Stream: store.MissionStream("M-1"), Type: "attempt_persisted", Actor: "controller",
				TaskID: task, AttemptID: attempt, Body: map[string]any{"pushed_commit": "2333e00753f91c74c8b2f4d693a803f8e89e7892"}}); err != nil {
				t.Fatal(err)
			}
			if _, err = persist.Exec(ctx, `UPDATE agentrt.attempt SET persisted_at = clock_timestamp(), pushed_ref = 'refs/heads/agent/t/a1',
			  pushed_commit = '2333e00753f91c74c8b2f4d693a803f8e89e7892' WHERE attempt_id = $1`, attempt); err != nil {
				t.Fatal(err)
			}
			other := w.db.Connect(t, w.db.Runtime)
			var otherPID int
			ok(t, other.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&otherPID))
			done := make(chan error, 1)
			go func() {
				done <- store.InTx(ctx, other, func(tx pgx.Tx) error {
					ev, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte("ok"))
					if err != nil {
						return err
					}
					_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
					  evidence_artifact_id, accepts_attempt) VALUES ($1,$2,$3,'deterministic','pass',$4,true)`, store.NewVerificationID(), task, attempt, ev)
					return err
				})
			}()
			blockedOn(t, w, otherPID, persistPID, "INSERT INTO agentrt.verification")
			if commit {
				ok(t, persist.Commit(ctx))
				ok(t, <-done)
			} else {
				ok(t, persist.Rollback(ctx))
				wantErr(t, <-done, "before its result is persisted")
			}
			want := map[bool]int{true: 1, false: 0}[commit]
			if n := w.count(t, `SELECT count(*) FROM agentrt.verification WHERE attempt_id = $1 AND accepts_attempt`, attempt); n != want {
				t.Fatalf("%d accepting verifications; want %d", n, want)
			}
		})
	}
}
