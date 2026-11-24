// Package planloop holds the plan tree's passes of the agent runtime's
// controller (design v5 §5.2 控制器): the facts the done rules read, the plan
// decisions, the done rules themselves and the plan completion atoms'
// observations. The controller (agentrt/controller, verification) runs them
// on its timers; the assembly (server) binds each pass to a step of its loops.
//
// Origin: 2026-11-19. These passes were the controller's own methods, which
// made the controller a package of three domains (foundation, verification,
// coordination) that also reached into the plan's packages; they moved here
// unchanged so that each package belongs to one domain (OWNER_LAW #95).
package planloop

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/mainhistory"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// Sources read the records the plan passes do not own. ok is false when a
// record cannot be read now: nothing is recorded for it, and a standing
// condition that depends on it is unknown. A source is read under the
// record's lock, so it must return when its context ends: each read gets
// ReadTimeout (review 152 B2).
type Sources struct {
	Plan     planstore.PlanSources
	Approval func(ctx context.Context, number int64) (seenfacts.ApprovalFact, bool)
	Question func(ctx context.Context, id string) (q seenfacts.Question, ok bool)
	// Ack tells the question store the runtime used the answer of a card it
	// asked (a plan change's); nil acknowledges nothing.
	Ack      func(ctx context.Context, id string) error
	CI       func(ctx context.Context, repo, commit string) (status, run string, ok bool)
	Deployed seenfacts.Deployed
	// Laws lists the executed law_amend cards the approval store still
	// holds; nil, or ok false, records nothing.
	Laws func(ctx context.Context) (cards []planstore.LawCard, ok bool)
	// LawTable reads OWNER_LAW.md as main holds it and names the file and
	// commit it read; nil, or ok false, transcribes nothing.
	LawTable func(ctx context.Context) (md, source string, ok bool)
}

// Loop holds what the plan passes need.
type Loop struct {
	DB      eventlog.DB
	Sources Sources
	// ReadTimeout bounds each source read; zero means 10 seconds.
	ReadTimeout time.Duration
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
func (l *Loop) PlanObservations(ctx context.Context) (int, error) {
	src := l.Sources.Plan
	if src.History != nil {
		original := src.History
		src.History = func(ctx context.Context, repo, task, result, snapshot string) (mainhistory.MainHistory, error) {
			rctx, cancel := context.WithTimeout(ctx, PlanReadTimeout)
			defer cancel()
			return original(rctx, repo, task, result, snapshot)
		}
	}
	return planstore.CollectPlanObservations(ctx, l.DB, src)
}

// boundedDeployed reads a deployment with the per-read timeout, for the done
// rules, which read it while they hold the step (review 153 B1).
func (l *Loop) boundedDeployed(ctx context.Context, env string) (string, map[string]any, bool) {
	type r struct {
		commit string
		ref    map[string]any
		ok     bool
	}
	x := bounded(l, ctx, func(ctx context.Context) r {
		commit, ref, ok := l.Sources.Deployed(ctx, env)
		return r{commit, ref, ok}
	})
	return x.commit, x.ref, x.ok
}

// bounded runs a source read with the per-read timeout.
func bounded[T any](l *Loop, ctx context.Context, read func(context.Context) T) T {
	d := l.ReadTimeout
	if d <= 0 {
		d = 10 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return read(rctx)
}

// Facts is one pass of the external facts loop: each outside record a rule
// of a live step or of an architecture row depends on is read, and a change
// is recorded. One record that cannot be read or recorded does not stop the
// others; the errors come back joined. It returns how many it recorded.
func (l *Loop) Facts(ctx context.Context) (int, error) {
	nodes, err := planstore.RuledNodes(ctx, l.DB)
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
		did, err := l.see(ctx, w)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", w.Kind, w.ID, err))
		}
		if did {
			recorded++
		}
	}
	return recorded, errors.Join(errs...)
}

func (l *Loop) see(ctx context.Context, w plan.Watch) (bool, error) {
	// Each source is read under the record's lock in the store, so reads from
	// two controllers are recorded in the order they were made.
	switch w.Kind {
	case plan.WatchApproval:
		n, err := strconv.ParseInt(w.ID, 10, 64)
		if err != nil {
			return false, err
		}
		return seenfacts.SeeApproval(ctx, l.DB, n, func(ctx context.Context) (seenfacts.ApprovalFact, bool) {
			type r struct {
				s  seenfacts.ApprovalFact
				ok bool
			}
			x := bounded(l, ctx, func(ctx context.Context) r { s, ok := l.Sources.Approval(ctx, n); return r{s, ok} })
			return x.s, x.ok
		})
	case plan.WatchQuestion:
		return seenfacts.SeeQuestion(ctx, l.DB, w.ID, func(ctx context.Context) (seenfacts.Question, bool) {
			type r struct {
				q  seenfacts.Question
				ok bool
			}
			x := bounded(l, ctx, func(ctx context.Context) r { q, ok := l.Sources.Question(ctx, w.ID); return r{q, ok} })
			return x.q, x.ok
		})
	case plan.WatchCI:
		// Read before the record's transaction, not inside it: the CI
		// verdict comes from the evidence ledger, through the same pool,
		// and a pool of two (STG) with one connection listening has none
		// left while the transaction holds the other (see WatchDeploy).
		repo, commit, _ := strings.Cut(w.ID, "@")
		type r struct {
			s, run string
			ok     bool
		}
		x := bounded(l, ctx, func(ctx context.Context) r { s, run, ok := l.Sources.CI(ctx, repo, commit); return r{s, run, ok} })
		return seenfacts.SeeCI(ctx, l.DB, repo, commit, func(context.Context) (string, string, bool) { return x.s, x.run, x.ok })
	case plan.WatchDeploy:
		// The deployment is read before the record's transaction opens.
		// Read inside it, the read asked the pool for a second connection
		// while the transaction held one; STG's pool is two, one of them
		// held by the bus listener, so every pass waited the full read
		// timeout and the deployment was never recorded (2026-11-19: the
		// facts step took 10.004-10.009 s each pass). One controller runs
		// (platform-workers), so reading first does not reorder two
		// readers; the lock still orders the recording.
		commit, ref, ok := l.boundedDeployed(ctx, w.ID)
		return seenfacts.SeeDeploy(ctx, l.DB, w.ID, func(context.Context, string) (string, map[string]any, bool) { return commit, ref, ok })
	}
	return false, nil
}

