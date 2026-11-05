package store_test

import (
	"testing"
	"time"

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
	ev, err := store.ReadStepEvidence(ctx, w.rt, step, state, min)
	ok(t, err)
	return ev
}

// moveToStep moves an off-plan task and the checks of it onto step, as an
// authorized plan change does.
func moveToStep(t *testing.T, w *world, task, step string) {
	t.Helper()
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

// Serial: FreezeEventClock. step_landed follows the repository's landing
// fold: false while the work waits for its claim or is on its way, unknown
// while main's answer is not settled, true once landed (the step moves),
// false again when another accepted implementation's landing fails.
// step_stg_settled implies landed; under stg_release none no batch admits
// the landed operation, so it is false.
func TestStepLandedFollowsTheLandingFold(t *testing.T) {
	at := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	defer store.FreezeEventClock(at)()
	w := liveWorld(t, "none", at)
	w.rule(t, s2, `{"step_landed":{}}`)
	d := &deployments{}
	want := func(why string, landed, stg plan.Tri) {
		t.Helper()
		if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != landed || ev.STG != stg {
			t.Fatalf("%s: step_landed %s, step_stg_settled %s; want %s, %s", why, ev.Landed, ev.STG, landed, stg)
		}
	}
	want("no work", plan.False, plan.False)

	store.FreezeEventClock(at.Add(-40 * time.Minute))
	r1 := w.ready(t, store.Task{MissionID: "M-3", PlanNode: s2, BaseCommit: commitB})
	w.passRound(t, r1, "h1", "h2", "h3")
	want("accepted, in the handoff, not claimed yet", plan.False, plan.False)

	op1 := "land:" + r1 + ":1"
	prodChain(t, w, r1, op1, at.Add(-30*time.Minute), "branch_pushed")
	want("branch pushed", plan.False, plan.False)
	store.FreezeEventClock(at.Add(-26 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op1, "ci_seen", "run", "ci-green", "status", "Succeeded", "R", commitB,
		"created_at", at.Add(-26*time.Minute-30*time.Second).Format(time.RFC3339Nano))))
	store.FreezeEventClock(at.Add(-25 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op1, "main_intent", "R", commitB, "M0", commitA)))
	want("main asked to move", plan.Unknown, plan.Unknown)
	store.FreezeEventClock(at.Add(-24 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op1, "main_result", "outcome", "ok")))
	want("pushed, not yet reconciled", plan.Unknown, plan.Unknown)
	if moved := w.evaluate(t, s2, d); moved != "" || w.stepState(t, s2) != "planned" {
		t.Fatalf("an unknown landing moved the step: %q", moved)
	}

	store.FreezeEventClock(at.Add(-23 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op1, "landed", "R", commitB)))
	want("landed, no B5 batch under none", plan.True, plan.False)
	// An implementation that writes no code has nothing to land.
	store.FreezeEventClock(at.Add(-22 * time.Minute))
	exempt := w.ready(t, store.Task{MissionID: "M-3", PlanNode: s2, Effect: "none"})
	w.passRound(t, exempt, "h1", "h2", "h3")
	want("beside an accepted implementation that writes no code", plan.True, plan.False)
	if moved := w.evaluate(t, s2, d); moved != "done" {
		t.Fatalf("landed work did not move the step: %q", moved)
	}

	store.FreezeEventClock(at.Add(-20 * time.Minute))
	r2 := w.ready(t, store.Task{MissionID: "M-3", PlanNode: s2, BaseCommit: commitB})
	w.passRound(t, r2, "h1", "h2", "h3")
	want("a second implementation waits for its claim", plan.False, plan.False)
	op2 := "land:" + r2 + ":1"
	prodChain(t, w, r2, op2, at.Add(-15*time.Minute), "branch_pushed")
	store.FreezeEventClock(at.Add(-5 * time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op2, "failed", "why", "stale")))
	want("the second landing failed", plan.False, plan.False)
	// Done stays done: the rule names no standing condition.
	if moved := w.evaluate(t, s2, d); moved != "" || w.stepState(t, s2) != "done" {
		t.Fatalf("a done step moved: %q", moved)
	}
}

