package agentrtapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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
func (a *API) keepActions(ctx context.Context, attempt string, raw json.RawMessage, out map[string]any) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var in []actions.Action
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		out["actions_refused"] = "actions: not a list of actions: " + err.Error()
		return
	}
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
	if after != "" && !validStreamID(after) {
		writeError(w, http.StatusBadRequest, "bad_request", "after is a stream id from a previous answer (digits-digits)")
		return
	}
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

// streamID is a Redis stream id, the cursor of an action stream; each half
// fits an unsigned 64-bit integer (review 265 C7).
var streamID = regexp.MustCompile(`^[0-9]{1,20}-[0-9]{1,20}$`)

func validStreamID(s string) bool {
	if !streamID.MatchString(s) {
		return false
	}
	ms, seq, _ := strings.Cut(s, "-")
	_, e1 := strconv.ParseUint(ms, 10, 64)
	_, e2 := strconv.ParseUint(seq, 10, 64)
	return e1 == nil && e2 == nil
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
// right), limit= 1 to 100. A thread reads only with the token its session is
// held to (store.Unread, TD-370).
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
		qctx := r.Context()
		if wait > 0 {
			qctx = ctx
		}
		ms, err := store.Unread(qctx, a.DB, who, limit)
		switch {
		case err != nil && r.Context().Err() != nil:
			return // the client went away
		case err != nil && wait > 0 && ctx.Err() != nil:
			ms = nil // the wait is over (review 264 B5)
		case err != nil:
			a.fail(w, err)
			return
		}
		if ctx.Err() != nil && wait > 0 {
			if ms == nil {
				ms = []store.Message{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "messages": ms})
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

// threadMessages is the caller's conversation with another actor, both
// directions, read or not (GET /messages/thread?with=…&limit=1..100), for the
// Console's chief of staff page.
func (a *API) threadMessages(w http.ResponseWriter, r *http.Request) {
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	// A thread's identity is the session header it sends, which no token
	// binds: a conversation, read or not, is the Owner's or a seat's to read
	// (review 269 B5).
	if who.Actor != "owner" && !strings.HasPrefix(who.Actor, "seat:") {
		writeError(w, http.StatusForbidden, "forbidden", "a conversation is read by the Owner or a seat")
		return
	}
	q := r.URL.Query()
	limit := 50
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit is 1 to 100")
			return
		}
	}
	ms, err := store.Thread(r.Context(), a.DB, who.Actor, q.Get("with"), limit)
	if err != nil {
		a.fail(w, err)
		return
	}
	if ms == nil {
		ms = []store.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "with": q.Get("with"), "messages": ms})
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
