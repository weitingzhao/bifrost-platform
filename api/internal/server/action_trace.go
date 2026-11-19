package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// The trace (what it records, when, and what it redacts) lives in actionrecord;
// the server's names are kept for actions_wire.go, which calls them.
const traceRequiredMessage = actionrecord.TraceRequiredMessage

var traceFrom = actionrecord.TraceFrom

// tracer is actionrecord.Tracer on this process's runtime, audit log and routes.
func (s *Server) tracer() actionrecord.Tracer {
	return actionrecord.Tracer{
		Env: func() string {
			if s.agentRuntime == nil {
				return ""
			}
			return s.agentRuntime.env
		},
		DB: func() eventlog.DB {
			if s.agentRuntime == nil || s.agentRuntime.late == nil {
				return nil
			}
			return s.agentRuntime.late
		},
		Redact:       approvalnotify.Redact,
		FromExecutor: fromExecutor,
		ReadBody:     readAndRestore,
		PipelineOf:   func(r *http.Request) string { return chi.URLParam(r, "name") },
		MissingFields: func(r *http.Request, action string) {
			if s.audit != nil {
				s.audit.Record(r, "action_trace.missing_fields", action, "warning", "reason, unchecked or rollback not provided by caller; compatibility mode")
			}
		},
		AppendFailed: func(action, detail string) {
			if s.audit != nil {
				s.audit.RecordDirect("controller", actuation.RoleOperator, "action_trace.append_failed", action, "alert", detail)
			}
		},
	}
}

func (s *Server) tracedAction(id string, next http.HandlerFunc) http.HandlerFunc {
	return s.tracer().Middleware(id, next)
}

func (s *Server) beginApprovalTrace(ctx context.Context, a approvals.Approval) context.Context {
	return s.tracer().BeginApproval(ctx, actionrecord.ApprovalRun{ID: a.ID, Action: a.Action, Reason: a.Reason, Rollback: a.Rollback,
		Requester: a.Requester, RequesterThread: a.RequesterThread, Params: a.Params, Record: &a})
}

func (s *Server) endApprovalTrace(ctx context.Context, result any, err error) {
	s.tracer().EndApproval(ctx, result, err)
}