// Serial: real PostgreSQL, signing anchor, local Git PATH and event clock.
// step_stg_settled on the B5 production paths: unknown while the batch that
// admitted the step's landed operation runs, true once it settled the
// member delivered, and the step moves.
func TestStepSTGSettledFollowsB5(t *testing.T) {
	x := b5hFixture(t)
	w := x.w
	moveToStep(t, w, x.root, s2)
	w.rule(t, s2, `{"step_stg_settled":{}}`)
	d := &deployments{}
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.True || ev.STG != plan.Unknown {
		t.Fatalf("landed, batch open: %+v", ev)
	}
	if moved := w.evaluate(t, s2, d); moved != "" || w.stepState(t, s2) != "planned" {
		t.Fatalf("an open batch moved the step: %q", moved)
	}
	ok(t, x.c.Pass(ctx))
	x.runs.work = b5hWork("d", "f")
	x.terminal(t, "Succeeded")
	ok(t, x.c.Pass(ctx))
	ok(t, x.c.Pass(ctx))
	x.until(t, b5hClosed)
	x.assertDelivered(t, 1)
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.True || ev.STG != plan.True {
		t.Fatalf("delivered, batch settled: %+v", ev)
	}
	if moved := w.evaluate(t, s2, d); moved != "done" {
		t.Fatalf("settled work did not move the step: %q", moved)
	}
	// Another accepted implementation on the step, in bifrost-trade-infra,
	// with no landing at all: the landed and settled platform work does not
	// mask it (review 630).
	other := w.ready(t, store.Task{MissionID: "M-3", PlanNode: s2, Repo: infraRepo, BaseCommit: commitB})
	w.passRound(t, other, "h1", "h2", "h3")
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.False || ev.STG != plan.False {
		t.Fatalf("an accepted implementation never landed was masked: %+v", ev)
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

// Serial: global clock and signing anchor. Work landed only in
// bifrost-trade-infra never joins a B5 batch (B5 releases bifrost-platform
// only), so it has no STG release to wait for: step_stg_settled holds with
// step_landed, and the step's usual rule moves it (R5:5.5-9; before, a
// documents-only step such as R5:5.8-1 stayed active for ever).
func TestStepSTGSettledInfraOnlyHoldsOnceLanded(t *testing.T) {
	w, at := s66World(t, true, 5)
	root := s66Landed(t, w, "M-3", infraRepo, at)
	if o := landingState(t, w, "landing:"+infraRepo).Operations["land:"+root+":1"]; o.Phase != "landed" {
		t.Fatalf("fixture: infra operation %s", o.Phase)
	}
	moveToStep(t, w, root, s2)
	if ev := stepEvidence(t, w, s2, "accepted", 1); ev.Landed != plan.True || ev.STG != plan.True {
		t.Fatalf("infra work landed: %+v", ev)
	}
	w.rule(t, s2, `{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}}]}`)
	if moved := w.evaluate(t, s2, &deployments{}); moved != "done" {
		t.Fatalf("landed infra work did not move the step: %q", moved)
	}
}

// Serial: global clock and signing anchor. Infra work landed beside
// platform work B5 never admitted: the infra root settles nothing for the
// platform one, whichever comes first (review of 24642337: a skip that
// answered for the whole step passed every other test).
func TestStepSTGSettledInfraBesidePlatformWaitsForB5(t *testing.T) {
	w, at := s66World(t, true, 5)
	infra := s66Landed(t, w, "M-3", infraRepo, at)
	platform := s66Landed(t, w, "M-3", "bifrost-platform", at)
	moveToStep(t, w, infra, s2)
	moveToStep(t, w, platform, s2)
	if ev := stepEvidence(t, w, s2, "accepted", 2); ev.Landed != plan.True || ev.STG != plan.False {
		t.Fatalf("infra and platform landed, platform not admitted: %+v", ev)
	}
}
