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
// cancelled task of its own, and what it already had added on its own.
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

// M-308 (2026-11-06): a mission that has used its attempts gets one round
// more on its own while the rule's cap has room, recorded as the
// controller's mission_amended; its ready tasks stay ready and no card is
// asked. One grant a pass: the second ready task runs on the same round.
func TestAutoGrantGrowsBudget(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	autoGrantRule(t, w, "auto_grant: {max_attempts: 6, max_wall_hours: 0}\n")
	spentGoal(t, w, "M-3", 40, 0)
	one := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	two := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	for _, task := range []string{one, two} {
		if s := w.state(t, "task", task); s != "ready" {
			t.Fatalf("task %s is %s; want ready", task, s)
		}
	}
	amended := w.amended(t, "M-3")
	if len(amended) != 1 || !strings.HasPrefix(amended[0], "controller ") ||
		!strings.Contains(amended[0], `"reason": "budget grown automatically, cap 6: it had used 40 of 40 attempts`) ||
		!strings.Contains(amended[0], `"max_attempts": 43`) || strings.Contains(amended[0], "max_wall_hours") {
		t.Fatalf("amended %v", amended)
	}
	if b := w.budget(t, "M-3"); fmt.Sprint(b) != "map[auto_granted_attempts:3 auto_granted_wall_hours:0 max_attempts:43 max_wall_hours:48]" {
		t.Fatalf("budget %v", b)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 || len(w.amended(t, "M-3")) != 1 {
		t.Fatalf("a pass with room blocked %d (%v) or granted again", n, err)
	}
}

// Once what the platform added reaches the cap, the task is blocked and its
// card says the cap is reached, so it is the Owner's.
func TestAutoGrantCapReachedAsks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	autoGrantRule(t, w, "auto_grant: {max_attempts: 6, max_wall_hours: 0}\n")
	spentGoal(t, w, "M-3", 46, 6)
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", task) != "blocked" {
		t.Fatalf("blocked %d (%v), task %s", n, err, w.state(t, "task", task))
	}
	if amended := w.amended(t, "M-3"); len(amended) != 0 {
		t.Fatalf("amended %v past the cap", amended)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	says := "already added 6 of its cap of 6 attempts (0 of 0 hours) automatically and the cap is reached, so this card is yours"
	if c := a.cards[0]; !strings.Contains(c.Body, says) || !strings.Contains(c.RecommendedWhy, says) {
		t.Fatalf("the card (why %q):\n%s", c.RecommendedWhy, c.Body)
	}
}

// A task blocked only for its budget goes back to ready when a grant gives
// the mission room.
func TestAutoGrantReadiesBudgetBlocked(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	spentGoal(t, w, "M-3", 40, 0)
	held := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	// The fixture's rule grows nothing: the task is blocked for its budget.
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", held) != "blocked" {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	autoGrantRule(t, w, "auto_grant: {max_attempts: 6, max_wall_hours: 0}\n")
	next := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	if s, r := w.state(t, "task", held), w.lastMove(t, held); s != "ready" || r != "budget grown automatically, cap 6" {
		t.Fatalf("the budget-blocked task is %s (%q)", s, r)
	}
	if s := w.state(t, "task", next); s != "ready" {
		t.Fatalf("the new task is %s", s)
	}
}

// The standing envelope a mission started under bounds its budget less what
// the platform added on its own: after an automatic grant the claim takes
// the task.
func TestAutoGrantInsideEnvelope(t *testing.T) {
	t.Parallel()
	w, mission, _, check := spentEnvelope(t)
	autoGrantRule(t, w, "auto_grant: {max_attempts: 6, max_wall_hours: 0}\n")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 || w.state(t, "task", check) != "ready" {
		t.Fatalf("blocked %d (%v), the check is %s", n, err, w.state(t, "task", check))
	}
	if b := w.budget(t, mission); fmt.Sprint(b["max_attempts"], b["auto_granted_attempts"]) != "4 3" {
		t.Fatalf("budget %v", b)
	}
	if c := w.claim(t, "h2", time.Hour, deterministic); c == nil || c.TaskID != check {
		t.Fatalf("after the automatic grant the claim took %+v; want the check %s", c, check)
	}
}

// A budget rule that states no auto_grant lets the platform add 30 attempts.
func TestAutoGrantDefaultCap(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		auto  int
		grows bool
	}{"room for a round": {27, true}, "short of a round": {28, false}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			autoGrantRule(t, w, "")
			spentGoal(t, w, "M-3", 40, c.auto)
			task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
			if _, err := store.BlockOverBudget(ctx, w.rt); err != nil {
				t.Fatal(err)
			}
			amended := w.amended(t, "M-3")
			if (w.state(t, "task", task) == "ready") != c.grows || (len(amended) == 1) != c.grows {
				t.Fatalf("task %s, amended %v; want grown %v", w.state(t, "task", task), amended, c.grows)
			}
			if c.grows && !strings.Contains(amended[0], "budget grown automatically, cap 30: ") {
				t.Fatalf("amended %v", amended)
			}
		})
	}
}
