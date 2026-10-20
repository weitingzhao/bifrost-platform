// Package controller runs the agent runtime's loops (design v5 §5.2
// 控制器): leases, external facts and done rules. Each loop is a method that
// runs one pass, so tests drive passes directly; Start runs them on timers.
// The loops change state only through the store, so every change is an event.
package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Sources read the records the runtime does not own. ok is false when a
// record cannot be read now: nothing is recorded for it, and a standing
// condition that depends on it is unknown. A source is read under the
// record's lock, so it must return when its context ends: each read gets
// ReadTimeout (review 152 B2).
type Sources struct {
	Approval func(ctx context.Context, number int64) (status string, ok bool)
	Question func(ctx context.Context, id string) (q store.Question, ok bool)
	// Ack tells the question store the runtime used the answer of a card it
	// asked (a plan change's); nil acknowledges nothing.
	Ack func(ctx context.Context, id string) error
	// Ask puts a plan's decision cards to the Owner (store.AcceptPlan); nil
	// leaves a plan with questions submitted until there is one. Withdraw
	// takes back the cards of a plan's round that was refused; nil leaves
	// them open.
	Ask      store.Ask
	Withdraw store.Withdraw
	CI       func(ctx context.Context, repo, commit string) (status, run string, ok bool)
	Deployed store.Deployed
	// MainResults reads a repository's main for the runtime's results that
	// stand there (store.OnMain); ok false when it cannot be read now. nil
	// records no landings (TD-383).
	MainResults store.MainResults
}

// Controller holds what the loops need. Zero intervals take the design's
// values: leases every 10 seconds, facts and rules every 15.
type Controller struct {
	DB          store.DB
	Sources     Sources
	Logf        func(format string, args ...any)
	LeaseEvery  time.Duration
	FactsEvery  time.Duration
	ReadTimeout time.Duration // per source read; zero means 10 seconds
	// Briefs has the controller leave the chief of staff its scheduled briefs
	// and nudges (W-31 2.4 slice ④); Now is the clock they read, time.Now
	// when nil.
	Briefs bool
	// FailFirstVerify honours store.TestNeedFailFirstVerify: on STG only
	// (T26's rework path, the Owner 2026-10-18).
	FailFirstVerify bool
	Now             func() time.Time
}

// boundedDeployed reads a deployment with the per-read timeout, for the done
// rules, which read it while they hold the step (review 153 B1).
func (c *Controller) boundedDeployed(ctx context.Context, env string) (string, map[string]any, bool) {
	type r struct {
		commit string
		ref    map[string]any
		ok     bool
	}
	x := bounded(c, ctx, func(ctx context.Context) r {
		commit, ref, ok := c.Sources.Deployed(ctx, env)
		return r{commit, ref, ok}
	})
	return x.commit, x.ref, x.ok
}

// bounded runs a source read with the per-read timeout.
func bounded[T any](c *Controller, ctx context.Context, read func(context.Context) T) T {
	d := c.ReadTimeout
	if d <= 0 {
		d = 10 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return read(rctx)
}

func (c *Controller) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Sweep is one pass of the lease loop.
func (c *Controller) Sweep(ctx context.Context) error {
	_, _, err := store.SweepLeases(ctx, c.DB)
	return err
}

// Facts is one pass of the external facts loop: each outside record a rule
// of a live step or of an architecture row depends on is read, and a change
// is recorded. One record that cannot be read or recorded does not stop the
// others; the errors come back joined. It returns how many it recorded.
func (c *Controller) Facts(ctx context.Context) (int, error) {
	nodes, err := store.RuledNodes(ctx, c.DB)
	if err != nil {
		return 0, err
	}
	watches := map[plan.Watch]bool{}
	for _, n := range nodes {
		for _, w := range n.Rule.Watches() {
			watches[w] = true
		}
	}
	recorded := 0
	var errs []error
	for w := range watches {
		did, err := c.see(ctx, w)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", w.Kind, w.ID, err))
		}
		if did {
			recorded++
		}
	}
	return recorded, errors.Join(errs...)
}

