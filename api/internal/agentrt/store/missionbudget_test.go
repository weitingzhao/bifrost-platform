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

// budget is a mission's budget_json.
func (w *world) budget(t *testing.T, mission string) map[string]any {
	t.Helper()
	var b map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT budget_json FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&b))
	return b
}

// amended is every mission_amended of a mission: actor, task and body.
func (w *world) amended(t *testing.T, mission string) []string {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT actor_id || ' ' || COALESCE(task_id, '') || ' ' || body_json::text FROM agentrt.event
	  WHERE event_type = 'mission_amended' AND mission_id = $1 ORDER BY event_id`, mission)
	ok(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		ok(t, rows.Scan(&s))
		out = append(out, s)
	}
	ok(t, rows.Err())
	return out
}

// lastMove is the reason of a task's last state change.
func (w *world) lastMove(t *testing.T, task string) string {
	t.Helper()
	var r string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'reason', '') FROM agentrt.event WHERE task_id = $1
	  AND event_type = 'task_state_changed' ORDER BY event_id DESC LIMIT 1`, task).Scan(&r))
	return r
}

// TD-384, PROD M-9 (2026-10-19): the Owner gave a blocked task one more round
// when its mission had used every attempt of its budget; the task went back
// to ready and no claim ever took it. Now the card says the round raises the
// budget, the answer raises it to what one round needs (three attempts more
// than used) as the Owner's mission_amended, and a host takes the task in the
// next claim. A mission with room for a whole round gets nothing added and the
// card says nothing of it; one with room for part of a round gets the rest
// (review 428: "room left" is room for a round).
func TestRetryOnSpentBudgetLeases(t *testing.T) {
	t.Parallel()
	for name, max := range map[string]int{"spent": 4, "one short of a round": 6, "room for a round": 7, "room left": 40} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			grows := max < 7
			w := newKernelWorld(t)
			w.goalMission(t, "M-3")
			impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
			if did := w.failRound(t, impl, "h1", "h2"); did != "rework" {
				t.Fatalf("round 1 did %q", did)
			}
			if did := w.failRound(t, impl, "h3", "h4"); did != "blocked" {
				t.Fatalf("round 2 did %q", did)
			}
			// Four attempts used: two implementations, two verifications.
			w.legacy(t, fmt.Sprintf(`UPDATE agentrt.mission SET budget_json = '{"max_attempts": %d, "max_wall_hours": 48}' WHERE mission_id = 'M-3'`, max))
			a := &asker{}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
				t.Fatalf("asked %d (%v)", n, err)
			}
			raise := fmt.Sprintf("mission M-3 has used 4 of its %d attempts, so this answer also raises its budget to 7 attempts, recorded as yours", max)
			if body := a.cards[0].Body; strings.Contains(body, raise) != grows || strings.Contains(body, "raises its budget") != grows {
				t.Fatalf("the card (budget grows: %v):\n%s", grows, body)
			}
			q := a.lastID()
			a.answer(q, store.QuestionAnswered, store.CardApplyKey)
			if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", impl) != "ready" {
				t.Fatalf("one more round: decided %d (%v), task %s", n, err, w.state(t, "task", impl))
			}
			b, amended := w.budget(t, "M-3"), w.amended(t, "M-3")
			if !grows {
				if len(amended) != 0 || fmt.Sprint(b) != fmt.Sprintf("map[max_attempts:%d max_wall_hours:48]", max) {
					t.Fatalf("with room left: amended %v, budget %v", amended, b)
				}
			} else if fmt.Sprint(b) != fmt.Sprintf("map[max_attempts:7 max_wall_hours:48 owner_granted_attempts:%d]", 7-max) || len(amended) != 1 ||
				!strings.HasPrefix(amended[0], "owner "+impl+" ") || !strings.Contains(amended[0], `"max_attempts": 7`) ||
				!strings.Contains(amended[0], "one more round on its card (question "+q+")") || strings.Contains(amended[0], "max_wall_hours") {
				t.Fatalf("budget %v, amended %v", b, amended)
			}
			if cl := w.claim(t, "h5", time.Hour, cursor); cl == nil || cl.TaskID != impl {
				t.Fatalf("after one more round the claim took %+v; want %s", cl, impl)
			}
		})
	}
}

