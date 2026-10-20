package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// failRound carries impl, ready, through one round whose deterministic
// verification fails, and returns what using that result did.
func (w *world) failRound(t *testing.T, impl, author, checker string) string {
	t.Helper()
	if c := w.run(t, author, cursor, "done"); c.TaskID != impl {
		t.Fatalf("claimed %s, not %s", c.TaskID, impl)
	}
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, checker, deterministic, "failed")
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	return did
}

// proposed proposes a task, defaults filled in as ready does, and leaves it
// proposed.
func (w *world) proposed(t *testing.T, task store.Task) string {
	t.Helper()
	task.ID = store.NewTaskID()
	if task.Role == "" {
		task.Role = "implement"
	}
	if task.Effect == "" {
		task.Effect = "branch"
	}
	task.Repo, task.Class, task.Title, task.SpecArtifact = "bifrost-platform", "go", "t", w.spec
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.ProposeTask(ctx, tx, "controller", task)
		return err
	}))
	return task.ID
}

// closedCard is the owner_wait_closed of a card: its outcome, answer and text.
func (w *world) closedCard(t *testing.T, question string) (outcome, answer, text string) {
	t.Helper()
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'outcome', COALESCE(body_json->>'answer', ''), COALESCE(body_json->>'text', '')
	  FROM agentrt.event WHERE event_type = 'owner_wait_closed' AND body_json->>'question_id' = $1`, question).Scan(&outcome, &answer, &text))
	return outcome, answer, text
}

func (w *world) rounds(t *testing.T, task string) (int, int) {
	t.Helper()
	var round, max int
	ok(t, w.rt.QueryRow(ctx, `SELECT round_no, max_rounds FROM agentrt.task WHERE task_id = $1`, task).Scan(&round, &max))
	return round, max
}

// T26's first run (PROD M-9, 2026-10-19): an implementation whose checks
// failed in its last round was blocked with no card and no way out, and its
// mission stayed active for ever. Now the block gets one card; one more
// round readies the task with one round more; blocked again it gets a new
// card; "none of these" closes the card and moves nothing, and the next card
// carries the Owner's words; not doing it cancels the task, and the mission
// finishes without it. Each answer is kept and acknowledged once, and the
// open card is in needs_you.
func TestBlockedTaskCard(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.accept(t, w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"}))
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	if did := w.failRound(t, impl, "h1", "h2"); did != "rework" {
		t.Fatalf("round 1 did %q", did)
	}
	if did := w.failRound(t, impl, "h3", "h4"); did != "blocked" {
		t.Fatalf("round 2 did %q", did)
	}
	if closed, err := store.CloseMissions(ctx, w.rt); err != nil || len(closed) != 0 {
		t.Fatalf("closed %v %v with a task blocked", closed, err)
	}

	a := &asker{}
	n, err := store.AskBlocked(ctx, w.rt, a.ask)
	if err != nil || n != 1 || len(a.cards) != 1 {
		t.Fatalf("asked %d (%v), cards %d; want one card", n, err, len(a.cards))
	}
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 || len(a.cards) != 1 {
		t.Fatalf("a second pass asked %d (%v): the block has its card", n, err)
	}
	c := a.cards[0]
	if !slices.Equal(c.Options, []string{store.BlockedRetry, store.BlockedDrop}) || c.TaskID != impl || c.OwnerDuty != "goal" ||
		c.Recommended != 0 || !strings.Contains(c.Meanwhile, "M-3") || c.Urgency == "" || c.Title != "Blocked in M-3: t" {
		t.Fatalf("card %+v", c)
	}
	for _, want := range []string{"Task " + impl + " (implement, bifrost-platform) of mission M-3 is blocked; card 1 for this block",
		"Why: deterministic failed in round 2 of 2", "Last check: deterministic, fail, on attempt 2", "Its report begins:\n\nh4failed",
		store.BlockedRetry + ": it goes back to ready for round 3 of 3", store.BlockedDrop + ": the task is cancelled"} {
		if !strings.Contains(c.Body, want) {
			t.Errorf("the card's body lacks %q:\n%s", want, c.Body)
		}
	}
	q1 := a.lastID()
	doc := w.progress(t)
	var item map[string]any
	for _, it := range doc.NeedsYou.Items {
		if it["kind"] == "blocked_task" {
			item = it
		}
	}
	action, _ := item["action"].(map[string]any)
	blocks, _ := item["blocks"].(map[string]any)
	if item == nil || item["id"] != "question:"+q1 || item["task_id"] != impl || item["mission_id"] != "M-3" || action["question_id"] != q1 ||
		fmt.Sprint(action["options"]) != fmt.Sprint([]string{store.BlockedRetry, store.BlockedDrop}) || fmt.Sprint(blocks["missions"]) != "[M-3]" ||
		blocks["why"] != "deterministic failed in round 2 of 2" {
		t.Fatalf("needs_you %v; want the blocked task on its card", doc.NeedsYou.Items)
	}

	// Open: nothing moves. One more round: ready, round 3 of 3.
	if n, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 0 || w.state(t, "task", impl) != "blocked" {
		t.Fatalf("an open card decided %d (%v)", n, err)
	}
	a.answer(q1, store.QuestionAnswered, store.CardApplyKey)
	if n, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", impl) != "ready" {
		t.Fatalf("one more round: decided %d (%v), task %s", n, err, w.state(t, "task", impl))
	}
	if r, m := w.rounds(t, impl); r != 3 || m != 3 {
		t.Fatalf("round %d of %d; want 3 of 3", r, m)
	}
	if o, ans, _ := w.closedCard(t, q1); o != store.OutcomeRetried || ans != store.CardApplyKey {
		t.Fatalf("the card closed %q with %q", o, ans)
	}
	var seen int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'question_seen' AND body_json->>'question' = $1
	  AND body_json->>'option' = 'a'`, q1).Scan(&seen))
	if seen != 1 || a.acked[q1] != 1 {
		t.Fatalf("the answer kept %d times, acknowledged %d", seen, a.acked[q1])
	}
	if n, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 0 || a.acked[q1] != 1 {
		t.Fatalf("a second pass decided %d (%v), acknowledged %d", n, err, a.acked[q1])
	}
	if doc = w.progress(t); len(doc.NeedsYou.Items) != 0 {
		t.Fatalf("needs_you %v after the answer", doc.NeedsYou.Items)
	}

	// Round 3 fails too: a new block, a new card.
	if did := w.failRound(t, impl, "h5", "h6"); did != "blocked" {
		t.Fatalf("round 3 did %q", did)
	}
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || len(a.cards) != 2 || a.cards[1].Key == c.Key {
		t.Fatalf("blocked again: asked %d (%v), cards %d", n, err, len(a.cards))
	}
	q2 := a.lastID()
	if body := a.cards[1].Body; !strings.Contains(body, "card 1 for this block") || !strings.Contains(body, "round 4 of 4") {
		t.Fatalf("the second block's card:\n%s", body)
	}

	// None of these: the card closes, the task stays blocked, the next card
	// carries his words.
	a.answers[q2] = store.Question{ID: q2, Status: store.QuestionAnswered, Option: "none", Text: "read the review first",
		AnsweredBy: "owner", AnsweredAt: "2026-10-19T21:00:00Z"}
	if n, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", impl) != "blocked" {
		t.Fatalf("none of these: decided %d (%v), task %s", n, err, w.state(t, "task", impl))
	}
	if o, ans, text := w.closedCard(t, q2); o != store.OutcomeNone || ans != "none" || text != "read the review first" {
		t.Fatalf("the card closed %q %q %q", o, ans, text)
	}
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || len(a.cards) != 3 {
		t.Fatalf("after none of these: asked %d (%v)", n, err)
	}
	q3 := a.lastID()
	if body := a.cards[2].Body; !strings.Contains(body, "card 2 for this block") || !strings.Contains(body, `"read the review first"`) {
		t.Fatalf("the card after none of these:\n%s", body)
	}

	// Not doing it: cancelled, and the mission finishes without it.
	a.answer(q3, store.QuestionAnswered, store.CardRefuseKey)
	if n, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", impl) != "cancelled" {
		t.Fatalf("not doing it: decided %d (%v), task %s", n, err, w.state(t, "task", impl))
	}
	var cancel string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'cancelled'`, impl).Scan(&cancel))
	if cancel != "the Owner chose not to do it (question "+q3+")" {
		t.Fatalf("the cancel's reason %q", cancel)
	}
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
		t.Fatalf("a dropped task asked %d (%v)", n, err)
	}
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || !slices.Equal(closed, []string{"M-3"}) || w.state(t, "mission", "M-3") != "done" {
		t.Fatalf("closed %v (%v), M-3 %s", closed, err, w.state(t, "mission", "M-3"))
	}
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE mission_id = 'M-3' AND event_type = 'mission_state_changed'
	  ORDER BY event_id DESC LIMIT 1`).Scan(&reason))
	if !strings.HasSuffix(reason, "or was dropped by the Owner on its card (1)") {
		t.Fatalf("the closing reason %q", reason)
	}
	for _, q := range []string{q1, q2, q3} {
		if a.acked[q] != 1 {
			t.Errorf("question %s acknowledged %d times", q, a.acked[q])
		}
	}
	w.chainHolds(t, "mission:M-3")
}

