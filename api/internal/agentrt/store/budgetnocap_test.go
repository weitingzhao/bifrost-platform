package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// autoGrantRule signs the dev tenant's budget rule again (version 2) with
// the given auto_grant block, "" for none.
func autoGrantRule(t *testing.T, w *world, autoGrant string) {
	t.Helper()
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, ruleText("budget:dev", 2, rules.Loosen, since, until,
		"tenant: dev\nmission: {max_attempts: 1000, max_wall_hours: 720}\nattempt_hard_limit_s: 3600\n"+autoGrant))))
}

// spentGoal is goal mission id with a budget of max attempts, all used by a
// cancelled task of its own, and what was already added on its own.
func spentGoal(t *testing.T, w *world, id string, max, auto int) {
	t.Helper()
	w.goalMission(t, id)
	spent := w.ready(t, store.Task{MissionID: id, OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		for i := 1; i <= max; i++ {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
			    lease_expires_at, heartbeat_at, state, conclusion) VALUES ($1, $2, $3, 'controller', 1, now(), now(), 'failed', 'failed')`,
				store.NewAttemptID(), spent, i); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = 'cancelled' WHERE task_id = $1`, spent); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET budget_json = $2::jsonb WHERE mission_id = $1`, id,
			fmt.Sprintf(`{"max_attempts": %d, "max_wall_hours": 48, "auto_granted_attempts": %d}`, max, auto))
		return err
	}))
}

// M-308 (2026-11-06): a mission that has used its attempts and is past its
// hours leases its ready tasks, whatever auto_grant its budget rule states
// (the runtime grows no budget any more), and its budget stays as it was:
// no mission_amended, no card.
func TestBudgetPastAttemptsAndHoursLeases(t *testing.T) {
	t.Parallel()
	for name, autoGrant := range map[string]string{"auto_grant 0": "", "auto_grant 6": "auto_grant: {max_attempts: 6, max_wall_hours: 0}\n",
		"auto_grant reached": "auto_grant: {max_attempts: 6, max_wall_hours: 0}\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			if autoGrant != "" {
				autoGrantRule(t, w, autoGrant)
			}
			auto := 0
			if strings.HasSuffix(name, "reached") {
				auto = 6
			}
			spentGoal(t, w, "M-3", 40, auto)
			w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '100 hours' WHERE mission_id = 'M-3'`)
			one := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
			two := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
			got := map[string]bool{}
			for _, h := range []string{"h1", "h2"} {
				if c := w.claim(t, h, time.Hour, cursor); c != nil {
					got[c.TaskID] = true
				}
			}
			if !got[one] || !got[two] {
				t.Fatalf("claimed %v; want %s and %s", got, one, two)
			}
			if amended := w.amended(t, "M-3"); len(amended) != 0 {
				t.Fatalf("amended %v", amended)
			}
			if b := w.budget(t, "M-3"); fmt.Sprint(b) != fmt.Sprintf("map[auto_granted_attempts:%d max_attempts:40 max_wall_hours:48]", auto) {
				t.Fatalf("budget %v", b)
			}
			a := &asker{}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
				t.Fatalf("asked %d (%v)", n, err)
			}
		})
	}
}

// What the platform added on its own before stays left out of what the
// standing envelope bounds: a mission whose budget is above the envelope's
// only by its automatic grants still leases.
func TestBudgetAutoGrantedBeforeInsideEnvelope(t *testing.T) {
	t.Parallel()
	w, mission, _, check := spentEnvelope(t)
	w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 4, "max_wall_hours": 48, "auto_granted_attempts": 3}' WHERE mission_id = '`+mission+`'`)
	if c := w.claim(t, "h2", time.Hour, deterministic); c == nil || c.TaskID != check {
		t.Fatalf("the claim took %+v; want the check %s", c, check)
	}
}

// The money cap stays: a mission past its attempts and hours leases only
// through a vendor pool with room, and the claim records the pool it passed
// by at the routing rule's quota stop; with every pool at the stop it leases
// nothing.
func TestBudgetPastAttemptsStillStopsAtThePoolCap(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, -1, 0))
	w.ready(t, store.Task{})
	if c := w.claim(t, "h3", time.Hour, cursor); c == nil {
		t.Fatal("no claim to use the mission's one attempt with")
	}
	w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '100 hours',
	  budget_json = '{"max_attempts": 1, "max_wall_hours": 48}' WHERE mission_id = 'M-1'`)
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 0.95, time.Hour)
	w.reading(t, "codex:owner:weekly", 0.5, 6*24*time.Hour)
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude at 95 %% took %+v", c)
	}
	c := w.claim(t, "h1", store.LeaseTTL, claude, codex)
	if c == nil || c.Tool.Executor != "exec:codex" {
		t.Fatalf("claim = %+v; want codex", c)
	}
	if ch := w.choice(t, c); fmt.Sprint(ch["passed_by_quota"]) != "[implementer/exec:claude/anthropic/*]" {
		t.Fatalf("choice = %v", ch)
	}
	w.ready(t, store.Task{})
	w.reading(t, "codex:owner:weekly", 0.95, time.Hour)
	if c := w.claim(t, "h2", store.LeaseTTL, claude, codex); c != nil {
		t.Fatalf("both pools at the stop, %s took the task", c.Tool.Executor)
	}
}
