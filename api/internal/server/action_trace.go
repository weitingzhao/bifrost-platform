package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// Action traces copy materials already used by the executor. They never
// authorize an action and a failed append must not change its result.
type actionTrace struct {
	Environment string              `json:"environment"`
	Version     int                 `json:"version"`
	Action      string              `json:"action"`
	Mode        string              `json:"mode"`
	At          time.Time           `json:"at"`
	RequestHash string              `json:"request_hash"`
	Object      any                 `json:"object"`
	Initiator   any                 `json:"initiator"`
	Reason      string              `json:"reason"`
	Decision    any                 `json:"decision"`
	Unchecked   string              `json:"unchecked"`
	Rollback    string              `json:"rollback"`
	Result      any                 `json:"result"`
	Approval    *approvals.Approval `json:"approval,omitempty"`
}

type traceContextKey struct{}

const traceMissing = "not_provided_by_caller"
const traceUnavailable = "unavailable_at_this_step"
const traceRequiredMessage = "reason, unchecked and rollback are required: reason explains why this action is needed; unchecked lists checks not performed (say none if all were performed); rollback explains how to revert"

func traceValue(p map[string]any, key string) string {
	v, _ := p[key].(string)
	if strings.TrimSpace(v) == "" {
		return traceMissing
	}
	return v
}

func newActionTrace(action, mode string, params map[string]any) *actionTrace {
	raw, _ := json.Marshal(params)
	hash := sha256.Sum256(raw)
	return &actionTrace{Version: 1, Action: action, Mode: mode, At: time.Now().UTC(), RequestHash: hex.EncodeToString(hash[:]), Object: params,
		Reason: traceValue(params, "reason"), Unchecked: traceValue(params, "unchecked"), Rollback: traceValue(params, "rollback"), Decision: traceUnavailable, Result: traceUnavailable}
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
		missing := t.Reason == traceMissing || t.Unchecked == traceMissing || t.Rollback == traceMissing
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

// Redact before serializing the final envelope, so newlines in secret shapes
// remain visible to the existing redactor and the resulting JSON stays valid.
func redactTrace(v any) any {
	switch x := v.(type) {
	case string:
		return approvalnotify.Redact(x)
	case map[string]any:
		for k, value := range x {
			if text := approvalnotify.Redact(k + "=trace-probe"); text != k+"=trace-probe" {
				x[k] = "[redacted]"
			} else {
				x[k] = redactTrace(value)
			}
		}
	case []any:
		for i := range x {
			x[i] = redactTrace(x[i])
		}
	}
	return v
}

func (s *Server) recordActionTrace(ctx context.Context, t *actionTrace) {
	t.Environment = traceUnavailable
	if s.agentRuntime != nil {
		t.Environment = s.agentRuntime.env
	}
	raw, err := json.Marshal(t)
	if err == nil {
		var value any
		err = json.Unmarshal(raw, &value)
		if err == nil {
			raw, err = json.Marshal(redactTrace(value))
		}
	}
	if err == nil {
		if s.agentRuntime == nil || s.agentRuntime.late == nil {
			err = errors.New("agent runtime event database unavailable")
		} else {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = appendActionTrace(ctx, s.agentRuntime.late, string(raw))
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

func appendActionTrace(ctx context.Context, db store.DB, raw string) error {
	return store.InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "action-trace", Type: "note", Actor: "controller", Body: map[string]any{"text": raw}})
		return err
	})
}
