// Package assessmentreadings reads recorded advice for Owner cards and approvals.
package assessmentreadings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type Querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func Events(ctx context.Context, q Querier, stream string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := q.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		  'event_type', event_type, 'body_json', body_json, 'at', at) ORDER BY seq), '[]')
		  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&raw)
	return raw, err
}