func (c *Controller) see(ctx context.Context, w plan.Watch) (bool, error) {
	// Each source is read under the record's lock in the store, so reads from
	// two controllers are recorded in the order they were made.
	switch w.Kind {
	case plan.WatchApproval:
		n, err := strconv.ParseInt(w.ID, 10, 64)
		if err != nil {
			return false, err
		}
		return store.SeeApproval(ctx, c.DB, n, func(ctx context.Context) (string, bool) {
			type r struct {
				s  string
				ok bool
			}
			x := bounded(c, ctx, func(ctx context.Context) r { s, ok := c.Sources.Approval(ctx, n); return r{s, ok} })
			return x.s, x.ok
		})
	case plan.WatchQuestion:
		return store.SeeQuestion(ctx, c.DB, w.ID, func(ctx context.Context) (store.Question, bool) {
			type r struct {
				q  store.Question
				ok bool
			}
			x := bounded(c, ctx, func(ctx context.Context) r { q, ok := c.Sources.Question(ctx, w.ID); return r{q, ok} })
			return x.q, x.ok
		})
	case plan.WatchCI:
		repo, commit, _ := strings.Cut(w.ID, "@")
		return store.SeeCI(ctx, c.DB, repo, commit, func(ctx context.Context) (string, string, bool) {
			type r struct {
				s, run string
				ok     bool
			}
			x := bounded(c, ctx, func(ctx context.Context) r { s, run, ok := c.Sources.CI(ctx, repo, commit); return r{s, run, ok} })
			return x.s, x.run, x.ok
		})
	case plan.WatchDeploy:
		return store.SeeDeploy(ctx, c.DB, w.ID, c.boundedDeployed)
	}
	return false, nil
}

