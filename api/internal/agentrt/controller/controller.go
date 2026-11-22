// Package controller runs the agent runtime's loops (design v5 §5.2
// 控制器): external records, pipeline records and their evidence, the full
// CI, and the facts-and-rules pass. Each loop is a method that runs one pass,
// so tests drive passes directly; Start runs them on timers. The plan tree's
// passes (agentrt/planloop) and the acknowledging of used answers
// (agentrt/questionbridge) are other domains': the assembly binds them as Steps
// and Loops, which Start runs as its own. The loops change state only
// through the store, so every change is an event.
package controller

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// Sources read the records the runtime does not own. ok is false when a
// record cannot be read now: nothing is recorded for it, and a standing
// condition that depends on it is unknown. A source is read under the
// record's lock, so it must return when its context ends: each read gets
// ReadTimeout (review 152 B2).
type Sources struct {
	Approvals    func(context.Context) ([]seenfacts.ApprovalFact, error)
	Questions    func(context.Context) ([]seenfacts.Question, error)
	PipelineRuns func(context.Context) ([]seenfacts.PipelineRun, error)
	// PipelineRun reads one run by name, as PipelineRuns reads each (ok is
	// false when it is not kept or not completed): the run a ci.run.finished
	// names (RecordPipelineRun). nil: no event path.
	PipelineRun func(ctx context.Context, name string) (p seenfacts.PipelineRun, ok bool, err error)
	// CommitTree reads the git tree of a commit of a repository, for a CI
	// run that reports a commit it used without its tree (evidence.go);
	// nil: such a run gives no evidence.
	CommitTree func(ctx context.Context, repo, commit string) (string, error)
	// RunFailure reads what a failed CI run failed on from its tasks: the
	// tasks, the tests they name and the class (seenfacts.PipelineRun's fields),
	// for its evidence row; nil: the row says only that it failed.
	RunFailure func(ctx context.Context, run string) (tasks, tests []string, class string, err error)
	// RunEvidenced is told each completed run that is evidence, with its
	// trees and what it failed on, just before its row goes to the ledger:
	// once a run in this process unless the write fails (it is then told
	// again within PipelineFullEvery), and again after a restart, when the
	// ledger already has the run. It is called inside the pass and must
	// return at once; nil tells no one.
	RunEvidenced func(ctx context.Context, p seenfacts.PipelineRun)
	// UnitRuns records the unit tasks that finished and settles what they
	// add up to (server: receiveUnits); nil: this platform runs no units.
	UnitRuns func(ctx context.Context) error
	// UnitsDaily starts and reports the daily unit run of main; nil: none.
	UnitsDaily func(ctx context.Context) error
	// CINode keeps the node new CI runs go to current; nil: this platform does not.
	CINode func(ctx context.Context) error
	// HeadRelease takes each deliverable the platform releases by itself
	// one step (server/head_release.go); nil when that is switched off.
	HeadRelease func(ctx context.Context) error
	// Deployed reads what an environment runs: the full CI's STG commit
	// (fullci_loop.go); nil: the full CI reads none.
	Deployed seenfacts.Deployed
}

// Step is one step of the facts-and-rules pass (Controller.Steps): Run is
// timed and logged as "agentrt-facts-rules: <Name>" when it is slow, and its
// error joins the pass's.
type Step struct {
	Name string
	Run  func(ctx context.Context) error
}

// Loop is a loop of its own the controller runs on a timer (Controller.Loops),
// named and alerted as the controller's other loops are.
type Loop struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
}

// Controller holds what the loops need. Zero intervals take the design's
// values: leases every 10 seconds, facts and rules every 15.
type Controller struct {
	// tuning is the tuning rule the last pass read (tuning.go).
	tuning  atomic.Pointer[rules.Tuning]
	DB      eventlog.DB
	Sources Sources
	Logf    func(format string, args ...any)
	// Infof logs what the controller did that needs no one's attention (a
	// reconciliation that recorded what events missed); nil uses Logf.
	Infof       func(format string, args ...any)
	LeaseEvery  time.Duration
	FactsEvery  time.Duration
	ReadTimeout time.Duration // per source read; zero means 10 seconds
	// Steps are the steps of the facts-and-rules pass, run in order after
	// the tuning rule is read (the assembly binds them: the plan's facts,
	// plan decisions, the used answers, the done rules).
	Steps []Step
	// Loops are loops of their own, started in order after the key purge
	// and before the facts-and-rules loop (the plan's observations).
	Loops []Loop
	// Now is the clock the passes read, time.Now when nil.
	Now func() time.Time
	// Env is the platform's own environment (AGENTRT_ENV).
	Env     string
	Started time.Time
	FullCI  *fullci.FullCI // the daily full CI (agentrt/fullci, fullci_loop.go); nil runs none
	// FullCIZone is the zone of the full CI's day (fullci.Host.Zone): the
	// assembly binds the zone of the briefs; "" starts no run.
	FullCIZone string
	// PipelineEvery is how often the retained pipeline runs are listed for
	// the ones that completed (PipelineRecords); zero means
	// DefaultPipelineEvery. PipelineFullEvery is how often a pass compares
	// every listed run with the database whatever it remembers; zero means
	// DefaultPipelineFullEvery. PipelineRecorded, when set, is called after
	// a pass recorded a completed run: the server has the release recorder
	// write that run's record then instead of at its own next pass.
	PipelineEvery     time.Duration
	PipelineFullEvery time.Duration
	PipelineRecorded  func()
	// PipelineFinished, when set, names the runs the bus says finished
	// (ci.run.finished, gate-cluster in this process): each is recorded at
	// once (RecordPipelineRun). The loop's pass over every run is the
	// reconciliation, once per PipelineReconcileEvery (bus/RULES.md §8).
	PipelineFinished <-chan string
	// pipelines remembers the completed runs this process knows are
	// recorded (external_records.go); memory only.
	pipelines pipelineMemo
}

