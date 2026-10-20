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
)

// withdrawals is a question store's withdraw that records what it withdrew.
type withdrawals struct {
	mu       sync.Mutex
	ids, why []string
}

func (w *withdrawals) withdraw(_ context.Context, id, reason string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ids = append(w.ids, id)
	w.why = append(w.why, reason)
	return nil
}

// taskEvents is how many events of typ a task has.
func (w *world) taskEvents(t *testing.T, typ, task string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = $1 AND task_id = $2`, typ, task).Scan(&n))
	return n
}

// lastMissionMove is a mission's last mission_state_changed body.
func (w *world) lastMissionMove(t *testing.T, mission string) map[string]any {
	t.Helper()
	var b map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE mission_id = $1 AND event_type = 'mission_state_changed'
	  ORDER BY event_id DESC LIMIT 1`, mission).Scan(&b))
	return b
}

// claimed claims on host the one ready task there is, which must be id, and
// starts it when start is set.
func (w *world) claimed(t *testing.T, id, host string, tool store.Tool, start bool) *store.Claim {
	t.Helper()
	c := w.claim(t, host, time.Hour, tool)
	if c == nil || c.TaskID != id {
		t.Fatalf("claim on %s: %+v; want %s", host, c, id)
	}
	if start {
		ok(t, store.Start(ctx, w.rt, fence(c, host), "", ""))
	}
	return c
}

