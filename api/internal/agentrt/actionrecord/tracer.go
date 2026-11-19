package actionrecord

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

type traceContextKey struct{}

// TraceFrom is the trace of the action this context runs, nil when none.
func TraceFrom(ctx context.Context) *Trace {
	t, _ := ctx.Value(traceContextKey{}).(*Trace)
	return t
}

// Traced reports whether the action id is traced.
func Traced(id string) bool { return id == "start_pipeline_run" || id == "apply_manifest" }

// Tracer is what recording an action's trace stands on, handed in by the
// process that runs the actions.
type Tracer struct {
	Env    func() string      // the environment, "" when no runtime is wired
	DB     func() eventlog.DB // the runtime's event database, nil when none
	Redact func(string) string
	// FromExecutor says the call is the approval executor's: its record is the
	// approval observer's.
	FromExecutor func(context.Context) bool
	ReadBody     func(*http.Request) ([]byte, error)
	// PipelineOf is the pipeline a start_pipeline_run request names.
	PipelineOf func(*http.Request) string
	// MissingFields audits a call that succeeded without its trace fields;
	// AppendFailed a trace that could not be written.
	MissingFields func(r *http.Request, action string)
	AppendFailed  func(action, detail string)
}

// Middleware observes the existing guard and handler as a unit, including
// refusals and uncertain responses. Executor calls are recorded by the
// approval observer.
func (c Tracer) Middleware(id string, next http.HandlerFunc) http.HandlerFunc {
	if !Traced(id) {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if c.FromExecutor(r.Context()) {
			next(w, r)
			return
		}
		raw, err := c.ReadBody(r)
		if err != nil {
			next(w, r)
			return
		}
		p := map[string]any{}
		if len(raw) > 0 && json.Unmarshal(raw, &p) != nil {
			next(w, r)
			return
		}
		if p == nil {
			p = map[string]any{}
		}
		if id == "start_pipeline_run" {
			p["name"] = c.PipelineOf(r)
		}
		t := NewTrace(id, "direct", p)
		t.Initiator = map[string]any{"authenticated_principal": actuation.PrincipalFromContext(r.Context()), "authenticated_scope": "direct_request", "claimed_who": p["who"], "claimed_session": r.Header.Get("X-Bifrost-Session"), "claimed_agent_session": r.Header.Get("X-Agent-Session-ID")}
		r = r.WithContext(context.WithValue(r.Context(), traceContextKey{}, t))
		missing := t.Missing()
		// Console and MCP can share an operator credential. Browser metadata is
		// only a caller claim, never authorization or proof of a human identity.
		// Internal approval/system paths are distinguished by Go context/calls.
		browser := r.Header.Get("Sec-Fetch-Mode") != "" && r.Header.Get("X-Bifrost-Session") == "" && r.Header.Get("X-Agent-Session-ID") == ""
		t.Initiator.(map[string]any)["claimed_browser"] = browser
		required := !browser
		strict := strings.EqualFold(os.Getenv("ACTION_TRACE_REQUIRED"), "true") || os.Getenv("ACTION_TRACE_REQUIRED") == "1"
		rec := httptest.NewRecorder()
		if required && missing && strict {
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(rec).Encode(map[string]string{"error": TraceRequiredMessage})
		} else {
			next(rec, r)
		}
		var result any
		if json.Unmarshal(rec.Body.Bytes(), &result) != nil {
			result = rec.Body.String()
		}
		t.Result = map[string]any{"http_status": rec.Code, "response": result}
		if missing && rec.Code < 400 {
			c.MissingFields(r, id)
		}
		c.Record(r.Context(), t)
		for k, vs := range rec.Header() {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}
}

// ApprovalRun is the approval an action is executed under, as its trace says it.
type ApprovalRun struct {
	ID, Action, Reason, Rollback string
	Requester, RequesterThread   string
	Params                       map[string]any
	Record                       any // the approval itself
}

// BeginApproval starts the trace of an approved execution.
func (c Tracer) BeginApproval(ctx context.Context, a ApprovalRun) context.Context {
	if !Traced(a.Action) {
		return ctx
	}
	t := NewTrace(a.Action, "human_approval", a.Params)
	t.Approval = a.Record
	t.Reason, t.Rollback = a.Reason, a.Rollback
	if t.Rollback == "" {
		t.Rollback = TraceMissing
	}
	t.Unchecked = "not_separately_provided; see approval.reason verbatim"
	t.Initiator = map[string]any{"authenticated_principal": actuation.PrincipalFromContext(ctx), "authenticated_scope": "execution_context_not_original_requester", "original_authenticated_requester": TraceUnavailable, "claimed_requester": a.Requester, "claimed_thread": a.RequesterThread}
	t.Decision = map[string]any{"approval_id": a.ID, "approval": a.Record}
	return context.WithValue(ctx, traceContextKey{}, t)
}

// EndApproval records the trace BeginApproval started, with the result.
func (c Tracer) EndApproval(ctx context.Context, result any, err error) {
	t := TraceFrom(ctx)
	if t == nil {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	t.Result = map[string]any{"response": result, "error": msg}
	c.Record(ctx, t)
}

// RecordSystem records a decision the platform took by itself.
func (c Tracer) RecordSystem(ctx context.Context, action, mode, principal, why string, params map[string]any, decision, result any) {
	t := NewTrace(action, mode, params)
	t.Initiator = map[string]any{"internal_principal": principal, "external_authenticated_requester": TraceUnavailable}
	t.Reason, t.Unchecked, t.Rollback = why, TraceUnavailable, TraceUnavailable
	t.Decision = decision
	t.Result = result
	c.Record(ctx, t)
}

// Record seals and appends a trace. A failure is logged and audited; it never
// changes the action's result.
func (c Tracer) Record(ctx context.Context, t *Trace) {
	env := c.Env()
	if env == "" {
		env = TraceUnavailable
	}
	raw, err := t.Seal(env, c.Redact)
	if err == nil {
		if db := c.DB(); db == nil {
			err = errors.New("agent runtime event database unavailable")
		} else {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = AppendTrace(ctx, db, raw)
		}
	}
	if err != nil {
		msg := c.Redact(err.Error())
		slog.Error("action trace append failed", "action", t.Action, "request_hash", t.RequestHash, "err", msg)
		c.AppendFailed(t.Action, msg+" request_hash="+t.RequestHash)
	}
}