// spentEnvelope signs a standing envelope whose budget is exactly the
// mission's (one attempt), proposes a mission inside it, and carries its
// implementation to verification: the one attempt is used and the
// verification waits in ready.
func spentEnvelope(t *testing.T) (w *world, mission, impl, check string) {
	t.Helper()
	w = newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, ruleText("standing-envelope:dev", 1, rules.Initial, since, until, `tenant: dev
max_action: branch
budget: {max_attempts: 1, max_wall_hours: 48}
executors: [exec:codex, exec:deterministic]
hosts: [h1, h2, h3, h4]
`))))
	out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "m", store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"},
		MaxAttempts: 1, MaxWallHours: 48}, nil)
	ok(t, err)
	mission = out["mission_id"].(string)
	if out["state"] != "active" {
		t.Fatalf("proposal: %v", out)
	}
	impl = w.ready(t, store.Task{MissionID: mission, OffPlanReason: "a goal"})
	w.run(t, "h1", codex, "done")
	check, err = store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if c := w.claim(t, "h2", time.Hour, deterministic); c != nil {
		t.Fatalf("claimed %s on a spent budget", c.TaskID)
	}
	return w, mission, impl, check
}

// TD-384: a ready task its mission's budget no longer lets run is blocked by
// the controller's pass, with the reason, and gets a card naming the budget;
// once. Its one more round raises the budget past the standing envelope the
// mission started under: what the Owner added is his, the envelope bounds
// what the mission started with, so the check is taken.
func TestSpentBudgetBlocksReadyTask(t *testing.T) {
	t.Parallel()
	w, mission, impl, check := spentEnvelope(t)
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", check) != "blocked" {
		t.Fatalf("blocked %d (%v), the check is %s", n, err, w.state(t, "task", check))
	}
	why := "mission " + mission + " has used 1 of its 1 attempts: no host may take this task until the budget grows"
	if r := w.lastMove(t, check); r != why {
		t.Fatalf("blocked because %q; want %q", r, why)
	}
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a second pass blocked %d (%v)", n, err)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	for _, want := range []string{"Why: " + why, "so this answer also raises its budget to 4 attempts",
		store.BlockedDrop + ": this check is cancelled; when it is the only check of the latest work of task " + impl + ", that task is blocked"} {
		if !strings.Contains(a.cards[0].Body, want) {
			t.Errorf("the card lacks %q:\n%s", want, a.cards[0].Body)
		}
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", check) != "ready" {
		t.Fatalf("one more round: decided %d (%v), the check is %s", n, err, w.state(t, "task", check))
	}
	if b := w.budget(t, mission); fmt.Sprint(b) != "map[max_attempts:4 max_wall_hours:48 owner_granted_attempts:3]" {
		t.Fatalf("budget %v", b)
	}
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("after the grant the pass blocked %d (%v)", n, err)
	}
	if c := w.claim(t, "h2", time.Hour, deterministic); c == nil || c.TaskID != check {
		t.Fatalf("after one more round the claim took %+v; want the check %s", c, check)
	}
}

