package store

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// LandingCandidates orders by acceptance, never task creation or map order.
func LandingCandidates(ctx context.Context, db DB) ([]string, error) {
	var tasks []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.event e ON e.task_id=t.task_id WHERE t.repo='bifrost-platform' AND t.role='implement' AND t.state='accepted' AND e.event_type='task_state_changed' AND e.body_json->>'to'='accepted' GROUP BY t.task_id ORDER BY max(e.at),t.task_id`)
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
