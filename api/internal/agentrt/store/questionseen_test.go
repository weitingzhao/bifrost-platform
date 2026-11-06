package store_test

import (
	"errors"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// question_seen keeps the option the card recommended when the reader gave
// it, open or answered, and leaves it out when it did not.
func TestQuestionSeenRecommendedBody(t *testing.T) {
	t.Parallel()
	q := answered("Q1", "a")
	q.Recommended = "b"
	body, err := store.QuestionSeenBodyForTest("Q1", q)
	ok(t, err)
	if body["recommended"] != "b" || body["option"] != "a" {
		t.Fatalf("body %v", body)
	}
	body, err = store.QuestionSeenBodyForTest("Q1", store.Question{ID: "Q1", Status: "open", Recommended: "a"})
	ok(t, err)
	if body["recommended"] != "a" {
		t.Fatalf("open body %v", body)
	}
	body, err = store.QuestionSeenBodyForTest("Q1", answered("Q1", "a"))
	ok(t, err)
	if _, has := body["recommended"]; has {
		t.Fatalf("a card read without its recommendation kept one: %v", body)
	}
}

// What a writer decides does not depend on the recommendation: SeeQuestion
// keeps it, and records again only when the card read gives another.
func TestQuestionSeenRecommendedSeen(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	q := answered("Q7", "a")
	q.Recommended = "b"
	if did, err := store.SeeQuestion(ctx, w.rt, "Q7", questionIs(q)); err != nil || !did {
		t.Fatalf("seen %v %v", did, err)
	}
	if did, err := store.SeeQuestion(ctx, w.rt, "Q7", questionIs(q)); err != nil || did {
		t.Fatalf("seen again %v %v", did, err)
	}
	var rec string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'recommended', '') FROM agentrt.event WHERE event_type = 'question_seen'
	  AND body_json->>'question' = 'Q7' ORDER BY event_id DESC LIMIT 1`).Scan(&rec))
	if rec != "b" {
		t.Fatalf("recommended kept as %q", rec)
	}
}

// A blocked task's card and a mission's card keep the recommendation of the
// card the pass read, whatever the Owner answered, and act on the answer
// alone.
func TestQuestionSeenRecommendedCards(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.accept(t, w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"}))
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.failRound(t, impl, "h1", "h2")
	if did := w.failRound(t, impl, "h3", "h4"); did != "blocked" {
		t.Fatalf("round 2 did %q", did)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	q1 := a.lastID()
	a.answer(q1, store.QuestionAnswered, store.CardApplyKey)
	q := a.answers[q1]
	q.Recommended = store.CardRefuseKey
	a.answers[q1] = q
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "task", impl) != "ready" {
		t.Fatalf("one more round: decided %d (%v), task %s", n, err, w.state(t, "task", impl))
	}
	if got := recommendedSeen(t, w, q1); got != store.CardRefuseKey {
		t.Fatalf("blocked card: recommended kept as %q", got)
	}

	m, q2 := proposeOutside(t, w, a.ask, "r")
	a.answer(q2, store.QuestionAnswered, store.CardApplyKey)
	q = a.answers[q2]
	q.Recommended = store.CardApplyKey
	a.answers[q2] = q
	if n, err := store.DecideMissions(ctx, w.rt, a.question, a.ack); err != nil || n != 1 || w.state(t, "mission", m) != "active" {
		t.Fatalf("mission card: decided %d (%v), mission %s", n, err, w.state(t, "mission", m))
	}
	if got := recommendedSeen(t, w, q2); got != store.CardApplyKey {
		t.Fatalf("mission card: recommended kept as %q", got)
	}
}

// Closing the mission on a blocked task's card keeps the recommendation too.
func TestQuestionSeenRecommendedClose(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.accept(t, w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"}))
	impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.failRound(t, impl, "h1", "h2")
	if did := w.failRound(t, impl, "h3", "h4"); did != "blocked" {
		t.Fatalf("round 2 did %q", did)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	q1 := a.lastID()
	a.answer(q1, store.QuestionAnswered, store.CardCloseKey)
	q := a.answers[q1]
	q.Recommended = store.CardRefuseKey
	a.answers[q1] = q
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("close: decided %d (%v)", n, err)
	}
	if o, _, _ := w.closedCard(t, q1); o != store.OutcomeClosed {
		t.Fatalf("the card closed %q", o)
	}
	if got := recommendedSeen(t, w, q1); got != store.CardRefuseKey {
		t.Fatalf("recommended kept as %q", got)
	}
}

func recommendedSeen(t *testing.T, w *world, question string) string {
	t.Helper()
	var rec string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'recommended', '') FROM agentrt.event WHERE event_type = 'question_seen'
	  AND body_json->>'question' = $1 ORDER BY event_id DESC LIMIT 1`, question).Scan(&rec))
	return rec
}

// An ending's self-check is kept when its counts can be, and one that cannot
// is dropped with the reason; the ending goes through either way.
func TestSelfCheckWellFormed(t *testing.T) {
	t.Parallel()
	ok(t, store.SelfCheckForTest(store.SelfCheck{Runs: 3, Red: 2, Fixed: 1, Seconds: 9}))
	ok(t, store.SelfCheckForTest(store.SelfCheck{}))
	for _, c := range []store.SelfCheck{{Runs: -1}, {Runs: 1, Red: 2}, {Runs: 2, Red: 1, Fixed: 2}, {Runs: 1, Seconds: -1}} {
		if err := store.SelfCheckForTest(c); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestSelfCheckComputed(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	f := w.started(t, "h1", cursor)
	out, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		SelfCheck: &store.SelfCheck{Runs: 4, Red: 1, Fixed: 1, LastGreen: true, Seconds: 70}})
	ok(t, err)
	if out["computed"] != true || out["self_check_refused"] != nil {
		t.Fatalf("answer %v", out)
	}
	var sc string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->'self_check', 'null'::jsonb)::text FROM agentrt.event WHERE event_type = 'attempt_computed'
	  AND attempt_id = $1`, f.AttemptID).Scan(&sc))
	if sc != `{"red": 1, "runs": 4, "fixed": 1, "seconds": 70, "last_green": true}` {
		t.Fatalf("self_check kept as %s", sc)
	}
	// The same ending again, with another count, is the same request.
	again, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		SelfCheck: &store.SelfCheck{Runs: 5}})
	if err != nil || again["computed"] != true {
		t.Fatalf("retry %v %v", again, err)
	}

	g := w.started(t, "h2", cursor)
	out, err = store.ComputedWith(ctx, w.rt, g, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		SelfCheck: &store.SelfCheck{Runs: 1, Red: 2}})
	ok(t, err)
	if out["computed"] != true || out["self_check_refused"] == nil {
		t.Fatalf("answer %v", out)
	}
	var has bool
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json ? 'self_check' FROM agentrt.event WHERE event_type = 'attempt_computed'
	  AND attempt_id = $1`, g.AttemptID).Scan(&has))
	if has {
		t.Fatal("a malformed self_check was kept")
	}
}
