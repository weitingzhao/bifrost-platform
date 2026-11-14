package store_test

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// stopWorld is a kernel world with: a ready task under a task stop; a
// submitted task under a task stop and the verification of it (stopped by
// its subject); a ready task under no stop; mission M-3 under a mission stop
// with its only task; a task the proposer's thread withdrew and one the
// controller cancelled.
type stopWorld struct {
	*world
	stopped, subject, check, free, goal, withdrawn, cancelled string
	taskStop, subjectStop, missionStop                        string
}

func newStopWorld(t *testing.T) *stopWorld {
	t.Helper()
	w := &stopWorld{world: newKernelWorld(t)}
	w.stopped = w.ready(t, store.Task{})
	w.subject = w.ready(t, store.Task{})
	w.free = w.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, to := range []string{"leased", "running", "submitted"} {
			if err := seedMove(ctx, tx, "controller", w.subject, to, "test"); err != nil {
				return err
			}
		}
		return nil
	}))
	var err error
	w.check, err = w.task(t, "M-1", s1, w.subject, "verify")
	ok(t, err)
	w.goalMission(t, "M-3")
	w.goal = w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.withdrawn = w.seedProposed(t, "M-1", s1)
	w.cancelled = w.ready(t, store.Task{})
	w.taskStop, w.subjectStop, w.missionStop = store.NewControlID(), store.NewControlID(), store.NewControlID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, c := range []struct{ id, scope, ref, reason string }{
			{w.taskStop, "task", w.stopped, "wrong branch"},
			{w.subjectStop, "task", w.subject, "its result is in doubt"},
			{w.missionStop, "mission", "M-3", "the goal changed"},
		} {
			if err := store.SetControl(ctx, tx, "owner", c.id, c.scope, c.ref, "stop", c.reason); err != nil {
				return err
			}
		}
		if err := seedMove(ctx, tx, "thread:s1", w.withdrawn, "cancelled", "not needed any more"); err != nil {
			return err
		}
		return seedMove(ctx, tx, "controller", w.cancelled, "cancelled", "superseded")
	}))
	return w
}

// generic is a document as the Console reads it.
func generic(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	ok(t, err)
	var out map[string]any
	ok(t, json.Unmarshal(raw, &out))
	return out
}

// Once its stop is lifted the task is queued again.
func TestStoppedViewLiftedStop(t *testing.T) {
	t.Parallel()
	w := newStopWorld(t)
	_, err := store.LiftControl(ctx, w.rt, "owner", w.taskStop)
	ok(t, err)
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	for _, m := range generic(t, out)["work"].(map[string]any)["missions"].([]any) {
		for _, x := range m.(map[string]any)["tasks"].([]any) {
			if x := x.(map[string]any); x["task_id"] == w.stopped && (x["status"] != store.StatusQueued || x["stopped"] != nil) {
				t.Errorf("the task after its stop was lifted: %v, stopped %v; want queued", x["status"], x["stopped"])
			}
		}
	}
}
