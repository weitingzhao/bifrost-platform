package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// InTx runs fn in one transaction and commits it; any error rolls back.
func InTx(ctx context.Context, conn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, fn func(pgx.Tx) error) error {
	// The database refuses state writes under any other isolation level:
	// its locks rely on each statement reading what was committed before it.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MissionStream and PlanStream name the event streams of a mission and of a
// plan tree (its root node).
func MissionStream(missionID string) string { return "mission:" + missionID }
func PlanStream(rootID string) string       { return "plan:" + rootID }

// PutArtifact stores a body by content and returns its id. Artifacts are
// append only; the same body stored twice is one row.
func PutArtifact(ctx context.Context, tx pgx.Tx, actor, kind, mediaType string, body []byte) (string, error) {
	if len(body) > 1<<20 {
		return "", errors.New("store: an artifact is at most 1 MiB")
	}
	h := sha256.Sum256(body)
	id := "sha256:" + hex.EncodeToString(h[:])
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.artifact (artifact_id, artifact_kind, media_type, size_bytes, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5) ON CONFLICT (artifact_id) DO NOTHING`, id, kind, mediaType, len(body), actor); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.artifact_blob (artifact_id, body) VALUES ($1,$2) ON CONFLICT (artifact_id) DO NOTHING`,
		id, body); err != nil {
		return "", err
	}
	return id, nil
}

// Mission is the part of a mission row the store writes at creation.
type Mission struct {
	ID, WorkID, Title, Goal string
	Deadline                time.Time
}

// CreateMission writes mission_proposed and the mission row.
func CreateMission(ctx context.Context, tx pgx.Tx, actor string, m Mission) (Appended, error) {
	ev, err := Append(ctx, tx, Event{Stream: MissionStream(m.ID), Type: "mission_proposed", Actor: actor,
		MissionID: m.ID, Body: map[string]any{"title": m.Title}})
	if err != nil {
		return ev, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.mission (mission_id, work_id, title, goal, success_json, scope_json,
	  state, deadline_at, budget_json, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,'{}','{"max_action":"branch"}','proposed',$5,'{}',$6)`,
		m.ID, nullable(m.WorkID), m.Title, m.Goal, m.Deadline, actor)
	return ev, err
}

// PlanNode is a node to create.
type PlanNode struct {
	ID, Parent, Kind, Label, Title, WorkID, MissionID, StepKind string
	Ordinal                                                     int
	Root                                                        string // root of the tree, for the stream name
}

// CreatePlanNode writes plan_node_created and the node. A step starts planned.
func CreatePlanNode(ctx context.Context, tx pgx.Tx, actor string, n PlanNode) (Appended, error) {
	ev, err := Append(ctx, tx, Event{Stream: PlanStream(n.Root), Type: "plan_node_created", Actor: actor,
		PlanNode: n.ID, MissionID: n.MissionID,
		Body: map[string]any{"node_kind": n.Kind, "label": n.Label, "title": n.Title, "ordinal": n.Ordinal}})
	if err != nil {
		return ev, err
	}
	var state any
	if n.Kind == "step" {
		state = "planned"
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.plan_node (plan_node_id, parent_plan_node_id, node_kind, ordinal, label, title,
	  work_id, mission_id, step_kind, state, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		n.ID, nullable(n.Parent), n.Kind, n.Ordinal, n.Label, n.Title, nullable(n.WorkID), nullable(n.MissionID),
		nullable(n.StepKind), state, actor)
	return ev, err
}

// SetStepState moves a step's state with its plan_node_state_changed event.
func SetStepState(ctx context.Context, tx pgx.Tx, actor, root, stepID, to, cause string) (Appended, error) {
	var from string
	var mission *string
	if err := tx.QueryRow(ctx, `SELECT state, mission_id FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`,
		stepID).Scan(&from, &mission); err != nil {
		return Appended{}, err
	}
	m := ""
	if mission != nil {
		m = *mission
	}
	ev, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_node_state_changed", Actor: actor,
		PlanNode: stepID, MissionID: m, Body: map[string]any{"from": from, "to": to, "cause": cause}})
	if err != nil {
		return ev, err
	}
	_, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET state = $2, state_event_id = $3, version = version + 1
	  WHERE plan_node_id = $1`, stepID, to, ev.EventID)
	return ev, err
}

// Task is a task to propose.
type Task struct {
	ID, MissionID, PlanNode, OffPlanReason, Subject, Role, Class, Title, SpecArtifact, Effect string
	HardLimit                                                                                 time.Duration
}

// ProposeTask writes task_proposed and the task. A task outside the plan
// carries its reason instead of a step.
func ProposeTask(ctx context.Context, tx pgx.Tx, actor string, t Task) (Appended, error) {
	if t.ID == "" {
		t.ID = NewTaskID()
	}
	body := map[string]any{"title": t.Title, "role": t.Role}
	if t.OffPlanReason != "" {
		body["off_plan_reason"] = t.OffPlanReason
	}
	ev, err := Append(ctx, tx, Event{Stream: MissionStream(t.MissionID), Type: "task_proposed", Actor: actor,
		MissionID: t.MissionID, PlanNode: t.PlanNode, TaskID: t.ID, Body: body})
	if err != nil {
		return ev, err
	}
	limit := int(t.HardLimit / time.Second)
	if limit <= 0 {
		limit = 3600
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, plan_node_id, off_plan, off_plan_reason,
	  subject_task_id, role, task_class, title, spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'proposed',$12,$13)`,
		t.ID, t.MissionID, nullable(t.PlanNode), t.PlanNode == "", nullable(t.OffPlanReason), nullable(t.Subject),
		t.Role, t.Class, t.Title, t.SpecArtifact, t.Effect, limit, actor)
	return ev, err
}

// AddDependency records that taskID waits for dependsOn.
func AddDependency(ctx context.Context, tx pgx.Tx, actor, missionID, taskID, dependsOn string) (Appended, error) {
	ev, err := Append(ctx, tx, Event{Stream: MissionStream(missionID), Type: "task_dependency_added", Actor: actor,
		MissionID: missionID, TaskID: taskID, Body: map[string]any{"depends_on_task_id": dependsOn}})
	if err != nil {
		return ev, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.task_dependency (task_id, depends_on_task_id) VALUES ($1,$2)`, taskID, dependsOn)
	return ev, err
}

// LeaseAttempt opens attempt n of a task for an executor with a fresh
// fencing epoch.
func LeaseAttempt(ctx context.Context, tx pgx.Tx, actor, missionID, taskID, executor, hostID string, n int, lease time.Duration) (string, int64, error) {
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT nextval('agentrt.lease_epoch_seq')`).Scan(&epoch); err != nil {
		return "", 0, err
	}
	id := NewAttemptID()
	body := map[string]any{"attempt_no": n, "lease_epoch": epoch, "executor": executor}
	if hostID != "" {
		body["host_id"] = hostID
	}
	if _, err := Append(ctx, tx, Event{Stream: MissionStream(missionID), Type: "attempt_leased", Actor: actor,
		TaskID: taskID, AttemptID: id, Body: body}); err != nil {
		return "", 0, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, host_id,
	  lease_epoch, lease_expires_at, heartbeat_at, state)
	  VALUES ($1,$2,$3,$4,$5,$6, clock_timestamp() + make_interval(secs => $7), clock_timestamp(), 'leased')`,
		id, taskID, n, executor, nullable(hostID), epoch, lease.Seconds())
	return id, epoch, err
}
