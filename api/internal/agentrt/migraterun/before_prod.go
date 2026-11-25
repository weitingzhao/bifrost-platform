package migraterun

import (
	"context"
	"log/slog"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// ActionID is the action's catalog id.
const ActionID = "run_agentrt_migration"

// BeforeProd runs a commit's pending migrations when a platform PROD delivery
// is about to start, if the signed policy covers them (LAW-6, LAW-3,
// 2026-11-18: the delivery's schema check refused and someone had to ask for the
// same run by hand; 0035 held PROD for hours). Same judgement and same Job as
// run_agentrt_migration. Anything else is left to that check, which refuses.
type BeforeProd struct {
	Pending  func(ctx context.Context, commit string) (releasepolicy.MigrationState, error)
	Evaluate func(ctx context.Context, action string, params map[string]any) releasepolicy.Decision
	// Run is the action's executor.
	Run func(ctx context.Context, params map[string]any) (any, error)
}

// Prepare does it for commit; it says nothing back, the delivery's own schema
// check decides what a failure means.
func (b BeforeProd) Prepare(ctx context.Context, commit string) {
	if !commitID.MatchString(commit) {
		return
	}
	state, err := b.Pending(ctx, commit)
	if err != nil || len(state.Pending) == 0 {
		return
	}
	params := map[string]any{"commit": commit}
	if d := b.Evaluate(ctx, ActionID, params); !d.Auto {
		slog.Info("agentrt: pending migrations are not covered; the PROD delivery will refuse", "commit", commit, "reasons", d.Reasons)
		return
	}
	out, err := b.Run(ctx, params)
	if err != nil {
		slog.Warn("agentrt: migration before the PROD delivery failed", "commit", commit, "error", err)
		return
	}
	slog.Info("agentrt: migrated before the PROD delivery", "commit", commit, "result", out)
}
