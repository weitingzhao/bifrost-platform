package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
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
	var holdErrs []error
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
			// A savepoint: an unreadable landing stream holds this mission
			// only, and the pass goes on with the others.
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			held, err := landingHolds(ctx, sp, id, eventClock())
			if err != nil {
				holdErrs = append(holdErrs, fmt.Errorf("mission %s: landing: %w", id, err))
				if rerr := sp.Rollback(ctx); rerr != nil {
					return rerr
				}
				return nil
			}
			if err = sp.Commit(ctx); err != nil || held {
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
			return closed, errors.Join(append(holdErrs, fmt.Errorf("mission %s: %w", id, err))...)
		}
		if did {
			closed = append(closed, id)
		}
	}
	return closed, errors.Join(holdErrs...)
}

// landingHolds: an implementation the landing loop is carrying to main keeps
// its mission active (CUT-landing §2.7's hold, brought forward for the S2
// drill). Every landing gate reads the mission as active, so closing it would
// end that operation stale before its CI finished. It holds while
//   - the root's latest operation has not ended, or ended discarded (the
//     replay k+1 is due), and at most landingHoldLimit after the root's first
//     claim (or retry) — the six-hour bound plus a margin, so a mission is
//     released even when no loop runs to record failed{bound};
//   - or the root was never claimed, landing is enabled for its repo (a
//     current landing rule, an observed enable time no later than the
//     mission's creation and the task's acceptance), and it was accepted less
//     than landingClaimGrace ago: the loop (every 15 s) gets its first claim
//     before the close. A loop that misses the grace misses the drill; the
//     mission closes as before, nothing is held for good. A busy pass (an
//     older operation's external reads, or max_in_flight_per_repo taken for
//     longer than the grace) can miss it too: one drill task at a time
//     (review 640; a durable first-claim handoff is TD-447).
//
// Without a landing rule (PROD today) nothing holds. The caller holds the
// mission's lock; ClaimLandingDrill locks the task, then its mission, before
// the repository stream, so a claim and a close are ordered.
const (
	landingClaimGrace = 2 * time.Minute
	landingHoldLimit  = 6*time.Hour + 15*time.Minute
)

func landingHolds(ctx context.Context, tx pgx.Tx, mission string, now time.Time) (bool, error) {
	type root struct {
		task     string
		accepted *time.Time
	}
	rows, err := tx.Query(ctx, `SELECT t.task_id, t.repo, (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = t.task_id
	    AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'accepted')
	  FROM agentrt.task t WHERE t.mission_id = $1 AND t.role = 'implement' AND t.repo <> '' ORDER BY t.repo, t.task_id`, mission)
	if err != nil {
		return false, err
	}
	roots, repos := map[string][]root{}, []string{}
	for rows.Next() {
		var r root
		var repo string
		if err = rows.Scan(&r.task, &repo, &r.accepted); err != nil {
			rows.Close()
			return false, err
		}
		if len(roots[repo]) == 0 {
			repos = append(repos, repo)
		}
		roots[repo] = append(roots[repo], r)
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(repos) == 0 {
		return false, err
	}
	var tenant string
	var created time.Time
	var noMerge bool
	if err = tx.QueryRow(ctx, `SELECT tenant, created_at, COALESCE(scope_json->>'no_merge','') <> '' FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&tenant, &created, &noMerge); err != nil {
		return false, err
	}
	rule, err := CurrentRule(ctx, tx, "landing:"+tenant, now)
	if err != nil {
		return false, err
	}
	for _, repo := range repos {
		state, err := LandingState(ctx, tx, "landing:"+repo)
		if err != nil {
			return false, err
		}
		enabled := false
		if rule != nil {
			_, enabled = rule.Landing.Repo(repo)
		}
		// A no_merge mission is never claimed (the gate reads it stale): no grace.
		enabled = enabled && !noMerge && !state.EnabledAt.IsZero() && !created.Before(state.EnabledAt)
		for _, r := range roots[repo] {
			if latest, ok := state.Operations[state.LatestOperation(r.task)]; ok {
				first, _ := landing.RootBudget(state, r.task)
				if (!latest.Terminal() || latest.Phase == "discarded") && now.Before(first.Add(landingHoldLimit)) {
					return true, nil
				}
				continue
			}
			if enabled && r.accepted != nil && !r.accepted.Before(state.EnabledAt) && now.Before(r.accepted.Add(landingClaimGrace)) {
				return true, nil
			}
		}
	}
	return false, nil
}
