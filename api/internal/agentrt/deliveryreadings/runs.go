// Package deliveryreadings reads recorded delivery observations in caller-owned transactions.
package deliveryreadings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// Run is a recorded pipeline observation, not permission to deploy.
type Run struct {
	Run, Pipeline, Kind, Result, Repo, Revision string
	CreatedAt, StartedAt, CompletedAt           time.Time
}

func Runs(ctx context.Context, tx pgx.Tx, commit string) ([]Run, error) {
	rows, err := tx.Query(ctx, `SELECT body FROM (SELECT DISTINCT ON (body_json->>'run', body_json->>'uid') event_id, body_json AS body FROM agentrt.event
	  WHERE event_type = 'pipeline_seen' AND body_json @> jsonb_build_object('revision', $1::text)
	  ORDER BY body_json->>'run', body_json->>'uid', event_id DESC) x ORDER BY event_id`, commit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var b struct {
			Run, Pipeline, Kind, Result, Repo, Revision string
			CreatedAt                                   time.Time `json:"created_at"`
			StartedAt                                   time.Time `json:"started_at"`
			CompletedAt                                 time.Time `json:"completed_at"`
		}
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &b); err != nil {
			return nil, err
		}
		out = append(out, Run{Run: b.Run, Pipeline: b.Pipeline, Kind: b.Kind, Result: b.Result, Repo: b.Repo, Revision: b.Revision,
			CreatedAt: b.CreatedAt, StartedAt: b.StartedAt, CompletedAt: b.CompletedAt})
	}
	return out, rows.Err()
}
