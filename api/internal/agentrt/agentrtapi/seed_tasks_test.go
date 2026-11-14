package agentrtapi_test

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Rows of the retired dispatch lane, written as it wrote them (store cut 4,
// 2026-11-14): nothing proposes or moves a task any more, and the tests here
// still need the rows to exist.

// seedTask writes task in state, in the caller's transaction.
func seedTask(ctx context.Context, tx pgx.Tx, task store.Task, state string) error {
	if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
		Body: map[string]any{"source": "test"}}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.task
 (task_id, mission_id, plan_node_id, off_plan, off_plan_reason, subject_task_id, role, task_class, title, spec_artifact_id,
  repo, base_commit, effect_class, state, hard_limit_s, created_by_actor_id)
 VALUES ($1, $2, NULLIF($3, ''), $3 = '', NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9, NULLIF($10, ''), NULLIF($11, ''), $12, $13, 3600, 'controller')`,
		task.ID, task.MissionID, task.PlanNode, task.OffPlanReason, task.Subject, task.Role, task.Class, task.Title, task.SpecArtifact,
		task.Repo, task.BaseCommit, task.Effect, state)
	return err
}

// seedMove moves task to state as the lane recorded a move: the event on its
// mission's stream, then the row.
func seedMove(ctx context.Context, tx pgx.Tx, actor, task, to, reason string) error {
	var mission, step, from string
	if err := tx.QueryRow(ctx, `SELECT mission_id, COALESCE(plan_node_id, ''), state FROM agentrt.task WHERE task_id = $1 FOR UPDATE`,
		task).Scan(&mission, &step, &from); err != nil {
		return err
	}
	body := map[string]any{"from": from, "to": to}
	if reason != "" {
		body["reason"] = reason
	}
	if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream(mission), Type: "task_state_changed", Actor: actor,
		MissionID: mission, PlanNode: step, TaskID: task, Body: body}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = $2, version = version + 1, updated_at = clock_timestamp() WHERE task_id = $1`, task, to)
	return err
}
