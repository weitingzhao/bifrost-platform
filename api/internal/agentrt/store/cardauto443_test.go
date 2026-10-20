package store_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// onStep makes M-1's step s1 hold a blocked task whose card recommends want:
// a (M-1 three hours old on two: only the budget stopped it) or b (its checks
// failed in its last round); M-2 is active and needs the Owner. It returns
// the task and its card, answered by the rule.
func (w *world) onStep(t *testing.T, s *cardStore, want string) (string, string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	w.legacy(t, `UPDATE agentrt.mission SET scope_json = scope_json || '{"needs_owner": "the walk tests his buttons"}' WHERE mission_id = 'M-2'`)
	task := w.ready(t, store.Task{})
	if want == "a" {
		w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '3 hours',
		  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = 'M-1'`)
		if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
			t.Fatalf("blocked %d (%v)", n, err)
		}
	} else {
		w.failRound(t, task, "h1", "h2")
		w.failRound(t, task, "h3", "h4")
	}
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.order[0]
	s.aged(card, time.Hour)
	if got := s.cards[card].card.Recommended; got != want {
		t.Fatalf("the card recommends %q, want %q", got, want)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	return task, card
}

// Review 443 A1: a card the rule answered, then its task rebound to a
// mission that needs the Owner before the answer is used: one more round
// and not doing it are moot, as closing is; nothing of that mission moves,
// its budget included.
func TestRuleAnswerThenMoveIsMoot(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"a", "b"} {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			var s cardStore
			task, card := w.onStep(t, &s, want)
			if n, _ := w.rebind(t, w.rt, "k1"); n != 1 {
				t.Fatalf("the rebind applied %d", n)
			}
			before := w.budget(t, "M-2")
			if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
				t.Fatalf("decided %d (%v)", n, err)
			}
			if o, _, _ := w.closedCard(t, card); o != store.OutcomeMoot || w.state(t, "task", task) != "blocked" {
				t.Fatalf("the card closed %q; the task is %s", o, w.state(t, "task", task))
			}
			if after := w.budget(t, "M-2"); len(w.amended(t, "M-2")) != 0 || len(after) != len(before) {
				t.Fatalf("M-2's budget %v -> %v, amended %v", before, after, w.amended(t, "M-2"))
			}
			// Review 444 B1: the answer is noted once, on the mission the card
			// was asked in, though its task is in another now.
			var m1, all int
			ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FILTER (WHERE mission_id = 'M-1'), count(*) FROM agentrt.event WHERE event_type = 'note'
			  AND body_json->>'text' LIKE '% answered question ' || $1 || ' %'`, card).Scan(&m1, &all))
			if m1 != 1 || all != 1 {
				t.Fatalf("%d notes of the answer, %d on M-1", all, m1)
			}
		})
	}
}

// Review 443 A2: a later version stored ahead of time bounds the one in
// force: an answer that reaches the question store after the later version
// takes effect is not written.
// Serial: it waits for a second to pass.
func TestRuleAnswerEndsWhenALaterVersionTakesEffect(t *testing.T) {
	w := newKernelWorld(t)
	w.overBudget(t, "M-3", "")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	s.aged(s.order[0], 31*time.Minute)
	key := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(key.sign(w, ruleText(rules.KindCardAuto, 1, rules.Initial, since, until, "after_minutes: 30\nkinds: [blocked]\n"))))
	at := time.Now().UTC().Add(2 * time.Second).Truncate(time.Second)
	ok(t, firstErr(key.sign(w, ruleText(rules.KindCardAuto, 2, rules.Tighten, at.Format(time.RFC3339), until, "after_minutes: 60\nkinds: [blocked]\n"))))
	s.during = func(string) { time.Sleep(time.Until(at) + 100*time.Millisecond) }
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); n != 0 || err == nil || !strings.Contains(err.Error(), "no longer holds") {
		t.Fatalf("answered %d (%v); want none: v2 took effect before the answer was written", n, err)
	}
	if s.cards[s.order[0]].status != "open" {
		t.Fatalf("the card is %s", s.cards[s.order[0]].status)
	}
}

// Review 443 B1: a rule's answer whose note its transaction did not keep is
// noted by the decision that uses it, which also acknowledges it, so no scan
// of unacknowledged answers is needed for it.
func TestRuleNoteWrittenByTheDecision(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.overBudget(t, "M-3", "")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.order[0]
	// Answered by the rule in the question store; the database kept nothing.
	ok(t, s.answer(ctx, card, "c", "rule card-auto-answer v1", time.Now().Add(time.Hour)))
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	var notes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND mission_id = 'M-3'
	  AND body_json->>'text' = 'rule card-auto-answer v1 answered question ' || $1 || ' with c, the option it recommends'`, card).Scan(&notes))
	if notes != 1 || s.acked[card] != 1 || w.state(t, "mission", "M-3") != "stopped" {
		t.Fatalf("%d notes, acknowledged %d, M-3 %s", notes, s.acked[card], w.state(t, "mission", "M-3"))
	}
}

// Review 443 B2: two passes finishing the same answer write one note: the
// mission's stream is held from the look for a note to its writing.
// Serial: it holds the package's test hook (SetHook).
func TestRuleNoteOnceForTwoPasses(t *testing.T) {
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	plan := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	var s cardStore
	s.put(store.RuntimeCard{ID: "q_plannerquestion1", TaskID: plan, Recommended: "a", CreatedAt: time.Now().Add(-time.Hour)})
	s.cards["q_plannerquestion1"].status, s.cards["q_plannerquestion1"].option, s.cards["q_plannerquestion1"].by = store.QuestionAnswered, "a", "rule card-auto-answer v1"
	signCardAuto(t, w, "30", "[plan_question]")
	pool := w.db.Pool(t, w.db.Runtime)
	inside, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unhook := store.SetHook(func(point, arg string) {
		if point == "rule note: none yet" && arg == "q_plannerquestion1" {
			once.Do(func() { close(inside); <-release })
		}
	})
	defer unhook()
	first := make(chan error, 1)
	go func() { _, err := store.AnswerByRule(ctx, pool, s.rule()); first <- err }()
	<-inside
	second := make(chan error, 1)
	go func() { _, err := store.AnswerByRule(ctx, pool, s.rule()); second <- err }()
	w.waitFor(t, "agentrt.event_stream WHERE event_stream_id = $1 FOR UPDATE", time.Now().Add(10*time.Second))
	close(release)
	ok(t, returned(t, first))
	ok(t, returned(t, second))
	var notes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND body_json->>'text' LIKE '% answered question q_plannerquestion1 %'`).Scan(&notes))
	if notes != 1 {
		t.Fatalf("%d notes", notes)
	}
}

// Review 444 B2: a pass that wrote a rule's answer and has not noted it yet,
// and a later pass that finds the answer unacknowledged and notes it first:
// the answer is noted once.
func TestRuleNoteOnceWhenAPassFinishesFirst(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.overBudget(t, "M-3", "")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.order[0]
	s.aged(card, time.Hour)
	signCardAuto(t, w, "30", "[blocked]")
	pool := w.db.Pool(t, w.db.Runtime) // the later pass: the first's transaction is on w.rt
	var later error
	s.after = func(string) {
		s.after = nil
		_, later = store.AnswerByRule(ctx, pool, s.rule())
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 || later != nil {
		t.Fatalf("answered %d (%v); the later pass: %v", n, err, later)
	}
	var notes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND body_json->>'text' LIKE '% answered question ' || $1 || ' %'`,
		card).Scan(&notes))
	if notes != 1 {
		t.Fatalf("%d notes of the answer", notes)
	}
}
