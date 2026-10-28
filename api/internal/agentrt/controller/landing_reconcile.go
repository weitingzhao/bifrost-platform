package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The main reconciler's own loop (S3 must 3 wiring, review 658 C-R2 / R3,
// 659 R659-4). It runs in every environment with landing records, apart from
// the STG drill: neither the drill switch, its Git nor the current rule stops
// it. denied is settled from the database alone; main is read only where a
// MainEvidence source is wired (STG today; PROD once the lander identity is).
//
// One operation's failing reads never starve the others: each operation is
// read at most once a pass, from a rotating start, within the pass's main
// budget, and an operation whose read failed waits (1, 2, 4 … 30 minutes,
// set on the clock when the read ended) under a key of its expected state,
// which a change of that state clears. What only time or the Owner can
// settle (ok with R missing, a late landing) is read again every 30 minutes.
// The waits live in memory: a restart may read once early.

const (
	reconcileMainBudget = 3 * time.Minute
	reconcileReadLimit  = 90 * time.Second
	reconcileRecheck    = 30 * time.Minute
	reconcileMaxWait    = 30 * time.Minute
)

// LandingReconcile is the reconciler's per-controller state.
type LandingReconcile struct {
	// MainEvidence reads main; nil settles denied only.
	MainEvidence store.MainEvidenceFunc
	// Repos are the repositories reconciled (bifrost-platform today).
	Repos []string

	mu    sync.Mutex
	waits map[string]reconcileWait
	start int
}

type reconcileWait struct {
	until time.Time
	step  time.Duration
}

func reconcileKey(c store.MainCandidate) string {
	return fmt.Sprintf("%s|%s|%s|%t", c.Op, c.Phase, c.Outcome, c.Late)
}

// LandingReconcilePass is one pass of the main reconciler over each repo.
func (c *Controller) LandingReconcilePass(ctx context.Context) error {
	r := c.Reconcile
	if r == nil {
		return nil
	}
	repos := r.Repos
	if len(repos) == 0 {
		repos = []string{"bifrost-platform"}
	}
	var errs []error
	for _, repo := range repos {
		if _, err := store.ReconcileDenied(ctx, c.DB, repo); err != nil {
			errs = append(errs, err)
		}
		if r.MainEvidence == nil {
			continue
		}
		if err := c.reconcileMainReads(ctx, repo); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) reconcileMainReads(ctx context.Context, repo string) error {
	r := c.Reconcile
	cands, err := store.MainCandidates(ctx, c.DB, repo)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.waits == nil {
		r.waits = map[string]reconcileWait{}
	}
	live := map[string]bool{}
	for _, cand := range cands {
		live[reconcileKey(cand)] = true
	}
	for k := range r.waits {
		if !live[k] {
			delete(r.waits, k) // its operation moved on: the wait no longer applies
		}
	}
	n := len(cands)
	first := 0
	if n > 0 {
		first = r.start % n
		r.start++
	}
	r.mu.Unlock()

	// Operations an open closing round waits on are read now, whatever
	// their wait (CUT §11.8 v3 item 2).
	urgent, err := store.OpenCloseRoundOps(ctx, c.DB, c.clock())
	if err != nil {
		return err
	}
	budget, cancel := context.WithTimeout(ctx, reconcileMainBudget)
	defer cancel()
	var errs []error
	for i := 0; i < n && budget.Err() == nil; i++ {
		cand := cands[(first+i)%n]
		key := reconcileKey(cand)
		r.mu.Lock()
		w := r.waits[key]
		r.mu.Unlock()
		if c.clock().Before(w.until) && !urgent[cand.Op] {
			continue
		}
		started := store.DecisionAt()
		readCtx, cancelRead := context.WithTimeout(budget, reconcileReadLimit)
		ev, err := r.MainEvidence(readCtx, repo, cand.Root, cand.A, cand.R)
		cancelRead()
		ev.ReadAt = started
		did := ""
		if err == nil {
			did, err = store.ReconcileMainOne(ctx, c.DB, repo, cand, ev)
		} else if cand.Phase == "landed" {
			// A landing's earlier evidence no longer stands: record that the
			// read failed (review 662 B-04); the error still backs it off.
			if _, recErr := store.ReconcileMainOne(ctx, c.DB, repo, cand, store.MainEvidence{Unreadable: true, ReadAt: started}); recErr != nil {
				err = errors.Join(err, recErr)
			}
		}
		r.mu.Lock()
		switch {
		case err != nil:
			step := min(max(2*w.step, time.Minute), reconcileMaxWait)
			r.waits[key] = reconcileWait{until: c.clock().Add(step), step: step}
			errs = append(errs, fmt.Errorf("%s: %w", cand.Op, err))
		case did == "landed":
			// Just landed: its next read, as a landing, is thirty minutes
			// on (review 662 R-04), under the key it is listed with now.
			next := cand
			next.Phase = "landed"
			if cand.Phase == "main_intent" {
				next.Outcome = "unknown"
			}
			delete(r.waits, key)
			r.waits[reconcileKey(next)] = reconcileWait{until: c.clock().Add(reconcileRecheck)}
		case cand.Phase == "landed":
			// Landed: read again for a reversal or a change of evidence every
			// thirty minutes, whatever this read found (review 662 R-04).
			r.waits[key] = reconcileWait{until: c.clock().Add(reconcileRecheck)}
		case did == "" && !cand.Late && (cand.Phase == "main_intent" || cand.Phase == "main_result" && cand.Outcome == "unknown"):
			// Inside the fence: read again once it has passed.
			r.waits[key] = reconcileWait{until: later(cand.IntentAt.Add(store.MainFence), c.clock().Add(time.Minute))}
		case did == "" || did == "ok_missing" || did == "late_landing":
			// Waiting on time or the Owner: read again later, not every pass.
			r.waits[key] = reconcileWait{until: c.clock().Add(reconcileRecheck)}
		default:
			delete(r.waits, key)
		}
		r.mu.Unlock()
	}
	return errors.Join(errs...)
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
