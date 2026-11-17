package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// PlanStream names the event stream of a plan tree (its root node).
func PlanStream(rootID string) string { return "plan:" + rootID }

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
