package controller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// chiefOnly makes exec:claude (anthropic, any model) the one candidate of the
// chief of staff's job, as job:chief_of_staff v2 has it.
func chiefOnly(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: the chief of staff's job"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('chief_of_staff', 'dev', 'job:test', 1)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, model_pattern,
		  status, rule_id, rule_version) VALUES ('chief_of_staff/exec:claude', 'chief_of_staff', 'exec:claude', 'anthropic', '*', 'active', 'job:test', 1)`)
		return err
	}))
}

// pollAs is a claim request of h1 offering claude, at the database's clock.
func pollAs(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	tools := []store.Tool{{Executor: "exec:claude", Provider: "anthropic", Model: "claude-opus-5-5"}}
	_, err := store.RecordHostOffered(ctx, pool, "h1", tools, false)
	must(t, err)
	_, err = store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 0, Tools: tools})
	must(t, err)
	var at time.Time
	must(t, pool.QueryRow(ctx, `SELECT last_poll_at FROM agentrt.host WHERE host_id = 'h1'`).Scan(&at))
	return at
}

func ownerNotes(t *testing.T, pool *pgxpool.Pool) []store.Message {
	t.Helper()
	all, err := store.Thread(ctx, pool, "owner", store.Seat, 100)
	must(t, err)
	var out []store.Message
	for _, m := range all {
		if m.From == store.Seat && strings.HasPrefix(m.Body, "From the platform, not the chief of staff") {
			out = append(out, m)
		}
	}
	return out
}

// W-31 3.5's budget, on the clock: the only host that runs the chief of
// staff stops; however the presence loop's ticks fall against its last poll,
// the controller records it and tells the Owner (in his conversation with
// the seat, which his Chief page reads every 5 seconds) no later than 35
// seconds after that poll, never at 30 or before, and names the chief of
// staff's job as left with no executor. A controller started afresh (a
// restart) says nothing it said; the host's polling again is told at the
// next tick.
func TestPresenceIsToldWithinTheBudget(t *testing.T) {
	if budget := store.HostLostAfter + controller.PresenceEvery; budget > 35*time.Second {
		t.Fatalf("a host is told up to %v after it stops; the cut's budget is 35 s (40 s with the Console's 5-second read)", budget)
	}
	pool := setup(t)
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	chiefOnly(t, pool)
	phases := []time.Duration{0, 1300 * time.Millisecond, 2500 * time.Millisecond, 4999 * time.Millisecond}
	for i, phase := range phases {
		last := pollAs(t, pool)
		now := last.Add(phase)
		c := &controller.Controller{DB: pool, Now: func() time.Time { return now }}
		var told time.Duration
		for ; now.Sub(last) < 2*time.Minute; now = now.Add(controller.PresenceEvery) {
			n, err := c.Presence(ctx)
			must(t, err)
			if n > 0 {
				told = now.Sub(last)
				if n != 2 {
					t.Fatalf("phase %v: %d turns; want h1 and exec:claude", phase, n)
				}
				break
			}
		}
		if told <= store.HostLostAfter || told > 35*time.Second {
			t.Fatalf("phase %v: told %v after the last poll", phase, told)
		}
		notes := ownerNotes(t, pool)
		if len(notes) != 2*i+1 || !strings.Contains(notes[len(notes)-1].Body, "Host h1 is offline") ||
			!strings.Contains(notes[len(notes)-1].Body, "chief_of_staff (so the chief of staff cannot answer you until one can)") {
			t.Fatalf("phase %v: notes %+v", phase, notes)
		}
		restarted := &controller.Controller{DB: pool, Now: func() time.Time { return now.Add(controller.PresenceEvery) }}
		if n, err := restarted.Presence(ctx); err != nil || n != 0 {
			t.Fatalf("phase %v: a restarted controller recorded %d (%v)", phase, n, err)
		}
		back := pollAs(t, pool)
		now = back.Add(phase)
		if n, err := c.Presence(ctx); err != nil || n != 2 {
			t.Fatalf("phase %v: %d turns at the first tick after h1 polls again (%v)", phase, n, err)
		}
		if notes = ownerNotes(t, pool); len(notes) != 2*i+2 || !strings.Contains(notes[len(notes)-1].Body, "Host h1 is back online") {
			t.Fatalf("phase %v: notes %+v", phase, notes)
		}
	}
}

// Review 526 A1b, on the controller's clock: the platform was released (its
// workers down a minute) and the controller started at started; h1 last
// polled a minute before that. Until 30 seconds after the start nothing is
// told, though h1 has been silent 90; past them, h1 is.
func TestPresenceCountsFromTheControllersStart(t *testing.T) {
	pool := setup(t)
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	chiefOnly(t, pool)
	last := pollAs(t, pool)
	started := last.Add(time.Minute)
	now := started
	c := &controller.Controller{DB: pool, Now: func() time.Time { return now }, Started: started}
	for ; !now.After(started.Add(store.HostLostAfter)); now = now.Add(controller.PresenceEvery) {
		if n, err := c.Presence(ctx); err != nil || n != 0 {
			t.Fatalf("%v after the controller started: %d turns (%v)", now.Sub(started), n, err)
		}
	}
	if n, err := c.Presence(ctx); err != nil || n != 2 || now.Sub(started) > 35*time.Second {
		t.Fatalf("%v after the controller started: %d turns (%v)", now.Sub(started), n, err)
	}
}

// pollAt records host's claim offering claude, its last poll set to at.
func pollAt(t *testing.T, pool *pgxpool.Pool, host string, at time.Time) {
	t.Helper()
	tools := []store.Tool{{Executor: "exec:claude", Provider: "anthropic", Model: "claude-opus-5-5"}}
	_, err := store.RecordHostOffered(ctx, pool, host, tools, false)
	must(t, err)
	_, err = pool.Exec(ctx, `UPDATE agentrt.host SET last_poll_at = $2 WHERE host_id = $1`, host, at)
	must(t, err)
}

// ticks runs c's presence pass every PresenceEvery from from to to, the clock
// at each tick; it returns how many turns were recorded and at which tick
// the first was (zero for none).
func ticks(t *testing.T, c *controller.Controller, now *time.Time, from, to time.Time) (int, time.Time) {
	t.Helper()
	total, first := 0, time.Time{}
	for *now = from; !now.After(to); *now = now.Add(controller.PresenceEvery) {
		n, err := c.Presence(ctx)
		must(t, err)
		if n > 0 && first.IsZero() {
			first = *now
		}
		total += n
	}
	return total, first
}

// Review 528 A1b, on the clock: the controller asks every pass whether the
// platform takes agentd's calls (its probe of the API). h1 and h2 offer the
// chief's executor; h1 then stays silent, h2 keeps polling once it can.
//
//   - The API is killed (no record of it) or restarted alone: nothing is told
//     while it is down, nor until 30 s after it answers again; h1, silent throughout, is told
//     then, within 35 s; h2, polling within those 30 s, never.
//   - A long outage (15 minutes): still nothing told; h3, silent through it,
//     is told 30 to 35 s after the platform answers again, not before.
//   - The API answering all along (two instances, one gone, the Service
//     sending the probe to the other): a host going silent is told on time.
//   - The workers restarted alone: a host silent since just before the new
//     controller's start is told 30 to 35 s after it; one already told is
//     not told again.
func TestPresenceWaitsForThePlatform(t *testing.T) {
	pool := setup(t)
	for _, h := range []string{"h1", "h2", "h3"} {
		must(t, store.RegisterHost(ctx, pool, "owner", h, "dev"))
	}
	chiefOnly(t, pool)
	var t0 time.Time
	must(t, pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t0))
	for _, h := range []string{"h1", "h2", "h3"} {
		pollAt(t, pool, h, t0)
	}
	now, up := t0, true
	c := &controller.Controller{DB: pool, Now: func() time.Time { return now }, Started: t0,
		PlatformUp: func(context.Context) bool { return up }}
	if n, _ := ticks(t, c, &now, t0, t0.Add(5*time.Second)); n != 0 {
		t.Fatalf("%d turns with every host polling", n)
	}
	// The API is killed at +6 s and answers again at +72 s.
	up = false
	if n, _ := ticks(t, c, &now, t0.Add(10*time.Second), t0.Add(70*time.Second)); n != 0 {
		t.Fatalf("%d turns while the API was down", n)
	}
	up = true
	back := t0.Add(75 * time.Second)
	pollAt(t, pool, "h2", back.Add(10*time.Second))
	pollAt(t, pool, "h3", back.Add(25*time.Second))
	if n, _ := ticks(t, c, &now, back, back.Add(30*time.Second)); n != 0 {
		t.Fatalf("%d turns within 30 s of the API answering again", n)
	}
	n, first := ticks(t, c, &now, back.Add(35*time.Second), back.Add(35*time.Second))
	if n != 1 || first.Sub(back) > 35*time.Second {
		t.Fatalf("%d turns at %v after the API answered again; want h1 within 35 s", n, first.Sub(back))
	}
	// A long outage: h3 last polled just before it, h2 polls within 30 s of
	// its end.
	outage := back.Add(time.Minute)
	pollAt(t, pool, "h2", outage.Add(-time.Second))
	pollAt(t, pool, "h3", outage.Add(-time.Second))
	up = false
	if n, _ := ticks(t, c, &now, outage, outage.Add(15*time.Minute)); n != 0 {
		t.Fatalf("%d turns in a 15-minute outage", n)
	}
	up = true
	back = outage.Add(15*time.Minute + 5*time.Second)
	pollAt(t, pool, "h2", back.Add(20*time.Second))
	if n, _ := ticks(t, c, &now, back, back.Add(30*time.Second)); n != 0 {
		t.Fatalf("%d turns within 30 s of the end of a long outage", n)
	}
	if n, first = ticks(t, c, &now, back.Add(35*time.Second), back.Add(35*time.Second)); n != 1 || first.Sub(back) > 35*time.Second {
		t.Fatalf("%d turns after a long outage; want h3 within 35 s", n)
	}
	// The API answering all along: h2 goes silent and is told on time.
	silent := back.Add(40 * time.Second)
	pollAt(t, pool, "h2", silent)
	if n, first = ticks(t, c, &now, silent.Add(5*time.Second), silent.Add(time.Minute)); n != 2 ||
		first.Sub(silent) <= store.HostLostAfter || first.Sub(silent) > 35*time.Second {
		t.Fatalf("%d turns, the first %v after h2 went silent; want h2 and the chief's executor within 35 s", n, first.Sub(silent))
	}
	// The workers restarted alone: a new controller, the API up all along.
	restart := silent.Add(5 * time.Minute)
	for _, h := range []string{"h1", "h2", "h3"} {
		pollAt(t, pool, h, restart.Add(-30*time.Second))
	}
	if n, _ = ticks(t, c, &now, restart.Add(-25*time.Second), restart.Add(-25*time.Second)); n != 4 {
		t.Fatalf("%d turns with every host back; want h1, h2, h3 and the executor", n)
	}
	pollAt(t, pool, "h1", restart.Add(-20*time.Second))
	pollAt(t, pool, "h2", restart.Add(10*time.Second))
	pollAt(t, pool, "h3", restart.Add(10*time.Second))
	again := &controller.Controller{DB: pool, Now: func() time.Time { return now }, Started: restart,
		PlatformUp: func(context.Context) bool { return true }}
	if n, _ := ticks(t, again, &now, restart, restart.Add(30*time.Second)); n != 0 {
		t.Fatalf("%d turns within 30 s of the controller's start", n)
	}
	if n, first = ticks(t, again, &now, restart.Add(35*time.Second), restart.Add(35*time.Second)); n != 1 {
		t.Fatalf("%d turns 35 s after the controller's start; want h1", n)
	}
	pollAt(t, pool, "h2", restart.Add(40*time.Second))
	pollAt(t, pool, "h3", restart.Add(40*time.Second))
	if n, _ = ticks(t, again, &now, restart.Add(40*time.Second), restart.Add(time.Minute)); n != 0 {
		t.Fatalf("told again: %d", n)
	}
}
