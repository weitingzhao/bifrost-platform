package workregistration

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// WorkSpans are the stretches registered work ran between from and to (no
// runtime attempt runs any more): each registered external work from its
// start (or registration)
// to its end (or last heartbeat), read from the same latest notes plan-view
// reads.
func WorkSpans(ctx context.Context, tx pgx.Tx, from, to time.Time) ([][2]time.Time, error) {
	var spans [][2]time.Time
	rows, err := tx.Query(ctx, `SELECT body FROM (SELECT DISTINCT ON(event_stream_id) body_json->'external_work' AS body FROM agentrt.event
	  WHERE event_type IN ('external_work_noted','note') AND body_json ? 'external_work' ORDER BY event_stream_id, seq DESC) x`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var n Note
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, err
		}
		start := parseNoteTime(n.StartedAt)
		if start.IsZero() {
			start = parseNoteTime(n.RegisteredAt)
		}
		end := parseNoteTime(n.EndedAt)
		if end.IsZero() {
			end = parseNoteTime(n.HeartbeatAt)
		}
		if !start.IsZero() && !end.IsZero() && !start.After(to) && !end.Before(from) {
			spans = append(spans, [2]time.Time{start, end})
		}
	}
	return spans, rows.Err()
}

func parseNoteTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
