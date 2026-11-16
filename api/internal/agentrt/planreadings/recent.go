package planreadings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// recentScope picks the events Recent changes lists for root $2 (plan stream $1):
// the plan's own events, its external work except heartbeats (a dispatch run
// sends one every 30 seconds; they only say it is alive, which the run's own
// row already shows), and plan observations that changed a value.
const recentScope = `event_stream_id=$1 OR (body_json->'external_work'->>'root'=$2 AND body_json->'external_work'->>'type'<>'heartbeat') OR (body_json->'plan_observation'->>'root'=$2 AND body_json->'plan_observation'->>'changed'='true')`

// RecentStreams captures each relevant stream's committed sequence within tx.
func RecentStreams(ctx context.Context, tx pgx.Tx, stream, root string) (map[string]int64, error) {
	bounds := map[string]int64{}
	rows, err := tx.Query(ctx, `SELECT event_stream_id,max(seq) FROM agentrt.event WHERE `+recentScope+` GROUP BY event_stream_id`, stream, root)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var stream string
		var seq int64
		if err = rows.Scan(&stream, &seq); err != nil {
			rows.Close()
			return nil, err
		}
		bounds[stream] = seq
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return bounds, nil
}

// RecentEvent retains the exact database JSON plus its pagination position.
type RecentEvent struct {
	Raw json.RawMessage
	ID  int64
}

// RecentEvents reads at most 21 events under the caller's fixed stream bounds.
// The caller owns cursor validation and the public 20-event page size.
func RecentEvents(ctx context.Context, tx pgx.Tx, stream, root string, bounds []byte, before int64) ([]RecentEvent, error) {
	rows, err := tx.Query(ctx, `SELECT to_jsonb(e),e.event_id FROM (SELECT event_id,event_stream_id,seq,event_type,actor_id,at,body_json FROM agentrt.event WHERE (`+recentScope+`) AND seq<=COALESCE(($3::jsonb->>event_stream_id)::bigint,0) AND ($4::bigint=0 OR event_id<$4) ORDER BY event_id DESC LIMIT 21) e`, stream, root, bounds, before)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecentEvent, error) {
		var e RecentEvent
		err := r.Scan(&e.Raw, &e.ID)
		return e, err
	})
}
