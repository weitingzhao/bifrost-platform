package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S5 never consults the legacy uiRevision=main CI path, even when that path
// is green and the original soft refusal set was empty (caller-only changes).
func (c *Controller) landingS5CI(ctx context.Context, src DrillSources, op string) (landing.DrillAction, error) {
	o, d, err := store.PrepareS5Evidence(ctx, c.DB, src.Facts, c.Env, op, nil)
	if err != nil {
		return landing.DrillAction{Kind: "wait", Why: "S5_EVIDENCE_MISSING"}, err
	}
	if !d.Allowed {
		return c.settleS5(ctx, o, op, d.Reason, landing.S5CIResult{}, false)
	}
	if !o.S5.Key.Valid() || src.EnsureS5CI == nil || src.Facts.S5CI == nil {
		return landing.DrillAction{Kind: "wait", Why: "S5_EVIDENCE_MISSING"}, nil
	}
	// Ensure returns the independently assigned UID after checking an existing
	// run's complete parameters. A crash before binding resumes this same key.
	uid, err := src.EnsureS5CI(ctx, o.S5.Key)
	if err != nil {
		if strings.Contains(err.Error(), "S5_CI_IDENTITY") {
			// A run of this name with other parameters: this generation can
			// never be read, and its key cannot change on its own (§5.1).
			return c.settleS5(ctx, o, op, "S5_CI_IDENTITY", landing.S5CIResult{}, false)
		}
		return landing.DrillAction{Kind: "wait", Why: "S5_CI_IDENTITY"}, err
	}
	if err = store.BindS5Run(ctx, c.DB, o.S5.Key, uid); err != nil {
		return landing.DrillAction{Kind: "wait", Why: "S5_CI_IDENTITY"}, err
	}
	f, _, err := c.LandingDecision(ctx, src.Facts, o.Task, op, "claim", store.DecisionAt())
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	v, ci, why, err := store.VerifyLandingS5(ctx, src.Facts, f, store.DecisionAt())
	if err != nil {
		return landing.DrillAction{Kind: "wait", Why: why}, err
	}
	if why != "" {
		// A terminal result whose binding no longer matches only because the
		// material moved (rule/profile/review) is re-keyed by the next pass.
		moved := false
		if why == "S5_BINDING" || why == "S5_GENERATION_STALE" {
			k, kwhy := landing.NewS5CIKey(f, op, f.Operation.S5.Key.Generation)
			moved = kwhy == "" && k != f.Operation.S5.Key
		}
		return c.settleS5(ctx, o, op, why, ci, moved)
	}
	why, soft := landing.AuthorizeLanding(f, v, store.DecisionAt())
	if why != "" || len(soft) > 0 {
		return landing.DrillAction{Kind: "wait", Why: why}, nil
	}
	main, err := src.Main(ctx, f.Repo)
	if err != nil {
		return landing.DrillAction{Kind: "wait"}, err
	}
	if main != o.M0 {
		return landing.DrillAction{Kind: "wait", Why: "S5_BINDING"}, nil
	}
	blocked, err := src.WindowBlocked(ctx, f.Repo)
	if err != nil || blocked {
		return landing.DrillAction{Kind: "wait", Why: "window"}, err
	}
	if o.Phase == "parked" {
		err = store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
			_, err := store.Append(ctx, tx, store.Event{Stream: "landing:" + f.Repo, Type: "note", Actor: "controller", Body: map[string]any{"text": "S5 candidate resumes", "landing": map[string]any{"v": landing.Version, "kind": "unparked", "op": op}}})
			return err
		})
		return landing.DrillAction{Kind: "unparked"}, err
	}
	if c.Env == "prod" {
		return landing.DrillAction{Kind: "ready"}, c.landLive(ctx, src, op, o.R, f.Repo)
	}
	if ci.Status != "Succeeded" {
		return landing.DrillAction{Kind: "wait"}, fmt.Errorf("S5_EVIDENCE_MISSING")
	}
	return landing.DrillAction{Kind: "ready"}, c.finishLandingDrill(ctx, src, op, store.DecisionAt(), f.Repo)
}

// settleS5 disposes of a refused S5 step (design §5.1). Only evidence that
// is missing or still running, a missing review before any key, or material
// that moved (the next pass opens a new generation) waits. A release freeze
// failure opens a bounded new generation. Every other S5 refusal, and every
// terminal result for the generation, ends the operation with its S5 code:
// it cannot become evidence by waiting, and the operation would otherwise
// hold the repository's in-flight slot until the root's deadline. Non-S5
// reasons (policy, window, limits) keep the existing flow.
func (c *Controller) settleS5(ctx context.Context, o landing.Operation, op, why string, ci landing.S5CIResult, moved bool) (landing.DrillAction, error) {
	wait := landing.DrillAction{Kind: "wait", Why: why}
	// Never reached in awaiting_grant: LandingDrillCI refuses every phase
	// but branch_pushed/parked before landingS5CI, the branch preparation
	// runs only from claimed/prepared/branch_intent, and a waiting
	// operation is resumed on the grant path (resumeLandingDrill). The
	// protocol ends awaiting_grant only with declined/approval_expired/
	// bound/stale, so failed{S5_*} there would be refused and the slot kept:
	// fail loudly instead of pretending to settle.
	if o.Phase == "awaiting_grant" {
		return wait, fmt.Errorf("S5 settlement reached an operation awaiting a grant")
	}
	if !landing.S5Code(why) {
		return wait, nil
	}
	if ci.Retryable {
		n := o.S5.InfraRetries
		if n >= landing.S5InfraRetries {
			return c.failS5(ctx, o, op, "S5_EVIDENCE_MISSING")
		}
		if store.DecisionAt().Before(o.S5.KeyAt.Add(landing.S5InfraRetryAfter(n))) {
			return wait, nil
		}
		return wait, store.RetryS5Generation(ctx, c.DB, o.S5.Key)
	}
	terminal := ci.Status == "Failed" || ci.Status == "Succeeded"
	switch why {
	case "S5_PREPARATION_ONLY":
		// The preparation gate's own answer (terminal or main-bound
		// operation, no R yet). A terminal CI result is never a wait.
		if !terminal {
			return wait, nil
		}
	case "S5_EVIDENCE_MISSING":
		if !terminal {
			return wait, nil
		}
	case "S5_REVIEW_MISSING":
		// Before a key the review entry is still open (§6.1); after one, a
		// review that disappeared is a changed binding.
		if !terminal && !o.S5.Key.Valid() {
			return wait, nil
		}
	case "S5_BINDING", "S5_GENERATION_STALE":
		if !terminal || moved {
			return wait, nil
		}
	}
	return c.failS5(ctx, o, op, why)
}

func (c *Controller) failS5(ctx context.Context, o landing.Operation, op, why string) (landing.DrillAction, error) {
	return landing.DrillAction{Kind: "failed", Why: why}, store.FailS5Landing(ctx, c.DB, op, o.S5.Key, why)
}

func s5CIRun(ci landing.S5CIResult) landing.CIRun {
	return landing.CIRun{Run: ci.Run, UID: ci.UID, R: ci.Key.R, Status: ci.Status, CreatedAt: ci.CreatedAt}
}
