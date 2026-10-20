package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// closingCard is mission M-3 with one blocked task whose card the Owner
// answered "Close mission M-3"; it returns the card's question.
func (w *world) closingCard(t *testing.T, a *asker) string {
	t.Helper()
	blocked := w.proposed(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, blocked) }))
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	q := a.lastID()
	a.answer(q, store.QuestionAnswered, store.CardCloseKey)
	return q
}

// lockedElsewhere holds task id FOR NO KEY UPDATE in a transaction of its
// own until release is closed; then it runs then (in that transaction) and
// commits. done gets its error.
func (w *world) lockedElsewhere(t *testing.T, id string, release <-chan struct{}, then func(pgx.Tx) error) (locked <-chan struct{}, done <-chan error) {
	t.Helper()
	pool := w.db.Pool(t, w.db.Runtime)
	l, d := make(chan struct{}), make(chan error, 1)
	go func() {
		d <- store.InTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE`, id); err != nil {
				return err
			}
			close(l)
			<-release
			return then(tx)
		})
	}()
	return l, d
}

// closeTries counts the close's tries of mission M-3 (the hook at its first
// lock); unhook removes it. Serial tests only.
func closeTries() (*atomic.Int32, func()) {
	var n atomic.Int32
	return &n, store.SetHook(func(point, arg string) {
		if point == "close: attempts locked" && arg == "M-3" {
			n.Add(1)
		}
	})
}

// stalled waits until the close of M-3 waits for a task's lock (it did
// before review 439) or has started again (it does not wait now).
func (w *world) stalled(t *testing.T, tries *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'
		  AND position('ORDER BY subject_task_id IS NOT NULL' in query) > 0`).Scan(&waiting))
		if waiting > 0 || tries.Load() >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the close neither waited nor started again; the case proves nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Review 439 A1: a task made in the mission while the close read its tasks
// (a check StartVerification makes under the work's lock, then commits) is
// cancelled with the rest, not left ready in a mission that ended.
// Serial: it holds the package's test hook (SetHook).
func TestCloseMissionTakesALateTask(t *testing.T) {
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	work := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	a := &asker{}
	w.closingCard(t, a)
	late := store.NewTaskID()
	release := make(chan struct{})
	locked, made := w.lockedElsewhere(t, work, release, func(tx pgx.Tx) error {
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: late, MissionID: "M-3", OffPlanReason: "a goal", Role: "implement",
			Effect: "branch", Repo: "bifrost-platform", Class: "go", Title: "t", SpecArtifact: w.spec}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", late)
	})
	<-locked
	tries, unhook := closeTries()
	defer unhook()
	pool := w.db.Pool(t, w.db.Runtime)
	decided := make(chan error, 1)
	go func() {
		_, err := store.DecideBlocked(ctx, pool, a.question, nil)
		decided <- err
	}()
	w.stalled(t, tries)
	close(release)
	ok(t, returned(t, made))
	ok(t, returned(t, decided))
	if s := w.state(t, "task", late); s != "cancelled" || w.state(t, "mission", "M-3") != "stopped" {
		t.Fatalf("the late task is %s, M-3 %s; want cancelled, stopped", s, w.state(t, "mission", "M-3"))
	}
}

