package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Closing a goal's mission (W-31 2.2b, iteration 1: a goal that was done
// still showed as active). A mission a goal made is bound to no plan step;
// once every one of its tasks is accepted, or was dropped by the Owner on its
// blocked card (blockedcard.go: cancelled in the transaction that records
// owner_wait_closed with outcome dropped), nothing is left for it to do, and
// the controller moves it on (mission_state_changed): to done when an
// implementation (or, for a message to the chief of staff, its brief) is
// among the accepted, to stopped when the Owner dropped the rest and nothing
// the goal asked for was accepted (its planning, say). A mission bound to plan
// steps is left to its steps and their rules. One with a task that is neither
// (open, rejected, blocked, or cancelled any other way) stays as it is, for
// the Owner or the coordinator to see why.
//
// A task is cancelled only by a controller move (MoveTask's table). Today the
// Owner's "do not do this task" on a blocked task's card is the only caller
// that cancels; no route and no other loop does, and an import writes no
// task (planimport.go writes steps). A task cancelled by any other way that
// comes later holds its mission open until someone decides what it means.
//
// A task proposed over HTTP while the mission closes may land in a done
// mission (the proposal reads the mission's state without its lock); it stays
// proposed there, since dispatch readies tasks of active missions only. A
// goal's tasks come from its plan, in the controller's own pass, so this does
// not arise for them.

// dropped is a task (t) the Owner dropped on its card.
const dropped = `t.state = 'cancelled' AND EXISTS (SELECT 1 FROM agentrt.event d WHERE d.task_id = t.task_id
    AND d.event_type = 'owner_wait_closed' AND d.body_json->>'outcome' = 'dropped')`

// closable is the condition, read again under the mission's lock.
const closable = `m.state = 'active'
  AND NOT EXISTS (SELECT 1 FROM agentrt.plan_node n WHERE n.mission_id = m.mission_id)
  AND EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id
    AND ((t.role IN ('implement','brief') AND t.state = 'accepted') OR ` + dropped + `))
  AND NOT EXISTS (SELECT 1 FROM agentrt.task t WHERE t.mission_id = m.mission_id AND t.state <> 'accepted' AND NOT (` + dropped + `))`

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
			var accepted, drops int
			var delivered bool
			err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task WHERE mission_id = m.mission_id AND state = 'accepted'),
			    (SELECT count(*) FROM agentrt.task t WHERE t.mission_id = m.mission_id AND `+dropped+`),
			    EXISTS (SELECT 1 FROM agentrt.task WHERE mission_id = m.mission_id AND role IN ('implement','brief') AND state = 'accepted')
			  FROM agentrt.mission m WHERE m.mission_id = $1 AND `+closable+` FOR NO KEY UPDATE OF m`, id).Scan(&accepted, &drops, &delivered)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if held, err := landingInFlight(ctx, tx, id); err != nil || held {
				return err
			}
			did = true
			switch {
			case drops == 0:
				return MoveMission(ctx, tx, "controller", id, "done", fmt.Sprintf("every task is accepted (%d)", accepted))
			case delivered:
				return MoveMission(ctx, tx, "controller", id, "done",
					fmt.Sprintf("every task is accepted (%d) or was dropped by the Owner on its card (%d)", accepted, drops))
			}
			return MoveMission(ctx, tx, "controller", id, "stopped",
				fmt.Sprintf("the Owner dropped its remaining tasks on their cards (%d); nothing the goal asked for was accepted", drops))
		}); err != nil {
			return closed, fmt.Errorf("mission %s: %w", id, err)
		}
		if did {
			closed = append(closed, id)
		}
	}
	return closed, nil
}

// landingInFlight: an implementation the landing loop is carrying to main
// keeps its mission active (CUT-landing §2.7, brought forward for the S2
// drill). Every landing gate reads the mission as active, so closing it under
// a running operation would end that operation stale before its CI finished.
// A root holds while its latest operation has not ended, or ended discarded
// (the next replay is due; the bound turns it into failed{bound} within six
// hours). A task the loop never claimed holds nothing, and neither does a
// finished one. The caller holds the mission's lock; ClaimLandingDrill takes
// it FOR SHARE before the repository stream, so a claim and a close are
// ordered: the close sees the claim, or the claim sees the mission done.
func landingInFlight(ctx context.Context, tx pgx.Tx, mission string) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT task_id, repo FROM agentrt.task WHERE mission_id = $1 AND role = 'implement' AND repo <> '' ORDER BY repo, task_id`, mission)
	if err != nil {
		return false, err
	}
	tasks, repos := map[string][]string{}, []string{}
	for rows.Next() {
		var task, repo string
		if err = rows.Scan(&task, &repo); err != nil {
			rows.Close()
			return false, err
		}
		if len(tasks[repo]) == 0 {
			repos = append(repos, repo)
		}
		tasks[repo] = append(tasks[repo], task)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	for _, repo := range repos {
		state, err := LandingState(ctx, tx, "landing:"+repo)
		if err != nil {
			return false, fmt.Errorf("landing state of %s: %w", repo, err)
		}
		for _, task := range tasks[repo] {
			latest, ok := state.Operations[state.LatestOperation(task)]
			if ok && (!latest.Terminal() || latest.Phase == "discarded") {
				return true, nil
			}
		}
	}
	return false, nil
}
