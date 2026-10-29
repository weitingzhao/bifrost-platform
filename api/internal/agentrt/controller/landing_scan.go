package controller

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingDrillPass resumes durable operations before scanning accepted work.
// Each pass reconstructs state; errors on one operation do not starve others.
func (c *Controller) LandingDrillPass(ctx context.Context) (result error) {
	if !c.landingRuns() {
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
	// Roots in the first-claim handoff that will never be claimed are
	// recorded from the database alone, before any external read and
	// whatever the rule (TD-447 4): a slow recovery below cannot starve it.
	if _, err = store.SettleUnclaimed(ctx, c.DB, "bifrost-platform", now); err != nil {
		errs = append(errs, err)
	}
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
			// PROD keeps its land/prod branches (B-05): the record of what
			// was pushed to main.
			if o.Terminal() && o.Outcome != "branch_collision" && o.Ref != "" && c.Env != "prod" {
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
		enabledAt, e = store.ObserveLandingEnabled(ctx, tx, c.Env, "dev", "bifrost-platform", now)
		// No fence yet (PROD before its mode): no candidate is in scope.
		enabled = e == nil && !enabledAt.IsZero()
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
	// Waits run on the clock as it is when each is read or set, not the
	// pass's start: a slow read must not use up the wait it earns (review
	// 653 R653-2).
	tasks = c.drillWaits.due(unsettled(tasks, settled.Operations), drillGeneration(settled), c.clock())
	for i, task := range tasks {
		one, done := drillWorkShare(ctx, len(tasks)-i)
		op, d, e := store.ClaimLanding(one, c.DB, c.Drill.Facts, c.Env, task, now)
		c.drillWaits.record(task, e == nil && (op != "" || d.Reason == "changed"), c.clock())
		if e != nil {
			errs = append(errs, e)
			done()
			continue
		}
		if op == "" && d.Reason != "" {
			// Shown on the mission's landing status (CUT §11.13).
			if _, err := store.RecordClaimRefusal(one, c.DB, "bifrost-platform", task, d.Reason); err != nil {
				errs = append(errs, err)
			}
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
	case o.Phase == "awaiting_grant":
		// C-5 will resume with a verified grant. No Git/CI polling, branch
		// rewrite, repeated event or card while authorization is absent.
		return nil
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
		if d.Reason == "floor" {
			return c.appendDrill(ctx, id, "failed", map[string]any{"why": "floor"})
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
// retried). The claim refuses those too, but terminal only after its Result
// and History reads (review 641 §3), now done with no lock held (TD-447 5).
// ops is the stream as read after the pass's recovery, so a discard it
// recorded opens the k+1 now.
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

// drillBackoff: a root never claimed stays a candidate until its claim or its
// refusal is recorded, up to its window (TD-447 4), so a refusal that may
// pass by itself (dependencies, rule, limit, unknown history, a read error)
// is not retried every pass, each time reading Git again (review 641 §3): 1,
// 2, 4, then 5 minutes. A claim, or "changed" (the bindings moved under the
// reads: look again next pass), clears it; so does any change of the
// repository stream's operations or retries (an operation ends or parks, so
// a slot frees; a retry). Memory only: a restart tries once sooner. The cost,
// stated (reviews 651 R651-5, 652 R652-4): a rule version that newly allows
// it, or a daily quota that resets, is seen only when the wait ends (at most
// five minutes); any root's new record clears every wait (coarse, not
// wrong); a root whose eligibility comes and goes within a wait can miss a
// claim it could have had and end bound. Recording ends (SettleUnclaimed, Close) never
// pass through here.
type drillBackoff struct {
	mu         sync.Mutex
	generation string
	waits      map[string]drillWait
}

type drillWait struct {
	until time.Time
	tries int
}

// drillGeneration changes whenever an operation or a retry is added or moves.
func drillGeneration(s landing.State) string {
	keys := make([]string, 0, len(s.Operations)+len(s.Retries))
	for id, o := range s.Operations {
		keys = append(keys, id+"="+o.Phase+"/"+o.Outcome)
	}
	for root, r := range s.Retries {
		keys = append(keys, root+"<"+r.AfterOp+">"+r.ConsumedBy+"@"+r.At.String())
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// due drops the tasks still waiting, forgets the ones no longer listed, and
// forgets every wait when the stream moved.
func (b *drillBackoff) due(tasks []string, generation string, now time.Time) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation != b.generation {
		b.generation, b.waits = generation, nil
	}
	listed := map[string]bool{}
	for _, t := range tasks {
		listed[t] = true
	}
	for t := range b.waits {
		if !listed[t] {
			delete(b.waits, t)
		}
	}
	return slices.DeleteFunc(tasks, func(t string) bool { return now.Before(b.waits[t].until) })
}

func (b *drillBackoff) record(task string, clear bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if clear {
		delete(b.waits, task)
		return
	}
	if b.waits == nil {
		b.waits = map[string]drillWait{}
	}
	w := b.waits[task]
	w.tries++
	w.until = now.Add(min(time.Minute<<min(w.tries-1, 3), 5*time.Minute))
	b.waits[task] = w
}

// clock is Now when set (tests), else the wall clock.
func (c *Controller) clock() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
