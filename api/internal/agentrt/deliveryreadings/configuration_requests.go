package deliveryreadings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// ConfigurationRequests reads recorded requests; it never establishes installed policy.
func ConfigurationRequests(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, limit int) ([]map[string]any, error) {
	out := []map[string]any{}
	rows, err := q.Query(ctx, `SELECT mission_id,goal,state,created_at FROM agentrt.mission WHERE starts_with(goal,'Delivery configuration: ') ORDER BY created_at DESC,mission_id DESC LIMIT $1`, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, goal, state string
		var created time.Time
		if err = rows.Scan(&id, &goal, &state, &created); err != nil {
			return out, err
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
	return out, rows.Err()
}

// ConfigurationRequestsReadOnly runs the read in its own read-only transaction.
func ConfigurationRequestsReadOnly(ctx context.Context, db interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, limit int) ([]map[string]any, error) {
	out := []map[string]any{}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return ConfigurationRequests(ctx, tx, limit)
}
