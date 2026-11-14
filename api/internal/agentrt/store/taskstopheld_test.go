package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

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
