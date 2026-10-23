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
// Reading on across streams is by transaction, not by event id: the cursor
// "<txid>.<event_id>" and its rules are the store's (store/eventcursor.go),
// shared with an intake client's updates (DESIGN-crew-intake §4.3).

const (
	recentEventsDefault = 100
	recentEventsMax     = 500
)

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
	var after *store.EventCursor
	if s := q.Get("after"); s != "" {
		c, ok := store.ParseEventCursor(s)
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
	filter := store.EventFilter{Types: types, Hidden: hiddenFrom(r)}
	var doc []byte
	var next store.EventCursor
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		var err error
		if after == nil {
			doc, next, err = store.LatestEvents(r.Context(), tx, before, limit, filter)
		} else {
			doc, next, err = store.EventsAfter(r.Context(), tx, *after, limit, filter)
		}
		return err
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": json.RawMessage(doc), "next": next.String()})
}
