package store_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Two presses of the Owner's Sign off at once (a double tap, two devices)
// write one sign-off: the second waits for the first and answers it
// (SignOffStep). The first is held after it found the step unsigned; the case
// counts only once the second waits for its lock or has reached the same
// point. Serial: it holds the package's test hook (SetHook).
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
		var waiting int
		ok(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
		  AND wait_event = 'advisory'`).Scan(&waiting))
		if waiting > 0 || arrivals.Load() >= 2 {
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
