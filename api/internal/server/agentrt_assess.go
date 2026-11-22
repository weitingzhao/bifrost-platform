package server

import (
	"context"
	"encoding/json"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/assessmentreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentview"
)

// What the runtime's assessments read from the platform (TD-391,
// store/assess.go): the cards the runtime asked that are open and the
// approvals waiting for the Owner, as he sees them; and, for each, the
// commits on main that tie to it and the ledger items it names. Every read
// here writes nothing: the question store by RuleCards (questions_wire.go),
// the approvals by Waiting (no sweep), the repositories through the Gitea
// mirror.

// assessmentReader only reads the persisted stream. No evidence or AI is run
// as a side effect of opening a card, including when no assessment exists.
func (s *Server) assessmentReader(kind string) func(context.Context, string) map[string]any {
	return func(ctx context.Context, id string) map[string]any {
		if s.agentRuntime == nil || s.agentRuntime.api.DB == nil {
			return map[string]any{"error": "assessment runtime is unavailable"}
		}
		return assessmentview.Read(ctx, func(ctx context.Context) (json.RawMessage, error) {
			return assessmentreadings.ReadAssessment(ctx, s.agentRuntime.api.DB, kind, id)
		})
	}
}