// Review 439 A2: Dispatch takes a dependent, then what it waits for; the
// close takes a mission's tasks in id order. With the dependent held and
// what it waits for wanted next, neither waits for ever or is ended as a
// deadlock: the close does not wait for a task's lock, and starts again.
// Serial: it holds the package's test hook (SetHook).
func TestCloseMissionNeverDeadlocksWithDispatch(t *testing.T) {
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	first := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	time.Sleep(2 * time.Millisecond)
	dependent := w.proposed(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	if dependent < first {
		t.Skip("the dependent's id sorts first; the order this case needs did not happen")
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.AddDependency(ctx, tx, "controller", "M-3", dependent, first)
		return err
	}))
	a := &asker{}
	w.closingCard(t, a)
	release := make(chan struct{})
	locked, dispatched := w.lockedElsewhere(t, dependent, release, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR SHARE`, first)
		return err
	})
	<-locked
	tries, unhook := closeTries()
	defer unhook()
	pool := w.db.Pool(t, w.db.Runtime)
	decided := make(chan error, 1)
	go func() {
		_, err := store.DecideBlocked(ctx, pool, a.question, nil)
		decided <- err
	}()
	w.stalled(t, tries)
	close(release)
	if err := returned(t, dispatched); err != nil {
		t.Fatalf("the dispatch: %v", err)
	}
	if err := returned(t, decided); err != nil {
		t.Fatalf("the close: %v", err)
	}
	if w.state(t, "mission", "M-3") != "stopped" || w.state(t, "task", first) != "cancelled" || w.state(t, "task", dependent) != "cancelled" {
		t.Fatalf("M-3 %s, tasks %s %s", w.state(t, "mission", "M-3"), w.state(t, "task", first), w.state(t, "task", dependent))
	}
}

// Review 439 A1: a task proposed into a mission after its close committed
// (a proposal reads the mission without its lock) is cancelled by the next
// pass; one of a mission that ended any other way is left as it is.
func TestCancelAfterClose(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.goalMission(t, "M-4")
	a := &asker{}
	q := w.closingCard(t, a)
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, nil); err != nil || n != 1 || w.state(t, "mission", "M-3") != "stopped" {
		t.Fatalf("decided %d (%v), M-3 %s", n, err, w.state(t, "mission", "M-3"))
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-4", "stopped", "test") }))
	late := w.proposed(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	other := w.proposed(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
	if n, err := store.CancelAfterClose(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("cancelled %d (%v)", n, err)
	}
	if s, o := w.state(t, "task", late), w.state(t, "task", other); s != "cancelled" || o != "proposed" {
		t.Fatalf("the late task is %s, M-4's %s", s, o)
	}
	if r := w.lastMove(t, late); r != "the Owner closed mission M-3 on a card (question "+q+"); this task came after the close" {
		t.Fatalf("cancelled because %q", r)
	}
	if n, err := store.CancelAfterClose(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a second pass cancelled %d (%v)", n, err)
	}
}

// Review 439 A4: a card closes the mission it was asked in, whose name its
// option carries. Its task moved to another mission since (a plan change
// rebinds its step): the card is withdrawn while open, an answer on it moves
// nothing, and the next card names the mission the task is in.
func TestCloseNamesTheCardsMission(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	other := w.ready(t, store.Task{MissionID: "M-2", OffPlanReason: "a goal"})
	moved := w.ready(t, store.Task{}) // on step s1 of M-1
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, moved) }))
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[0].Options[2] != "Close mission M-1" {
		t.Fatalf("asked %d (%v) %+v", n, err, a.cards)
	}
	q := a.lastID()
	a.answer(q, store.QuestionAnswered, store.CardCloseKey)
	plan := &asker{}
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, plan.ask)
	ok(t, err)
	plan.answer(plan.lastID(), store.QuestionAnswered, store.CardApplyKey)
	if n, err := store.DecidePlanChanges(ctx, w.rt, plan.question, nil); err != nil || n != 1 {
		t.Fatalf("the rebind: %d %v", n, err)
	}
	var now string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, moved).Scan(&now))
	if now != "M-2" {
		t.Fatalf("the task is in %s after the rebind", now)
	}
	var wd withdrawals
	open := func(ctx context.Context, id string) (store.Question, bool) {
		return store.Question{ID: id, Status: "open", Options: 3}, true
	}
	if n, err := store.WithdrawStaleCards(ctx, w.rt, open, wd.withdraw); err != nil || n != 1 ||
		wd.why[0] != "its task is in mission M-2 now; a card naming that mission takes its place" {
		t.Fatalf("withdrew %d (%v) %v", n, err, wd.why)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if o, _, _ := w.closedCard(t, q); o != store.OutcomeMoot || w.state(t, "mission", "M-1") != "active" || w.state(t, "mission", "M-2") != "active" ||
		w.state(t, "task", other) != "ready" || w.state(t, "task", moved) != "blocked" {
		t.Fatalf("the card closed %q; M-1 %s, M-2 %s, M-2's tasks %s %s", o, w.state(t, "mission", "M-1"), w.state(t, "mission", "M-2"),
			w.state(t, "task", other), w.state(t, "task", moved))
	}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[1].Options[2] != "Close mission M-2" {
		t.Fatalf("asked %d (%v) %v", n, err, a.cards[len(a.cards)-1].Options)
	}
}

// Review 439 C2: a card whose task has left the block it is about closes
// nothing, as one more round and not doing it go.
func TestCloseFromACardWhoseTaskMovedOn(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	a := &asker{}
	q := w.closingCard(t, a)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTaskForTest(ctx, tx, "controller", a.cards[0].TaskID, "ready") }))
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, nil); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if o, _, _ := w.closedCard(t, q); o != store.OutcomeMoot || w.state(t, "mission", "M-3") != "active" || w.state(t, "task", a.cards[0].TaskID) != "ready" {
		t.Fatalf("the card closed %q; M-3 %s", o, w.state(t, "mission", "M-3"))
	}
}

// Review 439 B1: the card fits the question store with every part at its
// longest, in characters JSON escapes (a quote or a backslash is two bytes;
// the store does not escape HTML, so & is one): a failed command's line
// (never cut), an output's last line, a repository, title, reason and words,
// the longest budget sentence, a recommendation and three answers. And
// whatever is never cut, a part of the quote too long for any card leaves
// the report unquoted rather than a card the store refuses on every pass.
func TestBlockedCardFitsWithEverything(t *testing.T) {
	t.Parallel()
	bs := strings.Repeat("\\", 300)
	report, err := json.Marshal(map[string]any{"commands": []map[string]any{{"argv": []string{"sh", "-c", bs}, "exit_code": 1,
		"tail": strings.Repeat("\\", 100)}}})
	ok(t, err)
	budget := "One round needs up to 3 attempts (implementation, verification, review); mission M-3 has used 1000000 of its 1000000 attempts " +
		"and 123456.7 of its 100000 hours, so this answer also raises its budget to 1000003 attempts and 223457 hours, recorded as yours."
	why := strings.Repeat("w", store.RecommendWhyMax)
	for _, words := range []string{"", bs} {
		body := store.BlockedCardBodyFullForTest(bs, "deterministic", string(report), bs, bs, words, budget, why)
		if n := questions.BodyBytes(body); n > questions.BodyLimit || !strings.Contains(body, "First failed command: sh -c") {
			t.Fatalf("%d bytes (the store takes %d):\n%s", n, questions.BodyLimit, body)
		}
	}
	body := store.BlockedCardBodyLeadForTest("First failed command: "+strings.Repeat("\"", 1500)+" exited 1.\nIts output ends:", budget, why)
	if n := questions.BodyBytes(body); n > questions.BodyLimit || !strings.Contains(body, "Its report is too long to quote here") ||
		strings.Contains(body, "First failed command") {
		t.Fatalf("%d bytes (the store takes %d):\n%s", n, questions.BodyLimit, body)
	}
	for _, want := range []string{"Recommended: Close mission M-3 — " + why, budget, "Close mission M-3: every task of mission M-3"} {
		if !strings.Contains(body, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
}

// Review 439 C1: the card after a "none of these" has three answers, and
// says to choose one of them.
func TestNoneOfTheseNamesNoCount(t *testing.T) {
	t.Parallel()
	body := store.BlockedCardBodyForTest("implement", "t", "review failed in round 2 of 2", "read it", "")
	if !strings.Contains(body, "choose one of the options.") || strings.Contains(body, "of the two") {
		t.Fatalf("the card:\n%s", body)
	}
}
