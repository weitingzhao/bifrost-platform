package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// onMainSHA is the commit main holds the tasks' changes as.
const onMainSHA = "0123456789abcdef0123456789abcdef01234567"

// onMainWorld is a kernel world with, each with a landed note: a ready task;
// a ready task of mission M-2 (on step S2) under a mission stop; a task the proposer's
// thread cancelled (it would read withdrawn). And without one: a ready task
// of M-2 under the same stop.
type onMainWorld struct {
	*world
	ready, stopped, cancelled, stillStopped, missionStop string
}

func newOnMainWorld(t *testing.T) *onMainWorld {
	t.Helper()
	w := &onMainWorld{world: newKernelWorld(t)}
	w.ready = w.world.ready(t, store.Task{})
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	w.stopped = w.world.ready(t, store.Task{MissionID: "M-2", PlanNode: s2})
	w.stillStopped = w.world.ready(t, store.Task{MissionID: "M-2", PlanNode: s2})
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "k", "M-1", []store.TaskProposal{proposal("c", w.impl)})
	ok(t, err)
	w.cancelled = taskIDs(out)["c"]
	w.missionStop = store.NewControlID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.SetControl(ctx, tx, "owner", w.missionStop, "mission", "M-2", "stop", "stopped by hand"); err != nil {
			return err
		}
		if err := store.MoveTask(ctx, tx, "thread:s1", w.cancelled, "cancelled", "the mission was stopped"); err != nil {
			return err
		}
		for _, x := range []struct{ mission, task string }{{"M-1", w.ready}, {"M-2", w.stopped}, {"M-1", w.cancelled}} {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream(x.mission), Type: "note", Actor: "controller",
				MissionID: x.mission, TaskID: x.task, Body: map[string]any{"text": "its accepted result is on main", "landed": onMainSHA}}); err != nil {
				return err
			}
		}
		return nil
	}))
	return w
}

// onMainReason is the reason of a task on main whose note has no landed_via.
const onMainReason = "on main of bifrost-platform as 0123456789ab (on main)"

// PROD 2026-11-06: a task whose change is on main reads finished in the
// progress projection, with the on-main reason, whether ready, stopped or
// cancelled; it is not counted as open work. A stopped task without a note
// still reads stopped.
func TestProgressOnMain(t *testing.T) {
	t.Parallel()
	w := newOnMainWorld(t)
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
	for _, task := range []string{w.ready, w.stopped, w.cancelled} {
		x := tasks[task]
		if x == nil {
			t.Errorf("task %s is not listed", task)
			continue
		}
		om, _ := x["on_main"].(map[string]any)
		if x["status"] != store.StatusFinished || x["reason"] != onMainReason || om == nil || om["commit"] != onMainSHA || om["via"] != "on main" {
			t.Errorf("task %s: %v (%q), on_main %v; want finished (%q)", task, x["status"], x["reason"], x["on_main"], onMainReason)
		}
		if x["stopped"] != nil || x["withdrawn_by"] != nil {
			t.Errorf("task %s reads stopped or withdrawn: %v", task, x)
		}
	}
	if x := tasks[w.stillStopped]; x == nil || x["status"] != store.StatusStopped || x["stopped"] == nil || x["on_main"] != nil {
		t.Errorf("the stopped task without a note: %v; want stopped", x)
	}
	// M-1's only open task is on main: nothing of it is open work.
	if m1 := missions["M-1"]; m1["status"] == store.StatusQueued || m1["status"] == store.StatusStopped {
		t.Errorf("M-1: %v (%v); want no open work", m1["status"], m1["reason"])
	} else if eta := m1["eta"].(map[string]any); eta["state"] != "unknown" || eta["reason"] != "no unfinished task: nothing proposed to estimate" {
		t.Errorf("M-1 eta %v; want nothing to estimate", eta)
	}
	if m2 := missions["M-2"]; m2["status"] != store.StatusStopped {
		t.Errorf("M-2: %v; want stopped by its task without a note", m2["status"])
	}
	for _, r := range doc["recent"].(map[string]any)["tasks"].([]any) {
		r := r.(map[string]any)
		if r["task_id"] != w.cancelled {
			continue
		}
		if r["state"] != "cancelled" || r["status"] != store.StatusFinished || r["reason"] != onMainReason || r["withdrawn_by"] != nil {
			t.Errorf("the cancelled task among the recent: %v; want cancelled, finished, not withdrawn", r)
		}
	}
}

// The plan view: the tasks on main carry on_main and wait for nothing, not
// stopped or withdrawn; the stopped task without a note still reads stopped.
func TestPlanViewOnMain(t *testing.T) {
	t.Parallel()
	w := newOnMainWorld(t)
	out, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	tasks := map[string]map[string]any{}
	for _, x := range generic(t, out)["tasks"].([]any) {
		x := x.(map[string]any)
		tasks[x["task_id"].(string)] = x
	}
	for _, task := range []string{w.ready, w.stopped, w.cancelled} {
		x := tasks[task]
		om, _ := x["on_main"].(map[string]any)
		if om == nil || om["commit"] != onMainSHA || om["via"] != "on main" {
			t.Errorf("task %s in the plan view: on_main %v", task, x["on_main"])
		}
		if x["stopped"] != nil || x["withdrawn"] != nil || x["waiting"] != nil {
			t.Errorf("task %s in the plan view: %v; want neither stopped, withdrawn nor waiting", task, x)
		}
	}
	x := tasks[w.stillStopped]
	if stopped, _ := x["stopped"].(map[string]any); stopped == nil || stopped["control_id"] != w.missionStop || x["on_main"] != nil {
		t.Errorf("the stopped task without a note in the plan view: %v; want stopped by %s", x, w.missionStop)
	} else if wait, _ := x["waiting"].(map[string]any); wait == nil || wait["on"] != store.WaitStop {
		t.Errorf("the stopped task without a note waits on %v; want the stop", x["waiting"])
	}
}
