package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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

// ArtifactStream is the event stream of uploaded artifacts.
const ArtifactStream = "artifacts"

// UploadArtifact stores an uploaded body (PUT /artifacts, v5 §5.2) and, the
// first time this body is stored, records artifact_put in the same
// transaction: the writer it is recorded as (a thread registered on its
// first write, as for proposals; review 175 B4), the token that uploaded it,
// its kind and size (review 156 A3). Uploading the same body again stores
// and records nothing. Artifacts that other events create and name (a task
// spec with task_proposed, a message with message_posted) go through
// PutArtifact.
func UploadArtifact(ctx context.Context, db DB, w Writer, kind, mediaType string, body []byte) (string, bool, error) {
	if len(body) > 1<<20 {
		return "", false, errors.New("store: an artifact is at most 1 MiB")
	}
	h := sha256.Sum256(body)
	id := "sha256:" + hex.EncodeToString(h[:])
	actor, uploadedBy := w.Actor, w.Token
	var created bool
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var have bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.artifact WHERE artifact_id = $1)`, id).Scan(&have); err != nil {
			return err
		}
		if have {
			// Nothing to store: the writer is still checked, without
			// registering a thread for it (review 176 B3).
			_, err := CheckWriter(ctx, tx, w)
			return err
		}
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO agentrt.artifact (artifact_id, artifact_kind, media_type, size_bytes, created_by_actor_id)
		  VALUES ($1,$2,$3,$4,$5) ON CONFLICT (artifact_id) DO NOTHING`, id, kind, mediaType, len(body), actor)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		created = true
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.artifact_blob (artifact_id, body) VALUES ($1,$2)`, id, body); err != nil {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: ArtifactStream, Type: "artifact_put", Actor: actor,
			Body: map[string]any{"artifact_id": id, "artifact_kind": kind, "size_bytes": len(body), "uploaded_by": uploadedBy}})
		return err
	})
	return id, created, err
}

// Mission is the part of a mission row the store writes at creation. The
// success criteria, repos and budget are optional here (an empty list or a
// zero is left out of the row's JSON); MaxAction defaults to branch and
// Tenant to dev.
type Mission struct {
	ID, WorkID, Title, Goal string
	Deadline                time.Time
	Tenant, MaxAction       string
	Success, Repos          []string
	MaxAttempts             int
	MaxWallHours            int
	Token                   string // the token that proposed it, when a route did (review 176 B2)
}

// CreateMission writes mission_proposed and the mission row.
func CreateMission(ctx context.Context, tx pgx.Tx, actor string, m Mission) (Appended, error) {
	if m.Tenant == "" {
		m.Tenant = "dev"
	}
	if m.MaxAction == "" {
		m.MaxAction = "branch"
	}
	success := map[string]any{}
	if len(m.Success) > 0 {
		success["criteria"] = m.Success
	}
	scope := map[string]any{"max_action": m.MaxAction}
	if len(m.Repos) > 0 {
		scope["repos"] = m.Repos
	}
	budget := map[string]any{}
	if m.MaxAttempts > 0 {
		budget["max_attempts"] = m.MaxAttempts
	}
	if m.MaxWallHours > 0 {
		budget["max_wall_hours"] = m.MaxWallHours
	}
	var docs [3][]byte
	for i, v := range []any{success, scope, budget} {
		var err error
		if docs[i], err = canon.Encode(v); err != nil {
			return Appended{}, err
		}
	}
	body := map[string]any{"title": m.Title}
	if m.Token != "" {
		body["token"] = m.Token
	}
	ev, err := Append(ctx, tx, Event{Stream: MissionStream(m.ID), Type: "mission_proposed", Actor: actor,
		MissionID: m.ID, Body: body})
	if err != nil {
		return ev, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.mission (mission_id, work_id, title, goal, success_json, scope_json,
	  tenant, state, deadline_at, budget_json, created_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,'proposed',$8,$9::jsonb,$10)`,
		m.ID, nullable(m.WorkID), m.Title, m.Goal, string(docs[0]), string(docs[1]), m.Tenant, m.Deadline, string(docs[2]), actor)
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

// stepMoves is every step state change there is: a done step goes back to
// active when a standing condition lapses; a dropped step is final.
var stepMoves = map[string][]string{
	"planned": {"active", "done", "stopped", "dropped"},
	"active":  {"done", "stopped", "dropped"},
	"done":    {"active", "dropped"},
	"stopped": {"active", "dropped"},
}

