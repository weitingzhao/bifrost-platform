package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LandedOnMain is, of tasks, those with a 'landed' note (RecordLandings: its
// accepted result is on main) and the main commit the first note names. One
// query; tasks without one are absent.
func LandedOnMain(ctx context.Context, db DB, tasks []string) (map[string]string, error) {
	out := map[string]string{}
	if len(tasks) == 0 {
		return out, nil
	}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (task_id) task_id, body_json->>'landed' FROM agentrt.event
		  WHERE task_id = ANY($1) AND event_type = 'note' AND body_json ? 'landed' ORDER BY task_id, event_id`, tasks)
		if err != nil {
			return err
		}
		landed, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]string, error) {
			var p [2]string
			return p, r.Scan(&p[0], &p[1])
		})
		for _, p := range landed {
			out[p[0]] = p[1]
		}
		return err
	})
	return out, err
}
