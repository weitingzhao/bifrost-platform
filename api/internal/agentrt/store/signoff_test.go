package store_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// waitsOnTheStep: some transaction waits for a plan node's row lock.
func waitsOnTheStep(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var waiting int
	ok(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
	  AND position('agentrt.plan_node' in query) > 0 AND position('FOR UPDATE' in query) > 0`).Scan(&waiting))
	return waiting > 0
}

// Two presses of the Owner's Sign off at once (a double tap, two devices)
// write one sign-off: the second waits for the first and answers it
// (SignOffStep). The first is held after it found the step unsigned; the case
// counts only once the second waits for its lock or has reached the same
// point. It holds the package's test hook:
// Serial: SetHook.
func TestSignOffStepOnceAtATime(t *testing.T) {
	w := newWorld(t)
	w.rule(t, s1, `{"owner_signoff":"x"}`)
	pool := w.db.Pool(t, w.db.Runtime)
	first, release := make(chan struct{}), make(chan struct{})
	var arrivals atomic.Int32
	defer store.SetHook(func(point, arg string) {
		if point == "signoff: unsigned" && arg == s1 && arrivals.Add(1) == 1 {
			close(first)
			<-release
		}
	})()
	type result struct {
		out map[string]any
		err error
	}
	results := make(chan result, 2)
	press := func() {
		out, err := store.SignOffStep(ctx, pool, store.OwnerActor, s1, "")
		results <- result{out, err}
	}
	go press()
	<-first
	go press()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if waitsOnTheStep(t, pool) || arrivals.Load() >= 2 {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("the second press neither waited nor reached the first's point; the case proves nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	fresh := 0
	var ids []any
	for range 2 {
		r := <-results
		ok(t, r.err)
		if r.out["already_signed"] == false {
			fresh++
		}
		ids = append(ids, r.out["event_id"])
	}
	var n int
	ok(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_signoff' AND plan_node_id = $1`, s1).Scan(&n))
	if fresh != 1 || n != 1 || ids[0] != ids[1] {
		t.Fatalf("two presses at once: %d answered as the first, %d events, event ids %v", fresh, n, ids)
	}
}

// Only the Owner signs, whatever the route lets through.
func TestSignOffStepIsTheOwners(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.rule(t, s1, `{"owner_signoff":"x"}`)
	for _, actor := range []string{"thread:night", "seat:chief_of_staff", "controller", ""} {
		if _, err := store.SignOffStep(ctx, w.rt, actor, s1, ""); !errors.Is(err, store.ErrNotOwner) {
			t.Fatalf("%q signed a step off: %v", actor, err)
		}
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_signoff'`).Scan(&n))
	if n != 0 {
		t.Fatalf("%d sign-offs written", n)
	}
}

// A sign-off and a change to its step at once (review 485 A1): a drop, a new
// rule, the controller's rules pass. The sign-off is held after it read the
// step unsigned; the change runs meanwhile. Either the change waits for the
// sign-off, which then lands first, or the sign-off is refused: it is never
// recorded after its step was closed or its rule replaced. The case counts
// only once the change waits on the step's lock or has finished.
// Serial: SetHook.
func TestSignOffStepAndAChangeAtOnce(t *testing.T) {
	// The event a change wrote on the step, the last one.
	last := func(pool *pgxpool.Pool, types ...string) (int64, error) {
		var id int64
		err := pool.QueryRow(ctx, `SELECT COALESCE(max(event_id), 0) FROM agentrt.event WHERE event_type = ANY($1)
		  AND (plan_node_id = $2 OR event_type = 'plan_changed')`, types, s1).Scan(&id)
		return id, err
	}
	for name, c := range map[string]struct {
		rule   string
		before func(t *testing.T, w *world)
		change func(pool *pgxpool.Pool) (int64, error)
	}{
		"a drop": {rule: `{"owner_signoff":"x"}`, change: func(pool *pgxpool.Pool) (int64, error) {
			var id int64
			err := store.InTx(ctx, pool, func(tx pgx.Tx) error {
				ev, err := store.SetStepState(ctx, tx, "controller", root, s1, "dropped", "test")
				id = ev.EventID
				return err
			})
			return id, err
		}},
		// As a plan change does it: the step locked, then the event, then the rule.
		"a new rule": {rule: `{"owner_signoff":"x"}`, change: func(pool *pgxpool.Pool) (int64, error) {
			var id int64
			err := store.InTx(ctx, pool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`, s1); err != nil {
					return err
				}
				ev, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
					Body: map[string]any{"change": "set_done_rule"}})
				if err != nil {
					return err
				}
				id = ev.EventID
				_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = '{"owner_signoff":"y"}'::jsonb WHERE plan_node_id = $1`, s1)
				return err
			})
			return id, err
		}},
		// The rule holds by its manual atom already: the pass moves the step to done.
		"the rules pass": {rule: `{"any":[{"owner_signoff":"x"},{"manual":"m"}]}`, before: func(t *testing.T, w *world) {
			proof := w.artifact(t, "other", "screenshot")
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				return store.RecordManual(ctx, tx, "owner", s1, "m", "checked", []string{proof})
			}))
		}, change: func(pool *pgxpool.Pool) (int64, error) {
			if to, err := store.EvaluateStep(ctx, pool, s1, nil); err != nil || to != "done" {
				return 0, errors.Join(err, errors.New("the pass did not move the step to done: "+to))
			}
			return last(pool, "plan_node_state_changed")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.rule(t, s1, c.rule)
			if c.before != nil {
				c.before(t, w)
			}
			pool := w.db.Pool(t, w.db.Runtime)
			held, release := make(chan struct{}), make(chan struct{})
			var arrivals atomic.Int32
			defer store.SetHook(func(point, arg string) {
				if point == "signoff: unsigned" && arg == s1 && arrivals.Add(1) == 1 {
					close(held)
					<-release
				}
			})()
			type signed struct {
				out map[string]any
				err error
			}
			signedC := make(chan signed, 1)
			go func() {
				out, err := store.SignOffStep(ctx, pool, store.OwnerActor, s1, "x")
				signedC <- signed{out, err}
			}()
			<-held
			type changed struct {
				id  int64
				err error
			}
			changedC := make(chan changed, 1)
			var finished atomic.Bool
			go func() {
				id, err := c.change(pool)
				finished.Store(true)
				changedC <- changed{id, err}
			}()
			deadline := time.Now().Add(10 * time.Second)
			for !waitsOnTheStep(t, pool) && !finished.Load() {
				if time.Now().After(deadline) {
					close(release)
					t.Fatal("the change neither waited on the step nor finished; the case proves nothing")
				}
				time.Sleep(5 * time.Millisecond)
			}
			close(release)
			sg, ch := <-signedC, <-changedC
			ok(t, ch.err)
			if sg.err != nil {
				if !errors.Is(sg.err, store.ErrState) && !errors.Is(sg.err, store.ErrInvalid) {
					t.Fatalf("the sign-off failed otherwise: %v", sg.err)
				}
				return
			}
			if id, _ := sg.out["event_id"].(int64); id == 0 || id > ch.id {
				t.Fatalf("the sign-off (event %v) was recorded after the change (event %d)", sg.out["event_id"], ch.id)
			}
		})
	}
}
