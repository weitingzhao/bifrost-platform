package server

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// What the runtime's assessments read from the platform (TD-391,
// store/assess.go): the cards the runtime asked that are open and the
// approvals waiting for the Owner, as he sees them; and, for each, the
// commits on main that tie to it and the ledger items it names. Every read
// here writes nothing: the question store by RuleCards (questions_wire.go),
// the approvals by Waiting (no sweep), the repositories through the Gitea
// mirror.

// checkAssessment reads status at the moment the Owner requests advice.
func (s *Server) checkAssessment(ctx context.Context, kind, id string) error {
	switch kind {
	case store.AssessCard:
		return s.questionRuntime.checkAssessmentCard(ctx, id)
	case store.AssessApproval:
		if s.approvals == nil {
			return fmt.Errorf("approval store is unavailable")
		}
		a, found, err := s.approvals.Lookup(id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: approval does not exist", pgx.ErrNoRows)
		}
		if strconv.Itoa(a.Number) != id {
			return fmt.Errorf("%w: use the approval number", store.ErrState)
		}
		if a.Status != approvals.StatusPending || !time.Now().Before(a.ExpiresAt) {
			return fmt.Errorf("%w: approval is %s or expired; assessment requires a pending approval", store.ErrState, a.Status)
		}
		return nil
	default:
		return fmt.Errorf("%w: assessment kind must be card or approval", store.ErrState)
	}
}

// assessmentReader only reads the persisted stream. No evidence or AI is run
// as a side effect of opening a card, including when no assessment exists.
func (s *Server) assessmentReader(kind string) func(context.Context, string) map[string]any {
	return func(ctx context.Context, id string) map[string]any {
		if s.agentRuntime == nil || s.agentRuntime.api.DB == nil {
			return map[string]any{"error": "assessment runtime is unavailable"}
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		events, err := store.ReadAssessment(ctx, s.agentRuntime.api.DB, kind, id)
		if err != nil {
			return map[string]any{"error": "could not read the assessment record"}
		}
		return map[string]any{"events": events}
	}
}
