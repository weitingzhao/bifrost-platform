package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// LandingCandidates lists the accepted bifrost-platform implement tasks the
// STG drill may still claim, oldest acceptance first. It leaves out, from the
// database alone, what EligibleToClaim would refuse before any Git read:
// a task accepted, or of a mission created, before enabledAt (before_enable),
// and a task whose mission is not active or carries no_merge (stale). Each
// candidate costs the drill fresh fetches (read between two transactions
// since TD-447 5; on 2026-10-27, when they were read inside one, twelve
// pre-enable tasks starved every other loop of STG's two connections). Roots
// of missions no longer active are not lost by being left out here: the
// pass's database step records them (SettleUnclaimed, TD-447 4).
func LandingCandidates(ctx context.Context, db DB, enabledAt time.Time) ([]string, error) {
	var tasks []string
	if enabledAt.IsZero() {
		return nil, nil
	}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id=t.mission_id
  JOIN agentrt.event e ON e.task_id=t.task_id
  WHERE t.repo='bifrost-platform' AND t.role='implement' AND t.state='accepted'
    AND m.state='active' AND COALESCE(m.scope_json->>'no_merge','')='' AND m.created_at >= $1
    AND e.event_type='task_state_changed' AND e.body_json->>'to'='accepted'
  GROUP BY t.task_id HAVING max(e.at) >= $1 ORDER BY max(e.at),t.task_id`, enabledAt)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var task string
			if err = rows.Scan(&task); err != nil {
				return err
			}
			tasks = append(tasks, task)
		}
		return rows.Err()
	})
	return tasks, err
}
