package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-401 (STG 2026-10-21 M-79): the rule answered a card at 00:10:52.540 and
// the assessments pass, which had listed it open, still asked its assessment
// at 52.605, a paid run that could change nothing. A card is read again when
// its assessment's task is made.

// answeredAsks counts what the card's assessment left: events on its stream
// and missions assessing it.
func answeredAsks(t *testing.T, w *world, card string) (events, missions int) {
	t.Helper()
	stream := store.AssessStream(store.AssessCard, card)
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1`, stream).Scan(&events))
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.mission WHERE scope_json->>'assessment_of' = $1`, stream).Scan(&missions))
	return events, missions
}

// A card answered after it is listed and before its assessment's task is
// made gets no task; one the question store cannot read now gets none until
// it reads open.
func TestAnAnsweredCardIsNotAssessed(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	answered, unread := w.ledgerMission(t, "M-3"), w.ledgerMission(t, "M-4")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	// Both listed open, as the pass lists them before it reads their evidence.
	first, second := s.item(s.cardOf(t, answered)), s.item(s.cardOf(t, unread))
	ok(t, s.answer(ctx, first.ID, first.Recommended, "the Owner", time.Now().Add(time.Hour), ""))
	if task, err := store.GiveAssessment(ctx, w.rt, first, ratchetEvidence(), s.question); err != nil || task != "" {
		t.Fatalf("the answered card was asked: %q (%v)", task, err)
	}
	if events, missions := answeredAsks(t, w, first.ID); events != 0 || missions != 0 {
		t.Fatalf("the answered card left %d events and %d missions", events, missions)
	}
	unreadable := func(context.Context, string) (store.Question, bool) { return store.Question{}, false }
	if task, err := store.GiveAssessment(ctx, w.rt, second, ratchetEvidence(), unreadable); err != nil || task != "" {
		t.Fatalf("a card the store cannot read was asked: %q (%v)", task, err)
	}
	if task, err := store.GiveAssessment(ctx, w.rt, second, ratchetEvidence(), s.question); err != nil || task == "" {
		t.Fatalf("the open card was not asked once it reads open: %q (%v)", task, err)
	}
}

// The M-79 order: the rule answers while the pass gives the card's
// assessment. The rule holds the card's assessment stream from its read of
// the assessment to its answer's commit, so the ask waits for it, reads the
// card answered and makes nothing.
func TestTheRulesAnswerAndAnAskTakeTurns(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	m := w.ledgerMission(t, "M-3")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.cardOf(t, m)
	item := s.item(card) // listed open
	s.aged(card, 31*time.Minute)
	signCardAuto(t, w, "30", "[blocked]")
	type ask struct {
		task string
		err  error
	}
	asked := make(chan ask, 1)
	pass, probe := w.db.Connect(t, w.db.Runtime), w.db.Connect(t, w.db.Runtime) // the pass's connection, and one to look
	s.during = func(string) {
		go func() {
			task, err := store.GiveAssessment(ctx, pass, item, ratchetEvidence(), s.question)
			asked <- ask{task, err}
		}()
		// The ask waits on a lock the rule's transaction holds.
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			var waiting int
			if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
			  AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
				t.Error(err)
				return
			}
			if waiting > 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Error("the ask never waited for the rule")
				return
			}
		}
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	if a := <-asked; a.err != nil || a.task != "" {
		t.Fatalf("the ask after the rule's answer made %q (%v)", a.task, a.err)
	}
	if events, missions := answeredAsks(t, w, card); events != 0 || missions != 0 {
		t.Fatalf("the answered card left %d events and %d missions", events, missions)
	}
}

// Capability scoring skips an assessment asked of a card the runtime had
// recorded answered (as M-79 was, before the read again): its run counts for
// nobody; an assessment asked while its card was open counts.
func TestAnAssessmentOfAnAnsweredCardIsNotScored(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	open, early := w.ledgerMission(t, "M-3"), w.ledgerMission(t, "M-4")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s seenCards
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	reasons := func(task string) [][2]string {
		return [][2]string{{"任务停了一天多。", w.waitEvent(t, task)}, {"台账没有更多要做的。", "ledger:TD-378"}}
	}
	inTime, _ := w.askAndReady(t, s.item(s.cardOf(t, open)), ratchetEvidence())
	w.work(t, inTime, report("c", reasons(open)), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, inTime); err != nil || did != "assessed" {
		t.Fatalf("accept the one asked in time: %q %v", did, err)
	}
	// The rule answers both; then the old code asks the answered one.
	for _, id := range s.order {
		s.aged(id, 31*time.Minute)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 2 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	late, _ := w.askAndReady(t, s.item(s.cardOf(t, early)), ratchetEvidence())
	w.work(t, late, report("c", reasons(early)), "done")
	if did, err := store.AcceptBrief(ctx, w.rt, late); err != nil || did != "assessed" {
		t.Fatalf("accept the one asked after the answer: %q %v", did, err)
	}
	_, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	var attempts, firstPass int
	ok(t, w.rt.QueryRow(ctx, `SELECT attempts, first_pass FROM agentrt.capability_score WHERE job_id = 'chief_of_staff'
	  AND task_class = 'assessment' ORDER BY window_end DESC LIMIT 1`).Scan(&attempts, &firstPass))
	if attempts != 1 || firstPass != 1 {
		t.Fatalf("the seat's assessments scored %d attempts, %d first pass; want only the one asked in time", attempts, firstPass)
	}
}
