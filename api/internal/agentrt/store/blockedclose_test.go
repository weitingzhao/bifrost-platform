package store_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

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
		if n, err := store.BlockSpentForTest(ctx, w.rt); err != nil || n != 2 {
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
			if n, err := store.BlockSpentForTest(ctx, w.rt); err != nil || n != 1 {
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
			v, err := startVerify(ctx, w.rt, impl)
			ok(t, err)
			w.run(t, fmt.Sprintf("h%d", 3*round+2), deterministic, "done")
			if did, err := store.ConcludeWith(ctx, w.rt, v, verifyMode); err != nil || did != "review" {
				t.Fatalf("round %d's verification: %q %v", round+1, did, err)
			}
			review := w.checker(t, impl, "review")
			w.run(t, fmt.Sprintf("h%d", 3*round+3), codex, "failed")
			if did, err := store.ConcludeWith(ctx, w.rt, review, verifyMode); err != nil || did != want {
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
