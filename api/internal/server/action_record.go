package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// The action record (Owner 2026-11-15): a direct call of a catalogued action
// is recorded when it happens, by seat, with its reason, what undoes it, the
// law it rested on, how it ended and what the caller did not check, for the
// Owner's review afterwards. In this first phase the record only records: it
// never refuses or delays an action, and a failed append is logged, not
// returned. Calls made by the approval executor are not recorded here: the
// card is their record.

// recordDirect appends the record of a direct call in the background; the
// action's answer never waits for it and never changes because of it. What
// the record says is decided in actionrecord.FromDirect; this reads the
// request and the action for it.
func (s *Server) recordDirect(r *http.Request, act actions.Action, params map[string]any, said map[string]any, law string, status int) {
	db := s.recordDB()
	if db == nil {
		slog.Warn("action record not written: the agent runtime database is not connected", "action", act.ID)
		return
	}
	d := actionrecord.Direct{
		SessionHeader: r.Header.Get(approvals.SessionHeader),
		Principal:     actuation.PrincipalFromContext(r.Context()).Name,
		Action:        act.ID,
		Target:        act.Describe(r.Context(), params, "").Summary,
		Law:           law,
		Params:        withCallerWords(params, said),
		Status:        status,
	}
	if hash, err := actions.ParamsHash(params); err == nil {
		d.ParamsHash = hash
	}
	actionrecord.InsertInBackground(db, actionrecord.FromDirect(d), approvals.Redact)
}

// traceWords are the body fields a caller writes for the record, not for the
// action: why, what undoes it, what was not checked.
var traceWords = []string{"reason", "rollback", "unchecked"}

// callerWords reads traceWords from a request body. The catalog's Normalize
// keeps only an action's declared params, so these were dropped before the
// record was written: every record of 11-15 to 11-19 said "(the caller gave
// no reason)" and regret_state undeclared, though callers sent all three
// (2026-11-19, LANE-ci-w1; 823 rows read).
func callerWords(raw []byte) map[string]any {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	out := map[string]any{}
	for _, k := range traceWords {
		if v, ok := body[k].(string); ok && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

// withCallerWords is params with the caller's words added where params has
// none; the action's own params win.
func withCallerWords(params, said map[string]any) map[string]any {
	if len(said) == 0 {
		return params
	}
	out := make(map[string]any, len(params)+len(said))
	for k, v := range said {
		out[k] = v
	}
	for k, v := range params {
		out[k] = v
	}
	return out
}

// recordDB is the agent runtime's database, nil when it is not connected.
func (s *Server) recordDB() eventlog.DB {
	if s.agentRuntime == nil || s.agentRuntime.api == nil {
		return nil
	}
	return s.agentRuntime.api.DB
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
