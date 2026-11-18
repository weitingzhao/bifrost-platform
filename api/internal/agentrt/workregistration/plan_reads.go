package workregistration

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// What the lineage offers the plan domain's reads of a root's work. The plan
// reaches these through its ExternalWorkPorts, which store binds; each reads
// in the caller's transaction, so its answer belongs to the caller's snapshot.

// recentRecords are the events a root's recent changes take from its work:
// every event carrying a record of that root, except heartbeats (a dispatch
// run sends one every 30 seconds; they only say it is alive).
const recentRecords = `FROM agentrt.event WHERE body_json->'external_work'->>'root'=$1 AND body_json->'external_work'->>'type'<>'heartbeat'`

// rootRecords are the records of a root's work, old and new alike.
const rootRecords = `FROM agentrt.event WHERE event_type IN ('external_work_noted','note') AND body_json->'external_work'->>'root'=$1`

// RecentRecord is one such event: the exact database JSON of the event and
// its id.
type RecentRecord struct {
	Raw json.RawMessage
	ID  int64
}

// RecentStreams gives each stream holding such events its highest sequence.
func RecentStreams(ctx context.Context, tx pgx.Tx, root string) (map[string]int64, error) {
	rows, err := tx.Query(ctx, `SELECT event_stream_id,max(seq) `+recentRecords+` GROUP BY event_stream_id`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var stream string
		var seq int64
		if err = rows.Scan(&stream, &seq); err != nil {
			return nil, err
		}
		out[stream] = seq
	}
	return out, rows.Err()
}

// RecentRecords lists such events, newest first and at most limit: only those
// within bounds (a JSON object of stream to highest sequence; a stream it does
// not name has none), with an id below before (0: any) and above after.
func RecentRecords(ctx context.Context, tx pgx.Tx, root string, bounds []byte, before, after int64, limit int) ([]RecentRecord, error) {
	rows, err := tx.Query(ctx, `SELECT to_jsonb(e),e.event_id FROM (SELECT event_id,event_stream_id,seq,event_type,actor_id,at,body_json `+recentRecords+` AND seq<=COALESCE(($2::jsonb->>event_stream_id)::bigint,0) AND ($3::bigint=0 OR event_id<$3) AND event_id>$4 ORDER BY event_id DESC LIMIT $5) e`, root, bounds, before, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecentRecord, error) {
		var e RecentRecord
		err := r.Scan(&e.Raw, &e.ID)
		return e, err
	})
}

// HasWork says whether any record names root.
func HasWork(ctx context.Context, tx pgx.Tx, root string) (bool, error) {
	var has bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 `+rootRecords+`)`, root).Scan(&has)
	return has, err
}

// Registrants lists, in no set order, who any record of root says registered
// its work.
func Registrants(ctx context.Context, tx pgx.Tx, root string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT body_json->'external_work'->>'registrant' `+rootRecords+` AND body_json->'external_work'->>'registrant' IS NOT NULL`, root)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
