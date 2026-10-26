package controller

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingDrillPass resumes durable operations before scanning accepted work.
// Each pass reconstructs state; errors on one operation do not starve others.
func (c *Controller) LandingDrillPass(ctx context.Context) error {
	if c.Env != "stg" || c.Drill == nil || c.DrillGit == nil {
		return nil
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	var state landing.State
	var errs []error
	err := store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		state, e = store.LandingState(ctx, tx, "landing:bifrost-platform")
		return e
	})
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(state.Operations))
	for id := range state.Operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := state.Operations[id]
		if o.Task == "" {
			continue
		}
		switch {
		case o.Terminal():
			// A collided branch belongs to someone else; never delete it.
			if o.Outcome != "branch_collision" && o.Ref != "" {
				errs = append(errs, c.DrillGit.Delete(ctx, o.Ref, o.R))
			}
		case o.Phase == "parked" && o.Resume != "branch_pushed":
			first, n := landing.RootBudget(state, o.Task)
			if landing.Bound(first, n-1, now) {
				errs = append(errs, c.appendDrill(ctx, id, "failed", map[string]any{"why": "bound"}))
				continue
			}
			if now.Before(o.RetryAt) {
				continue
			}
			_, d, e := c.LandingDecision(ctx, c.Drill.Facts, o.Task, id, "claim", now)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			if d.Allowed {
				errs = append(errs, c.appendDrill(ctx, id, "unparked", map[string]any{}))
			} else if d.Reason != "rule" && d.Reason != "limit" {
				errs = append(errs, c.appendDrill(ctx, id, "failed", map[string]any{"why": "stale"}))
			}
		case o.Phase == "branch_pushed" || o.Phase == "parked":
			_, e := c.LandingDrillCI(ctx, *c.Drill, id, now)
			errs = append(errs, e)
		default:
			errs = append(errs, c.LandingDrillBranch(ctx, c.DrillGit, id, now))
		}
	}
	// No current rule means no new claims, but recovery and bounds above run.
	enabled := false
	err = store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		rule, e := store.CurrentRule(ctx, tx, "landing:dev", now)
		if e != nil {
			return e
		}
		if rule == nil || rule.Landing == nil {
			return nil
		}
		if _, ok := rule.Landing.Repo("bifrost-platform"); !ok {
			return nil
		}
		enabled = true
		_, e = store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", now)
		return e
	})
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	if !enabled {
		return errors.Join(errs...)
	}
	tasks, err := store.LandingCandidates(ctx, c.DB)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, task := range tasks {
		op, _, e := store.ClaimLandingDrill(ctx, c.DB, c.Drill.Facts, task, now)
		if e != nil {
			errs = append(errs, e)
			continue
		}
		if op != "" {
			errs = append(errs, c.LandingDrillBranch(ctx, c.DrillGit, op, now))
			break
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) appendDrill(ctx context.Context, op, kind string, fields map[string]any) error {
	fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
	return store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"text": "STG drill: " + kind, "landing": fields}})
		return err
	})
}