// Which blocked tasks get a card, and what one more round is for each: a
// task blocked because what it depends on was dropped gets one and runs
// without it, in the same round; an external task gets none while its lost
// attempt's end is not confirmed, then one; a brief and a task of a mission
// that ended get none. Any other cancel holds a mission open; a mission whose
// only work the Owner dropped is stopped, not done.
func TestBlockedCardCoverage(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for _, m := range []string{"M-3", "M-4", "M-5", "M-6", "M-7"} {
		w.goalMission(t, m)
	}
	block := func(id string) {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
	}
	// M-3: a depends on b; b fails its last round.
	b := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	dep := w.proposed(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.AddDependency(ctx, tx, "controller", "M-3", dep, b)
		return err
	}))
	block(b)
	// M-4: a brief blocked. M-5: a task blocked, then the mission stopped.
	brief := w.ready(t, store.Task{MissionID: "M-4", Role: "brief", Effect: "none", OffPlanReason: "a message"})
	block(brief)
	ended := w.ready(t, store.Task{MissionID: "M-5", OffPlanReason: "a goal"})
	block(ended)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-5", "stopped", "test") }))
	// M-1: an external task whose attempt was lost.
	external := w.ready(t, store.Task{Effect: "external"})
	lost := w.claimExternal(t, "h3", -store.Grace-time.Second, codex)
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if lost.TaskID != external || w.state(t, "task", external) != "blocked" {
		t.Fatalf("the external task is %s", w.state(t, "task", external))
	}

	a := &asker{}
	n, err := store.AskBlocked(ctx, w.rt, a.ask)
	if err != nil || n != 1 || a.cards[0].TaskID != b {
		t.Fatalf("asked %d (%v) %+v; want b's card only", n, err, a.cards)
	}
	if !strings.Contains(a.cards[0].Body, "round 2 of 2") {
		t.Fatalf("b failed round 1 of 2 as the test blocks it; one more round is round 2:\n%s", a.cards[0].Body)
	}
	_, err = store.Ended(ctx, w.rt, lost.AttemptID, "h3", "host_reboot", true)
	ok(t, err)
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[1].TaskID != external {
		t.Fatalf("after the end is confirmed: asked %d (%v)", n, err)
	}
	if !strings.Contains(a.cards[1].Body, "it goes back to ready, where it waits: no host takes external work yet") {
		t.Fatalf("an external task's card:\n%s", a.cards[1].Body)
	}

	// b dropped: its dependent is blocked at dispatch and gets a card; one
	// more round readies it in its first round.
	a.answer("q_a", store.QuestionAnswered, store.CardRefuseKey)
	_, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	if _, err = store.Dispatch(ctx, w.rt); err != nil || w.state(t, "task", dep) != "blocked" {
		t.Fatalf("dispatch: %v; the dependent is %s", err, w.state(t, "task", dep))
	}
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[2].TaskID != dep ||
		!strings.Contains(a.cards[2].Body, "Why: dependency "+b+" is cancelled") {
		t.Fatalf("the dependent's card: asked %d (%v) %+v", n, err, a.cards)
	}
	a.answer("q_c", store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	if r, m := w.rounds(t, dep); w.state(t, "task", dep) != "ready" || r != 1 || m != 2 {
		t.Fatalf("the dependent is %s in round %d of %d; want ready, 1 of 2", w.state(t, "task", dep), r, m)
	}

	// M-6: one implementation accepted, another cancelled with no card: open.
	// M-7: its only task, the plan, dropped on its card: stopped.
	w.accept(t, w.ready(t, store.Task{MissionID: "M-6", OffPlanReason: "a goal"}))
	other := w.ready(t, store.Task{MissionID: "M-6", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", other, "cancelled") }))
	plan := w.ready(t, store.Task{MissionID: "M-7", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	block(plan)
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[3].TaskID != plan {
		t.Fatalf("the plan's card: asked %d (%v)", n, err)
	}
	a.answer("q_d", store.QuestionAnswered, store.CardRefuseKey)
	_, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	closed, err := store.CloseMissions(ctx, w.rt)
	if err != nil || !slices.Equal(closed, []string{"M-7"}) {
		t.Fatalf("closed %v (%v); want M-7 only", closed, err)
	}
	for m, want := range map[string]string{"M-3": "active", "M-4": "active", "M-6": "active", "M-7": "stopped"} {
		if got := w.state(t, "mission", m); got != want {
			t.Errorf("%s is %s, want %s", m, got, want)
		}
	}
	if n, err = store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 || len(a.cards) != 4 {
		t.Fatalf("a brief or an ended mission's task asked %d (%v)", n, err)
	}
}

// A planning task refused in its last round goes again with the refusal in
// its spec, as a rework round does, in a round more.
func TestBlockedPlanOneMoreRound(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	var s cards
	id := w.handIn(t, "no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "rework" || err != nil {
		t.Fatalf("round 1: %q %v", did, err)
	}
	w.work(t, id, "still no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "blocked" || err != nil {
		t.Fatalf("round 2: %q %v", did, err)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || !strings.Contains(a.cards[0].Body, "round 3 of 3") {
		t.Fatalf("asked %d (%v) %+v", n, err, a.cards)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", id) != "ready" {
		t.Fatalf("decided %d (%v), plan %s", n, err, w.state(t, "task", id))
	}
	if r, m := w.rounds(t, id); r != 3 || m != 3 {
		t.Fatalf("round %d of %d", r, m)
	}
	var text string
	ok(t, w.rt.QueryRow(ctx, `SELECT convert_from(b.body, 'UTF8')::jsonb->>'text' FROM agentrt.task t
	  JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id WHERE t.task_id = $1`, id).Scan(&text))
	if !strings.Contains(text, "## Round 3\n\nThe plan you handed in before was not used: the report has no ```json block\n") {
		t.Fatalf("the spec of round 3:\n%s", text)
	}
	w.work(t, id, goodPlan, "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw); did != "accepted" || err != nil {
		t.Fatalf("round 3 with a plan: %q %v", did, err)
	}
}

// Two passes at once make one card and act on one answer once.
func TestBlockedCardOnce(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, impl) }))
	a := &asker{}
	pool := w.db.Pool(t, w.db.Runtime)
	race := func(pass func() (int, error)) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		total := 0
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				n, err := pass()
				ok(t, err)
				mu.Lock()
				total += n
				mu.Unlock()
			}()
		}
		wg.Wait()
		return total
	}
	if n := race(func() (int, error) { return store.AskBlocked(ctx, pool, a.ask) }); n != 1 {
		t.Fatalf("two passes asked %d cards", n)
	}
	var opened int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_wait_opened' AND task_id = $1`, impl).Scan(&opened))
	if opened != 1 {
		t.Fatalf("%d cards recorded", opened)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n := race(func() (int, error) { return store.DecideBlocked(ctx, pool, a.question, nil) }); n != 1 {
		t.Fatalf("two passes decided %d", n)
	}
	var moves int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1
	  AND body_json->>'from' = 'blocked'`, impl).Scan(&moves))
	if moves != 1 || w.state(t, "task", impl) != "ready" {
		t.Fatalf("%d moves out of blocked; task %s", moves, w.state(t, "task", impl))
	}
}

// The Owner's words on a "none of these" go on the next card of the same
// block, not on a card of a later block.
func TestBlockedCardWordsStayWithTheirBlock(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	block := func() {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, impl) }))
	}
	a := &asker{answers: map[string]store.Question{}}
	ask := func(want int) string {
		t.Helper()
		if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || len(a.cards) != want {
			t.Fatalf("asked %d (%v), %d cards; want card %d", n, err, len(a.cards), want)
		}
		return a.cards[want-1].Body
	}
	block()
	ask(1)
	a.answers[a.lastID()] = store.Question{ID: a.lastID(), Status: store.QuestionAnswered, Option: "none", Text: "old words",
		AnsweredBy: "owner", AnsweredAt: "2026-10-19T21:00:00Z"}
	_, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	if body := ask(2); !strings.Contains(body, `"old words"`) {
		t.Fatalf("the same block's next card lacks his words:\n%s", body)
	}
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	block()
	if body := ask(3); strings.Contains(body, "old words") || !strings.Contains(body, "card 1 for this block") {
		t.Fatalf("a later block's card:\n%s", body)
	}
}

// Review 420 A2: a card whose reason and Owner's words are long, with no
// report left to cut, still fits the question store; the reason, words and
// title are cut in turn, and what each answer does is kept whole.
func TestBlockedCardFitsTheQuestionStore(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ title, reason, words, report string }{
		"no report":  {strings.Repeat("修", 60), "plan not used: " + strings.Repeat("🚧", 480), strings.Repeat("修", 300), ""},
		"all of it":  {strings.Repeat("修", 200), strings.Repeat("🚧", 500), strings.Repeat(`"`, 300), strings.Repeat("報", 4000)},
		"only words": {"t", "review failed in round 2 of 2", strings.Repeat("🚧", 300), strings.Repeat("報", 1000)},
	} {
		body := store.BlockedCardBodyForTest("plan", c.title, c.reason, c.words, c.report)
		if n := questions.BodyBytes(body); n > questions.BodyLimit {
			t.Errorf("%s: %d bytes, more than the question store's %d", name, n, questions.BodyLimit)
		}
		for _, want := range []string{store.BlockedRetry + ": it goes back to ready for round 3 of 3, and a host takes it as usual.",
			store.BlockedDrop + ": the task is cancelled; mission M-9 finishes once every other task is accepted"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the card lacks %q:\n%s", name, want, body)
			}
		}
	}
	short := store.BlockedCardBodyForTest("implement", "t", "review failed in round 2 of 2", "read it", "a short report")
	if strings.Contains(short, "…") || strings.Contains(short, "(cut;") || !strings.Contains(short, `none of these: "read it"`) ||
		!strings.Contains(short, "a short report") {
		t.Errorf("a short card was cut:\n%s", short)
	}
}

