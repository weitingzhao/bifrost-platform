package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// SettleUnclaimed is the drill pass's database step for roots in the
// first-claim handoff that have no operation yet (TD-447 4, CUT §7.7). It
// runs before any external read, whatever the current rule, backoff or work
// budget, and reads no Git: a root whose mission is done, stopped or expired
// ends failed{stale}; a root whose window (landing.ClaimWindow since its
// acceptance) is over ends failed{bound}. Each root is read again under the
// task, mission, then stream locks and recorded at most once (a claim or a
// close may have recorded it meanwhile). It returns the roots it recorded.
//
// The handoff is the durable facts landingHolds reads: an implementation in
// repo, accepted no earlier than the stream's recorded enable, in a mission
// created no earlier than it and not no_merge.
func SettleUnclaimed(ctx context.Context, db DB, repo string, now time.Time) ([]string, error) {
	stream := "landing:" + repo
	var state landing.State
	type root struct{ task string }
	var due []root
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		if state, err = LandingState(ctx, tx, stream); err != nil || state.EnabledAt.IsZero() {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
  JOIN agentrt.event e ON e.task_id = t.task_id AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'accepted'
  WHERE t.repo = $1 AND t.role = 'implement' AND t.state = 'accepted'
    AND m.created_at >= $2 AND COALESCE(m.scope_json->>'no_merge','') = ''
  GROUP BY t.task_id, m.state HAVING max(e.at) >= $2 AND (m.state IN ('done','stopped','expired') OR max(e.at) <= $3)
  ORDER BY max(e.at), t.task_id`, repo, state.EnabledAt, now.Add(-landing.ClaimWindow))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r root
			if err = rows.Scan(&r.task); err != nil {
				return err
			}
			if state.LatestOperation(r.task) == "" {
				due = append(due, r)
			}
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	var recorded []string
	for _, r := range due {
		appended := false
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			appended = false
			var mission, missionState string
			var accepted *time.Time
			var created time.Time
			var noMerge bool
			if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, r.task).Scan(&mission); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT m.state, m.created_at, COALESCE(m.scope_json->>'no_merge','') <> '',
			    (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id = $2
			    AND e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'accepted')
			  FROM agentrt.mission m WHERE m.mission_id = $1 FOR SHARE`, mission, r.task).Scan(&missionState, &created, &noMerge, &accepted); err != nil {
				return err
			}
			why := ""
			switch {
			case noMerge || created.Before(state.EnabledAt) || accepted == nil || accepted.Before(state.EnabledAt):
				return nil // rebound out of the handoff since the list (CUT §7.7 item 5)
			case missionState == "done" || missionState == "stopped" || missionState == "expired":
				why = "stale"
			case accepted != nil && !now.Before(accepted.Add(landing.ClaimWindow)):
				why = "bound"
			default:
				return nil // moved since the list (a rebind, a resume): the next pass reads it again
			}
			evidence := "mission " + missionState
			if why == "bound" {
				evidence = "its window ended unclaimed; mission " + missionState
			}
			var err error
			appended, err = failUnclaimedLocked(ctx, tx, stream, mission, r.task, why, evidence)
			return err
		}); err != nil {
			return recorded, err
		}
		// Reported only once committed (review 653 N653-1).
		if appended {
			recorded = append(recorded, r.task)
		}
	}
	return recorded, nil
}
