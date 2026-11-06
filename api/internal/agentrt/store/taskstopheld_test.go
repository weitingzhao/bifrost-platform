package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A stop holds a task when it is the task's own or its mission's, and only
// while it is not cleared; a pause or another task's stop does not.
func TestTaskStopHeld(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	other := w.ready(t, store.Task{})
	held := func(id string) bool {
		t.Helper()
		on, err := store.TaskStopHeld(ctx, w.rt, id)
		ok(t, err)
		return on
	}
	set := func(id, scope, ref, kind string) {
		t.Helper()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", id, scope, ref, kind, "test") }))
	}
	if held(task) {
		t.Fatal("held before any control")
	}
	set("c1", "mission", "M-1", "pause")
	if held(task) {
		t.Fatal("a pause read as a stop")
	}
	set("c2", "task", other, "stop")
	if held(task) || !held(other) {
		t.Fatal("a task stop held the wrong task")
	}
	set("c3", "mission", "M-1", "stop")
	if !held(task) {
		t.Fatal("the mission's stop does not hold its task")
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.ClearControl(ctx, tx, "owner", "c3") }))
	if held(task) {
		t.Fatal("a cleared stop still holds")
	}
}

// TaskWait reads a task's state and whether a stop holds it, and finds no
// task that does not exist (controller.AckUsedAnswers).
func TestTaskWaitForAckUsedAnswers(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	state, stopped, found, err := store.TaskWait(ctx, w.rt, task)
	ok(t, err)
	if !found || state != "ready" || stopped {
		t.Fatal("read", state, stopped, found)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", "c1", "task", task, "stop", "test") }))
	if _, stopped, _, err = store.TaskWait(ctx, w.rt, task); err != nil || !stopped {
		t.Fatal("a stop does not read as holding the task", stopped, err)
	}
	if _, _, found, err = store.TaskWait(ctx, w.rt, "t_01M6S77BXRBGYCB5GD5NRDH15N"); err != nil || found {
		t.Fatal("a task that does not exist was found", found, err)
	}
}
