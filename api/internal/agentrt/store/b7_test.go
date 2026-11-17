package store_test

import (
	"context"
	"errors"
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

// Relay A, 2026-11-15: messages and controls remain durable without producing
// unread Redis signals. (The outbox they once wrote is retired, 0028.)
func TestMessagesAndControlsNeedNoRedisOutbox(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var message string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		message, err = store.PostMessage(ctx, tx, "owner", store.Seat, "ask", []byte("status?"))
		return err
	}))
	control := store.NewControlID()
	w.control(t, control, "mission", "M-1", "stop")
	if n := w.count(t, `SELECT count(*) FROM agentrt.message m JOIN agentrt.event e ON e.event_id=m.event_id WHERE m.message_id=$1 AND e.event_type='message_posted'`, message); n != 1 {
		t.Fatalf("message/event pairs: %d", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.control WHERE control_id=$1 AND cleared_at IS NULL`, control); n != 1 {
		t.Fatalf("active controls: %d", n)
	}
}
