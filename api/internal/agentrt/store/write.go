package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// InTx runs fn in one transaction and commits it; any error rolls back
// (eventlog.InTx).
func InTx(ctx context.Context, conn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, fn func(pgx.Tx) error) error {
	return eventlog.InTx(ctx, conn, fn)
}

// MissionStream and PlanStream name the event streams of a mission and of a
// plan tree (its root node).
func MissionStream(missionID string) string { return "mission:" + missionID }
func PlanStream(rootID string) string       { return "plan:" + rootID }

// maxArtifactBytes is the largest artifact body.
const maxArtifactBytes = 1 << 20

// PutArtifact stores a body by content and returns its id. Artifacts are
// append only; the same body stored twice is one row.
func PutArtifact(ctx context.Context, tx pgx.Tx, actor, kind, mediaType string, body []byte) (string, error) {
	if err := validateViewArtifact(kind, body); err != nil {
		return "", err
	}
	if len(body) > maxArtifactBytes {
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
	if err := validateViewArtifact(kind, body); err != nil {
		return "", false, err
	}
	if len(body) > maxArtifactBytes {
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
// success criteria, repos, bases and budget are optional here (an empty list
// or a zero is left out of the row's JSON); MaxAction defaults to branch and
// Tenant to dev. Bases are a goal's: the commit of each repository's main its
// tasks start from (scope_json.bases).
type Mission struct {
	ID, WorkID, Title, Goal string
	Deadline                time.Time
	Tenant, MaxAction       string
	Success, Repos          []string
	Bases                   map[string]string
	MaxAttempts             int
	MaxWallHours            int
	NoMerge                 string         // why its accepted results stay off main, "" for none (scope_json.no_merge, TD-383)
	NeedsOwner              string         // why its cards wait for the Owner himself, "" for none (scope_json.needs_owner, TD-390)
	GateRun                 map[string]any // a batch of gate tasks, the controller's own (scope_json.gate_run, gates.go, TD-397)
	AssessmentOf            string         // the stream of the item it assesses, "" for none (scope_json.assessment_of, TD-391)
	Token                   string         // the token that proposed it, when a route did (review 176 B2)
}

// issueWorkPrefix started a business issue's work id (BI-<n>). Business
// issues were removed (the Owner 2026-11-15) and nothing makes such a mission
// any more; the prefix stays refused while the database's check does (0009
// check_issue_mission).
const issueWorkPrefix = "BI-"

// CreateMission writes mission_proposed and the mission row. A business
// issue's work id is refused here, on every path.
func CreateMission(ctx context.Context, tx pgx.Tx, actor string, m Mission) (Appended, error) {
	if strings.HasPrefix(m.WorkID, issueWorkPrefix) {
		return Appended{}, invalid("work id %s is a business issue's: business issues were removed", m.WorkID)
	}
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
	if len(m.Bases) > 0 {
		scope["bases"] = anyMap(m.Bases)
	}
	if m.NoMerge != "" {
		scope["no_merge"] = m.NoMerge
	}
	if m.NeedsOwner != "" {
		scope["needs_owner"] = m.NeedsOwner
	}
	if m.AssessmentOf != "" {
		scope["assessment_of"] = m.AssessmentOf
	}
	if m.GateRun != nil {
		scope["gate_run"] = m.GateRun
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
	if n.Parent != "" && n.MissionID != "" {
		if err := checkViewBinding(ctx, tx, n.Parent, n.MissionID); err != nil {
			return Appended{}, err
		}
	}
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
	Wave                                                                                      string
	Urgent                                                                                    bool
	ID, MissionID, PlanNode, OffPlanReason, Subject, Role, Class, Title, SpecArtifact, Effect string
	Repo, BaseCommit                                                                          string
	Core                                                                                      bool
	Needs                                                                                     []string
	HardLimit                                                                                 time.Duration
	Token                                                                                     string // the proposing token, when a route did
	// pin, viaSession and pinReason: the executor pin, the session header
	// and the reason (pinreason.go) of a pinned proposal, set only by proposeBatchTx after checkPin (pin.go);
	// nothing outside this package, and nothing else in it, sets them.
	pin, viaSession, pinReason string
	providers                  []string
}

// anyMap is m as canonical JSON takes it.
func anyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
