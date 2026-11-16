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
	"sync/atomic"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Sources read the records the runtime does not own. ok is false when a
// record cannot be read now: nothing is recorded for it, and a standing
// condition that depends on it is unknown. A source is read under the
// record's lock, so it must return when its context ends: each read gets
// ReadTimeout (review 152 B2).
type Sources struct {
	Approvals    func(context.Context) ([]store.ApprovalFact, error)
	Questions    func(context.Context) ([]store.Question, error)
	PipelineRuns func(context.Context) ([]store.PipelineRun, error)
	// PipelineRun reads one run by name, as PipelineRuns reads each (ok is
	// false when it is not kept or not completed): the run a ci.run.finished
	// names (RecordPipelineRun). nil: no event path.
	PipelineRun func(ctx context.Context, name string) (p store.PipelineRun, ok bool, err error)
	// CommitTree reads the git tree of a commit of a repository, for a CI
	// run that reports a commit it used without its tree (evidence.go);
	// nil: such a run gives no evidence.
	CommitTree func(ctx context.Context, repo, commit string) (string, error)
	// RunFailure reads what a failed CI run failed on from its tasks: the
	// tasks, the tests they name and the class (store.PipelineRun's fields),
	// for its evidence row; nil: the row says only that it failed.
	RunFailure func(ctx context.Context, run string) (tasks, tests []string, class string, err error)
	// RunEvidenced is told each completed run that is evidence, with its
	// trees and what it failed on, just before its row goes to the ledger:
	// once a run in this process unless the write fails (it is then told
	// again within PipelineFullEvery), and again after a restart, when the
	// ledger already has the run. It is called inside the pass and must
	// return at once; nil tells no one.
	RunEvidenced func(ctx context.Context, p store.PipelineRun)
	// UnitRuns records the unit tasks that finished and settles what they
	// add up to (server: receiveUnits); nil: this platform runs no units.
	UnitRuns func(ctx context.Context) error
	// HeadRelease takes each deliverable the platform releases by itself
	// one step (server/head_release.go); nil when that is switched off.
	HeadRelease func(ctx context.Context) error
	// PostRelease judges the newest finished PROD release once it has
	// settled and records the verdict (server/post_release.go); nil when
	// that is switched off.
	PostRelease func(ctx context.Context) error
	Plan        store.PlanSources
	Approval    func(ctx context.Context, number int64) (store.ApprovalFact, bool)
	Question    func(ctx context.Context, id string) (q store.Question, ok bool)
	// Ack tells the question store the runtime used the answer of a card it
	// asked (a plan change's); nil acknowledges nothing.
	Ack func(ctx context.Context, id string) error
	// Unacked lists the runtime's answered cards it has not acknowledged
	// (AckUsedAnswers); nil acknowledges none of them.
	Unacked func(ctx context.Context) ([]store.AnsweredCard, error)
	// Ask puts a plan's decision cards to the Owner (store.AcceptPlan); nil
	// leaves a plan with questions submitted until there is one. Withdraw
	// takes back the cards of a plan's round that was refused; nil leaves
	// them open.
	Ask      store.Ask
	Withdraw store.Withdraw
	CI       func(ctx context.Context, repo, commit string) (status, run string, ok bool)
	Deployed store.Deployed
}

// Controller holds what the loops need. Zero intervals take the design's
// values: leases every 10 seconds, facts and rules every 15.
type Controller struct {
	// tuning is the tuning rule the last pass read (tuning.go).
	tuning  atomic.Pointer[rules.Tuning]
	DB      store.DB
	Sources Sources
	Logf    func(format string, args ...any)
	// Infof logs what the controller did that needs no one's attention (an
	// answer it acknowledged); nil uses Logf.
	Infof          func(format string, args ...any)
	answersAckedAt time.Time // the last AckUsedAnswers pass of the facts loop
	LeaseEvery     time.Duration
	FactsEvery     time.Duration
	ReadTimeout    time.Duration // per source read; zero means 10 seconds
	// Now is the clock the passes read, time.Now when nil.
	Now func() time.Time
	// Env is the platform's own environment (AGENTRT_ENV).
	Env     string
	Started time.Time
	FullCI  *FullCI // the daily full CI (fullci.go); nil runs none
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
	// once (RecordPipelineRun). PipelineLive says whether those names come;
	// while they do, the loop's pass over every run is the reconciliation
	// and runs once per PipelineReconcileEvery (bus/RULES.md §8). nil: the
	// loop passes every PipelineEvery as before.
	PipelineFinished <-chan string
	PipelineLive     func() bool
	// pipelines remembers the completed runs this process knows are
	// recorded (external_records.go); memory only.
	pipelines pipelineMemo
}

