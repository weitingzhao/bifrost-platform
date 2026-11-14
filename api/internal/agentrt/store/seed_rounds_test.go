package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Scenes of the retired dispatch lane, written as its rows (store cut 3b-2,
// 2026-11-14). Nothing leases, runs or checks a task any more, but the plan's
// bindings, the artifact search and the progress read still read the rows a
// round left: the tasks, their attempts and the verifications. These
// fixtures write those rows as an import does (a legacy_imported event of
// the transaction), in the shape the lane left them.

// seedHost makes host id exist: an attempt names the host it ran on.
func seedHost(t *testing.T, tx pgx.Tx, id string) {
	t.Helper()
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
 VALUES ('host:' || $1, 'host', 'Agent host ' || $1) ON CONFLICT DO NOTHING`, id)
	ok(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state)
 VALUES ($1, 'host:' || $1, 'dev', 'active') ON CONFLICT DO NOTHING`, id)
	ok(t, err)
}

// seedAttempt writes task's next attempt as one that ran on host with tool
// and was submitted with conclusion; an implementation's carries the pushed
// commit.
func seedAttempt(t *testing.T, tx pgx.Tx, task, host string, tool store.Tool, conclusion, pushed string) string {
	t.Helper()
	seedHost(t, tx, host)
	id := store.NewAttemptID()
	report, err := store.PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte(id+":"+conclusion))
	ok(t, err)
	var ref, commit any
	if pushed != "" {
		ref, commit = "refs/heads/agent/"+task, pushed
	}
	exit := 0
	if conclusion == "failed" {
		exit = 1
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt
 (attempt_id, task_id, attempt_no, executor_actor_id, host_id, model_provider, lease_epoch, lease_expires_at, heartbeat_at,
  computed_at, persisted_at, state, started_at, end_how, exit_code, conclusion, report_artifact_id, pushed_ref, pushed_commit)
 SELECT $1, $2, COALESCE((SELECT max(attempt_no) FROM agentrt.attempt WHERE task_id = $2), 0) + 1, $3, $4, $5,
  COALESCE((SELECT max(lease_epoch) FROM agentrt.attempt), 0) + 1, clock_timestamp() + interval '2 minutes', clock_timestamp(),
  clock_timestamp(), clock_timestamp(), 'submitted', clock_timestamp(), 'exit', $6, $7, $8, $9, $10`,
		id, task, tool.Executor, host, tool.Provider, exit, conclusion, report, ref, commit)
	ok(t, err)
	return id
}

// seedCheck writes a check task of subject (role verify or review), accepted,
// with its one attempt, and the verification it left on subject's attempt.
func seedCheck(t *testing.T, tx pgx.Tx, subject, attempt, role, host string, tool store.Tool, verdict string, accepts bool) string {
	t.Helper()
	id := store.NewTaskID()
	// A check's spec names what it checks: the round, the attempt, its commit.
	var round int
	ok(t, tx.QueryRow(ctx, `SELECT round_no FROM agentrt.task WHERE task_id = $1`, subject).Scan(&round))
	spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json",
		[]byte(fmt.Sprintf(`{"attempt":%q,"commit":%q,"round":%d,"subject_task":%q}`, attempt, commitA, round, subject)))
	ok(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.task
 (task_id, mission_id, plan_node_id, subject_task_id, role, task_class, title, spec_artifact_id, repo, base_commit, effect_class, state, hard_limit_s, created_by_actor_id)
 SELECT $1, mission_id, plan_node_id, task_id, $2, task_class, $2 || ' ' || task_id, $5, repo, $3, 'none', 'accepted', hard_limit_s, 'controller'
 FROM agentrt.task WHERE task_id = $4`, id, role, commitA, subject, spec)
	ok(t, err)
	conclusion := "done"
	if verdict == "fail" {
		conclusion = "failed"
	}
	checker := seedAttempt(t, tx, id, host, tool, conclusion, "")
	method := "deterministic"
	if role == "review" {
		method = "review"
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification
 (verification_id, task_id, attempt_id, method, verifier_attempt_id, verdict, evidence_artifact_id, subject_commit, accepts_attempt)
 SELECT $1, $2, $3, $4, a.attempt_id, $5, a.report_artifact_id, $6, $7 FROM agentrt.attempt a WHERE a.attempt_id = $8`,
		store.NewVerificationID(), subject, attempt, method, verdict, commitA, accepts, checker)
	ok(t, err)
	return id
}

// seeded runs fn in a transaction that carries an import event, as a
// fixture's rows need.
func (w *world) seeded(t *testing.T, fn func(tx pgx.Tx)) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		fn(tx)
		return nil
	}))
}

// seedLease writes task's first attempt as leased to executor, in the
// caller's transaction, and answers its id and lease epoch: the row a lease
// left, for the tests that need an attempt to exist (the event rule on the
// attempt table, the steps whose work has started, the gate rows).
func seedLease(ctx context.Context, tx pgx.Tx, actor, mission, task, executor, host string, no int, ttl time.Duration) (string, int64, error) {
	if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
		Body: map[string]any{"source": "test"}}); err != nil {
		return "", 0, err
	}
	id := store.NewAttemptID()
	var epoch int64
	err := tx.QueryRow(ctx, `INSERT INTO agentrt.attempt
 (attempt_id, task_id, attempt_no, executor_actor_id, host_id, lease_epoch, lease_expires_at, heartbeat_at, state)
 SELECT $1, $2, $3, $4, NULLIF($5, ''), COALESCE((SELECT max(lease_epoch) FROM agentrt.attempt), 0) + 1,
  clock_timestamp() + make_interval(secs => $6), clock_timestamp(), 'leased' RETURNING lease_epoch`,
		id, task, no, executor, host, ttl.Seconds()).Scan(&epoch)
	return id, epoch, err
}

// seedTask writes task in state as the dispatch lane's proposal left one
// (store cut 4, 2026-11-14: nothing proposes a task any more, and the tests
// of what stays still need the row). It is the caller's transaction; the
// database's own guards still answer.
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

// seedProposed is a task of mission proposed by a thread, on step or off the
// plan when step is empty.
func (w *world) seedProposed(t *testing.T, mission, step string) string {
	t.Helper()
	id := store.NewTaskID()
	task := store.Task{ID: id, MissionID: mission, PlanNode: step, Role: "implement", Class: "go", Title: "t", SpecArtifact: w.impl,
		Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}
	if step == "" {
		task.OffPlanReason = "test"
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return seedTask(ctx, tx, task, "proposed") }))
	// The thread that proposed it is an actor, as its first write made it.
	w.seeded(t, func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:s1', 'thread', 'thread:s1') ON CONFLICT DO NOTHING`)
		ok(t, err)
	})
	return id
}
