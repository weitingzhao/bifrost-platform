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
// condition that depends on it is unknown.
type Sources struct {
	Approval func(ctx context.Context, number int64) (status string, ok bool)
	Question func(ctx context.Context, id string) (q store.Question, ok bool)
	CI       func(ctx context.Context, repo, commit string) (status, run string, ok bool)
	Deployed store.Deployed
}

// Controller holds what the loops need. Zero intervals take the design's
// values: leases every 10 seconds, facts and rules every 15.
type Controller struct {
	DB         store.DB
	Sources    Sources
	Logf       func(format string, args ...any)
	LeaseEvery time.Duration
	FactsEvery time.Duration
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
	switch w.Kind {
	case plan.WatchApproval:
		n, err := strconv.ParseInt(w.ID, 10, 64)
		if err != nil {
			return false, err
		}
		if status, ok := c.Sources.Approval(ctx, n); ok {
			return store.SeeApproval(ctx, c.DB, n, status)
		}
	case plan.WatchQuestion:
		if q, ok := c.Sources.Question(ctx, w.ID); ok {
			q.ID = w.ID
			return store.SeeQuestion(ctx, c.DB, q)
		}
	case plan.WatchCI:
		repo, commit, _ := strings.Cut(w.ID, "@")
		if status, run, ok := c.Sources.CI(ctx, repo, commit); ok {
			return store.SeeCI(ctx, c.DB, repo, commit, status, run)
		}
	case plan.WatchDeploy:
		if commit, ref, ok := c.Sources.Deployed(ctx, w.ID); ok {
			return store.SeeDeploy(ctx, c.DB, w.ID, commit, ref)
		}
	}
	return false, nil
}

// Accept is one pass of the acceptance loop (v5 §5.2 控制器「验收编排」):
// every submitted implementation goes to verification and every submitted
// verification or review result is used. One that fails does not stop the
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
		did, err := store.Conclude(ctx, c.DB, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", id, err))
		}
		if did != "" {
			moved++
		}
	}
	return moved, errors.Join(errs...)
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
		to, err := store.EvaluateStep(ctx, c.DB, n.ID, c.Sources.Deployed)
		if err != nil {
			errs = append(errs, fmt.Errorf("step %s: %w", n.ID, err))
		}
		if to != "" {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

// Start runs the loops until ctx ends and returns a function that waits for
// them to stop, for a clean shutdown. In one pass: facts, routes of ready
// tasks whose job's candidates changed, acceptance, then rules, so a change
// seen or a task accepted is evaluated at once.
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
		_, rerr := c.Rules(ctx)
		return errors.Join(ferr, oerr, aerr, rerr)
	})
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
