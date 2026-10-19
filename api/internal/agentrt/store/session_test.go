package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// checkWriter is the check whoami and every write make, without writing.
func checkWriter(w *world, wr store.Writer) error {
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CheckWriter(ctx, tx, wr)
		return err
	})
}

// TD-370: a thread's session is the token's that first wrote as it. Another
// token naming the session is refused on every write and on a read or an
// acknowledgement of its mail, with nothing written; the token that
// registered it goes on; the runtime's own relayed write is not held; the
// registration names the token in a field of its own.
func TestSessionHeldToItsToken(t *testing.T) {
	w := newKernelWorld(t)
	first := store.Writer{Actor: "thread:s", Token: "first"}
	second := store.Writer{Actor: "thread:s", Token: "second"}
	_, err := store.ProposeMission(ctx, w.rt, first, "k1", mission, nil)
	ok(t, err)
	_, _, err = store.UploadArtifact(ctx, w.rt, first, "task_spec", "text/plain", []byte("a spec"))
	ok(t, err)
	var msg string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var perr error
		msg, perr = store.PostMessage(ctx, tx, "owner", first.Actor, "note", []byte("for s"))
		return perr
	}))
	a := &asker{}
	before := w.footprint(t)
	for name, write := range map[string]func() error{
		"a mission": func() error { _, e := store.ProposeMission(ctx, w.rt, second, "k2", mission, nil); return e },
		"tasks": func() error {
			_, e := store.ProposeTasks(ctx, w.rt, second, "k3", "M-1", []store.TaskProposal{proposal("a", w.spec)})
			return e
		},
		"a plan change": func() error {
			_, e := store.ProposePlanChange(ctx, w.rt, second, "k4", root, "rename", []store.PlanChange{{Op: "retitle", Node: item, Title: "x"}}, a.ask)
			return e
		},
		"a control": func() error {
			_, e := store.RequestControl(ctx, w.rt, second, "k5", "global", "", "pause", "test")
			return e
		},
		"a message": func() error {
			_, e := store.SendMessage(ctx, w.rt, second, "owner", "note", []byte("hi"))
			return e
		},
		"an acknowledgement": func() error { _, e := store.AckMessages(ctx, w.rt, second, []string{msg}); return e },
		"a read of its mail": func() error { _, e := store.Unread(ctx, w.rt, second, 10); return e },
		"a new upload": func() error {
			_, _, e := store.UploadArtifact(ctx, w.rt, second, "task_spec", "text/plain", []byte("another spec"))
			return e
		},
		"an upload it has": func() error {
			_, _, e := store.UploadArtifact(ctx, w.rt, second, "task_spec", "text/plain", []byte("a spec"))
			return e
		},
		"a replay of its key": func() error { _, e := store.ProposeMission(ctx, w.rt, second, "k1", mission, nil); return e },
		"whoami's check":      func() error { return checkWriter(w, second) },
	} {
		if e := write(); !errors.Is(e, store.ErrNotYours) {
			t.Errorf("%s by another token: %v; want ErrNotYours", name, e)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a refused token left something: %s -> %s", before, after)
	}
	if len(a.cards) != 0 {
		t.Fatalf("a refused plan change asked the Owner")
	}
	ms, err := store.Unread(ctx, w.rt, first, 10)
	ok(t, err)
	if len(ms) != 1 || ms[0].ID != msg {
		t.Fatalf("the thread's own mail: %+v", ms)
	}
	done, err := store.AckMessages(ctx, w.rt, first, []string{msg})
	ok(t, err)
	if len(done) != 1 {
		t.Fatalf("acknowledged %v", done)
	}
	ok(t, checkWriter(w, first))
	ok(t, checkWriter(w, store.Writer{Actor: first.Actor, Token: "message:m_1", Relayed: true}))
	// A thread not registered yet holds nothing; neither does the Owner.
	ok(t, checkWriter(w, store.Writer{Actor: "thread:new", Token: "second"}))
	ok(t, checkWriter(w, store.Writer{Actor: "owner", Token: "anyone"}))
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? 'thread:s'
	  AND body_json->>'token' = 'first'`); n != 1 {
		t.Fatalf("the registration names its token in %d events; want 1", n)
	}
}

// A registration made before the token had a field of its own is held to
// the token its note names; a thread whose registration names none is held
// to none, so no token may act as it.
func TestSessionOfAnOlderRegistration(t *testing.T) {
	w := newKernelWorld(t)
	register := func(actor string, body map[string]any) {
		t.Helper()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller", Body: body}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'thread', 'old')`, actor)
			return err
		}))
	}
	register("thread:old", map[string]any{"actors": []string{"thread:old"}, "note": "first write of a thread, by token first"})
	register("thread:bare", map[string]any{"actors": []string{"thread:bare"}, "note": "imported"})
	ok(t, checkWriter(w, store.Writer{Actor: "thread:old", Token: "first"}))
	_, err := store.ProposeMission(ctx, w.rt, store.Writer{Actor: "thread:old", Token: "first"}, "k1", mission, nil)
	ok(t, err)
	_, err = store.ProposeMission(ctx, w.rt, store.Writer{Actor: "thread:old", Token: "second"}, "k2", mission, nil)
	is(t, err, store.ErrNotYours)
	_, err = store.ProposeMission(ctx, w.rt, store.Writer{Actor: "thread:bare", Token: "first"}, "k1", mission, nil)
	is(t, err, store.ErrNotYours)
	w.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:legacy', 'thread', 'no event')`)
	is(t, checkWriter(w, store.Writer{Actor: "thread:legacy", Token: "first"}), store.ErrNotYours)
}

// Two tokens writing first as one new session at once: one registers it and
// the other is refused, whichever comes first (the check is the write's own,
// under the writer's lock).
func TestSessionFirstWriteAtOnce(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	for round := range 10 {
		actor := "thread:race" + string(rune('a'+round))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, token := range []string{"first", "second"} {
			wg.Add(1)
			go func(i int, token string) {
				defer wg.Done()
				_, errs[i] = store.ProposeMission(context.Background(), pool, store.Writer{Actor: actor, Token: token}, "k", mission, nil)
			}(i, token)
		}
		wg.Wait()
		if (errs[0] == nil) == (errs[1] == nil) {
			t.Fatalf("round %d: %v, %v; want one to register and the other refused", round, errs[0], errs[1])
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, store.ErrNotYours) {
				t.Fatalf("round %d: %v; want ErrNotYours", round, err)
			}
		}
	}
}
