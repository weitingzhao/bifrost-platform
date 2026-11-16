package workregistration

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"time"
)

// Snapshot reads registered work inside the caller's existing snapshot.
// Description validation remains with the caller; errors are returned unchanged.
func Snapshot(ctx context.Context, tx pgx.Tx, root string, now time.Time, description func(context.Context, pgx.Tx, string) (any, error)) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT body,at FROM (SELECT DISTINCT ON(event_stream_id) body_json->'external_work' AS body,at FROM agentrt.event WHERE event_type='note' AND body_json ? 'external_work' ORDER BY event_stream_id,seq DESC) x WHERE body->>'root'=$1 ORDER BY body->>'external_work_id'`, root)
	if err != nil {
		return nil, err
	}
	type record struct {
		n  Note
		at time.Time
	}
	rs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (record, error) {
		var x record
		var raw []byte
		err := r.Scan(&raw, &x.at)
		if err == nil {
			err = json.Unmarshal(raw, &x.n)
		}
		return x, err
	})
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, r := range rs {
		raw, _ := json.Marshal(r.n)
		var m map[string]any
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		d, err := description(ctx, tx, r.n.Artifact)
		if err != nil {
			return nil, err
		}
		m["description"] = d
		m["recorded_at"] = r.at.UTC().Truncate(time.Microsecond).Format(eventlog.TimeLayout)
		hb, _ := time.Parse(time.RFC3339Nano, r.n.HeartbeatAt)
		m["heartbeat_age_s"] = int64(now.Sub(hb).Seconds())
		m["status"] = r.n.State
		if (r.n.State == "Running" || r.n.State == "Registered") && now.Sub(hb) > 60*time.Second {
			m["status"] = "Unconfirmed"
		}
		start, _ := time.Parse(time.RFC3339Nano, r.n.StartedAt)
		end := now
		if r.n.EndedAt != "" {
			end, _ = time.Parse(time.RFC3339Nano, r.n.EndedAt)
		}
		if !start.IsZero() {
			m["elapsed_s"] = max(int64(0), int64(end.Sub(start).Seconds()))
		}
		m["backfilled"] = r.n.SourceStartedAt != ""
		out = append(out, m)
	}
	return out, nil
}