// Review 420 B1: an external task whose attempt ended with its process group
// not cleared gets no card (the old process may still act); one whose end
// cleared the group gets a card that says no host takes it yet.
func TestBlockedExternalNeedsAClearedEnd(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	uncleared := w.ready(t, store.Task{Effect: "external"})
	e := w.claimExternal(t, "h2", store.LeaseTTL, codex)
	_, err := store.Ended(ctx, w.rt, e.AttemptID, "h2", "daemon_restart", false)
	ok(t, err)
	if e.TaskID != uncleared || w.state(t, "task", uncleared) != "blocked" {
		t.Fatalf("the external task is %s", w.state(t, "task", uncleared))
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
		t.Fatalf("an uncleared end asked %d (%v)", n, err)
	}
	cleared := w.ready(t, store.Task{Effect: "external"})
	c := w.claimExternal(t, "h3", store.LeaseTTL, codex)
	_, err = store.Ended(ctx, w.rt, c.AttemptID, "h3", "daemon_restart", true)
	ok(t, err)
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[0].TaskID != cleared ||
		!strings.Contains(a.cards[0].Body, "where it waits: no host takes external work yet") {
		t.Fatalf("a cleared end: asked %d (%v) %+v", n, err, a.cards)
	}
}

// Review 420 B2: a card is made, and an answer is used, under the mission's
// lock: a stop waits for them, so neither lands on a mission that stopped
// in between. Here the card is being asked when the stop comes.
func TestBlockedCardAskedUnderTheMission(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, impl) }))
	pool := w.db.Pool(t, w.db.Runtime)
	inside, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	a := &asker{}
	slow := func(ctx context.Context, c store.Card) (string, error) {
		close(inside)
		<-release
		return a.ask(ctx, c)
	}
	asked := make(chan error, 1)
	go func() {
		_, err := store.AskBlocked(ctx, pool, slow)
		asked <- err
	}()
	select {
	case <-inside:
	case <-time.After(10 * time.Second):
		t.Fatal("the card was never asked")
	}
	stopped := make(chan error, 1)
	go func() {
		stopped <- store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-3", "stopped", "test") })
	}()
	w.waitFor(t, "agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE", time.Now().Add(10*time.Second))
	released.Do(func() { close(release) })
	ok(t, <-asked)
	ok(t, <-stopped)
	var opened, stop int64
	ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT max(event_id) FROM agentrt.event WHERE event_type = 'owner_wait_opened' AND task_id = $1),
	  (SELECT max(event_id) FROM agentrt.event WHERE event_type = 'mission_state_changed' AND mission_id = 'M-3' AND body_json->>'to' = 'stopped')`,
		impl).Scan(&opened, &stop))
	if opened == 0 || stop < opened {
		t.Fatalf("the card (event %d) and the stop (event %d): the stop went first", opened, stop)
	}
}

// Review 420 B2, the answer's side: a stop that comes while an answer is
// used waits for it. The test holds the lock the answer's copy is kept under
// (question_seen), which the pass takes after the mission's.
func TestBlockedCardDecidesUnderTheMission(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, impl) }))
	a := &asker{}
	_, err := store.AskBlocked(ctx, w.rt, a.ask)
	ok(t, err)
	q := a.lastID()
	a.answer(q, store.QuestionAnswered, store.CardApplyKey)
	pool := w.db.Pool(t, w.db.Runtime)
	holder, err := pool.Begin(ctx)
	ok(t, err)
	t.Cleanup(func() { _ = holder.Rollback(ctx) })
	_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock(742104, hashtext($1))`, `question_seen|{"question":"`+q+`"}`)
	ok(t, err)
	decided := make(chan error, 1)
	go func() {
		_, derr := store.DecideBlocked(ctx, pool, a.question, nil)
		decided <- derr
	}()
	w.waitFor(t, "pg_advisory_xact_lock(742104", time.Now().Add(10*time.Second))
	stopped := make(chan error, 1)
	go func() {
		stopped <- store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-3", "stopped", "test") })
	}()
	w.waitFor(t, "agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE", time.Now().Add(10*time.Second))
	ok(t, holder.Rollback(ctx))
	ok(t, <-decided)
	ok(t, <-stopped)
	if o, _, _ := w.closedCard(t, q); o != store.OutcomeRetried || w.state(t, "task", impl) != "ready" || w.state(t, "mission", "M-3") != "stopped" {
		t.Fatalf("the card closed %q; task %s, mission %s", o, w.state(t, "task", impl), w.state(t, "mission", "M-3"))
	}
}