// The Owner 2026-10-20: "Close mission M-n" on a blocked card closes the
// whole mission in one go. Every task that has not ended is cancelled,
// whatever its state (proposed, ready, leased, running, a check running on
// work under verification, blocked); each live attempt is cancelled, so its
// host's next heartbeat is refused; the mission is done, an implementation of
// it having been accepted; the card closes as closed and the answer is kept.
// A cancelled check blocks nothing and no task gets a new card; the
// mission's other open cards are withdrawn, and an answer on one of them,
// "close" again included, moves nothing.
func TestCloseMissionOnCard(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	goal := store.Task{MissionID: "M-3", OffPlanReason: "a goal"}
	done := w.ready(t, goal)
	w.accept(t, done)
	// Work under verification, its check running on h2.
	checked := w.ready(t, goal)
	w.run(t, "h1", cursor, "done")
	check, err := store.StartVerification(ctx, w.rt, checked)
	ok(t, err)
	checking := w.claimed(t, check, "h2", deterministic, true)
	running := w.ready(t, goal)
	run := w.claimed(t, running, "h3", cursor, true)
	leased := w.ready(t, goal)
	lease := w.claimed(t, leased, "h4", cursor, false)
	ready := w.ready(t, goal)
	proposed := w.proposed(t, goal)
	var blocked []string
	for range 3 {
		id := w.proposed(t, goal)
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
		blocked = append(blocked, id)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 3 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := map[string]string{} // task -> question
	for i, c := range a.cards {
		card[c.TaskID] = "q_" + string(rune('a'+i))
		if !slices.Equal(c.Options, []string{store.BlockedRetry, store.BlockedDrop, "Close mission M-3"}) {
			t.Fatalf("options %v", c.Options)
		}
	}
	if w.state(t, "task", checked) != "verifying" || w.state(t, "task", check) != "running" || w.state(t, "task", leased) != "leased" {
		t.Fatalf("the set-up: %s %s %s", w.state(t, "task", checked), w.state(t, "task", check), w.state(t, "task", leased))
	}

	q := card[blocked[0]]
	a.answers = map[string]store.Question{q: {ID: q, Status: store.QuestionAnswered, Option: store.CardCloseKey, Text: "clear it",
		AnsweredBy: "owner", AnsweredAt: "2026-10-20T14:00:00Z"}}
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	for _, id := range append([]string{checked, check, running, leased, ready, proposed}, blocked...) {
		if s := w.state(t, "task", id); s != "cancelled" {
			t.Errorf("task %s is %s, want cancelled", id, s)
		}
		if r := w.lastMove(t, id); r != "the Owner closed mission M-3 on the card of task "+blocked[0]+" (question "+q+")" {
			t.Errorf("task %s moved because %q", id, r)
		}
	}
	if s := w.state(t, "task", done); s != "accepted" {
		t.Errorf("the accepted task is %s", s)
	}
	for c, host := range map[*store.Claim]string{checking: "h2", run: "h3", lease: "h4"} {
		if s := w.state(t, "attempt", c.AttemptID); s != "cancelled" || w.events(t, "attempt_cancelled", c.AttemptID) != 1 {
			t.Errorf("attempt %s is %s", c.AttemptID, s)
		}
		if _, err := store.Heartbeat(ctx, w.rt, fence(c, host)); !errors.Is(err, store.ErrFenced) {
			t.Errorf("the heartbeat of %s on %s: %v; want fenced", c.AttemptID, host, err)
		}
	}
	m := w.lastMissionMove(t, "M-3")
	if w.state(t, "mission", "M-3") != "done" || m["from"] != "active" || m["question_id"] != q || m["answer"] != store.CardCloseKey ||
		m["reason"] != "the Owner closed it on the card of task "+blocked[0]+" (question "+q+"); 1 task accepted, 9 cancelled with it" {
		t.Fatalf("M-3 is %s, moved %v", w.state(t, "mission", "M-3"), m)
	}
	if o, ans, text := w.closedCard(t, q); o != store.OutcomeClosed || ans != store.CardCloseKey || text != "clear it" {
		t.Fatalf("the card closed %q %q %q", o, ans, text)
	}
	if w.taskEvents(t, "owner_wait_closed", blocked[1]) != 0 || w.taskEvents(t, "owner_wait_closed", blocked[2]) != 0 {
		t.Fatal("the mission's other cards were closed before the question store withdrew them")
	}
	var seen int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'question_seen' AND body_json->>'question' = $1
	  AND body_json->>'option' = 'c'`, q).Scan(&seen))
	if seen != 1 || a.acked[q] != 1 {
		t.Fatalf("the answer kept %d times, acknowledged %d", seen, a.acked[q])
	}

	// No chain: nothing is blocked, no task gets a card, no work is closed
	// again, needs_you shows nothing of M-3.
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 0 {
		t.Fatalf("after the close: asked %d (%v)", n, err)
	}
	if closed, err := store.CloseMissions(ctx, w.rt); err != nil || len(closed) != 0 {
		t.Fatalf("closed again %v (%v)", closed, err)
	}
	if doc := w.progress(t); len(doc.NeedsYou.Items) != 0 {
		t.Fatalf("needs_you %v", doc.NeedsYou.Items)
	}

	// The other cards: one answered "close" before it was withdrawn moves
	// nothing; the other is withdrawn, once, and closed as withdrawn.
	again := card[blocked[1]]
	a.answers[again] = store.Question{ID: again, Status: store.QuestionAnswered, Option: store.CardCloseKey, AnsweredBy: "owner",
		AnsweredAt: "2026-10-20T14:01:00Z"}
	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 1 || !slices.Equal(wd.ids, []string{card[blocked[2]]}) ||
		wd.why[0] != "mission M-3 has ended (done); this card no longer decides anything" {
		t.Fatalf("withdrew %d (%v): %v %v", n, err, wd.ids, wd.why)
	}
	a.answers[card[blocked[2]]] = store.Question{ID: card[blocked[2]], Status: "withdrawn"}
	moves := w.taskEvents(t, "task_state_changed", blocked[1])
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 2 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if o, ans, _ := w.closedCard(t, again); o != store.OutcomeMoot || ans != store.CardCloseKey {
		t.Fatalf("a second close closed its card %q %q", o, ans)
	}
	if o, _, _ := w.closedCard(t, card[blocked[2]]); o != "withdrawn" {
		t.Fatalf("the withdrawn card closed %q", o)
	}
	var stateMoves int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE mission_id = 'M-3' AND event_type = 'mission_state_changed'
	  AND body_json->>'from' = 'active'`).Scan(&stateMoves))
	if stateMoves != 1 || w.taskEvents(t, "task_state_changed", blocked[1]) != moves || a.acked[again] != 1 {
		t.Fatalf("after a second close: %d moves of M-3, %d of its task (was %d), acknowledged %d", stateMoves,
			w.taskEvents(t, "task_state_changed", blocked[1]), moves, a.acked[again])
	}
	wd.ids = nil
	if n, err := store.WithdrawStaleCards(ctx, w.rt, a.question, wd.withdraw); err != nil || n != 0 || len(wd.ids) != 0 {
		t.Fatalf("withdrew %d (%v) %v once every card is closed", n, err, wd.ids)
	}
	w.chainHolds(t, "mission:M-3")
}

