package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A planned step becomes active once work on it has started: while the
// plan-standing rule in force grants active_on_lease, the controller moves
// each planned step a task on which has been leased (CUT-plan-import-2026-10-19.md
// §7; Owner 2026-10-19 「三件照建议」, the rule his to sign). The state change
// names its cause, task_leased, and is caused by the first attempt_leased
// event of the step's tasks. Without the rule nothing moves.

// activatePage is how many steps one pass moves at most.
const activatePage = 100

// ActivateLeasedSteps is one pass; it returns how many steps it moved.
func ActivateLeasedSteps(ctx context.Context, db DB) (int, error) {
	var steps []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := inForce(ctx, tx, rules.KindPlan)
		if err != nil || r == nil || !r.Plan.ActiveOnLease {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT s.plan_node_id FROM agentrt.plan_node s
		  WHERE s.node_kind = 'step' AND s.state = 'planned'
		    AND EXISTS (SELECT 1 FROM agentrt.task t JOIN agentrt.attempt a ON a.task_id = t.task_id WHERE t.plan_node_id = s.plan_node_id)
		  ORDER BY s.plan_node_id LIMIT $1`, activatePage)
		if err != nil {
			return err
		}
		steps, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, err
	}
	moved := 0
	var errs []error
	for _, id := range steps {
		did, err := activateStep(ctx, db, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("step %s: %w", id, err))
		}
		if did {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

// activateStep moves one step, reading again under its lock that the rule is
// still in force and the step still planned.
func activateStep(ctx context.Context, db DB, id string) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var state string
		var mission *string
		if err := tx.QueryRow(ctx, `SELECT state, mission_id FROM agentrt.plan_node WHERE plan_node_id = $1 AND node_kind = 'step' FOR UPDATE`,
			id).Scan(&state, &mission); err != nil {
			return err
		}
		r, err := inForce(ctx, tx, rules.KindPlan)
		if err != nil || r == nil || !r.Plan.ActiveOnLease || state != "planned" {
			return err
		}
		var leased *int64
		if err = tx.QueryRow(ctx, `SELECT min(e.event_id) FROM agentrt.event e JOIN agentrt.task t ON t.task_id = e.task_id
		  WHERE e.event_type = 'attempt_leased' AND t.plan_node_id = $1`, id).Scan(&leased); err != nil {
			return err
		}
		if leased == nil {
			return nil
		}
		root, err := stepRoot(ctx, tx, id)
		if err != nil {
			return err
		}
		m := ""
		if mission != nil {
			m = *mission
		}
		ev, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_node_state_changed", Actor: "controller", PlanNode: id,
			MissionID: m, Cause: *leased, Body: map[string]any{"from": "planned", "to": "active", "cause": "task_leased"}})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE agentrt.plan_node SET state = 'active', state_event_id = $2, version = version + 1
		  WHERE plan_node_id = $1`, id, ev.EventID); err != nil {
			return err
		}
		did = true
		return nil
	})
	return did, err
}
