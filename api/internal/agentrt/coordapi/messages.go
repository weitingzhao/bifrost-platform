package coordapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

type messageBody struct {
	To   string `json:"to"`
	Kind string `json:"kind"`
	Body string `json:"body"`
}

// HandleSendMessage records a message from the caller (as for proposals: the
// Owner, a seat, or the thread its session header names) to another actor.
func (rt Routes) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	var b messageBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	id, err := coord.SendMessage(r.Context(), rt.Host.DB(), who, b.To, b.Kind, []byte(b.Body))
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"message_id": id, "from": who.Actor, "to": b.To})
}

// mailReconcile is how often a waiting reader re-reads its inbox without being
// woken: the wake is a hint from the bus (MailWake), the inbox is the record,
// and this covers a wake that did not come.
const mailReconcile = 5 * time.Second

// HandleReadMessages is the caller's unread messages, oldest first (GET /messages):
// wait= 0 to 25 seconds holds the request until one arrives, woken by the bus
// (seat.mail.arrived through MailWake), limit= 1 to 100. A thread reads only
// with the token its session is held to (coord.Unread, TD-370).
func (rt Routes) HandleReadMessages(w http.ResponseWriter, r *http.Request) {
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	q := r.URL.Query()
	wait, limit := 0, 100
	if s := q.Get("wait"); s != "" {
		if wait, err = strconv.Atoi(s); err != nil || wait < 0 || wait > 25 {
			rt.Host.Error(w, http.StatusBadRequest, "bad_request", "wait is 0 to 25 seconds")
			return
		}
	}
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > 100 {
			rt.Host.Error(w, http.StatusBadRequest, "bad_request", "limit is 1 to 100")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(wait)*time.Second)
	defer cancel()
	poll := rt.Host.ClaimPoll()
	if poll <= 0 {
		poll = mailReconcile
	}
	// Listen before the first read: mail landing between a read and the wait
	// still wakes this request.
	var woken <-chan struct{}
	if wait > 0 && rt.Host.MailWake() != nil {
		var stop func()
		woken, stop = rt.Host.MailWake()(who.Actor)
		defer stop()
	}
	for {
		qctx := r.Context()
		if wait > 0 {
			qctx = ctx
		}
		ms, err := coord.Unread(qctx, rt.Host.DB(), who, limit)
		switch {
		case err != nil && r.Context().Err() != nil:
			return // the client went away
		case err != nil && wait > 0 && ctx.Err() != nil:
			ms = nil // the wait is over (review 264 B5)
		case err != nil:
			rt.Host.Fail(w, err)
			return
		}
		if ctx.Err() != nil && wait > 0 {
			if ms == nil {
				ms = []coord.Message{}
			}
			rt.Host.JSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "messages": ms})
			return
		}
		if len(ms) > 0 || wait == 0 {
			if ms == nil {
				ms = []coord.Message{}
			}
			rt.Host.JSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "messages": ms})
			return
		}
		select {
		case <-ctx.Done():
			rt.Host.JSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "messages": []coord.Message{}})
			return
		case <-woken:
		case <-time.After(poll):
		}
	}
}

// HandleThreadMessages is the caller's conversation with another actor, both
// directions, read or not (GET /messages/thread?with=…&limit=1..100), for the
// Console's chief of staff page.
func (rt Routes) HandleThreadMessages(w http.ResponseWriter, r *http.Request) {
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	// A thread's identity is the session header it sends, which no token
	// binds: a conversation, read or not, is the Owner's or a seat's to read
	// (review 269 B5).
	if who.Actor != "owner" && !strings.HasPrefix(who.Actor, "seat:") {
		rt.Host.Error(w, http.StatusForbidden, "forbidden", "a conversation is read by the Owner or a seat")
		return
	}
	q := r.URL.Query()
	limit := 50
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > 100 {
			rt.Host.Error(w, http.StatusBadRequest, "bad_request", "limit is 1 to 100")
			return
		}
	}
	ms, err := coord.Thread(r.Context(), rt.Host.DB(), who.Actor, q.Get("with"), limit)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	if ms == nil {
		ms = []coord.Message{}
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "with": q.Get("with"), "messages": ms})
}

type ackBody struct {
	MessageIDs []string `json:"message_ids"`
}

// HandleAckMessages acknowledges the caller's own messages (POST /messages/ack).
func (rt Routes) HandleAckMessages(w http.ResponseWriter, r *http.Request) {
	var b ackBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	done, err := coord.AckMessages(r.Context(), rt.Host.DB(), who, b.MessageIDs)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	if done == nil {
		done = []string{}
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"actor": who.Actor, "acknowledged": done})
}