// TD-384: not doing a verification blocks the work it checks, which cannot
// be accepted unchecked: it gets a card of its own rather than waiting in
// verifying for ever.
func TestDroppedCheckBlocksItsWork(t *testing.T) {
	t.Parallel()
	w, _, impl, check := spentEnvelope(t)
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	q := a.lastID()
	a.answer(q, store.QuestionAnswered, store.CardRefuseKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if s, c := w.state(t, "task", impl), w.state(t, "task", check); s != "blocked" || c != "cancelled" {
		t.Fatalf("the work is %s, its check %s; want blocked, cancelled", s, c)
	}
	if r := w.lastMove(t, impl); r != "the Owner chose not to do its verify "+check+" (question "+q+")" {
		t.Fatalf("the work blocked because %q", r)
	}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[1].TaskID != impl {
		t.Fatalf("asked %d (%v) after the drop; want the work's own card", n, err)
	}
}

// TD-384: a mission past its hours blocks its ready task the same way, and
// one more round adds the hours a round's attempts may run; a mission with
// time and attempts left blocks nothing.
func TestSpentHoursBlockReadyTask(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.goalMission(t, "M-4")
	// A one-minute implementation: its verification and review still have an
	// hour each (review 428 B1), so a round needs three hours.
	late := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal", HardLimit: time.Minute})
	fresh := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
	w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '49 hours',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 48}' WHERE mission_id = 'M-3'`,
		`UPDATE agentrt.mission SET budget_json = '{"max_attempts": 9, "max_wall_hours": 48}' WHERE mission_id = 'M-4'`)
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 || w.state(t, "task", late) != "blocked" ||
		w.state(t, "task", fresh) != "ready" {
		t.Fatalf("blocked %d (%v): M-3's task %s, M-4's %s", n, err, w.state(t, "task", late), w.state(t, "task", fresh))
	}
	if r := w.lastMove(t, late); !strings.HasPrefix(r, "mission M-3 is past its 48 hours (it started 49.0 hours ago)") {
		t.Fatalf("blocked because %q", r)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || !strings.Contains(a.cards[0].Body, "raises its budget to 53 hours") {
		t.Fatalf("asked %d (%v):\n%v", n, err, a.cards)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	// A little over 49 hours gone and three attempts of up to an hour each: 53.
	if b := w.budget(t, "M-3"); fmt.Sprint(b) != "map[max_attempts:9 max_wall_hours:53 owner_granted_wall_hours:5]" {
		t.Fatalf("budget %v", b)
	}
	if c := w.claim(t, "h1", time.Hour, cursor); c == nil || c.TaskID != late {
		t.Fatalf("after one more round the claim took %+v; want %s", c, late)
	}
}

// salvageChecks makes the checks the acceptance pass makes for salvaged
// results now, and returns how many it made.
func (w *world) salvageChecks(t *testing.T) int {
	t.Helper()
	ids, err := store.PendingSalvaged(ctx, w.rt)
	ok(t, err)
	n := 0
	for _, id := range ids {
		v, err := store.StartSalvageCheck(ctx, w.rt, id)
		ok(t, err)
		if v != "" {
			n++
		}
	}
	return n
}

// checkOf is the one check of subject in a state.
func (w *world) checkOf(t *testing.T, subject, state string) string {
	t.Helper()
	var id string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.task WHERE subject_task_id = $1 AND state = $2`, subject, state).Scan(&id))
	return id
}

// Review 428 A1: the check of a lost attempt's salvaged result that the pass
// blocked for want of budget is still an open check: the acceptance pass
// makes no second one while it waits on its card, and once the Owner drops
// it, none for that attempt again. The work, running a new attempt, is left
// as it is.
func TestBlockedSalvageCheckIsNotMadeAgain(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task, _, _ := w.salvagedTask(t, true) // two attempts: the lost one and one running
	w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 2, "max_wall_hours": 48}' WHERE mission_id = 'M-1'`)
	if n := w.salvageChecks(t); n != 1 {
		t.Fatalf("checks of the salvaged result: %d", n)
	}
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	check := w.checkOf(t, task, "blocked")
	if n := w.salvageChecks(t); n != 0 {
		t.Fatalf("%d more checks of the salvaged result while its check waits blocked", n)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardRefuseKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if c, s := w.state(t, "task", check), w.state(t, "task", task); c != "cancelled" || s != "running" {
		t.Fatalf("the check is %s, the work %s; want cancelled, running", c, s)
	}
	if n := w.salvageChecks(t); n != 0 {
		t.Fatalf("%d checks of the salvaged result the Owner chose not to check", n)
	}
}

// Review 428 A2: dropping the blocked check of a lost attempt's salvaged
// result while the work's latest attempt is under its own check leaves that
// check and the work alone.
func TestDroppingAnOldCheckLeavesTheWork(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task, _, fresh := w.salvagedTask(t, true)
	w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 2, "max_wall_hours": 48}' WHERE mission_id = 'M-1'`)
	if n := w.salvageChecks(t); n != 1 {
		t.Fatalf("checks of the salvaged result: %d", n)
	}
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	old := w.checkOf(t, task, "blocked")
	// The new attempt hands in and goes to its own verification.
	f := fence(fresh, "h2")
	exit := 0
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "a2"), Ref: fresh.PushedRef, Commit: commitB},
		heads(map[string]string{fresh.PushedRef: commitB}, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	current, err := store.StartVerification(ctx, w.rt, task)
	ok(t, err)
	if current == "" || w.state(t, "task", task) != "verifying" {
		t.Fatalf("verification %q, the work %s", current, w.state(t, "task", task))
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[0].TaskID != old {
		t.Fatalf("asked %d (%v): %+v", n, err, a.cards)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardRefuseKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if o, s, c := w.state(t, "task", old), w.state(t, "task", task), w.state(t, "task", current); o != "cancelled" || s != "verifying" || c != "ready" {
		t.Fatalf("the old check is %s, the work %s, its own check %s; want cancelled, verifying, ready", o, s, c)
	}
}

// Review 428 C1: a budget of millions of hours is not read as spent.
func TestHugeHoursAreNotSpent(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 9, "max_wall_hours": 3000000}' WHERE mission_id = 'M-3'`)
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	if c := w.claim(t, "h1", time.Hour, cursor); c == nil || c.TaskID != task {
		t.Fatalf("claimed %+v; want %s", c, task)
	}
}

// Review 428, 430 C2: what the Owner added is the only part of a budget the
// standing envelope does not bound. After his grant and a plan that lowers
// the mission's attempts (his grant stays recorded), the check is taken under
// the envelope as it was, and not once the envelope is tightened below the
// hours the mission started with.
func TestEnvelopeStillBoundsWhatTheMissionStartedWith(t *testing.T) {
	t.Parallel()
	for name, tighten := range map[string]bool{"as it was": false, "tightened": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w, mission, _, check := spentEnvelope(t)
			if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
				t.Fatalf("blocked %d (%v)", n, err)
			}
			a := &asker{}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
				t.Fatalf("asked %d (%v)", n, err)
			}
			a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
			if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", check) != "ready" {
				t.Fatalf("decided %d (%v)", n, err)
			}
			// A plan lowers the attempts as amendMission writes it: the
			// Owner's grant stays.
			w.legacy(t, `UPDATE agentrt.mission SET budget_json = budget_json || '{"max_attempts": 3}' WHERE mission_id = '`+mission+`'`)
			if b := w.budget(t, mission); fmt.Sprint(b) != "map[max_attempts:3 max_wall_hours:48 owner_granted_attempts:3]" {
				t.Fatalf("budget %v", b)
			}
			if tighten {
				_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(ruleText("standing-envelope:dev", 2,
					rules.Tighten, since, until, "tenant: dev\nmax_action: branch\nbudget: {max_attempts: 1, max_wall_hours: 24}\n"+
						"executors: [exec:codex, exec:deterministic]\nhosts: [h1, h2, h3, h4]\n")))
				ok(t, err)
			}
			c := w.claim(t, "h2", time.Hour, deterministic)
			if tighten && c != nil {
				t.Fatalf("claimed %s of %s though its 48 hours are above the envelope's 24", c.TaskID, mission)
			}
			if !tighten && (c == nil || c.TaskID != check) {
				t.Fatalf("under the envelope as it was the claim took %+v; want %s", c, check)
			}
		})
	}
}

