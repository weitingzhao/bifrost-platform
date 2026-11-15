package server

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The action record (Owner 2026-11-15): a direct call of a catalogued action
// is recorded when it happens, by seat, with its reason, what undoes it, the
// law it rested on, how it ended and what the caller did not check, for the
// Owner's review afterwards. In this first phase the record only records: it
// never refuses or delays an action, and a failed append is logged, not
// returned. Calls made by the approval executor are not recorded here: the
// card is their record.

const noReason = "(the caller gave no reason)"

// directRecord turns a direct call into a record. The fields come from what
// the caller sent (reason, rollback, unchecked, the same keys the action
// trace reads) and from the action's own description; nothing is invented:
// what the caller left out says so.
func directRecord(ctx context.Context, r *http.Request, act actions.Action, params map[string]any, law string, status int) actionrecord.Record {
	seat := strings.TrimSpace(r.Header.Get(approvals.SessionHeader))
	if seat == "" {
		seat = actuation.PrincipalFromContext(ctx).Name
	}
	if seat == "" {
		seat = "unknown"
	}
	text := func(key string) string {
		v, _ := params[key].(string)
		return strings.TrimSpace(v)
	}
	rec := actionrecord.Record{
		Seat:        seat,
		Action:      act.ID,
		Target:      act.Describe(ctx, params, "").Summary,
		Reason:      text("reason"),
		Regret:      text("rollback"),
		RegretState: actionrecord.RegretDeclared,
		Law:         law,
		NotVerified: text("unchecked"),
	}
	if rec.Reason == "" {
		rec.Reason = noReason
	}
	if rec.Regret == "" {
		rec.RegretState = actionrecord.RegretUndeclared
	}
	switch {
	case status >= 200 && status < 300:
		rec.Outcome = actionrecord.OutcomeDone
	case status == http.StatusForbidden:
		rec.Outcome = actionrecord.OutcomeRefused
	default:
		rec.Outcome = actionrecord.OutcomeFailed
	}
	if hash, err := actions.ParamsHash(params); err == nil {
		rec.ParamsHash = hash
	}
	return rec
}

// recordDirect appends the record in the background; the action's answer
// never waits for it and never changes because of it.
func (s *Server) recordDirect(r *http.Request, act actions.Action, params map[string]any, law string, status int) {
	db := s.evidenceDB()
	if db == nil {
		slog.Warn("action record not written: the agent runtime database is not connected", "action", act.ID)
		return
	}
	rec := directRecord(r.Context(), r, act, params, law, status)
	safego.Go("action-record", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := actionrecord.Insert(ctx, db, rec, approvals.Redact); err != nil {
			slog.Error("action record not written", "action", rec.Action, "seat", rec.Seat, "err", err)
		}
	})
}

// statusCapture remembers the status code a handler wrote.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (c *statusCapture) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *statusCapture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.ResponseWriter.Write(b)
}

func (c *statusCapture) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *statusCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// handleRecords serves GET /api/v1/records?seat=&law=&action=&since=&until=&limit=,
// newest first. since and until are RFC 3339 times. more says the filter
// matched more than the limit, so a page is never mistaken for the whole.
func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	db := s.evidenceDB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the action record is not available on this platform"})
		return
	}
	q := r.URL.Query()
	f := actionrecord.Filter{Seat: q.Get("seat"), Law: q.Get("law"), Action: q.Get("action")}
	for key, dest := range map[string]*time.Time{"since": &f.Since, "until": &f.Until} {
		if v := q.Get(key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": key + " is an RFC 3339 time"})
				return
			}
			*dest = t
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is 1 to 1000"})
			return
		}
		f.Limit = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, more, err := actionrecord.List(ctx, db, f)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, rec := range list {
		out = append(out, map[string]any{
			"id": rec.ID, "at": rec.At, "seat": rec.Seat, "action": rec.Action, "target": rec.Target, "reason": rec.Reason,
			"regret": rec.Regret, "regret_state": rec.RegretState, "law": rec.Law, "outcome": rec.Outcome,
			"check_result": rec.CheckResult, "not_verified": rec.NotVerified, "params_hash": rec.ParamsHash,
			"approval_ref": rec.ApprovalRef, "correction_of": rec.CorrectionOf,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": out, "returned": len(out), "more": more})
}
