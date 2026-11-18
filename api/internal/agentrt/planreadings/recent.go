package planreadings

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// RecentLimit is how many events one read of Recent changes takes: the public
// page of 20 and one more that says another page follows.
const RecentLimit = 21

// recentFrom picks the events Recent changes takes from the plan itself for
// root $2 (plan stream $1): the plan's own events and plan observations that
// changed a value. The events of its external work are the lineage's, read
// through the plan's port and merged by MergeRecentStreams and MergeRecent.
const recentFrom = `FROM agentrt.event WHERE (event_stream_id=$1 OR (body_json->'plan_observation'->>'root'=$2 AND body_json->'plan_observation'->>'changed'='true'))`

// RecentStreams captures the committed sequence of each stream the plan's own
// part of Recent changes reads, within tx.
func RecentStreams(ctx context.Context, tx pgx.Tx, stream, root string) (map[string]int64, error) {
	bounds := map[string]int64{}
	rows, err := tx.Query(ctx, `SELECT event_stream_id,max(seq) `+recentFrom+` GROUP BY event_stream_id`, stream, root)
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

// RecentEvents reads at most RecentLimit of the plan's own events under the
// caller's fixed stream bounds.
// The caller owns cursor validation and the public 20-event page size.
func RecentEvents(ctx context.Context, tx pgx.Tx, stream, root string, bounds []byte, before int64) ([]RecentEvent, error) {
	rows, err := tx.Query(ctx, `SELECT to_jsonb(e),e.event_id FROM (SELECT event_id,event_stream_id,seq,event_type,actor_id,at,body_json `+recentFrom+` AND seq<=COALESCE(($3::jsonb->>event_stream_id)::bigint,0) AND ($4::bigint=0 OR event_id<$4) ORDER BY event_id DESC LIMIT `+strconv.Itoa(RecentLimit)+`) e`, stream, root, bounds, before)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecentEvent, error) {
		var e RecentEvent
		err := r.Scan(&e.Raw, &e.ID)
		return e, err
	})
}

// MergeRecentStreams is the highest sequence of each stream in either read.
func MergeRecentStreams(own, work map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(own)+len(work))
	for _, m := range []map[string]int64{own, work} {
		for stream, seq := range m {
			out[stream] = max(out[stream], seq)
		}
	}
	return out
}

// MergeRecent is the newest RecentLimit events of the two reads, each once:
// an event of the plan's stream may carry a work's record and be in both.
func MergeRecent(own, work []RecentEvent) []RecentEvent {
	out := make([]RecentEvent, 0, len(own)+len(work))
	out = append(append(out, own...), work...)
	slices.SortStableFunc(out, func(a, b RecentEvent) int {
		switch {
		case a.ID > b.ID:
			return -1
		case a.ID < b.ID:
			return 1
		}
		return 0
	})
	out = slices.CompactFunc(out, func(a, b RecentEvent) bool { return a.ID == b.ID })
	if len(out) > RecentLimit {
		out = out[:RecentLimit]
	}
	return out
}
