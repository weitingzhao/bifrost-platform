package store_test

import (
	"context"
	"errors"
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
	v, err := startVerify(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, checker, deterministic, "failed")
	did, err := store.ConcludeWith(ctx, w.rt, v, verifyMode)
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

// returned is what done sends, or the test fails after 30 seconds rather
// than waiting for go test's own timeout.
func returned(t *testing.T, done <-chan error) error {
	t.Helper()
	err, ok := within(done, 30*time.Second)
	if !ok {
		t.Fatal("a call the test waits for did not return in 30 seconds")
	}
	return err
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

// A planning task refused in its last round goes again with the refusal in
// its spec, as a rework round does, in a round more.
func TestBlockedPlanOneMoreRound(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	var s cards
	id := w.handIn(t, "no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw, nil); did != "rework" || err != nil {
		t.Fatalf("round 1: %q %v", did, err)
	}
	w.work(t, id, "still no plan", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw, nil); did != "blocked" || err != nil {
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
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw, nil); did != "accepted" || err != nil {
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
		for _, want := range []string{"Recommended: Close mission M-9 — " + store.BlockedCardWhyForTest + "\n\n",
			store.BlockedRetry + ": it goes back to ready for round 3 of 3, and a host takes it as usual.",
			store.BlockedDrop + ": the task is cancelled; mission M-9 finishes once every other task is accepted",
			"Close mission M-9: every task of mission M-9 that has not ended is cancelled"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the card lacks %q:\n%s", name, want, body)
			}
		}
	}
	// Review 421 B3: a repository is any name a proposal gave; C3: words cut
	// to nothing say where they are, and quote nothing.
	if body := store.BlockedCardBodyInForTest(strings.Repeat("r", 3000), "implement", "t", "dependency t_01M5B8BF2MZ2577S8MWMF0EJVQ is cancelled",
		"", ""); questions.BodyBytes(body) > questions.BodyLimit {
		t.Errorf("a long repository: %d bytes", questions.BodyBytes(body))
	}
	gone := store.BlockedCardBodyForTest("plan", strings.Repeat("修", 200), strings.Repeat("🚧", 500), strings.Repeat("修", 300), "")
	if !strings.Contains(gone, "none of these (your words are on that card)") || strings.Contains(gone, `none of these: "`) {
		t.Errorf("words cut to nothing:\n%s", gone)
	}
	short := store.BlockedCardBodyForTest("implement", "t", "review failed in round 2 of 2", "read it", "a short report")
	if strings.Contains(short, "…") || strings.Contains(short, "(cut;") || !strings.Contains(short, `none of these: "read it"`) ||
		!strings.Contains(short, "a short report") {
		t.Errorf("a short card was cut:\n%s", short)
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
	ok(t, returned(t, asked))
	ok(t, returned(t, stopped))
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
	ok(t, returned(t, decided))
	ok(t, returned(t, stopped))
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