// A task whose card cannot be made does not hold up the ones after it. The
// page it lowers gives every pass the same result, so other tests may run.
func TestBlockedCardsPagePastAFailure(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	ids := []string{w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"}), w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})}
	for _, id := range ids {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
	}
	slices.Sort(ids)
	defer store.BlockedPageForTest(1)()
	a := &asker{}
	refuse := func(ctx context.Context, c store.Card) (string, error) {
		if c.TaskID == ids[0] {
			return "", errors.New("the question store refused this one")
		}
		return a.ask(ctx, c)
	}
	n, err := store.AskBlocked(ctx, w.rt, refuse)
	if n != 1 || err == nil || !strings.Contains(err.Error(), ids[0]) || len(a.cards) != 1 || a.cards[0].TaskID != ids[1] {
		t.Fatalf("asked %d (%v) %+v; want the second task's card and the first's error", n, err, a.cards)
	}
}

// One more round, then the work passes: the third round is verified and
// reviewed on its own attempt, the task is accepted and its mission is done.
func TestBlockedRetryThenAccepted(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.failRound(t, impl, "h1", "h2")
	if did := w.failRound(t, impl, "h3", "h4"); did != "blocked" {
		t.Fatalf("round 2 did %q", did)
	}
	a := &asker{}
	_, err := store.AskBlocked(ctx, w.rt, a.ask)
	ok(t, err)
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	third := w.run(t, "h5", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if pin := w.checkSpec(t, v); third.TaskID != impl || third.AttemptNo != 3 || pin["attempt"] != third.AttemptID || fmt.Sprint(pin["round"]) != "3" {
		t.Fatalf("round 3: claim %+v, check pins %v", third, pin)
	}
	w.run(t, "h6", deterministic, "done")
	if did, cerr := store.Conclude(ctx, w.rt, v); cerr != nil || did != "review" {
		t.Fatalf("round 3's verification: %q %v", did, cerr)
	}
	review := w.checker(t, impl, "review")
	w.run(t, "h7", codex, "done")
	if did, cerr := store.Conclude(ctx, w.rt, review); cerr != nil || did != "accepted" || w.state(t, "task", impl) != "accepted" {
		t.Fatalf("round 3's review: %q %v; task %s", did, cerr, w.state(t, "task", impl))
	}
	if closed, cerr := store.CloseMissions(ctx, w.rt); cerr != nil || !slices.Equal(closed, []string{"M-3"}) || w.state(t, "mission", "M-3") != "done" {
		t.Fatalf("closed %v %v", closed, cerr)
	}
}
