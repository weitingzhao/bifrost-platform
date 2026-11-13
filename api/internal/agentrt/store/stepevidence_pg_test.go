package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The aggregate atoms of a step's done rule (DESIGN-module-progress D3) on
// PostgreSQL: what they read from the step's own tasks, the landing stream
// and the B5 batches, and that the controller's evaluation moves the step
// on them.

func stepEvidence(t *testing.T, w *world, step, state string, min int64) store.StepEvidence {
	t.Helper()
	ctx := ctxOf(t)
	ev, err := store.ReadStepEvidence(ctx, w.rt, step, state, min)
	ok(t, err)
	return ev
}

// moveToStep moves an off-plan task and the checks of it onto step, as an
// authorized plan change does.
func moveToStep(t *testing.T, w *world, task, step string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, planChange(w, func(tx pgx.Tx) error {
		// The subject first: a check moves to where its subject already sits.
		for _, q := range []string{`WHERE task_id = $1`, `WHERE subject_task_id = $1`} {
			if _, err := tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2, off_plan = false, off_plan_reason = NULL, version = version + 1 `+q,
				task, step); err != nil {
				return err
			}
		}
		return nil
	}))
}

// step_tasks counts the step's own work: an accepted implementation counts
// once, not again for its accepted verification and review.
func TestStepTasksCountsTheStepsOwnWork(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.rule(t, s1, `{"step_tasks":{"state":"accepted","min":2}}`)
	d := &deployments{}
	if ev := stepEvidence(t, w, s1, "accepted", 1); ev.Tasks != plan.False || ev.Landed != plan.False || ev.STG != plan.False {
		t.Fatalf("a step with no work: %+v", ev)
	}
	t1 := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, t1, "h1", "h2", "h3")
	var accepted int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE plan_node_id = $1 AND state = 'accepted'`, s1).Scan(&accepted))
	if accepted != 3 {
		t.Fatalf("fixture: %d accepted tasks on the step, want the implementation and its two checks", accepted)
	}
	for _, c := range []struct {
		state string
		min   int64
		want  plan.Tri
	}{{"accepted", 1, plan.True}, {"accepted", 2, plan.False}, {"ready", 1, plan.False}} {
		if ev := stepEvidence(t, w, s1, c.state, c.min); ev.Tasks != c.want {
			t.Errorf("step_tasks %s ≥ %d = %s, want %s", c.state, c.min, ev.Tasks, c.want)
		}
	}
	if moved := w.evaluate(t, s1, d); moved != "" || w.stepState(t, s1) != "planned" {
		t.Fatalf("one deliverable moved a rule asking two: %q", moved)
	}
	t2 := w.ready(t, store.Task{BaseCommit: commitB})
	if ev := stepEvidence(t, w, s1, "ready", 1); ev.Tasks != plan.True {
		t.Fatal("a ready task not counted", ev)
	}
	w.passRound(t, t2, "h1", "h2", "h3")
	if ev := stepEvidence(t, w, s1, "accepted", 2); ev.Tasks != plan.True {
		t.Fatal("two accepted implementations not counted", ev)
	}
	if moved := w.evaluate(t, s1, d); moved != "done" || w.stepState(t, s1) != "done" {
		t.Fatalf("the rule holds and the step did not move: %q", moved)
	}
}

// The plan-standing grant takes the aggregate atoms as a step's first rule
// when they do not hold yet; one that holds already, or names a manual atom
// or a CI run beside them, is the Owner's.
func TestPlanStandingStepEvidence(t *testing.T) {
	t.Parallel()
	a := &asker{}
	w := newKernelWorld(t)
	putPlanStanding(t, w, true, false)
	w.step(t, "R2:2.1-4", 3, "")
	w.step(t, "R2:2.1-5", 4, "")
	t1 := w.ready(t, store.Task{BaseCommit: commitB})
	w.passRound(t, t1, "h1", "h2", "h3")
	setRule := func(key, step, rule string) map[string]any {
		t.Helper()
		out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []store.PlanChange{{Op: "set_done_rule", Node: step,
			Rule: []byte(rule)}}, a.ask)
		ok(t, err)
		return out
	}
	for name, c := range map[string]struct {
		step, rule string
		applied    bool
	}{
		// R5:5.5-15: the aggregate atoms stand behind the Owner's sign-off, so
		// such a rule never holds when it is set; "holds already" is shown
		// with the step's own task instead.
		"step_tasks, not holding":   {s2, `{"all":[{"step_tasks":{"state":"accepted"}},{"owner_signoff":"s2"}]}`, true},
		"landed and settled":        {s3, `{"all":[{"step_landed":{}},{"step_stg_settled":{}},{"owner_signoff":"s3"}]}`, true},
		"its task, holding already": {s1, `{"task_accepted":"` + t1 + `"}`, false},
		"beside a manual atom":      {"R2:2.1-4", `{"all":[{"any":[{"step_landed":{}},{"manual":"by hand"}]},{"owner_signoff":"4"}]}`, false},
		"beside a CI run":           {"R2:2.1-5", `{"all":[{"step_landed":{}},{"ci_succeeded":{"repo":"bifrost-platform","commit":"` + commitA + `"}},{"owner_signoff":"5"}]}`, false},
	} {
		cards := len(a.cards)
		out := setRule("k-"+name, c.step, c.rule)
		if applied := out["decision"] == "applied" && len(a.cards) == cards; applied != c.applied {
			t.Errorf("%s: %v (cards %d → %d), want applied %v", name, out, cards, len(a.cards), c.applied)
		}
	}
	// Refused before any standing decision, whatever branch names them:
	// step_tasks of a state a proposer reaches by proposing a task (review
	// 630), and step_deployed, not in the grammar (this change reads no
	// deployment).
	for _, rule := range []string{`{"step_tasks":{"state":"proposed"}}`, `{"any":[{"step_landed":{}},{"step_tasks":{"state":"submitted"}}]}`,
		`{"step_deployed":{"env":"prod"}}`} {
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k-"+rule, root, "test", []store.PlanChange{{Op: "set_done_rule", Node: "R2:2.1-5",
			Rule: []byte(rule)}}, a.ask)
		wantErr(t, err, "does not parse")
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_change_decided' AND body_json->>'by' = 'standing authorization (plan-standing v1)'`).Scan(&n))
	if n != 2 {
		t.Fatalf("%d changes decided by the standing authorization, want 2", n)
	}
}