// A mission closed with nothing it asked for accepted is stopped, and so is
// a paused one, which may only stop; the reason says which.
func TestCloseMissionStops(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-4")
	w.goalMission(t, "M-5")
	w.accept(t, w.ready(t, store.Task{MissionID: "M-4", Role: "plan", Effect: "none", OffPlanReason: "a goal"}))
	w.accept(t, w.ready(t, store.Task{MissionID: "M-5", OffPlanReason: "a goal"}))
	var ids []string
	for _, m := range []string{"M-4", "M-5"} {
		id := w.ready(t, store.Task{MissionID: m, OffPlanReason: "a goal"})
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
		ids = append(ids, id)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-5", "paused", "test") }))
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := map[string]string{}
	for i, c := range a.cards {
		card[c.TaskID] = "q_" + string(rune('a'+i))
		a.answer(card[c.TaskID], store.QuestionAnswered, store.CardCloseKey)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, a.question, a.ack); err != nil || n != 2 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	for m, want := range map[string]string{
		"M-4": "the Owner closed it on the card of task " + ids[0] + " (question " + card[ids[0]] + "); 1 task cancelled with it, nothing the goal asked for was accepted",
		"M-5": "the Owner closed it on the card of task " + ids[1] + " (question " + card[ids[1]] + ") while it was paused; 1 task accepted, 1 cancelled with it",
	} {
		if s, r := w.state(t, "mission", m), w.lastMissionMove(t, m)["reason"]; s != "stopped" || r != want {
			t.Errorf("%s is %s because %q; want stopped because %q", m, s, r, want)
		}
	}
}

// An attempt leased between the close's lock of the live attempts and its
// lock of the tasks is not lost: the close starts again and cancels it too.
// Serial: it holds the package's test hook (SetHook).
func TestCloseMissionTakesALateAttempt(t *testing.T) {
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	late := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	id := w.proposed(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
	a := &asker{}
	_, err := store.AskBlocked(ctx, w.rt, a.ask)
	ok(t, err)
	a.answer(a.lastID(), store.QuestionAnswered, store.CardCloseKey)
	var c *store.Claim
	tries := 0
	unhook := store.SetHook(func(point, arg string) {
		if point != "close: attempts locked" || arg != "M-3" {
			return
		}
		tries++
		if tries == 1 {
			c = w.claimed(t, late, "h1", cursor, true)
		}
	})
	defer unhook()
	pool := w.db.Pool(t, w.db.Runtime)
	if n, err := store.DecideBlocked(ctx, pool, a.question, nil); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if tries != 2 || c == nil || w.state(t, "attempt", c.AttemptID) != "cancelled" || w.state(t, "task", late) != "cancelled" {
		t.Fatalf("%d tries; the late attempt %+v is %s, its task %s", tries, c, w.state(t, "attempt", c.AttemptID), w.state(t, "task", late))
	}
}

// What each rule recommends, on cards made from the record (the rules
// themselves: blockedrecommend_internal_test.go). R5 is TestBlockedTaskCard's.
func TestBlockedCardRecommends(t *testing.T) {
	t.Parallel()
	first := func(body string) string { line, _, _ := strings.Cut(body, "\n"); return line }

	t.Run("R1 and R3", func(t *testing.T) {
		t.Parallel()
		w := newKernelWorld(t)
		w.goalMission(t, "M-3")
		w.goalMission(t, "M-4")
		old := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
		fresh := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
		w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '30 hours 10 minutes',
		  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = 'M-3'`,
			`UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '3 hours',
		  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = 'M-4'`)
		if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
			t.Fatalf("blocked %d (%v)", n, err)
		}
		a := &asker{}
		if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 2 {
			t.Fatalf("asked %d (%v)", n, err)
		}
		for _, c := range a.cards {
			want := map[string]struct {
				rec       int
				why, line string
			}{
				old:   {3, "started 30 hours ago, used 15× its 2 hours, nothing has run", "Recommended: Close mission M-3 — started 30 hours ago, used 15× its 2 hours, nothing has run"},
				fresh: {1, "only the budget stopped it", "Recommended: Give it one more round — only the budget stopped it"},
			}[c.TaskID]
			if c.Recommended != want.rec || c.RecommendedWhy != want.why || first(c.Body) != want.line {
				t.Errorf("task %s: recommended %d %q, first line %q", c.TaskID, c.Recommended, c.RecommendedWhy, first(c.Body))
			}
		}
	})

	t.Run("R2", func(t *testing.T) {
		t.Parallel()
		for _, quiet := range []bool{false, true} {
			w, mission, impl, _ := spentEnvelope(t)
			if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
				t.Fatalf("blocked %d (%v)", n, err)
			}
			a := &asker{}
			_, err := store.AskBlocked(ctx, w.rt, a.ask)
			ok(t, err)
			if c := a.cards[0]; c.Recommended != 1 || c.RecommendedWhy != "only the budget stopped it" {
				t.Fatalf("the check's card recommends %d %q", c.Recommended, c.RecommendedWhy)
			}
			a.answer(a.lastID(), store.QuestionAnswered, store.CardRefuseKey)
			_, err = store.DecideBlocked(ctx, w.rt, a.question, a.ack)
			ok(t, err)
			want := "Recommended: Do not do this task — its only check was dropped on its card; other work in " + mission +
				" ran under an hour ago, so drop only this task"
			if quiet {
				w.legacy(t, `UPDATE agentrt.attempt SET heartbeat_at = clock_timestamp() - interval '2 days'`)
				want = "Recommended: Close mission " + mission + " — its only check was dropped on its card, and nothing in " + mission +
					" ran since " + time.Now().UTC().Add(-48*time.Hour).Format("01-02")
			}
			if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[1].TaskID != impl || first(a.cards[1].Body) != want {
				t.Fatalf("quiet %v: asked %d (%v), first line %q; want %q", quiet, n, err, first(a.cards[len(a.cards)-1].Body), want)
			}
		}
	})

	t.Run("R4", func(t *testing.T) {
		t.Parallel()
		w := newKernelWorld(t)
		w.goalMission(t, "M-3")
		impl := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
		for round, want := range []string{"rework", "blocked"} {
			w.run(t, fmt.Sprintf("h%d", 3*round+1), cursor, "done")
			v, err := store.StartVerification(ctx, w.rt, impl)
			ok(t, err)
			w.run(t, fmt.Sprintf("h%d", 3*round+2), deterministic, "done")
			if did, err := store.Conclude(ctx, w.rt, v); err != nil || did != "review" {
				t.Fatalf("round %d's verification: %q %v", round+1, did, err)
			}
			review := w.checker(t, impl, "review")
			w.run(t, fmt.Sprintf("h%d", 3*round+3), codex, "failed")
			if did, err := store.Conclude(ctx, w.rt, review); err != nil || did != want {
				t.Fatalf("round %d's review: %q %v", round+1, did, err)
			}
		}
		a := &asker{}
		if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 {
			t.Fatalf("asked %d (%v)", n, err)
		}
		if c := a.cards[0]; c.Recommended != 0 || c.RecommendedWhy != "" ||
			first(c.Body) != "No recommendation: needs your judgment: the checks pass, the reviewer failed it" {
			t.Fatalf("recommended %d %q, first line %q", c.Recommended, c.RecommendedWhy, first(c.Body))
		}
	})
}

// A card asked before cards had the third option (two options) is withdrawn
// while open, and its block is asked again with a card of today's kind; a
// card whose options the reader does not say, or that has three, is left.
func TestOldBlockedCardIsAskedAgain(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	var ids []string
	for range 3 {
		id := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, id) }))
		ids = append(ids, id)
	}
	a := &asker{}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 3 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	options := map[string]int{"q_a": 2, "q_b": 3, "q_c": 0}
	read := func(ctx context.Context, id string) (store.Question, bool) {
		q, ok := a.question(ctx, id)
		q.Options = options[id]
		return q, ok
	}
	var wd withdrawals
	if n, err := store.WithdrawStaleCards(ctx, w.rt, read, wd.withdraw); err != nil || n != 1 || !slices.Equal(wd.ids, []string{"q_a"}) ||
		wd.why[0] != "replaced by a card that can also close mission M-3 and says what it recommends" {
		t.Fatalf("withdrew %d (%v): %v %v", n, err, wd.ids, wd.why)
	}
	a.answers = map[string]store.Question{"q_a": {ID: "q_a", Status: "withdrawn"}}
	if n, err := store.DecideBlocked(ctx, w.rt, read, a.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if o, _, _ := w.closedCard(t, "q_a"); o != "withdrawn" || w.state(t, "task", a.cards[0].TaskID) != "blocked" {
		t.Fatalf("the old card closed %q; its task is %s", o, w.state(t, "task", a.cards[0].TaskID))
	}
	if n, err := store.AskBlocked(ctx, w.rt, a.ask); err != nil || n != 1 || a.cards[3].TaskID != a.cards[0].TaskID ||
		len(a.cards[3].Options) != 3 || !strings.Contains(a.cards[3].Body, "card 2 for this block") {
		t.Fatalf("asked %d (%v) %+v", n, err, a.cards[len(a.cards)-1])
	}
}