// handCheckOf proposes, by hand, a check of subject in mission, where its
// subject sits (step, "" off the plan), whose spec is body as given, and
// makes it ready.
func (w *world) handCheckOf(t *testing.T, mission, step, role, subject string, body []byte) string {
	t.Helper()
	id := store.NewTaskID()
	off := ""
	if step == "" {
		off = "a hand check"
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", body)
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: mission, PlanNode: step, OffPlanReason: off,
			Subject: subject, Role: role, Class: "go", Title: role, SpecArtifact: spec, Effect: "none", Repo: "bifrost-platform",
			BaseCommit: commitA}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", id)
	}))
	return id
}

// Reviews 430 A1, 432 A1: a check that only names the work's latest attempt
// does not keep the work checked: a hand check pinned to an older round, or
// to the latest round with another task spec, or a review before the
// attempt's deterministic pass, is closed by Conclude and accepts nothing, so
// not doing the work's own check blocks the work. A hand check Conclude would
// use keeps it checked, and the work stays verifying.
func TestDroppingTheLastCurrentCheckBlocksTheWork(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		role      string
		round     int
		otherSpec bool
		work      string
	}{
		"an older round":                       {"verify", 0, false, "blocked"},
		"another task spec":                    {"verify", 1, true, "blocked"},
		"a review before a deterministic pass": {"review", 1, false, "blocked"},
		"a check Conclude would use":           {"verify", 1, false, "verifying"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			w.goalMission(t, "M-3")
			impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
			cl := w.run(t, "h1", codex, "done")
			own, err := store.StartVerification(ctx, w.rt, impl)
			ok(t, err)
			spec := w.spec
			if c.otherSpec {
				spec = w.artifact(t, "task_spec", "another task spec")
			}
			hand := w.handCheckOf(t, "M-3", "", c.role, impl, []byte(fmt.Sprintf(
				`{"attempt":%q,"check":%q,"commit":%q,"repo":"bifrost-platform","round":%d,"subject_task":%q,"task_spec":%q}`,
				cl.AttemptID, c.role, commitA, c.round, impl, spec)))
			w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 1, "max_wall_hours": 48}' WHERE mission_id = 'M-3'`)
			if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
				t.Fatalf("blocked %d (%v); want the work's check and the hand check", n, err)
			}
			a := &asker{}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
				t.Fatalf("asked %d (%v)", n, err)
			}
			for i, card := range a.cards {
				if card.TaskID == own {
					a.answer(fmt.Sprintf("q_%c", 'a'+i), store.QuestionAnswered, store.CardRefuseKey)
				}
			}
			if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
				t.Fatalf("decided %d (%v)", n, err)
			}
			if o, s, h := w.state(t, "task", own), w.state(t, "task", impl), w.state(t, "task", hand); o != "cancelled" || s != c.work || h != "blocked" {
				t.Fatalf("its own check is %s, the work %s, the hand check %s; want cancelled, %s, blocked", o, s, h, c.work)
			}
		})
	}
}

