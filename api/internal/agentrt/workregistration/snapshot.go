package workregistration

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"time"
)

// workRecord is the latest record of one stream of a root's work, as
// ReadRootActivity found it.
type workRecord struct {
	n  Note
	at time.Time
}

// projectWork makes the registered work of a root from its records, in their
// order, inside the caller's snapshot. Description validation remains with the
// caller; errors are returned unchanged. The caller's rows are closed by now:
// description reads in the same transaction.
func projectWork(ctx context.Context, tx pgx.Tx, rs []workRecord, now time.Time, description func(context.Context, pgx.Tx, string) (any, error)) ([]map[string]any, error) {
	out := []map[string]any{}
	for _, r := range rs {
		raw, _ := json.Marshal(r.n)
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
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
