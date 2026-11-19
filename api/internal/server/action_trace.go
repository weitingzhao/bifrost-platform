package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// The trace envelope and what is redacted live in actionrecord (Trace); the
// server's names for them are kept for the callers that wire them.
type actionTrace = actionrecord.Trace

type traceContextKey struct{}

const (
	traceMissing         = actionrecord.TraceMissing
	traceUnavailable     = actionrecord.TraceUnavailable
	traceRequiredMessage = actionrecord.TraceRequiredMessage
)

func newActionTrace(action, mode string, params map[string]any) *actionTrace {
	return actionrecord.NewTrace(action, mode, params)
}

func traceFrom(ctx context.Context) *actionTrace {
	t, _ := ctx.Value(traceContextKey{}).(*actionTrace)
	return t
}

func traceAction(id string) bool { return id == "start_pipeline_run" || id == "apply_manifest" }

// Observe the existing guard and handler as a unit, including refusals and
// uncertain responses. Executor calls are recorded by the approval observer.
func (s *Server) tracedAction(id string, next http.HandlerFunc) http.HandlerFunc {
	if !traceAction(id) {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if fromExecutor(r.Context()) {
			next(w, r)
			return
		}
		raw, err := readAndRestore(r)
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
			p["name"] = chi.URLParam(r, "name")
		}
		t := newActionTrace(id, "direct", p)
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
			writeJSON(rec, http.StatusBadRequest, map[string]string{"error": traceRequiredMessage})
		} else {
			next(rec, r)
		}
		var result any
		if json.Unmarshal(rec.Body.Bytes(), &result) != nil {
			result = rec.Body.String()
		}
		t.Result = map[string]any{"http_status": rec.Code, "response": result}
		if missing && rec.Code < 400 && s.audit != nil {
			s.audit.Record(r, "action_trace.missing_fields", id, "warning", "reason, unchecked or rollback not provided by caller; compatibility mode")
		}
		s.recordActionTrace(r.Context(), t)
		for k, vs := range rec.Header() {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}
}

func (s *Server) beginApprovalTrace(ctx context.Context, a approvals.Approval) context.Context {
	if !traceAction(a.Action) {
		return ctx
	}
	t := newActionTrace(a.Action, "human_approval", a.Params)
	t.Approval = &a
	t.Reason, t.Rollback = a.Reason, a.Rollback
	if t.Rollback == "" {
		t.Rollback = traceMissing
	}
	t.Unchecked = "not_separately_provided; see approval.reason verbatim"
	t.Initiator = map[string]any{"authenticated_principal": actuation.PrincipalFromContext(ctx), "authenticated_scope": "execution_context_not_original_requester", "original_authenticated_requester": traceUnavailable, "claimed_requester": a.Requester, "claimed_thread": a.RequesterThread}
	t.Decision = map[string]any{"approval_id": a.ID, "approval": a}
	return context.WithValue(ctx, traceContextKey{}, t)
}

func (s *Server) endApprovalTrace(ctx context.Context, result any, err error) {
	t := traceFrom(ctx)
	if t == nil {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	t.Result = map[string]any{"response": result, "error": msg}
	s.recordActionTrace(ctx, t)
}

func (s *Server) recordActionTrace(ctx context.Context, t *actionTrace) {
	env := traceUnavailable
	if s.agentRuntime != nil {
		env = s.agentRuntime.env
	}
	raw, err := t.Seal(env, approvalnotify.Redact)
	if err == nil {
		if s.agentRuntime == nil || s.agentRuntime.late == nil {
			err = errors.New("agent runtime event database unavailable")
		} else {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = actionrecord.AppendTrace(ctx, s.agentRuntime.late, raw)
		}
	}
	if err != nil {
		msg := approvalnotify.Redact(err.Error())
		slog.Error("action trace append failed", "action", t.Action, "request_hash", t.RequestHash, "err", msg)
		if s.audit != nil {
			s.audit.RecordDirect("controller", actuation.RoleOperator, "action_trace.append_failed", t.Action, "alert", msg+" request_hash="+t.RequestHash)
		}
	}
}