// Review 430 B1: whether the Owner dropped a check of a salvaged attempt is
// read from the check's pin as Conclude reads it: a cancelled check whose
// spec only mentions the attempt in text pins nothing and does not stop its
// discovery; one whose JSON writes the attempt's id with an escape pins it.
func TestDroppedPinIsReadAsTheCheckReadsIt(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		spec  func(task, attempt, spec string) []byte
		found int
	}{
		"text that mentions the attempt": {func(task, attempt, _ string) []byte {
			return []byte("Compare " + attempt + " of " + task + " with the next attempt.")
		}, 1},
		"a pin written with an escape": {func(task, attempt, spec string) []byte {
			return []byte(fmt.Sprintf(`{"attempt":"\u0061%s","check":"verify","commit":%q,"repo":"bifrost-platform","round":1,`+
				`"salvage":true,"subject_task":%q,"task_spec":%q}`, attempt[1:], commitA, task, spec))
		}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			task, old, _ := w.salvagedTask(t, true)
			hand := w.handCheckOf(t, "M-1", s1, "verify", task, c.spec(task, old, w.spec))
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", hand, "cancelled", "test") }))
			if n := w.salvageChecks(t); n != c.found {
				t.Fatalf("%d checks of the salvaged result; want %d", n, c.found)
			}
		})
	}
}
