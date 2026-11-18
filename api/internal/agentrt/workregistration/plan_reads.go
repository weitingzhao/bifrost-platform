package workregistration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// What the lineage offers the plan domain's reads of a root's work. The plan
// reaches these through its ExternalWorkPorts, which store binds; each reads
// in the caller's transaction, so its answer belongs to the caller's snapshot.

// rootRecords are the records of a root's work, old and new alike.
const rootRecords = `FROM agentrt.event WHERE event_type IN ('external_work_noted','note') AND body_json->'external_work'->>'root'=$1`

// rootActivity reads, in one scan of the ledger, everything a root's plan view
// takes from the records of its work. seen is every event carrying a record,
// reduced to what choosing among them needs; latest is each stream's newest
// record, which says what root the stream's work is under now (any root: a
// stream that moved away is not this root's); mine is the records naming the
// root, whatever came later; listed leaves heartbeats out (a dispatch run sends
// one every 30 seconds; they only say it is alive); page is the part of listed
// asked for, $2 being its bounds (NULL: the first page, where every listed event
// is within them), $3 the id it stops below (0: none) and $4 its size. Only the
// events the answer shows are read whole, by id.
const rootActivity = `WITH seen AS MATERIALIZED (
 SELECT event_id,event_stream_id,seq,event_type,x->>'root' AS root,x->>'type' AS kind,x->>'registrant' AS registrant FROM (
  SELECT event_id,event_stream_id,seq,event_type,body_json->'external_work' AS x FROM agentrt.event WHERE body_json ? 'external_work') r
), noted AS (SELECT * FROM seen WHERE event_type IN ('external_work_noted','note')
), latest AS (SELECT DISTINCT ON(event_stream_id) event_id,root FROM noted ORDER BY event_stream_id,seq DESC
), current AS (SELECT event_id FROM latest WHERE root=$1
), listed AS (SELECT event_id,event_stream_id,seq FROM seen WHERE root=$1 AND kind<>'heartbeat'
), page AS (SELECT event_id FROM listed WHERE ($2::jsonb IS NULL OR seq<=COALESCE(($2::jsonb->>event_stream_id)::bigint,0)) AND ($3::bigint=0 OR event_id<$3) ORDER BY event_id DESC LIMIT $4
), whole AS (SELECT e.event_id,e.event_stream_id,e.seq,e.event_type,e.actor_id,e.at,e.body_json FROM agentrt.event e WHERE e.event_id IN (SELECT event_id FROM current UNION SELECT event_id FROM page))
SELECT event_stream_id,max(seq),NULL::text,NULL::jsonb,NULL::bigint,NULL::jsonb,NULL::timestamptz,NULL::bigint FROM listed GROUP BY event_stream_id
UNION ALL SELECT DISTINCT NULL::text,NULL::bigint,registrant,NULL::jsonb,NULL::bigint,NULL::jsonb,NULL::timestamptz,NULL::bigint FROM noted WHERE root=$1 AND registrant IS NOT NULL
UNION ALL SELECT NULL,NULL,NULL,to_jsonb(e),e.event_id,NULL,NULL,NULL FROM whole e WHERE e.event_id IN (SELECT event_id FROM page)
UNION ALL SELECT NULL,NULL,NULL,NULL,NULL,w.body_json->'external_work',w.at,row_number() OVER (ORDER BY w.body_json->'external_work'->>'external_work_id') FROM whole w WHERE w.event_id IN (SELECT event_id FROM current)
ORDER BY 8 NULLS LAST,5 DESC NULLS LAST`

// RecentRecord is one event of a root's work that its recent changes list:
// the exact database JSON of the event and its id.
type RecentRecord struct {
	Raw json.RawMessage
	ID  int64
}

// RecentPage says which of a root's listed events to read: within Bounds (a
// JSON object of stream to highest sequence, a stream it does not name having
// none; nil: all of them), with an id below Before (0: any), at most Limit.
type RecentPage struct {
	Bounds []byte
	Before int64
	Limit  int
}

// RootActivity is what one read gives of a root's work.
type RootActivity struct {
	// Work is the work registered under the root, each the JSON fields of
	// Note plus description, recorded_at, heartbeat_age_s, status, elapsed_s
	// and backfilled, by external_work_id.
	Work []map[string]any
	// Streams gives each stream holding listed events its highest sequence.
	Streams map[string]int64
	// Recent is the asked page of those events, newest first.
	Recent []RecentRecord
	// Registrants is, in no set order, who any record of the root says
	// registered its work.
	Registrants []string
}

// ReadRootActivity reads a root's activity in one statement, inside the
// caller's snapshot. Description validation remains with the caller.
func ReadRootActivity(ctx context.Context, tx pgx.Tx, root string, now time.Time, page RecentPage, description func(context.Context, pgx.Tx, string) (any, error)) (RootActivity, error) {
	out, records, err := readRootRows(ctx, tx, root, page)
	if err != nil {
		return RootActivity{}, err
	}
	if out.Work, err = projectWork(ctx, tx, records, now, description); err != nil {
		return RootActivity{}, err
	}
	return out, nil
}

func readRootRows(ctx context.Context, tx pgx.Tx, root string, page RecentPage) (RootActivity, []workRecord, error) {
	var bounds any
	if page.Bounds != nil {
		bounds = page.Bounds
	}
	out := RootActivity{Streams: map[string]int64{}, Recent: []RecentRecord{}, Registrants: []string{}}
	var records []workRecord
	rows, err := tx.Query(ctx, rootActivity, root, bounds, page.Before, page.Limit)
	if err != nil {
		return out, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stream, registrant *string
		var seq, id, order *int64
		var raw json.RawMessage
		var work []byte
		var at *time.Time
		if err = rows.Scan(&stream, &seq, &registrant, &raw, &id, &work, &at, &order); err != nil {
			return out, nil, err
		}
		switch {
		case stream != nil && seq != nil:
			out.Streams[*stream] = *seq
		case registrant != nil:
			out.Registrants = append(out.Registrants, *registrant)
		case id != nil:
			out.Recent = append(out.Recent, RecentRecord{Raw: raw, ID: *id})
		case order != nil && at != nil:
			r := workRecord{at: *at}
			if err = json.Unmarshal(work, &r.n); err != nil {
				return out, nil, err
			}
			records = append(records, r)
		}
	}
	return out, records, rows.Err()
}

// HasWork says whether any record names root.
func HasWork(ctx context.Context, tx pgx.Tx, root string) (bool, error) {
	var has bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 `+rootRecords+`)`, root).Scan(&has)
	return has, err
}
