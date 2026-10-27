package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock via acceptedDrill. The drill lists only tasks it
// could claim: one accepted, or of a mission created, before the landing was
// enabled, or whose mission is not active or carries no_merge, would be
// refused after a Git read inside a held transaction (2026-10-27 STG: twelve
// pre-enable tasks held one of the pool's two connections all the time and
// stalled the other loops). Each case moves one fact past one predicate, with
// the superuser as the tamper hand, so each predicate is pinned on its own
// (review 641).
func TestLandingCandidatesLeaveOutWhatOnlyTheDatabaseRefuses(t *testing.T) {
	type facts struct {
		w        *world
		task     string
		mission  string
		accepted time.Time
		admin    *pgx.Conn
	}
	setup := func(t *testing.T) facts {
		t.Helper()
		w, task, _ := acceptedDrill(t)
		f := facts{w: w, task: task, admin: w.db.Connect(t, w.db.Admin)}
		ok(t, w.rt.QueryRow(ctx, `SELECT t.mission_id, (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id=t.task_id
		  AND e.event_type='task_state_changed' AND e.body_json->>'to'='accepted') FROM agentrt.task t WHERE t.task_id=$1`, task).Scan(&f.mission, &f.accepted))
		return f
	}
	list := func(t *testing.T, f facts, enabledAt time.Time) []string {
		t.Helper()
		tasks, e := store.LandingCandidates(ctx, f.w.rt, enabledAt)
		ok(t, e)
		return tasks
	}
	// tamper writes the mission row past the kernel's triggers (superuser,
	// replica role for this transaction only).
	tamper := func(t *testing.T, f facts, q string, args ...any) {
		t.Helper()
		tx, e := f.admin.Begin(ctx)
		ok(t, e)
		defer func() { _ = tx.Rollback(ctx) }()
		_, e = tx.Exec(ctx, `SET LOCAL session_replication_role = replica`)
		ok(t, e)
		_, e = tx.Exec(ctx, q, args...)
		ok(t, e)
		ok(t, tx.Commit(ctx))
	}
	created := func(t *testing.T, f facts, at time.Time) {
		t.Helper()
		tamper(t, f, `UPDATE agentrt.mission SET created_at=$2 WHERE mission_id=$1`, f.mission, at)
	}
	listed := func(t *testing.T, got []string, task string, want bool) {
		t.Helper()
		if want && (len(got) != 1 || got[0] != task) || !want && len(got) != 0 {
			t.Fatalf("listed %v, want task listed=%v", got, want)
		}
	}

	t.Run("eligible, enable time equal to acceptance and creation", func(t *testing.T) {
		f := setup(t)
		listed(t, list(t, f, landingState(t, f.w, landingRepoStream).EnabledAt), f.task, true)
		created(t, f, f.accepted)
		listed(t, list(t, f, f.accepted), f.task, true)
	})
	t.Run("no enable time", func(t *testing.T) {
		f := setup(t)
		listed(t, list(t, f, time.Time{}), f.task, false)
	})
	t.Run("mission created before enable, accepted after", func(t *testing.T) {
		f := setup(t)
		created(t, f, f.accepted.Add(-time.Microsecond))
		listed(t, list(t, f, f.accepted), f.task, false)
	})
	t.Run("mission created after enable, accepted before", func(t *testing.T) {
		f := setup(t)
		created(t, f, f.accepted.Add(time.Second))
		listed(t, list(t, f, f.accepted.Add(time.Microsecond)), f.task, false)
		listed(t, list(t, f, f.accepted), f.task, true)
	})
	t.Run("no_merge", func(t *testing.T) {
		f := setup(t)
		enabledAt := landingState(t, f.w, landingRepoStream).EnabledAt
		for _, c := range []struct {
			scope string
			want  bool
		}{{`{"no_merge":""}`, true}, {`{"no_merge":null}`, true}, {`{"no_merge":"drill"}`, false}, {`{"no_merge":false}`, false}} {
			tamper(t, f, `UPDATE agentrt.mission SET scope_json=(scope_json-'no_merge')||$2::jsonb WHERE mission_id=$1`, f.mission, c.scope)
			got := list(t, f, enabledAt)
			if c.want != (len(got) == 1) {
				t.Fatalf("%s: listed %v", c.scope, got)
			}
		}
	})
	t.Run("mission not active", func(t *testing.T) {
		f := setup(t)
		ok(t, store.InTx(ctx, f.w.rt, func(tx pgx.Tx) error {
			return store.MoveMission(ctx, tx, "controller", f.mission, "stopped", "test")
		}))
		listed(t, list(t, f, landingState(t, f.w, landingRepoStream).EnabledAt), f.task, false)
	})
}
