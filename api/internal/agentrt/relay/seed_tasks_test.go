package relay

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// seedReady leaves the wake signal a task's move to ready left in the
// outbox: the relay's tests deliver that signal.
func seedReady(ctx context.Context, tx pgx.Tx, task store.Task) error {
	// No task row any more (0020): the event and its wake signal are what
	// the relay's tests deliver.
	ev, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream(task.MissionID), Type: "task_state_changed", Actor: "controller",
		MissionID: task.MissionID, PlanNode: task.PlanNode, TaskID: task.ID, Body: map[string]any{"from": "proposed", "to": "ready"}})
	if err != nil {
		return err
	}
	return store.Enqueue(ctx, tx, ev.EventID, store.TopicWake, map[string]any{"kind": "task_ready", "task_id": task.ID, "role": task.Role, "task_class": task.Class})
}
