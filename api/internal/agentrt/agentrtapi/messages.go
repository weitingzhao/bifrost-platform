package agentrtapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// keepActions takes a heartbeat's actions into the attempt's stream (W-31 2.3;
// Owner 2026-10-18: Redis only, 24 hours). It never fails the heartbeat: the
// lease is renewed whatever happens to the actions, and the answer says what
// was kept. Called only after the heartbeat's fence held, so only the
// leaseholder's actions are kept.
func (a *API) keepActions(ctx context.Context, attempt string, in []actions.Action, out map[string]any) {
	if len(in) == 0 {
		return
	}
	if a.Actions == nil {
		out["actions_refused"] = "this platform keeps no actions (no redis-agents)"
		return
	}
	acts, dropped, err := actions.Normalize(in, time.Now())
	if err != nil {
		out["actions_refused"] = err.Error()
		return
	}
	actx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.Actions.Append(actx, attempt, acts); err != nil {
		a.logf("actions of %s not kept: %v", attempt, err)
		out["actions_refused"] = "not kept: redis-agents did not take them"
		return
	}
	out["actions_kept"] = len(acts)
	if dropped > 0 {
		out["actions_dropped"] = dropped
	}
}

// attemptActions is an attempt's action stream (GET /attempts/{id}/actions):
// after= a stream id from a previous answer, limit= 1 to 500 (default 200);
// last=1 answers only the newest action, for the Console's "now" line.
func (a *API) attemptActions(w http.ResponseWriter, r *http.Request) {
	if a.Actions == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "this platform keeps no actions (no redis-agents)")
		return
	}
	id, q := chi.URLParam(r, "id"), r.URL.Query()
	if q.Get("last") == "1" {
		e, ok, err := a.Actions.Last(r.Context(), id)
		if err != nil {
			a.fail(w, err)
			return
		}
		out := map[string]any{"attempt_id": id, "actions": []actions.Entry{}}
		if ok {
			out["actions"] = []actions.Entry{e}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	limit := 200
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 500 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit is 1 to 500")
			return
		}
		limit = n
	}
	after := q.Get("after")
	es, err := a.Actions.Read(r.Context(), id, after, limit)
	if err != nil {
		a.fail(w, err)
		return
	}
	next := after
	if len(es) > 0 {
		next = es[len(es)-1].ID
	}
	if es == nil {
		es = []actions.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempt_id": id, "actions": es, "next": next})
}

type messageBody struct {
	To   string `json:"to"`
	Kind string `json:"kind"`
	Body string `json:"body"`
}

// sendMessage records a message from the caller (as for proposals: the
// Owner, a seat, or the thread its session header names) to another actor.
func (a *API) sendMessage(w http.ResponseWriter, r *http.Request) {
	var b messageBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	id, err := store.SendMessage(r.Context(), a.DB, who, b.To, b.Kind, []byte(b.Body))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": id, "from": who.Actor, "to": b.To})
}

// readMessages is the caller's unread messages, oldest first (GET /messages):
// wait= 0 to 25 seconds holds the request until one arrives (a long poll on
// PostgreSQL, the record; the mailbox signal in Redis is not needed to be
// right), limit= 1 to 100.
func (a *API) readMessages(w http.ResponseWriter, r *http.Request) {
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	q := r.URL.Query()
	wait, limit := 0, 100
	if s := q.Get("wait"); s != "" {
		if wait, err = strconv.Atoi(s); err != nil || wait < 0 || wait > 25 {
			writeError(w, http.StatusBadRequest, "bad_request", "wait is 0 to 25 seconds")
			return
		}
	}
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit is 1 to 100")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(wait)*time.Second)
	defer cancel()
	poll := a.ClaimPoll
	if poll <= 0 {
		poll = time.Second
	}
	for {
		ms, err := store.Unread(r.Context(), a.DB, who.Actor, limit)
		if err != nil {
			a.fail(w, err)
			return
		}
		if len(ms) > 0 || wait == 0 {
			if ms == nil {
				ms = []store.Message{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "messages": ms})
			return
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "messages": []store.Message{}})
			return
		case <-time.After(poll):
		}
	}
}

type ackBody struct {
	MessageIDs []string `json:"message_ids"`
}

// ackMessages acknowledges the caller's own messages (POST /messages/ack).
func (a *API) ackMessages(w http.ResponseWriter, r *http.Request) {
	var b ackBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	done, err := store.AckMessages(r.Context(), a.DB, who, b.MessageIDs)
	if err != nil {
		a.fail(w, err)
		return
	}
	if done == nil {
		done = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "acknowledged": done})
}
