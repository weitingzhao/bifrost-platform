//bifrost:owner coordination

package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	ctx := ctxOf(t)
	var n int
	ok(t, w.rt.QueryRow(ctx, q, args...).Scan(&n))
	return n
}

// footprint is what a write could leave: actors, events, streams and their
// heads, outbox rows, idempotency keys, missions, tasks, dependencies,
// controls, artifacts and their bodies.
func (w *world) footprint(t *testing.T) string {
	t.Helper()
	ctx := ctxOf(t)
	var s string
	ok(t, w.rt.QueryRow(ctx, `SELECT concat_ws(' ',
	  (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event), (SELECT count(*) FROM agentrt.event_stream),
	  (SELECT string_agg(event_stream_id || '=' || last_seq || ':' || last_sha256, ',' ORDER BY event_stream_id) FROM agentrt.event_stream),
	  (SELECT count(*) FROM agentrt.outbox), (SELECT count(*) FROM agentrt.idempotency_key), (SELECT count(*) FROM agentrt.mission),
	  (SELECT count(*) FROM agentrt.control),
	  (SELECT count(*) FROM agentrt.artifact), (SELECT count(*) FROM agentrt.artifact_blob))`).Scan(&s))
	return s
}

// wr is a writer as the routes make one: the actor and the token.
func wr(actor string) store.Writer { return store.Writer{Actor: actor, Token: "operator"} }

// legacy writes rows with the import event, which may write any table: the
// fixture for actors the routes do not create (retired ones, odd kinds).
func (w *world) legacy(t *testing.T, stmts ...string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

// Review 175 B3: thread registrations are bounded per day; threads already
// registered go on writing.
func TestThreadRegistrationBound(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	_, err := proposeForSession(ctx, w.rt, wr("thread:known"), "k1")
	ok(t, err)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
	  SELECT 'thread:bulk' || i, 'thread', 'bulk' FROM generate_series(1, %d) i`, store.ThreadsPerDay-1))
	before := w.footprint(t)
	_, err = proposeForSession(ctx, w.rt, wr("thread:one-more"), "k1")
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "threads were registered") {
		t.Fatalf("a thread past the day's bound: %v", err)
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a refused registration left something: %s -> %s", before, after)
	}
	_, err = proposeForSession(ctx, w.rt, wr("thread:known"), "k2")
	ok(t, err)
}

// Review 176 B1: the day's bound holds for registrations at once: with one
// left, of two new threads at once one is registered.
//
// Serial: it holds the package's test hook (SetHook).
func TestThreadQuotaAtOnce(t *testing.T) {
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
	  SELECT 'thread:bulk' || i, 'thread', 'bulk' FROM generate_series(1, %d) i`, store.ThreadsPerDay-1))
	hold, reached := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	var release sync.Once
	free := func() { release.Do(func() { close(hold) }) }
	defer free()
	defer store.SetHook(func(point, _ string) {
		if point == "counted" && first.CompareAndSwap(false, true) {
			close(reached)
			<-hold
		}
	})()
	got := make(chan error, 2)
	go func() {
		_, perr := proposeForSession(ctx, pool, wr("thread:new1"), "k")
		got <- perr
	}()
	<-reached
	go func() {
		_, perr := proposeForSession(ctx, pool, wr("thread:new2"), "k")
		got <- perr
	}()
	// The second waits for the registration lock, or (without it) is done.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if w.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
		  AND position('742109' in query) > 0`) > 0 || len(got) > 0 {
			break
		}
	}
	free()
	refused := 0
	for range 2 {
		if perr := <-got; errors.Is(perr, store.ErrInvalid) {
			refused++
		} else {
			ok(t, perr)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_kind = 'thread'`); refused != 1 || n != store.ThreadsPerDay {
		t.Fatalf("%d refused, %d threads; want 1 and %d", refused, n, store.ThreadsPerDay)
	}
}

// Review 176 part 2 A1: a full day of registrations does not keep a new
// session from stopping work; it still keeps it from proposing.
func TestControlsPastThreadQuota(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
	  SELECT 'thread:bulk' || i, 'thread', 'bulk' FROM generate_series(1, %d) i`, store.ThreadsPerDay))
	_, err := store.RequestControl(ctx, w.rt, wr("thread:late"), "k1", "global", "", "stop", "stop everything")
	ok(t, err)
	_, err = proposeForSession(ctx, w.rt, wr("thread:later"), "k1")
	is(t, err, store.ErrInvalid)
}

// Retired missions no longer register sessions. Exercise the same actor and
// idempotency guards through the live rule-proposal route (Relay A, 2026-11-15).
func proposeForSession(ctx context.Context, db store.DB, w store.Writer, key string) (map[string]any, error) {
	text := ruleText("tuning", 1, "initial", since, until, "values: {assess_per_pass: 5}\n")
	return store.ProposeRule(ctx, db, w, key, "tuning", []byte(text), "session guard fixture")
}

// Actor validation still applies to live proposals; every refusal is atomic.
func TestProposalWriterValidation(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.legacy(t,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:x', 'seat', 'x'), ('seat:odd', 'thread', 'odd')`,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name, retired_at) VALUES ('thread:old', 'thread', 'old', now()), ('seat:old', 'seat', 'old', now())`)
	before := w.footprint(t)
	for _, actor := range []string{"seat:ghost", "thread:x", "seat:odd", "thread:old", "seat:old", "someone"} {
		if _, err := proposeForSession(ctx, w.rt, wr(actor), "invalid"); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: %v; want invalid", actor, err)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("refused proposals left something: %s -> %s", before, after)
	}
}
