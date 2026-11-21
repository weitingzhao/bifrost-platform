package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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

// recordDirect appends the record of a direct call in the background; the
// action's answer never waits for it and never changes because of it. What
// the record says is decided in actionrecord.FromDirect; this reads the
// request and the action for it.
func (s *Server) recordDirect(r *http.Request, act actions.Action, params map[string]any, said map[string]any, law string, status int, body []byte) {
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
		Params:        actionrecord.WithCallerWords(params, said),
		Status:        status,
	}
	if hash, err := actions.ParamsHash(params); err == nil {
		d.ParamsHash = hash
	}
	rec := actionrecord.FromDirect(d)
	if run := applyRunOf(act.ID, body); run != "" && s.work != nil && s.work.Svc != nil {
		// The apply has only started: write the record once its run has
		// ended, with the run's outcome (actionrecord/apply_follow.go).
		outcome := s.work.Svc.ApplyRunOutcome
		safego.Go("action-record-apply-follow", func() {
			rec := actionrecord.AfterApplyRun(context.Background(), rec, run, outcome, applyFollowEvery, applyFollowMax)
			actionrecord.InsertInBackground(db, rec, approvals.Redact)
		})
		return
	}
	actionrecord.InsertInBackground(db, rec, approvals.Redact)
}

// How a direct apply's record follows its run: read every 5 s, for at most
// 15 minutes (a cicd apply takes about 30 s; a card's follow stops at 45 s
// because it holds the Owner's request, this one holds nothing).
const (
	applyFollowEvery = 5 * time.Second
	applyFollowMax   = 15 * time.Minute
)

// applyRunOf is the apply run an apply_manifest answer started, "" otherwise.
func applyRunOf(action string, body []byte) string {
	if action != "apply_manifest" || len(body) == 0 {
		return ""
	}
	var out struct {
		ApplyRun string `json:"apply_run"`
	}
	if json.Unmarshal(body, &out) != nil {
		return ""
	}
	return out.ApplyRun
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
	// keep holds the first bytes of the answer when keepBody is set (the
	// apply record reads the run it started from it).
	keepBody bool
	keep     []byte
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
	if c.keepBody && len(c.keep) < 16<<10 {
		c.keep = append(c.keep, b[:min(len(b), 16<<10-len(c.keep))]...)
	}
	return c.ResponseWriter.Write(b)
}

func (c *statusCapture) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *statusCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