// PlanDecisions is one pass of the plan decisions: proposals outside the
// standing authorization whose decision card is answered are applied or
// refused (store.DecidePlanChanges). Each question is read with the
// per-read timeout.
func (l *Loop) PlanDecisions(ctx context.Context) (int, error) {
	if l.Sources.Question == nil {
		return 0, nil
	}
	return planstore.DecidePlanChanges(ctx, l.DB, l.question, l.ack())
}

// question reads a question with the per-read timeout.
func (l *Loop) question(ctx context.Context, id string) (seenfacts.Question, bool) {
	type r struct {
		q  seenfacts.Question
		ok bool
	}
	x := bounded(l, ctx, func(ctx context.Context) r { q, ok := l.Sources.Question(ctx, id); return r{q, ok} })
	return x.q, x.ok
}

// ack acknowledges an answer with the per-read timeout; nil without a
// source.
func (l *Loop) ack() func(context.Context, string) error {
	if l.Sources.Ack == nil {
		return nil
	}
	return func(ctx context.Context, id string) error {
		return bounded(l, ctx, func(ctx context.Context) error { return l.Sources.Ack(ctx, id) })
	}
}

// Rules is one pass of the done-rule loop: every live step with a rule is
// evaluated, standing conditions against what is deployed now. One step that
// fails does not stop the others. It returns how many steps moved.
func (l *Loop) Rules(ctx context.Context) (int, error) {
	nodes, err := planstore.RuledNodes(ctx, l.DB)
	if err != nil {
		return 0, err
	}
	moved := 0
	var errs []error
	for _, n := range nodes {
		if n.Kind != "step" {
			continue
		}
		// A done step moves again only when a standing condition lapses. It
		// is skipped here, before EvaluateStep locks its row: 131 of the 248
		// ruled steps on PROD were locked each pass to do nothing (2026-11-19).
		if n.State == "done" && !n.Rule.Standing() {
			continue
		}
		to, err := planstore.EvaluateStep(ctx, l.DB, n.ID, l.boundedDeployed)
		if err != nil {
			errs = append(errs, fmt.Errorf("step %s: %w", n.ID, err))
		}
		if to != "" {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

// Laws is one pass of the changes of law: each executed law_amend card not
// yet in the plan becomes an item of the newest round, a step per standard
// (planstore.RecordLaws).
func (l *Loop) Laws(ctx context.Context) (int, error) {
	// The register first (planstore.TranscribeLaws): an added law takes the
	// number after the largest, so the table is read before any card.
	transcribed := 0
	if l.Sources.LawTable != nil {
		type read struct {
			md, source string
			ok         bool
		}
		r := bounded(l, ctx, func(rctx context.Context) read {
			md, source, ok := l.Sources.LawTable(rctx)
			return read{md, source, ok}
		})
		if r.ok {
			rows, err := planstore.ParseLawTable(r.md)
			if err != nil {
				return 0, err
			}
			if transcribed, err = planstore.TranscribeLaws(ctx, l.DB, rows, r.source); err != nil {
				return transcribed, err
			}
		}
	}
	// A law repealed seven days ago is deleted (planstore.PurgeRepealedLaws).
	// Its failure does not hold up the cards below; it is returned with them.
	_, purgeErr := planstore.PurgeRepealedLaws(ctx, l.DB, time.Now())
	if l.Sources.Laws == nil {
		return transcribed, purgeErr
	}
	type read struct {
		cards []planstore.LawCard
		ok    bool
	}
	r := bounded(l, ctx, func(rctx context.Context) read {
		cards, ok := l.Sources.Laws(rctx)
		return read{cards, ok}
	})
	cards, ok := r.cards, r.ok
	if !ok || len(cards) == 0 {
		return transcribed, purgeErr
	}
	n, err := planstore.RecordLaws(ctx, l.DB, cards)
	return transcribed + n, errors.Join(purgeErr, err)
}