// PlanObserveEvery is how often the plan completion atoms' sources are read
// (store.CollectPlanObservations), in a loop of its own so a slow Gitea never
// holds up facts and rules; PlanReadTimeout bounds each remote read, longer
// than ReadTimeout because the first read of a repo's history is a full one
// (PV2 review 2026-10-26).
const (
	PlanObserveEvery = time.Minute
	PlanReadTimeout  = 2 * time.Minute
)

// PlanObservations is one pass of the plan completion atoms' sources.
func (c *Controller) PlanObservations(ctx context.Context) (int, error) {
	src := c.Sources.Plan
	if src.History != nil {
		original := src.History
		src.History = func(ctx context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
			rctx, cancel := context.WithTimeout(ctx, PlanReadTimeout)
			defer cancel()
			return original(rctx, repo, task, result, snapshot)
		}
	}
	return store.CollectPlanObservations(ctx, c.DB, src)
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
		return store.SeeApproval(ctx, c.DB, n, func(ctx context.Context) (store.ApprovalFact, bool) {
			type r struct {
				s  store.ApprovalFact
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

// CardRuleEvery is how often the cards are answered by rule: a loop of its
// own, so a slow question store never holds up dispatch. Every second
// (TD-402, the coordinating thread 2026-10-21; TD-393's 「循环每 5 秒」 ran it
// with the facts loop, so a card the facts loop asked, a blocked card or a
// planner's question, was first seen just under five seconds old and
// answered on the next pass, about ten seconds after it opened): the loop
// adds at most about a second to a card's wait, plus what a pass takes (the
// question store's read, the cards answered before it). With no card for the
// rule to look at (none open, none it answered and the runtime has not
// read), a pass is one read of the question store's state (the whole state,
// decoded) and no database work; a pass that fails logs once. Only STG wires
// it (server/agentrt_wire.go).
const CardRuleEvery = time.Second

// LandingsEvery is how often the landings pass reads main: a loop of its own,
// so a slow Gitea never holds up dispatch and the decisions (review 436 B4).
const LandingsEvery = 5 * time.Minute

// DrillPassCeiling bounds one landing drill pass, a ceiling for a hung pass,
// not a schedule: a pass ends when its work does. 2026-11-07 at 5 minutes,
// split between two repositories and a cleanup, bifrost-platform had 90
// seconds: four operations each needing ~25s of Git fetches got 22.5s, were
// cut off every pass, and no claim ever ran.
const DrillPassCeiling = 15 * time.Minute

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
	if c.Sources.HeadRelease != nil {
		// Thirty seconds: the slow poll of Owner 2026-11-13 ("state is read
		// from the source; events are only hints"). No event hints it yet.
		c.every(ctx, &wg, "head-release", 30*time.Second, func() error { return c.Sources.HeadRelease(ctx) })
	}
	if c.Sources.PostRelease != nil {
		// The same slow poll: a run is judged within thirty seconds of its
		// settle time.
		c.every(ctx, &wg, "post-release-check", 30*time.Second, func() error { return c.Sources.PostRelease(ctx) })
	}
	// Always: an Owner signoff's observation needs no Git reader.
	c.every(ctx, &wg, "agentrt-plan-observations", PlanObserveEvery, func() error {
		_, err := c.PlanObservations(ctx)
		return err
	})
	// The old dispatch lane is retired (the Owner 2026-11-13): nothing is
	// leased, routed, dispatched, accepted, closed or timed any more, hosts
	// are not watched and no assessment is asked for. What stays in this
	// pass is what the plan tree and the rules need: the facts their rules
	// read, plan decisions, the acknowledging of used answers, and the rules themselves.
	c.every(ctx, &wg, "agentrt-facts-rules", facts, func() error {
		c.readTuning(ctx)
		_, ferr := c.Facts(ctx)
		_, perr := c.PlanDecisions(ctx)
		_, qerr := c.ackUsedAnswersDue(ctx)
		_, rerr := c.Rules(ctx)
		return errors.Join(ferr, perr, qerr, rerr)
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