// boundedDeployed reads a deployment with the per-read timeout, for the full
// CI's STG commit (review 153 B1).
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

func (c *Controller) infof(format string, args ...any) {
	if c.Infof != nil {
		c.Infof(format, args...)
		return
	}
	c.logf(format, args...)
}

// Start runs the loops until ctx ends and returns a function that waits for
// them to stop, for a clean shutdown. In one pass: facts, routes of ready
// tasks whose job's candidates changed, acceptance, blocked tasks' cards (a
// task blocked by acceptance gets its card in the same pass), dispatch of
// proposed tasks, the closing of missions whose goal is done, plan decisions, the
// steps whose work has started, then rules, so a change seen or a task accepted
// is evaluated at once, a task whose last dependency was just accepted is ready
// in the same pass, and a step a decision added is evaluated with it; last, the
// waits past their limit (Waits), read after the pass moved what it moves.
func (c *Controller) Start(ctx context.Context) (wait func()) {
	lease, facts := c.LeaseEvery, c.FactsEvery
	if lease <= 0 {
		lease = 10 * time.Second
	}
	if facts <= 0 {
		facts = 15 * time.Second
	}
	if c.Started.IsZero() {
		c.Started = time.Now()
		if c.Now != nil {
			c.Started = c.Now()
		}
	}
	var wg sync.WaitGroup
	c.startExternalRecords(ctx, &wg)
	if c.Sources.Approvals != nil || c.Sources.Questions != nil {
		c.every(ctx, &wg, "agentrt-external-records", time.Minute, func() error {
			_, err := c.ExternalRecords(ctx)
			return err
		})
	}
	if c.Sources.PipelineRuns != nil {
		pipelines := c.PipelineEvery
		if pipelines <= 0 {
			pipelines = DefaultPipelineEvery
		}
		c.every(ctx, &wg, "agentrt-pipeline-records", pipelines, func() error {
			_, err := c.pipelineTick(ctx)
			return err
		})
		c.startPipelineEvents(ctx, &wg)
	}
	if c.Sources.UnitRuns != nil {
		every := c.PipelineEvery
		if every <= 0 {
			every = DefaultPipelineEvery
		}
		c.every(ctx, &wg, "agentrt-unit-runs", every, func() error { return c.Sources.UnitRuns(ctx) })
	}
	if c.Sources.CINode != nil {
		c.every(ctx, &wg, "agentrt-ci-node", time.Minute, func() error { return c.Sources.CINode(ctx) })
	}
	if c.Sources.UnitsDaily != nil {
		c.every(ctx, &wg, "agentrt-units-daily", 5*time.Minute, func() error { return c.Sources.UnitsDaily(ctx) })
	}
	if c.Sources.HeadRelease != nil {
		// Thirty seconds: the slow poll of Owner 2026-11-13 ("state is read
		// from the source; events are only hints"). No event hints it yet.
		c.every(ctx, &wg, "head-release", 30*time.Second, func() error { return c.Sources.HeadRelease(ctx) })
	}
	// Expired idempotency keys go, a thousand at a time and at most two
	// thousand a pass (actor.PurgeExpiredKeys; OWNER_LAW 42, 2026-11-19).
	// Ten minutes, not an hour: a loop's first pass comes one interval after
	// the process starts, and STG restarts with every delivery, about every
	// twenty minutes; an hourly pass never ran there.
	c.every(ctx, &wg, "agentrt-key-purge", 10*time.Minute, func() error {
		n, err := actor.PurgeExpiredKeys(ctx, c.DB, 1000, 2)
		if n > 0 {
			c.logf("agentrt-key-purge: %d expired idempotency keys deleted", n)
		}
		return err
	})
	// The loops of their own the assembly binds (the plan's observations,
	// which always run: an Owner signoff's observation needs no Git reader).
	for _, l := range c.Loops {
		c.every(ctx, &wg, l.Name, l.Every, func() error { return l.Run(ctx) })
	}
	// The old dispatch lane is retired (the Owner 2026-11-13): nothing is
	// leased, routed, dispatched, accepted, closed or timed any more, hosts
	// are not watched and no assessment is asked for. What stays in this
	// pass is what the plan tree and the rules need (Steps, which the
	// assembly binds: the facts their rules read, plan decisions, the
	// acknowledging of used answers, and the rules themselves).
	// Each step that takes slowStepLog or more is logged on its own: PROD and
	// STG passes took a steady 10 seconds on 2026-11-19 and the pass's one
	// total did not say which step (the ten-second per-read timeout is the
	// suspect).
	c.every(ctx, &wg, "agentrt-facts-rules", facts, func() error {
		step := time.Now()
		c.readTuning(ctx)
		c.slow(slowStepLog, step, "agentrt-facts-rules: tuning")
		errs := make([]error, 0, len(c.Steps))
		for _, s := range c.Steps {
			step = time.Now()
			errs = append(errs, s.Run(ctx))
			c.slow(slowStepLog, step, "agentrt-facts-rules: %s", s.Name)
		}
		return errors.Join(errs...)
	})
	c.startFullCI(ctx, &wg)
	return wg.Wait
}

// clock is the controller's clock: Now when a test set it.
func (c *Controller) clock() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
