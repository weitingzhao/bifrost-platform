package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ConfigurationRequests reads the existing goal ledger. It is not another
// policy store: a request's state never establishes the installed value.
func ConfigurationRequests(ctx context.Context, db DB, limit int) ([]map[string]any, error) {
	out := []map[string]any{}
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT mission_id,goal,state,created_at FROM agentrt.mission WHERE starts_with(goal,'Delivery configuration: ') ORDER BY created_at DESC,mission_id DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, goal, state string
			var created time.Time
			if err = rows.Scan(&id, &goal, &state, &created); err != nil {
				return err
			}
			lines := strings.SplitN(goal, "\n", 3)
			if len(lines) < 2 {
				continue
			}
			var request map[string]string
			if json.Unmarshal([]byte(lines[1]), &request) != nil || request["boundary_id"] == "" {
				continue
			}
			out = append(out, map[string]any{"mission_id": id, "state": state, "created_at": created, "request": request})
		}
		return rows.Err()
	})
	return out, err
}
