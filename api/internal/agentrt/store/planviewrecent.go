package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type viewRecentCursor struct {
	Root    string           `json:"root"`
	AsOf    string           `json:"as_of"`
	Streams map[string]int64 `json:"streams"`
	Before  int64            `json:"before"`
}

// A continuation fixes each stream's committed sequence, not a global
// event-id high water mark: a late commit cannot enter an older page.
func planViewRecent(ctx context.Context, tx pgx.Tx, root, cursor string, now time.Time) (json.RawMessage, string, string, error) {
	c := viewRecentCursor{Root: root, AsOf: timeText(now), Streams: map[string]int64{}}
	if cursor != "" {
		if len(cursor) > 128*1024 {
			return nil, "", "", invalid("recent cursor too large")
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", "", invalid("invalid recent cursor")
		}
		if err = strictViewJSON(raw, &c); err != nil {
			return nil, "", "", err
		}
		if c.Root != root || len(c.Streams) > 2000 || c.Before < 1 {
			return nil, "", "", invalid("recent cursor belongs to another root or is invalid")
		}
		if _, err = time.Parse(time.RFC3339Nano, c.AsOf); err != nil {
			return nil, "", "", invalid("invalid cursor time")
		}
	} else {
		rows, err := tx.Query(ctx, `SELECT event_stream_id,max(seq) FROM agentrt.event WHERE event_stream_id=$1 OR body_json->'external_work'->>'root'=$2 OR (event_stream_id LIKE 'landing:%' AND body_json->'landing'->>'op' IN (SELECT e.body_json->'landing'->>'op' FROM agentrt.event e JOIN agentrt.task t ON t.task_id=e.body_json->'landing'->>'task' JOIN agentrt.plan_node step ON step.plan_node_id=t.plan_node_id JOIN agentrt.plan_node item ON item.plan_node_id=step.parent_plan_node_id WHERE e.event_stream_id LIKE 'landing:%' AND item.parent_plan_node_id=$2)) OR (body_json->'plan_observation'->>'root'=$2 AND body_json->'plan_observation'->>'changed'='true') GROUP BY event_stream_id`, PlanStream(root), root)
		if err != nil {
			return nil, "", "", err
		}
		for rows.Next() {
			var stream string
			var seq int64
			if err = rows.Scan(&stream, &seq); err != nil {
				rows.Close()
				return nil, "", "", err
			}
			c.Streams[stream] = seq
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", "", err
		}
	}
	bounds, err := json.Marshal(c.Streams)
	if err != nil {
		return nil, "", "", err
	}
	rows, err := tx.Query(ctx, `SELECT to_jsonb(e),e.event_id FROM (SELECT event_id,event_stream_id,seq,event_type,actor_id,at,body_json FROM agentrt.event WHERE (event_stream_id=$1 OR body_json->'external_work'->>'root'=$2 OR (event_stream_id LIKE 'landing:%' AND body_json->'landing'->>'op' IN (SELECT e.body_json->'landing'->>'op' FROM agentrt.event e JOIN agentrt.task t ON t.task_id=e.body_json->'landing'->>'task' JOIN agentrt.plan_node step ON step.plan_node_id=t.plan_node_id JOIN agentrt.plan_node item ON item.plan_node_id=step.parent_plan_node_id WHERE e.event_stream_id LIKE 'landing:%' AND item.parent_plan_node_id=$2)) OR (body_json->'plan_observation'->>'root'=$2 AND body_json->'plan_observation'->>'changed'='true')) AND seq<=COALESCE(($3::jsonb->>event_stream_id)::bigint,0) AND ($4::bigint=0 OR event_id<$4) ORDER BY event_id DESC LIMIT 21) e`, PlanStream(root), root, bounds, c.Before)
	if err != nil {
		return nil, "", "", err
	}
	type event struct {
		raw json.RawMessage
		id  int64
	}
	events, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (event, error) { var e event; err := r.Scan(&e.raw, &e.id); return e, err })
	if err != nil {
		return nil, "", "", err
	}
	next := ""
	if len(events) > 20 {
		events = events[:20]
		c.Before = events[19].id
		raw, err := json.Marshal(c)
		if err != nil {
			return nil, "", "", err
		}
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	out := []json.RawMessage{}
	for _, e := range events {
		out = append(out, e.raw)
	}
	raw, err := json.Marshal(out)
	return raw, next, c.AsOf, err
}
