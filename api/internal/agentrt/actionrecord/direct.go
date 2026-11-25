package actionrecord

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The action record of a direct call (LAW-6): a direct call of a
// catalogued action is recorded when it happens, by seat, with its reason,
// what undoes it, the law it rested on, how it ended and what the caller did
// not check, for the Owner's review afterwards. The record only records: it
// never refuses or delays an action. Calls made by the approval executor are
// not recorded here: the card is their record.

// NoReason is what a record says when the caller gave no reason.
const NoReason = "(the caller gave no reason)"

// Direct is what the server knows of one direct call: who (the session header
// and, failing it, the authenticated principal), which action, what it
// concerns, the parameters the caller sent, the law it rested on, the HTTP
// status it ended with, and the hash of the parameters.
type Direct struct {
	SessionHeader, Principal string
	Action, Target, Law      string
	Params                   map[string]any
	Status                   int
	ParamsHash               string
}

// FromDirect turns a direct call into a record. The fields come from what the
// caller sent (reason, rollback, unchecked, the same keys the action trace
// reads); nothing is invented: what the caller left out says so.
func FromDirect(d Direct) Record {
	seat := strings.TrimSpace(d.SessionHeader)
	if seat == "" {
		seat = d.Principal
	}
	if seat == "" {
		seat = "unknown"
	}
	text := func(key string) string {
		v, _ := d.Params[key].(string)
		return strings.TrimSpace(v)
	}
	rec := Record{
		Seat:        seat,
		Action:      d.Action,
		Target:      d.Target,
		Reason:      text("reason"),
		Regret:      text("rollback"),
		RegretState: RegretDeclared,
		Law:         d.Law,
		NotVerified: text("unchecked"),
		ParamsHash:  d.ParamsHash,
	}
	if rec.Reason == "" {
		rec.Reason = NoReason
	}
	if rec.Regret == "" {
		rec.RegretState = RegretUndeclared
	}
	switch {
	case d.Status >= 200 && d.Status < 300:
		rec.Outcome = OutcomeDone
	case d.Status == http.StatusForbidden:
		rec.Outcome = OutcomeRefused
	default:
		rec.Outcome = OutcomeFailed
	}
	return rec
}

// ListHandler serves GET /api/v1/records?seat=&law=&action=&since=&until=&limit=,
// newest first. since and until are RFC 3339 times. more says the filter
// matched more than the limit, so a page is never mistaken for the whole.
// db is read per request: nil means the record is not connected.
func ListHandler(db func() eventlog.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := db()
		if d == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the action record is not available on this platform"})
			return
		}
		q := r.URL.Query()
		f := Filter{Seat: q.Get("seat"), Law: q.Get("law"), Action: q.Get("action")}
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
		list, more, err := List(ctx, d, f)
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
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"records": out, "returned": len(out), "more": more})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// InsertInBackground appends the record in the background; the action's
// answer never waits for it and never changes because of it. A failed append
// is logged, not returned.
func InsertInBackground(db eventlog.DB, rec Record, redact Redactor) {
	safego.Go("action-record", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := Insert(ctx, db, rec, redact); err != nil {
			slog.Error("action record not written", "action", rec.Action, "seat", rec.Seat, "err", err)
		}
	})
}
