package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A stop on a ready task makes it stopped, saying who stopped it, why, by
// which control and that only the Owner lifts it; without one it is queued
// as before. A live attempt's lost host or lapsed lease still comes first.
func TestStoppedViewTaskStatus(t *testing.T) {
	t.Parallel()
	now := time.Now()
	stop := &store.ProgressStop{ControlID: "ctl_1", By: "seat:coordinator", Reason: "wrong branch", Since: now.UTC().Format(time.RFC3339Nano)}
	want := "stopped by seat:coordinator: wrong branch (control ctl_1); only the Owner lifts it"
	if status, reason := store.TaskStatus(store.StatusInput{TaskState: "ready", Stop: stop, Now: now}); status != store.StatusStopped || reason != want {
		t.Errorf("a stopped ready task: %s (%q); want %s (%q)", status, reason, store.StatusStopped, want)
	}
	if status, reason := store.TaskStatus(store.StatusInput{TaskState: "ready", Now: now}); status != store.StatusQueued || reason != "ready for a host to claim" {
		t.Errorf("a ready task: %s (%q); want queued", status, reason)
	}
	fresh, old, short := now.Add(-time.Second), now.Add(-10*time.Minute), int64(10)
	for name, c := range map[string]struct {
		in   store.StatusInput
		want string
	}{
		"stopped over working":         {store.StatusInput{TaskState: "running", Attempt: store.LiveForTest("a_1", "running", true, &fresh, &short, now)}, store.StatusStopped},
		"stopped over a waiting owner": {store.StatusInput{TaskState: "awaiting_owner"}, store.StatusStopped},
		"stopped over blocked":         {store.StatusInput{TaskState: "blocked"}, store.StatusStopped},
		"host lost over stopped":       {store.StatusInput{TaskState: "running", Attempt: store.LiveForTest("a_1", "running", true, &old, &short, now)}, store.StatusHostLost},
		"maybe stuck over stopped":     {store.StatusInput{TaskState: "running", Attempt: store.LiveForTest("a_1", "stale", true, &fresh, &short, now)}, store.StatusMaybeStuck},
		"unknown over stopped":         {store.StatusInput{TaskState: "running", Attempt: store.LiveForTest("a_1", "running", false, nil, &short, now)}, store.StatusUnknown},
	} {
		c.in.Stop, c.in.Now = stop, now
		if got, _ := store.TaskStatus(c.in); got != c.want {
			t.Errorf("%s: %s; want %s", name, got, c.want)
		}
	}
	if got := store.WorstForTest(store.StatusStopped, store.StatusBlocked, store.StatusQueued); got != store.StatusStopped {
		t.Errorf("stopped, blocked and queued show %s; want stopped", got)
	}
	if got := store.WorstForTest(store.StatusStopped, store.StatusWaitingOwner); got != store.StatusWaitingOwner {
		t.Errorf("stopped and waiting_owner show %s; want waiting_owner", got)
	}
}

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
			if err := store.MoveTaskForTest(ctx, tx, "controller", w.subject, to); err != nil {
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
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-1", []store.TaskProposal{proposal("w", w.spec)})
	ok(t, err)
	w.withdrawn = taskIDs(out)["w"]
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
		if err := store.MoveTask(ctx, tx, "thread:s1", w.withdrawn, "cancelled", "not needed any more"); err != nil {
			return err
		}
		return store.MoveTask(ctx, tx, "controller", w.cancelled, "cancelled", "superseded")
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

// The progress projection: stopped tasks with their stop, the mission
// whose only open task is stopped waiting on the Owner, and the withdrawn
// task among the recent ones.
func TestStoppedViewProgress(t *testing.T) {
	t.Parallel()
	w := newStopWorld(t)
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	doc := generic(t, out)
	tasks := map[string]map[string]any{}
	missions := map[string]map[string]any{}
	for _, m := range doc["work"].(map[string]any)["missions"].([]any) {
		m := m.(map[string]any)
		missions[m["mission_id"].(string)] = m
		for _, x := range m["tasks"].([]any) {
			x := x.(map[string]any)
			tasks[x["task_id"].(string)] = x
		}
	}
	for task, c := range map[string]struct{ control, reason string }{
		w.stopped: {w.taskStop, "wrong branch"}, w.subject: {w.subjectStop, "its result is in doubt"},
		w.check: {w.subjectStop, "its result is in doubt"}, w.goal: {w.missionStop, "the goal changed"},
	} {
		x := tasks[task]
		stopped, _ := x["stopped"].(map[string]any)
		want := "stopped by owner: " + c.reason + " (control " + c.control + "); only the Owner lifts it"
		if x["status"] != store.StatusStopped || x["reason"] != want || stopped == nil {
			t.Errorf("task %s: %v (%q), stopped %v; want stopped (%q)", task, x["status"], x["reason"], x["stopped"], want)
			continue
		}
		if stopped["control_id"] != c.control || stopped["by"] != "owner" || stopped["reason"] != c.reason || stopped["since"] == "" ||
			x["since"] != stopped["since"] {
			t.Errorf("task %s: stopped %v, since %v", task, stopped, x["since"])
		}
	}
	if x := tasks[w.free]; x["status"] != store.StatusQueued || x["stopped"] != nil {
		t.Errorf("the task under no stop: %v, stopped %v; want queued", x["status"], x["stopped"])
	}
	m3 := missions["M-3"]
	if m3["status"] != store.StatusStopped || m3["waiting_on"] != "owner" {
		t.Errorf("M-3: %v waiting on %v; want stopped waiting on owner", m3["status"], m3["waiting_on"])
	}
	if eta := m3["eta"].(map[string]any); eta["state"] != "unknown" {
		t.Errorf("M-3 eta %v; want unknown: a stopped task waits for the Owner", eta)
	}
	if m1 := missions["M-1"]; m1["status"] != store.StatusStopped || m1["waiting_on"] != "owner" {
		t.Errorf("M-1: %v waiting on %v; want stopped waiting on owner over its queued task", m1["status"], m1["waiting_on"])
	}
	recent := map[string]map[string]any{}
	for _, r := range doc["recent"].(map[string]any)["tasks"].([]any) {
		r := r.(map[string]any)
		recent[r["task_id"].(string)] = r
	}
	if r := recent[w.withdrawn]; r == nil || r["withdrawn_by"] != "thread:s1" || r["withdraw_reason"] != "not needed any more" {
		t.Errorf("the withdrawn task among the recent: %v", r)
	}
	if r := recent[w.cancelled]; r == nil || r["state"] != "cancelled" {
		t.Errorf("the cancelled task among the recent: %v", r)
	} else if _, by := r["withdrawn_by"]; by {
		t.Errorf("the task the controller cancelled reads withdrawn: %v", r)
	} else if _, why := r["withdraw_reason"]; why {
		t.Errorf("the task the controller cancelled has a withdraw reason: %v", r)
	}
}

// The plan view snapshot: the stopped tasks carry their stop, the withdrawn
// one how it was withdrawn, the others neither.
func TestWithdrawnViewPlanSnapshot(t *testing.T) {
	t.Parallel()
	w := newStopWorld(t)
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	tasks := map[string]map[string]any{}
	for _, x := range generic(t, out)["tasks"].([]any) {
		x := x.(map[string]any)
		tasks[x["task_id"].(string)] = x
	}
	for task, control := range map[string]string{w.stopped: w.taskStop, w.subject: w.subjectStop, w.check: w.subjectStop} {
		stopped, _ := tasks[task]["stopped"].(map[string]any)
		if stopped == nil || stopped["control_id"] != control || stopped["by"] != "owner" || stopped["reason"] == "" || stopped["since"] == "" {
			t.Errorf("task %s in the plan view: stopped %v; want control %s", task, tasks[task]["stopped"], control)
		}
		if tasks[task]["withdrawn"] != nil {
			t.Errorf("task %s reads withdrawn", task)
		}
	}
	wd, _ := tasks[w.withdrawn]["withdrawn"].(map[string]any)
	if wd == nil || wd["by"] != "thread:s1" || wd["reason"] != "not needed any more" || wd["at"] == "" {
		t.Errorf("the withdrawn task in the plan view: %v", tasks[w.withdrawn])
	}
	for _, task := range []string{w.free, w.cancelled} {
		if x := tasks[task]; x == nil || x["stopped"] != nil || x["withdrawn"] != nil {
			t.Errorf("task %s in the plan view: %v; want neither stopped nor withdrawn", task, x)
		}
	}
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
