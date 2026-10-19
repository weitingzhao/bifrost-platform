package agentrtapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /events without a stream reads across every stream (2.5 G6,
// CUT-round2-2.5-interface-2026-10-19.md §2; Owner 2026-10-19 「2.5 八条照建议」):
// what the Console's recent and activity views are made of, in place of the
// board's hand-written lines.
//
//	GET /events?limit=&types=&before_event_id=   the latest events, newest first; back by before_event_id
//	GET /events?after=<cursor>&limit=&types=     the events committed since the cursor
//
// Both answer {"events": [...], "next": "<cursor>"}: next is where to read
// on from. types is a comma-separated list of event types of the closed
// vocabulary; limit is 1 to 500 (default 100). The form with stream= is
// unchanged.
//
// Event ids are not commit order across streams: a transaction can take an
// id and commit after another that took a larger one (the stream form has
// no such gap, its seq is taken under the stream's lock). So reading on is
// by transaction, as the scores' computation does (store/scores.go): a read
// lists the events of transactions older than every transaction still
// running (its snapshot's xmin), ordered by (txid, event_id), and the cursor
// is the last one listed, "<txid>.<event_id>". An event shows once every
// transaction older than its own has ended; none is skipped, none listed
// twice. The latest form's next is its snapshot's xmin: reading on from it
// lists again the events it showed from transactions not older than that,
// so a reader keeps each event once by its id. An event committed after a
// latest read, with an id below the ones it showed, is read on, not back.

const (
	recentEventsDefault = 100
	recentEventsMax     = 500
)

// eventCursor is a read-on position: every event of a transaction below
// txid, and of txid itself up to event, has been listed.
type eventCursor struct{ txid, event int64 }

func (c eventCursor) String() string {
	return strconv.FormatInt(c.txid, 10) + "." + strconv.FormatInt(c.event, 10)
}

func parseEventCursor(s string) (eventCursor, bool) {
	t, e, found := strings.Cut(s, ".")
	txid, err1 := strconv.ParseInt(t, 10, 64)
	event, err2 := strconv.ParseInt(e, 10, 64)
	if !found || err1 != nil || err2 != nil || txid < 0 || event < 0 {
		return eventCursor{}, false
	}
	return eventCursor{txid, event}, true
}

func (a *API) getRecentEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("after_seq") != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "after_seq goes with stream; across streams the cursors are after and before_event_id")
		return
	}
	limit := recentEventsDefault
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > recentEventsMax {
			writeError(w, http.StatusBadRequest, "bad_request", "limit is 1 to 500")
			return
		}
		limit = n
	}
	var before int64
	if s := q.Get("before_event_id"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "before_event_id is an event id (a whole number from 1)")
			return
		}
		before = n
	}
	var after *eventCursor
	if s := q.Get("after"); s != "" {
		c, ok := parseEventCursor(s)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "after is a cursor a read gave as next (<txid>.<event_id>)")
			return
		}
		after = &c
	}
	if after != nil && before != 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "after or before_event_id, not both")
		return
	}
	var types []string
	if s := q.Get("types"); s != "" {
		for _, t := range strings.Split(s, ",") {
			if !store.IsEventType(t) {
				writeError(w, http.StatusBadRequest, "bad_request", "types names event types of the vocabulary; "+strconv.Quote(t)+" is none")
				return
			}
			types = append(types, t)
		}
	}
	var doc []byte
	var next eventCursor
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		if after == nil {
			// One statement: the horizon and the page from the same snapshot.
			return tx.QueryRow(r.Context(), `SELECT txid_snapshot_xmin(txid_current_snapshot()),
			  COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.event_id DESC) FROM (
			    SELECT * FROM agentrt.event WHERE ($1::text[] IS NULL OR event_type = ANY($1)) AND ($2::bigint = 0 OR event_id < $2)
			    ORDER BY event_id DESC LIMIT $3) e), '[]')`, types, before, limit).Scan(&next.txid, &doc)
		}
		var n int
		var horizon int64
		err := tx.QueryRow(r.Context(), `WITH h AS (SELECT txid_snapshot_xmin(txid_current_snapshot()) AS x),
		  page AS (SELECT e.* FROM agentrt.event e WHERE ($1::text[] IS NULL OR e.event_type = ANY($1))
		    AND e.txid >= $2 AND (e.txid, e.event_id) > ($2::bigint, $3::bigint) AND e.txid < (SELECT x FROM h)
		    ORDER BY e.txid, e.event_id LIMIT $4)
		  SELECT (SELECT x FROM h), COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.txid, p.event_id) FROM page p), '[]'),
		    (SELECT count(*) FROM page),
		    COALESCE((SELECT txid FROM page ORDER BY txid DESC, event_id DESC LIMIT 1), 0),
		    COALESCE((SELECT event_id FROM page ORDER BY txid DESC, event_id DESC LIMIT 1), 0)`,
			types, after.txid, after.event, limit).Scan(&horizon, &doc, &n, &next.txid, &next.event)
		switch {
		case err != nil:
		case n == limit:
			// There may be more below the horizon: on from the last listed.
		case horizon > after.txid:
			// Everything below the horizon is listed.
			next = eventCursor{txid: horizon}
		default:
			next = *after
		}
		return err
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": json.RawMessage(doc), "next": next.String()})
}
