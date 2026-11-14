package store_test

import (
	"testing"

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