// SetStepState moves a step's state with its plan_node_state_changed event,
// along stepMoves only.
func SetStepState(ctx context.Context, tx pgx.Tx, actor, root, stepID, to, cause string) (Appended, error) {
	var from string
	var mission *string
	if err := tx.QueryRow(ctx, `SELECT state, mission_id FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`,
		stepID).Scan(&from, &mission); err != nil {
		return Appended{}, err
	}
	if !slices.Contains(stepMoves[from], to) {
		return Appended{}, fmt.Errorf("%w: step %s %s -> %s", ErrState, stepID, from, to)
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

// Task is a task to propose. Core and Needs decide who may take it (v5
// §5.1.2 「分流顺序」); Repo and BaseCommit are where agentd starts from.
type Task struct {
	ID, MissionID, PlanNode, OffPlanReason, Subject, Role, Class, Title, SpecArtifact, Effect string
	Repo, BaseCommit                                                                          string
	Core                                                                                      bool
	Needs                                                                                     []string
	HardLimit                                                                                 time.Duration
	Token                                                                                     string // the proposing token, when a route did
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
	if t.Core {
		body["core"] = true
	}
	if len(t.Needs) > 0 {
		body["needs"] = t.Needs
	}
	if t.Repo != "" {
		body["repo"] = t.Repo
	}
	if t.BaseCommit != "" {
		body["base_commit"] = t.BaseCommit
	}
	if t.Token != "" {
		body["token"] = t.Token
	}
	needs, err := canon.Encode(nonNil(t.Needs))
	if err != nil {
		return Appended{}, err
	}
	// The locks the insert's guard takes, taken before the event's stream
	// (review 149 B1): the step, then the subject.
	if t.PlanNode != "" {
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE`, t.PlanNode); err != nil {
			return Appended{}, err
		}
	}
	if t.Subject != "" {
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR SHARE`, t.Subject); err != nil {
			return Appended{}, err
		}
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
	  subject_task_id, role, task_class, title, spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id,
	  core, needs_json, repo, base_commit)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'proposed',$12,$13,$14,$15::jsonb,$16,$17)`,
		t.ID, t.MissionID, nullable(t.PlanNode), t.PlanNode == "", nullable(t.OffPlanReason), nullable(t.Subject),
		t.Role, t.Class, t.Title, t.SpecArtifact, t.Effect, limit, actor,
		t.Core, string(needs), nullable(t.Repo), nullable(t.BaseCommit))
	return ev, err
}

// AddDependency records that taskID waits for dependsOn. It takes the
// dependency-graph lock, then the waiting task as Dispatch does (exclusively)
// and the one waited for, before the event's stream (review 149 B1). Only a
// proposed task takes a new dependency: once Dispatch has readied it (or
// holds it to decide) an edge added after would not be seen (review 175 A1).
func AddDependency(ctx context.Context, tx pgx.Tx, actor, missionID, taskID, dependsOn string) (Appended, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7421000001)`); err != nil {
		return Appended{}, err
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE`, taskID).Scan(&state); err != nil {
		return Appended{}, err
	}
	if state != "proposed" {
		return Appended{}, fmt.Errorf("%w: task %s is %s; a dependency is added only while a task is proposed", ErrState, taskID, state)
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR KEY SHARE`, dependsOn); err != nil {
		return Appended{}, err
	}
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
	return leaseAttempt(ctx, tx, actor, missionID, taskID, Tool{Executor: executor}, hostID, n, lease)
}

// leaseAttempt also records the model and provider the attempt is for:
// independence is checked against them.
func leaseAttempt(ctx context.Context, tx pgx.Tx, actor, missionID, taskID string, tl Tool, hostID string, n int, lease time.Duration) (string, int64, error) {
	// The task before the event's stream (review 149 B1).
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE`, taskID); err != nil {
		return "", 0, err
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT nextval('agentrt.lease_epoch_seq')`).Scan(&epoch); err != nil {
		return "", 0, err
	}
	id := NewAttemptID()
	body := map[string]any{"attempt_no": n, "lease_epoch": epoch, "executor": tl.Executor}
	if hostID != "" {
		body["host_id"] = hostID
	}
	if tl.Model != "" {
		body["model"] = tl.Model
	}
	if tl.Provider != "" {
		body["model_provider"] = tl.Provider
	}
	if _, err := Append(ctx, tx, Event{Stream: MissionStream(missionID), Type: "attempt_leased", Actor: actor,
		TaskID: taskID, AttemptID: id, Body: body}); err != nil {
		return "", 0, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, host_id,
	  model, model_provider, lease_epoch, lease_expires_at, heartbeat_at, state)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8, clock_timestamp() + make_interval(secs => $9), clock_timestamp(), 'leased')`,
		id, taskID, n, tl.Executor, nullable(hostID), nullable(tl.Model), nullable(tl.Provider), epoch, lease.Seconds())
	return id, epoch, err
}
