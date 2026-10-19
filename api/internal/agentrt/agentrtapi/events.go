package agentrtapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /events without a stream reads across every stream, by event id
// (2.5 G6, CUT-round2-2.5-interface-2026-10-19.md §2; Owner 2026-10-19
// 「2.5 八条照建议」): what the Console's recent and activity views are made
// of, in place of the board's hand-written lines.
//
//	GET /events?limit=&types=&before_event_id=   the latest events, newest first
//	GET /events?after_event_id=&limit=&types=    the events after one, oldest first
//
// types is a comma-separated list of event types of the closed vocabulary;
// limit is 1 to 500 (default 100). The form with stream= is unchanged.

const (
	recentEventsDefault = 100
	recentEventsMax     = 500
)

func (a *API) getRecentEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("after_seq") != "" {
		writeError(w, http.StatusBadRequest, "bad_request", "after_seq goes with stream; across streams the cursor is after_event_id or before_event_id")
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
	id := func(name string) (int64, bool) {
		s := q.Get(name)
		if s == "" {
			return 0, true
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", name+" is an event id (a whole number from 1)")
			return 0, false
		}
		return n, true
	}
	after, ok := id("after_event_id")
	if !ok {
		return
	}
	before, ok := id("before_event_id")
	if !ok {
		return
	}
	if after != 0 && before != 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "after_event_id or before_event_id, not both")
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
	query := `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.event_id DESC), '[]') FROM (
	  SELECT * FROM agentrt.event WHERE ($1::text[] IS NULL OR event_type = ANY($1)) AND ($2::bigint = 0 OR event_id < $2)
	  ORDER BY event_id DESC LIMIT $3) e`
	cursor := before
	if after != 0 {
		query = `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.event_id), '[]') FROM (
		  SELECT * FROM agentrt.event WHERE ($1::text[] IS NULL OR event_type = ANY($1)) AND event_id > $2
		  ORDER BY event_id LIMIT $3) e`
		cursor = after
	}
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), query, types, cursor, limit).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
