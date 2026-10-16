package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Closing a goal's mission (W-31 2.2b, iteration 1: a goal that was done
// still showed as active). A mission a goal made is bound to no plan step;
// once every one of its tasks is accepted and an implementation is among
// them, nothing is left for it to do, and the controller moves it to done
// (mission_state_changed). A mission bound to plan steps is left to its steps
// and their rules. One with a task that is not accepted (open, rejected,
// blocked, cancelled) stays as it is, for the Owner or the coordinator to
// see why.
//
// A task proposed over HTTP while the mission closes may land in a done
// mission (the proposal reads the mission's state without its lock); it stays
// proposed there, since dispatch readies tasks of active missions only. A
// goal's tasks come from its plan, in the controller's own pass, so this does
// not arise for them.

// closable is the condition, read again under the mission's lock.
const closable = `m.state = 'active'
  AND NOT EXISTS (SELECT 1 FROM agentrt.plan_node n WHERE n.mission_id = m.mission_id)
  AND EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id AND t.role = 'implement')
  AND NOT EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id AND t.state <> 'accepted')`

// CloseMissions is one pass: it closes every mission done as above and
// returns their ids.
func CloseMissions(ctx context.Context, db DB) ([]string, error) {
	var ids []string
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT m.mission_id FROM agentrt.mission m WHERE `+closable+` ORDER BY m.mission_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	var closed []string
	for _, id := range ids {
		did := false
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			var n int
			err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task WHERE mission_id = m.mission_id) FROM agentrt.mission m
			  WHERE m.mission_id = $1 AND `+closable+` FOR NO KEY UPDATE OF m`, id).Scan(&n)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			did = true
			return MoveMission(ctx, tx, "controller", id, "done", fmt.Sprintf("every task is accepted (%d)", n))
		}); err != nil {
			return closed, fmt.Errorf("mission %s: %w", id, err)
		}
		if did {
			closed = append(closed, id)
		}
	}
	return closed, nil
}
