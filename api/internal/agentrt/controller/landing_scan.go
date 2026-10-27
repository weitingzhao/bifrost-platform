package controller

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingDrillPass resumes durable operations before scanning accepted work.
// Each pass reconstructs state; errors on one operation do not starve others.
func (c *Controller) LandingDrillPass(ctx context.Context) (result error) {
	if c.Env != "stg" || c.Drill == nil || c.DrillGit == nil {
		return nil
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	state, err := store.SettleLandingDrillBounds(ctx, c.DB, now)
	if err != nil {
		return err
	}
	var errs []error
	ids := make([]string, 0, len(state.Operations))
	for id := range state.Operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pending := make([]string, 0, len(ids))
	for _, id := range ids {
		o := state.Operations[id]
		if o.Task != "" && !o.Terminal() {
			pending = append(pending, id)
		}
	}
	// Reserve a separate cleanup budget. Normal recovery also gets a fair
	// share per operation, so an unavailable external source cannot consume
	// all the time before the next pending operation is visited.
	parent := ctx
	cleanupBudget := time.Minute
	workBudget := 4 * time.Minute
	if deadline, ok := parent.Deadline(); ok {
		remaining := max(time.Duration(0), time.Until(deadline))
		cleanupBudget = min(cleanupBudget, remaining/2)
		workBudget = max(time.Duration(0), remaining-cleanupBudget)
	}
	ctx, cancelWork := context.WithTimeout(parent, workBudget)
	defer cancelWork()
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(parent, cleanupBudget)
		defer cancel()
		var cleanups []landing.Operation
		for _, id := range ids {
			o := state.Operations[id]
			// A collided branch belongs to someone else; never delete it.
			if o.Terminal() && o.Outcome != "branch_collision" && o.Ref != "" {
				cleanups = append(cleanups, o)
			}
		}
		cleanupErrors := []error{result}
		for i, o := range cleanups {
			one, done := drillWorkShare(cleanupCtx, len(cleanups)-i)
			cleanupErrors = append(cleanupErrors, c.DrillGit.Delete(one, o.Ref, o.R))
			done()
		}
		result = errors.Join(cleanupErrors...)
	}()
	for i, id := range pending {
		o := state.Operations[id]
		one, done := drillWorkShare(ctx, len(pending)-i)
		errs = append(errs, c.resumeLandingDrill(one, id, o, now))
		done()
	}
	// No current rule means no new claims, but recovery and bounds above run.
	enabled := false
	var enabledAt time.Time
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
		enabledAt, e = store.ObserveLandingEnabled(ctx, tx, "dev", "bifrost-platform", now)
		return e
	})
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	if !enabled {
		return errors.Join(errs...)
	}
	tasks, err := store.LandingCandidates(ctx, c.DB, enabledAt)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	// The stream as it is after the recovery above: an operation it ended
	// (a discard) opens its k+1 in this same pass.
	var settled landing.State
	if err = store.InTx(ctx, c.DB, func(tx pgx.Tx) error {
		var e error
		settled, e = store.LandingState(ctx, tx, "landing:bifrost-platform")
		return e
	}); err != nil {
		return errors.Join(append(errs, err)...)
	}
	tasks = unsettled(tasks, settled.Operations)
	for i, task := range tasks {
		one, done := drillWorkShare(ctx, len(tasks)-i)
		op, _, e := store.ClaimLandingDrill(one, c.DB, c.Drill.Facts, task, now)
		if e != nil {
			errs = append(errs, e)
			done()
			continue
		}
		if op != "" {
			errs = append(errs, c.LandingDrillBranch(one, c.DrillGit, op, now))
			done()
			break
		}
		done()
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

// Callers supply a bounded phase context; each pending item is visited even
// when all preceding external requests use their complete time allowance.
func drillWorkShare(ctx context.Context, remaining int) (context.Context, context.CancelFunc) {
	deadline, _ := ctx.Deadline()
	return context.WithTimeout(ctx, max(time.Duration(0), time.Until(deadline))/time.Duration(remaining))
}

func (c *Controller) resumeLandingDrill(ctx context.Context, id string, o landing.Operation, now time.Time) error {
	switch {
	case o.Phase == "parked" && o.Resume != "branch_pushed":
		if now.Before(o.RetryAt) {
			return nil
		}
		_, d, err := c.LandingDecision(ctx, c.Drill.Facts, o.Task, id, "claim", now)
		if err != nil {
			return err
		}
		if d.Allowed {
			return c.appendDrill(ctx, id, "unparked", map[string]any{})
		}
		if d.Reason != "rule" && d.Reason != "limit" {
			return c.appendDrill(ctx, id, "failed", map[string]any{"why": "stale"})
		}
		return nil
	case o.Phase == "branch_pushed" || o.Phase == "parked":
		_, err := c.LandingDrillCI(ctx, *c.Drill, id, now)
		return err
	default:
		return c.LandingDrillBranch(ctx, c.DrillGit, id, now)
	}
}

// unsettled leaves out the tasks ClaimLandingDrill would refuse from the
// landing stream alone, before reading Git for them: one already rehearsed
// (rehearsed), with an operation still in progress (in_progress), or whose
// operation blocks a claim (terminal: ever landed, or failed and not
// retried). Each of those is refused only after its Result and History reads
// inside the claim's locked transaction (review 641 §3). ops is the stream as
// read after the pass's recovery, so a discard it recorded opens the k+1 now.
func unsettled(tasks []string, ops map[string]landing.Operation) []string {
	return slices.DeleteFunc(tasks, func(task string) bool {
		for _, o := range ops {
			if o.Task == task && (o.Phase == "rehearsed" || !o.Terminal() || o.BlocksClaim()) {
				return true
			}
		}
		return false
	})
}
