package controller_test

import (
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
	_, err := store.RecordHostOffered(ctx, pool, "h1", tools)
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