// Accept is one pass of the acceptance loop (v5 §5.2 控制器「验收编排」):
// every submitted implementation goes to verification, every submitted
// verification or review result is used, and every salvaged result still to
// be checked gets its verification. One that fails does not stop the
// others. It returns how many tasks it moved.
func (c *Controller) Accept(ctx context.Context) (int, error) {
	implementations, checks, err := store.Pending(ctx, c.DB)
	if err != nil {
		return 0, err
	}
	moved := 0
	var errs []error
	for _, id := range implementations {
		v, err := store.StartVerification(ctx, c.DB, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", id, err))
		}
		if v != "" {
			moved++
		}
	}
	for _, id := range checks {
		did, err := store.ConcludeWith(ctx, c.DB, id, store.ConcludeOptions{FailFirstVerify: c.FailFirstVerify})
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", id, err))
		}
		if did != "" {
			moved++
		}
	}
	// Plans handed in by the chief of staff's seat (W-31 2.2b).
	plans, perr := store.PendingPlans(ctx, c.DB)
	if perr != nil {
		errs = append(errs, perr)
	}
	for _, id := range plans {
		did, err := store.AcceptPlan(ctx, c.DB, id, c.boundedAsk(), c.boundedWithdraw())
		if err != nil {
			errs = append(errs, fmt.Errorf("plan %s: %w", id, err))
		}
		if did != "" {
			moved++
		}
	}
	// The chief of staff's briefs at 8:00 and 21:00, and its nudges (W-31
	// 2.4 slice ④): notes to the seat, made briefs below like any mail.
	if c.Briefs {
		now := time.Now()
		if c.Now != nil {
			now = c.Now()
		}
		if left, err := store.ScheduleBrief(ctx, c.DB, now); err != nil {
			errs = append(errs, fmt.Errorf("scheduled brief: %w", err))
		} else if left {
			moved++
		}
		if n, err := store.Nudges(ctx, c.DB, now); err != nil {
			errs = append(errs, fmt.Errorf("nudges: %w", err))
		} else {
			moved += n
		}
	}
	// Mail to the chief of staff's seat becomes briefs, and the briefs'
	// answers go back to their senders (W-31 2.3).
	mail, perr := store.PendingMail(ctx, c.DB)
	if perr != nil {
		errs = append(errs, perr)
	}
	for _, id := range mail {
		task, err := store.TakeMail(ctx, c.DB, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("message %s: %w", id, err))
		}
		if task != "" {
			moved++
		}
	}
	briefs, perr := store.PendingBriefs(ctx, c.DB)
	if perr != nil {
		errs = append(errs, perr)
	}
	for _, id := range briefs {
		did, err := store.AcceptBrief(ctx, c.DB, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("brief %s: %w", id, err))
		}
		if did != "" {
			moved++
		}
	}
	// In-job scores, when a task has ended since the last computation (W-31
	// 2.4 slice ②).
	if _, err := store.ComputeScores(ctx, c.DB); err != nil {
		errs = append(errs, fmt.Errorf("scores: %w", err))
	}
	// Salvaged results of lost attempts get a check of their own (T34 case 8).
	salvaged, perr := store.PendingSalvaged(ctx, c.DB)
	if perr != nil {
		errs = append(errs, perr)
	}
	for _, id := range salvaged {
		v, err := store.StartSalvageCheck(ctx, c.DB, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("attempt %s: %w", id, err))
		}
		if v != "" {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

// boundedAsk is Sources.Ask with the per-read timeout: a card that is not
// made in time is an error, and the plan waits for the next pass.
func (c *Controller) boundedAsk() store.Ask {
	if c.Sources.Ask == nil {
		return nil
	}
	return func(ctx context.Context, card store.Card) (string, error) {
		type r struct {
			id  string
			err error
		}
		x := bounded(c, ctx, func(ctx context.Context) r { id, err := c.Sources.Ask(ctx, card); return r{id, err} })
		if x.id == "" && x.err == nil {
			return "", errors.New("the question store did not answer in time")
		}
		return x.id, x.err
	}
}

// boundedWithdraw is Sources.Withdraw with the per-read timeout.
func (c *Controller) boundedWithdraw() store.Withdraw {
	if c.Sources.Withdraw == nil {
		return nil
	}
	return func(ctx context.Context, id, reason string) error {
		return bounded(c, ctx, func(ctx context.Context) error { return c.Sources.Withdraw(ctx, id, reason) })
	}
}

// BlockedCards is one pass over the blocked tasks' decision cards (design v5
// §5.2 「验收编排」): an answered card gives its task one more round, drops
// it or closes its mission (store.DecideBlocked), a task that landed in a
// mission after the Owner closed it is cancelled (store.CancelAfterClose),
// the open cards of missions
// that have ended are withdrawn, and so are cards asked before the third
// option (store.WithdrawStaleCards: the other cards of a mission closed on a
// card, so their places in the question store are free for this pass), a
// ready task its mission's budget no longer lets run
// is blocked with the reason (store.BlockOverBudget, TD-384), then every
// blocked task that needs a card and has none open gets one
// (store.AskBlocked), the tasks blocked before this pass included. Without a
// question store there are no cards, and nothing is blocked for want of
// budget (no answer could release it). It returns how many cards it closed,
// withdrew and asked and how many tasks it blocked.
func (c *Controller) BlockedCards(ctx context.Context) (int, error) {
	if c.Sources.Question == nil || c.Sources.Ask == nil {
		return 0, nil
	}
	decided, derr := store.DecideBlocked(ctx, c.DB, c.question, c.ack())
	late, lerr := store.CancelAfterClose(ctx, c.DB)
	withdrawn, werr := store.WithdrawStaleCards(ctx, c.DB, c.question, c.boundedWithdraw())
	spent, serr := store.BlockOverBudget(ctx, c.DB)
	asked, aerr := store.AskBlocked(ctx, c.DB, c.boundedAsk())
	return decided + late + withdrawn + spent + asked, errors.Join(derr, lerr, werr, serr, aerr)
}

// LandingsEvery is how often the landings pass reads main: a loop of its own,
// so a slow Gitea never holds up dispatch and the decisions (review 436 B4).
const LandingsEvery = 5 * time.Minute

// Landings records accepted results first seen on main (store.RecordLandings,
// TD-383); each repository's main is read with a minute's bound. It returns
// how many it recorded.
func (c *Controller) Landings(ctx context.Context) (int, error) {
	if c.Sources.MainResults == nil {
		return 0, nil
	}
	return store.RecordLandings(ctx, c.DB, func(ctx context.Context, repo string) (store.OnMain, bool) {
		rctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return c.Sources.MainResults(rctx, repo)
	})
}

// Close is one pass of closing the missions whose goal is done: every task
// accepted or dropped by the Owner on its card, an implementation among the
// accepted, no plan step bound (store.CloseMissions). It returns how many it
// closed.
func (c *Controller) Close(ctx context.Context) (int, error) {
	closed, err := store.CloseMissions(ctx, c.DB)
	return len(closed), err
}

// PlanDecisions is one pass of the plan decisions: proposals outside the
// standing authorization whose decision card is answered are applied or
// refused (store.DecidePlanChanges). Each question is read with the
// per-read timeout.
func (c *Controller) PlanDecisions(ctx context.Context) (int, error) {
	if c.Sources.Question == nil {
		return 0, nil
	}
	return store.DecidePlanChanges(ctx, c.DB, c.question, c.ack())
}

// MissionDecisions is one pass over the missions outside the standing
// envelope that wait on a decision card: an answered card starts or stops the
// mission (store.DecideMissions).
func (c *Controller) MissionDecisions(ctx context.Context) (int, error) {
	if c.Sources.Question == nil {
		return 0, nil
	}
	return store.DecideMissions(ctx, c.DB, c.question, c.ack())
}

// question reads a question with the per-read timeout.
func (c *Controller) question(ctx context.Context, id string) (store.Question, bool) {
	type r struct {
		q  store.Question
		ok bool
	}
	x := bounded(c, ctx, func(ctx context.Context) r { q, ok := c.Sources.Question(ctx, id); return r{q, ok} })
	return x.q, x.ok
}

// ack acknowledges an answer with the per-read timeout; nil without a
// source.
func (c *Controller) ack() func(context.Context, string) error {
	if c.Sources.Ack == nil {
		return nil
	}
	return func(ctx context.Context, id string) error {
		return bounded(c, ctx, func(ctx context.Context) error { return c.Sources.Ack(ctx, id) })
	}
}

// Rules is one pass of the done-rule loop: every live step with a rule is
// evaluated, standing conditions against what is deployed now. One step that
// fails does not stop the others. It returns how many steps moved.
func (c *Controller) Rules(ctx context.Context) (int, error) {
	nodes, err := store.RuledNodes(ctx, c.DB)
	if err != nil {
		return 0, err
	}
	moved := 0
	var errs []error
	for _, n := range nodes {
		if n.Kind != "step" {
			continue
		}
		to, err := store.EvaluateStep(ctx, c.DB, n.ID, c.boundedDeployed)
		if err != nil {
			errs = append(errs, fmt.Errorf("step %s: %w", n.ID, err))
		}
		if to != "" {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

// Activate is one pass moving to active the planned steps whose work has
// started, while the plan-standing rule in force says so
// (store.ActivateLeasedSteps).
func (c *Controller) Activate(ctx context.Context) (int, error) {
	return store.ActivateLeasedSteps(ctx, c.DB)
}

// Start runs the loops until ctx ends and returns a function that waits for
// them to stop, for a clean shutdown. In one pass: facts, routes of ready
// tasks whose job's candidates changed, acceptance, blocked tasks' cards (a
// task blocked by acceptance gets its card in the same pass), dispatch of
// proposed tasks, the closing of missions whose goal is done, plan decisions, the
// steps whose work has started, then rules, so a change seen or a task
// accepted is
// evaluated at once, a task whose last dependency was just accepted is
// ready in the same pass, and a step a decision added is evaluated with it.
func (c *Controller) Start(ctx context.Context) (wait func()) {
	lease, facts := c.LeaseEvery, c.FactsEvery
	if lease <= 0 {
		lease = 10 * time.Second
	}
	if facts <= 0 {
		facts = 15 * time.Second
	}
	var wg sync.WaitGroup
	c.every(ctx, &wg, "agentrt-leases", lease, func() error { return c.Sweep(ctx) })
	c.every(ctx, &wg, "agentrt-facts-rules", facts, func() error {
		_, ferr := c.Facts(ctx)
		_, oerr := store.Reroute(ctx, c.DB)
		_, aerr := c.Accept(ctx)
		_, berr := c.BlockedCards(ctx)
		_, derr := store.Dispatch(ctx, c.DB)
		_, cerr := c.Close(ctx)
		_, perr := c.PlanDecisions(ctx)
		_, merr := c.MissionDecisions(ctx)
		_, verr := c.Activate(ctx)
		_, rerr := c.Rules(ctx)
		return errors.Join(ferr, oerr, aerr, berr, derr, cerr, perr, merr, verr, rerr)
	})
	if c.Sources.MainResults != nil {
		c.every(ctx, &wg, "agentrt-landings", LandingsEvery, func() error {
			_, err := c.Landings(ctx)
			return err
		})
	}
	return wg.Wait
}

func (c *Controller) every(ctx context.Context, wg *sync.WaitGroup, name string, d time.Duration, pass func() error) {
	wg.Add(1)
	safego.Go(name, func() {
		defer wg.Done()
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			completed := safego.Do(name, func() {
				if err := pass(); err != nil && ctx.Err() == nil {
					c.logf("%s: %v", name, err)
				}
			})
			if !completed {
				c.logf("%s: a pass panicked; the loop goes on", name)
			}
		}
	})
}
