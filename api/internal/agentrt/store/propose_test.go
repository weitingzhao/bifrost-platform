package store_test

import (
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

var mission = store.MissionProposal{Title: "m", Goal: "g", Success: []string{"it works"}}

// Missions: the platform numbers them; a thread is registered once, on its
// first write, with the token that wrote; a retry answers from the key; a
// bad proposal writes nothing at all.
func TestProposeMission(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	first, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", mission, nil)
	ok(t, err)
	if first["mission_id"] != "M-3" || first["state"] != "proposed" || first["waiting"] != store.EnvelopePending {
		t.Fatalf("answer %v", first)
	}
	again, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", mission, nil)
	ok(t, err)
	same(t, again, first)
	other := mission
	other.Goal = "another goal"
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", other, nil)
	is(t, err, store.ErrConflict)
	second, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k2", other, nil)
	ok(t, err)
	if second["mission_id"] != "M-4" {
		t.Fatalf("second mission %v", second)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? 'thread:s1'
	  AND body_json->>'note' LIKE '%token operator'`); n != 1 {
		t.Fatalf("%d registrations of one thread naming its token", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'thread:s1' AND actor_kind = 'thread'
	  AND display_name LIKE '%token operator)'`); n != 1 {
		t.Fatal("the thread is not an actor of kind thread with its token")
	}
	var by, scope, success, budget, tenant string
	ok(t, w.rt.QueryRow(ctx, `SELECT created_by_actor_id, scope_json::text, success_json::text, budget_json::text, tenant
	  FROM agentrt.mission WHERE mission_id = 'M-3'`).Scan(&by, &scope, &success, &budget, &tenant))
	if by != "thread:s1" || scope != `{"max_action": "branch"}` || success != `{"criteria": ["it works"]}` || budget != "{}" || tenant != "dev" {
		t.Fatalf("row: %s %s %s %s %s", by, scope, success, budget, tenant)
	}
	full := mission
	full.Repos, full.MaxAction, full.MaxAttempts, full.MaxWallHours, full.Tenant = []string{"bifrost-platform"}, "none", 40, 48, "business"
	full.Deadline = time.Now().Add(time.Hour)
	out, err := store.ProposeMission(ctx, w.rt, wr("owner"), "k3", full, nil)
	ok(t, err)
	ok(t, w.rt.QueryRow(ctx, `SELECT created_by_actor_id, scope_json::text, budget_json::text, tenant FROM agentrt.mission
	  WHERE mission_id = $1`, out["mission_id"]).Scan(&by, &scope, &budget, &tenant))
	if by != "owner" || scope != `{"repos": ["bifrost-platform"], "max_action": "none"}` || budget != `{"max_attempts": 40, "max_wall_hours": 48}` || tenant != "business" {
		t.Fatalf("row: %s %s %s %s", by, scope, budget, tenant)
	}

	// Actors the routes must refuse: a name of no kind, a thread name
	// registered as something else, a seat that is not one, retired ones.
	w.legacy(t,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:x', 'seat', 'x'), ('seat:odd', 'thread', 'odd')`,
		`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name, retired_at) VALUES ('thread:old', 'thread', 'old', now()),
		   ('seat:old', 'seat', 'old', now())`)
	before := w.footprint(t)
	for name, c := range map[string]struct {
		actor, key string
		edit       func(*store.MissionProposal)
	}{
		"no key":          {"thread:s1", "", func(*store.MissionProposal) {}},
		"no title":        {"thread:s1", "b1", func(p *store.MissionProposal) { p.Title = "" }},
		"no goal":         {"thread:s1", "b2", func(p *store.MissionProposal) { p.Goal = "" }},
		"no success":      {"thread:s1", "b3", func(p *store.MissionProposal) { p.Success = nil }},
		"empty criterion": {"thread:s1", "b4", func(p *store.MissionProposal) { p.Success = []string{"a", ""} }},
		"tenant":          {"thread:s1", "b5", func(p *store.MissionProposal) { p.Tenant = "prod" }},
		"external":        {"thread:s1", "b6", func(p *store.MissionProposal) { p.MaxAction = "external" }},
		"past deadline":   {"thread:new", "b7", func(p *store.MissionProposal) { p.Deadline = time.Now().Add(-time.Minute) }},
		"negative budget": {"thread:s1", "b8", func(p *store.MissionProposal) { p.MaxAttempts = -1 }},
		"empty repo":      {"thread:s1", "b9", func(p *store.MissionProposal) { p.Repos = []string{""} }},
		"unknown seat":    {"seat:ghost", "b10", func(*store.MissionProposal) {}},
		"thread a seat":   {"thread:x", "b11", func(*store.MissionProposal) {}},
		"seat a thread":   {"seat:odd", "b12", func(*store.MissionProposal) {}},
		"retired thread":  {"thread:old", "b13", func(*store.MissionProposal) {}},
		"retired seat":    {"seat:old", "b14", func(*store.MissionProposal) {}},
		"no kind":         {"someone", "b15", func(*store.MissionProposal) {}},
	} {
		p := mission
		c.edit(&p)
		if _, err := store.ProposeMission(ctx, w.rt, wr(c.actor), c.key, p, nil); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("refused proposals left something: %s -> %s", before, after)
	}
	w.chainHolds(t, "system")
}

// Review 175 A4: a proposal made before its deadline, retried after it, gets
// its first answer; the same key with another deadline is a conflict; a new
// proposal past its deadline is refused and writes nothing.
func TestMissionDeadlineRetry(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	p := mission
	// Seconds, not milliseconds: the first proposal must be checked before
	// the deadline whatever the tests beside this one do (review 322 A1).
	p.Deadline = time.Now().Add(3 * time.Second)
	first, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", p, nil)
	ok(t, err)
	time.Sleep(time.Until(p.Deadline) + 100*time.Millisecond)
	again, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", p, nil)
	ok(t, err)
	same(t, again, first)
	moved := p
	moved.Deadline = time.Now().Add(time.Hour)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k1", moved, nil)
	is(t, err, store.ErrConflict)
	before := w.footprint(t)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s2"), "k2", p, nil)
	is(t, err, store.ErrInvalid)
	if after := w.footprint(t); after != before {
		t.Fatalf("a proposal past its deadline left something: %s -> %s", before, after)
	}
}

// Review 175 B3: thread registrations are bounded per day; threads already
// registered go on writing.
func TestThreadRegistrationBound(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	_, err := store.ProposeMission(ctx, w.rt, wr("thread:known"), "k1", mission, nil)
	ok(t, err)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
	  SELECT 'thread:bulk' || i, 'thread', 'bulk' FROM generate_series(1, %d) i`, store.ThreadsPerDay-1))
	before := w.footprint(t)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:one-more"), "k1", mission, nil)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "threads were registered") {
		t.Fatalf("a thread past the day's bound: %v", err)
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a refused registration left something: %s -> %s", before, after)
	}
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:known"), "k2", mission, nil)
	ok(t, err)
}

// Two proposals at once (threads already registered, so nothing else orders
// them): the second runs while the first holds its number; with the number
// lock it waits, and the two get two numbers (without it the two would take
// the same number and one would fail).
//
// Serial: it holds the package's test hook (SetHook).
func TestMissionNumberLock(t *testing.T) {
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	for _, th := range []string{"thread:a", "thread:b"} {
		_, err := store.ProposeMission(ctx, pool, wr(th), "first", mission, nil)
		ok(t, err)
	}
	hold, reached := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	var release sync.Once
	free := func() { release.Do(func() { close(hold) }) }
	defer free()
	defer store.SetHook(func(point, _ string) {
		if point == "numbered" && first.CompareAndSwap(false, true) {
			close(reached)
			<-hold
		}
	})()
	got := make(chan any, 2)
	propose := func(th string) {
		out, err := store.ProposeMission(ctx, pool, wr(th), "second", mission, nil)
		if err != nil {
			got <- err
			return
		}
		got <- out["mission_id"]
	}
	go propose("thread:a")
	<-reached
	go propose("thread:b")
	// Until the second waits on the number lock, or (without it) is done.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if w.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
		  AND position('742108' in query) > 0`) > 0 || len(got) > 0 {
			break
		}
	}
	free()
	a, b := <-got, <-got
	for _, r := range []any{a, b} {
		if err, isErr := r.(error); isErr {
			t.Fatalf("a proposal at the same time failed: %v", err)
		}
	}
	if a == b {
		t.Fatalf("two proposals took one number: %v", a)
	}
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
		_, perr := store.ProposeMission(ctx, pool, wr("thread:new1"), "k", mission, nil)
		got <- perr
	}()
	<-reached
	go func() {
		_, perr := store.ProposeMission(ctx, pool, wr("thread:new2"), "k", mission, nil)
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
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:later"), "k1", mission, nil)
	is(t, err, store.ErrInvalid)
}
