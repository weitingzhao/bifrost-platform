package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reading events across streams (2.5 G6; DESIGN-crew-intake §4.3, §4.6: one
// algorithm for GET /events and an intake client's updates). Event ids are
// not commit order across streams: a transaction can take an id and commit
// after another that took a larger one (a stream's seq has no such gap, it
// is taken under the stream's lock). So reading on is by transaction, as the
// scores' computation does (scores.go): a read lists the events of
// transactions older than every transaction still running (its snapshot's
// xmin), ordered by (txid, event_id), and the cursor is the last one listed,
// "<txid>.<event_id>". An event shows once every transaction older than its
// own has ended; none is skipped, none listed twice. The latest form's next
// is its snapshot's xmin: reading on from it lists again the events it
// showed from transactions not older than that, so a reader keeps each event
// once by its id. An event committed after a latest read, with an id below
// the ones it showed, is read on, not back.

// EventCursor is a read-on position: every event of a transaction below
// TxID, and of TxID itself up to EventID, has been listed.
type EventCursor struct{ TxID, EventID int64 }

func (c EventCursor) String() string {
	return strconv.FormatInt(c.TxID, 10) + "." + strconv.FormatInt(c.EventID, 10)
}

// Less says whether c is before d.
func (c EventCursor) Less(d EventCursor) bool {
	return c.TxID < d.TxID || (c.TxID == d.TxID && c.EventID < d.EventID)
}

// ParseEventCursor reads a cursor a read gave as next.
func ParseEventCursor(s string) (EventCursor, bool) {
	t, e, found := strings.Cut(s, ".")
	txid, err1 := strconv.ParseInt(t, 10, 64)
	event, err2 := strconv.ParseInt(e, 10, 64)
	if !found || err1 != nil || err2 != nil || txid < 0 || event < 0 {
		return EventCursor{}, false
	}
	return EventCursor{txid, event}, true
}

// EventFilter narrows a cross-stream read: Types (nil = any), streams left
// out by name (Hidden) and by the start of their name (HiddenPrefixes: the
// issue and intake streams, which only the issue routes read,
// DESIGN-crew-intake §5.3). IntakeClient keeps only what that intake client
// is sent (its updates, §2.1, §4.3: the update events of the issues it sees
// and the guide's notes), in the same statement as the page and its horizon,
// so an issue it comes to see is never read past.
type EventFilter struct {
	Types          []string
	Hidden         []string
	HiddenPrefixes []string
	IntakeClient   string
}

// hidden is the filter's list as the queries take it (never NULL).
func (f EventFilter) hidden() []string {
	if f.Hidden == nil {
		return []string{}
	}
	return f.Hidden
}

// hiddenPrefixes is the filter's prefixes as the queries take them.
func (f EventFilter) hiddenPrefixes() []string {
	if f.HiddenPrefixes == nil {
		return []string{}
	}
	return f.HiddenPrefixes
}

// IntakeGuideStream is the stream whose notes tell intake clients that the
// intake guide changed (guide_changed, §3.1).
const IntakeGuideStream = "intake:guide"

// filterSQL is the rest of a filter over an event row e: the prefixes
// (parameter p, text[]) and the intake client (parameter c, "" for none).
func filterSQL(p, c int) string {
	pp, cc := fmt.Sprintf("$%d", p), fmt.Sprintf("$%d", c)
	return `AND NOT EXISTS (SELECT 1 FROM unnest(` + pp + `::text[]) h(p) WHERE starts_with(e.event_stream_id, h.p))
	AND (` + cc + ` = '' OR (e.event_stream_id = '` + IntakeGuideStream + `' AND e.event_type = 'note') OR (` + updateTypesSQL + `
	  AND EXISTS (SELECT 1 FROM agentrt.issue i JOIN agentrt.intake_client c ON c.intake_client_id = ` + cc + `
	    WHERE e.event_stream_id = 'issue:' || i.issue_id AND i.product = ANY (c.products)
	      AND ((i.served_person IS NOT NULL AND i.served_person = c.on_behalf_of) OR (i.served_person IS NULL AND i.created_by_actor_id = ` + cc + `)))))`
}

// LatestEvents is the newest events, newest first (below before when it is
// not 0), as a JSON array of the rows, and where to read on from: the
// snapshot's xmin. One statement: the horizon and the page from the same
// snapshot.
func LatestEvents(ctx context.Context, tx pgx.Tx, before int64, limit int, f EventFilter) ([]byte, EventCursor, error) {
	var doc []byte
	var next EventCursor
	err := tx.QueryRow(ctx, `SELECT txid_snapshot_xmin(txid_current_snapshot()),
	  COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.event_id DESC) FROM (
	    SELECT e.* FROM agentrt.event e WHERE ($1::text[] IS NULL OR e.event_type = ANY($1)) AND ($2::bigint = 0 OR e.event_id < $2)
	      AND NOT (e.event_stream_id = ANY($4::text[])) `+filterSQL(5, 6)+`
	    ORDER BY e.event_id DESC LIMIT $3) p), '[]')`, f.Types, before, limit, f.hidden(), f.hiddenPrefixes(), f.IntakeClient).
		Scan(&next.TxID, &doc)
	return doc, next, err
}

// EventsAfter is the events committed since after, in (txid, event_id)
// order, up to limit, as a JSON array of the rows, and where to read on
// from: the last one listed when the page is full, else the horizon when it
// has passed after, else after itself.
func EventsAfter(ctx context.Context, tx pgx.Tx, after EventCursor, limit int, f EventFilter) ([]byte, EventCursor, error) {
	var doc []byte
	var next EventCursor
	var n int
	var horizon int64
	err := tx.QueryRow(ctx, `WITH h AS (SELECT txid_snapshot_xmin(txid_current_snapshot()) AS x),
	  page AS (SELECT e.* FROM agentrt.event e WHERE ($1::text[] IS NULL OR e.event_type = ANY($1))
	    AND e.txid >= $2 AND (e.txid, e.event_id) > ($2::bigint, $3::bigint) AND e.txid < (SELECT x FROM h)
	    AND NOT (e.event_stream_id = ANY($5::text[])) `+filterSQL(6, 7)+`
	    ORDER BY e.txid, e.event_id LIMIT $4)
	  SELECT (SELECT x FROM h), COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.txid, p.event_id) FROM page p), '[]'),
	    (SELECT count(*) FROM page),
	    COALESCE((SELECT txid FROM page ORDER BY txid DESC, event_id DESC LIMIT 1), 0),
	    COALESCE((SELECT event_id FROM page ORDER BY txid DESC, event_id DESC LIMIT 1), 0)`,
		f.Types, after.TxID, after.EventID, limit, f.hidden(), f.hiddenPrefixes(), f.IntakeClient).Scan(&horizon, &doc, &n, &next.TxID, &next.EventID)
	switch {
	case err != nil:
	case n == limit:
		// There may be more below the horizon: on from the last listed.
	case horizon > after.TxID:
		// Everything below the horizon is listed.
		next = EventCursor{TxID: horizon}
	default:
		next = after
	}
	return doc, next, err
}
