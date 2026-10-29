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

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
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
	Plan     store.PlanSources
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
	// Cards lists the runtime's open cards and RuleAnswer answers one as the
	// card-auto-answer rule in force (store.AnswerByRule, TD-390); nil
	// answers nothing.
	Cards      func(ctx context.Context) ([]store.RuntimeCard, error)
	RuleAnswer func(ctx context.Context, id, option, rule string, until time.Time, why string) error
	// Assessable lists what is assessed now (TD-391): the runtime's open
	// cards and the approvals waiting for the Owner; with an error, those it
	// could read. Evidence reads what an assessment reads outside the
	// runtime (main's commits, the ledger). nil Assessable assesses nothing;
	// nil Evidence gives the assessments the runtime's records only.
	Assessable func(ctx context.Context) ([]store.AssessItem, error)
	Evidence   store.EvidenceReader
}

// Controller holds what the loops need. Zero intervals take the design's
// values: leases every 10 seconds, facts and rules every 15.
type Controller struct {
	Drill    *DrillSources // nil by default; workers opt in on STG (drill) or PROD (live)
	DrillGit DrillGit
	// Live runs the landing loop on PROD (S3, CUT §11.5): set only by the
	// wiring, with AGENTRT_LANDING_LIVE=on and the lander identity as
	// DrillGit. Without it PROD claims nothing and pushes nothing.
	Live bool
	// OpenLive opens the workspace the live landing pushes main from; nil
	// uses DrillGit.Open (tests replace it: a real push needs the lander).
	OpenLive func(ctx context.Context, r string) (LiveWorkspace, error)
	// LandingCards is the landing cards' reach into the question store
	// (landing_cards.go); zero carries no landing cards.
	LandingCards store.CardQuestions
	cardMu       sync.Mutex
	cardPause    time.Time // asking landing cards pauses until then (quota)
	// Reconcile is the main reconciler's loop (landing_reconcile.go): nil
	// runs none. Apart from the drill: any environment, any rule.
	Reconcile   *LandingReconcile
	DB          store.DB
	Sources     Sources
	Logf        func(format string, args ...any)
	LeaseEvery  time.Duration
	FactsEvery  time.Duration
	ReadTimeout time.Duration // per source read; zero means 10 seconds
	// Briefs has the controller leave the chief of staff its scheduled briefs
	// and nudges (W-31 2.4 slice ④); Now is the clock they and the waits
	// (Waits) read, time.Now when nil.
	Briefs bool
	// FailFirstVerify honours store.TestNeedFailFirstVerify: on STG only
	// (T26's rework path, the Owner 2026-10-18).
	FailFirstVerify bool
	// SkipCardsTheRuleAnswers leaves unassessed the cards the
	// card-auto-answer rule in force answers (store.ruleAnswersItem): on STG
	// only (TD-401's second half, the Owner 2026-10-20 20:0x 「STG 上规则会答
	// 的卡不起评估；needs_owner 的卡和审批单照常评估；PROD 不变」). Unset, every
	// card is assessed, whatever rule is in force.
	SkipCardsTheRuleAnswers bool
	Now                     func() time.Time
	// Env is the platform's own environment and GateRepo the repository its
	// deployments are of: the architecture table's gates run there
	// (Gates, TD-397). Either empty, no gate runs.
	Env, GateRepo string
	// WatchPresence has the controller watch the hosts and executors every
	// PresenceEvery (presence.go, W-31 3.5); the server sets it, and a test
	// that runs Start without it leaves the loop off. Started is when this
	// controller started (Start sets it when unset): a host's silence counts
	// from no earlier than it.
	WatchPresence bool
	Started       time.Time
	// PlatformUp says whether the platform takes agentd's calls now: nil
	// for yes, else why not (the server probes the API; review 528 A1b);
	// PlatformProbe names the probe in what is told. While neither it nor a
	// recent claim says yes, the presence pass tells nothing of the hosts;
	// a host's silence counts from no earlier than the moment one did again
	// (presence.go). nil: as if always.
	PlatformUp    func(ctx context.Context) error
	PlatformProbe string
	platform      platformSeen
	// drillWaits backs off drill claims refused for a reason that may pass
	// (landing_scan.go, TD-447 4); memory only.
	drillWaits drillBackoff
	// PageOwner pages the Owner's phone and says how the relay took it
	// ("sent", or "skipped" with no target), an error when it did not
	// (DiskPages, TD-415); nil: hosts' disk records are not paged.
	PageOwner func(ctx context.Context, title, body, id string) (string, error)
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
	if src.Deployed != nil {
		original := src.Deployed
		src.Deployed = func(ctx context.Context, app, env string) (string, map[string]any, bool) {
			type answer struct {
				sha string
				ref map[string]any
				ok  bool
			}
			r := bounded(c, ctx, func(ctx context.Context) answer { sha, ref, ok := original(ctx, app, env); return answer{sha, ref, ok} })
			return r.sha, r.ref, r.ok
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
// ready task no executor may take any more (store.BlockWithdrawn, TD-395)
// and one its mission's budget no longer lets run
// are blocked with the reason (store.BlockOverBudget, TD-384), then every
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
	// Withdrawn before budget: a task nobody may take is blocked for that,
	// not given budget on a card first (R3) only to be blocked again.
	gone, gerr := store.BlockWithdrawn(ctx, c.DB)
	spent, serr := store.BlockOverBudget(ctx, c.DB)
	asked, aerr := store.AskBlocked(ctx, c.DB, c.boundedAsk())
	return decided + late + withdrawn + gone + spent + asked, errors.Join(derr, lerr, werr, gerr, serr, aerr)
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

// AnswerByRule answers the runtime's cards by the card-auto-answer rule in
// force (store.AnswerByRule, TD-390): none without the rule, the lister or the
// answerer. Each call to the question store has the per-read timeout. It
// returns how many it answered.
func (c *Controller) AnswerByRule(ctx context.Context) (int, error) {
	if c.Sources.Cards == nil || c.Sources.RuleAnswer == nil {
		return 0, nil
	}
	type list struct {
		cards []store.RuntimeCard
		err   error
	}
	return store.AnswerByRule(ctx, c.DB, store.CardRule{
		Cards: func(ctx context.Context) ([]store.RuntimeCard, error) {
			x := bounded(c, ctx, func(ctx context.Context) list { cs, err := c.Sources.Cards(ctx); return list{cs, err} })
			return x.cards, x.err
		},
		Answer: func(ctx context.Context, id, option, rule string, until time.Time, why string) error {
			return bounded(c, ctx, func(ctx context.Context) error { return c.Sources.RuleAnswer(ctx, id, option, rule, until, why) })
		},
		Ack: c.ack(),
	})
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

// IssueCards is one pass over the Owner's cards on business issues: an
// answer he gave on the Console becomes the issue's evidence, the level of a
// priority he took on is asked, and a card the issue moved past is withdrawn
// (store.DecideIssueCards, W-31 BI-1 slice 2).
func (c *Controller) IssueCards(ctx context.Context) (int, error) {
	if c.Sources.Question == nil {
		return 0, nil
	}
	return store.DecideIssueCards(ctx, c.DB, c.question, c.boundedAsk(), c.boundedWithdraw(), c.ack())
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

// Waits is one pass recording the waits that passed their limit
// (store.WaitLimits: RULE-waits 总则 4, the platform keeps the clock) and
// telling the Owner of the chief of staff's briefs among them
// (store.TellWaitingBriefs), read at Now.
func (c *Controller) Waits(ctx context.Context) (int, error) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	n, err := store.WaitLimits(ctx, c.DB, now)
	// A brief past its wait is told to the Owner by the platform itself: the
	// chief of staff, who would tell him, is what does not run (review 487
	// A1).
	told, terr := store.TellWaitingBriefs(ctx, c.DB, now)
	if terr != nil {
		terr = fmt.Errorf("waiting briefs: %w", terr)
	}
	return n + told, errors.Join(err, terr)
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
// ready in the same pass, and a step a decision added is evaluated with it;
// last, the waits past their limit (Waits), read after the pass moved what
// it moves.
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
	// PROD records its live mode before its first claim (CUT §11.8 v3 item
	// 1); without it the live loop does not start.
	if c.Env == "prod" && c.landingRuns() {
		if _, err := store.EnsureLandingMode(ctx, c.DB, landingCardsRepo); err != nil {
			c.logf("agentrt landing live not started: recording the live mode: %v", err)
			c.Live = false
		}
	}
	if c.landingRuns() {
		wg.Add(1)
		safego.Go("agentrt-landing-drill", func() {
			defer wg.Done()
			run := func() error {
				passCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				return c.LandingDrillPass(passCtx)
			}
			if err := run(); err != nil {
				c.logf("agentrt-landing-drill startup: %v", err)
			}
			var loops sync.WaitGroup
			c.every(ctx, &loops, "agentrt-landing-drill", facts, run)
			loops.Wait()
		})
	}
	// Any reach runs the pass; each step checks only what it needs (review 675 R-04).
	if c.Env == "prod" && (c.LandingCards.Lookup != nil || c.LandingCards.Ack != nil || c.LandingCards.LookupKey != nil) {
		c.every(ctx, &wg, "agentrt-landing-cards", LandingCardsEvery, func() error {
			passCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			return c.LandingCardsPass(passCtx)
		})
	}
	if c.Reconcile != nil && c.Reconcile.Requests != nil && c.Env == "prod" {
		c.every(ctx, &wg, "agentrt-landing-requests", 30*time.Second, func() error {
			passCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return c.LandingRequestsPass(passCtx)
		})
	}
	if c.Reconcile != nil {
		c.every(ctx, &wg, "agentrt-landing-reconcile", facts, func() error {
			passCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			return c.LandingReconcilePass(passCtx)
		})
	}
	c.every(ctx, &wg, "agentrt-leases", lease, func() error { return c.Sweep(ctx) })
	if c.Sources.Plan.History != nil || c.Sources.Plan.Deployed != nil {
		c.every(ctx, &wg, "agentrt-plan-observations", PlanObserveEvery, func() error {
			_, err := c.PlanObservations(ctx)
			return err
		})
	}
	c.every(ctx, &wg, "agentrt-facts-rules", facts, func() error {
		_, ferr := c.Facts(ctx)
		_, gerr := c.Gates(ctx)
		_, oerr := store.Reroute(ctx, c.DB)
		_, aerr := c.Accept(ctx)
		_, berr := c.BlockedCards(ctx)
		_, derr := store.Dispatch(ctx, c.DB)
		_, cerr := c.Close(ctx)
		_, perr := c.PlanDecisions(ctx)
		_, merr := c.MissionDecisions(ctx)
		_, ierr := c.IssueCards(ctx)
		_, verr := c.Activate(ctx)
		_, rerr := c.Rules(ctx)
		_, werr := c.Waits(ctx)
		return errors.Join(ferr, gerr, oerr, aerr, berr, derr, cerr, perr, merr, ierr, verr, rerr, werr)
	})
	if c.Sources.Cards != nil && c.Sources.RuleAnswer != nil {
		c.every(ctx, &wg, "agentrt-card-rule", CardRuleEvery, func() error {
			_, err := c.AnswerByRule(ctx)
			return err
		})
	}
	if c.Sources.Assessable != nil {
		c.every(ctx, &wg, "agentrt-assess", AssessEvery, func() error {
			_, err := c.Assess(ctx)
			return err
		})
	}
	if c.Sources.MainResults != nil {
		c.every(ctx, &wg, "agentrt-landings", LandingsEvery, func() error {
			_, err := c.Landings(ctx)
			return err
		})
	}
	if c.WatchPresence {
		c.every(ctx, &wg, "agentrt-presence", PresenceEvery, func() error {
			_, err := c.Presence(ctx)
			return err
		})
	}
	if c.PageOwner != nil {
		c.every(ctx, &wg, "agentrt-disk-pages", DiskPagesEvery, func() error {
			_, err := c.DiskPages(ctx)
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
		var run failureRun // the loop's failures in a row (loopalert.go, TD-411)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			var err error
			completed := safego.Do(name, func() {
				if err = pass(); err != nil && ctx.Err() == nil {
					c.logf("%s: %v", name, err)
				}
			})
			if !completed {
				c.logf("%s: a pass panicked; the loop goes on", name)
				err = errPanicked
			}
			if ctx.Err() == nil {
				c.track(ctx, name, &run, err)
			}
		}
	})
}
