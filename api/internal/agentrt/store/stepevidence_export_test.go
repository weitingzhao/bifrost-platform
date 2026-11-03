package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// StepEvidence is what the aggregate atoms of a step read now.
type StepEvidence struct{ Tasks, Landed, STG plan.Tri }

// ReadStepEvidence reads step's aggregate atoms in one transaction, as a
// rule's evaluation does: step_tasks of state and min, step_landed and
// step_stg_settled.
func ReadStepEvidence(ctx context.Context, db DB, step, state string, min int64) (StepEvidence, error) {
	var out StepEvidence
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		f := &stepFacts{ctx: ctx, tx: tx, step: step}
		out = StepEvidence{Tasks: f.StepTasks(state, min), Landed: f.StepLanded(), STG: f.StepSTGSettled()}
		return f.err
	})
	return out, err
}

// ExplainContractProblem is contractProblem for the external tests.
func ExplainContractProblem(b map[string]any) string { return contractProblem(b) }
