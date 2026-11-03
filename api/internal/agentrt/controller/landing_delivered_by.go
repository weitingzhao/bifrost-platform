package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// ChangeReader is the Git half of the delivered-by backfill: the change
// identity of two accepted results, read from those two commits alone
// (landinggit.Git.Changes). A repository's Git without it gets no backfill.
type ChangeReader interface {
	Changes(ctx context.Context, a, b string) (landinggit.ChangeIdentity, landinggit.ChangeIdentity, error)
}

// DeliveredByBudget bounds one backfill pass. The landing pass runs it
// after its work, on what the work left beyond the cleanup reservation
// (deliveredByShare), never on the work's own share (DESIGN-step-landed-
// superseded-2026-11-01 §6 item 3), and not at all when that is under
// DeliveredByMinShare: a read that short would mostly time out.
// DeliveredByRetry is how long a read that failed and may pass waits to be
// tried again; DeliveredByNotServedRetry how long one waits whose commits
// the remote seemed not to serve (landinggit.ErrNotServed).
const (
	DeliveredByBudget         = time.Minute
	DeliveredByMinShare       = 5 * time.Second
	DeliveredByRetry          = 10 * time.Minute
	DeliveredByNotServedRetry = 6 * time.Hour
)

// deliveredByShare is what the backfill may take of ctx after the work: what
// is left beyond reserve (the cleanup's), at most DeliveredByBudget; zero
// when nothing is.
func deliveredByShare(ctx context.Context, reserve time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return DeliveredByBudget
	}
	return min(DeliveredByBudget, max(0, time.Until(deadline)-reserve))
}

// LandingDeliveredByPass is one pass of the delivered-by backfill for repo
// (R5:5.6-1, §6 item 3), level-triggered: from s, the stream's fold, it
// lists the roots the Owner declined whose step has a landed sibling and no
// note naming one (store.LandingDeliveredByCandidates), and compares the
// changes of at most one pair it has not settled. The note is written only
// when the two are the same change (landinggit.SameChange); the store checks
// it again when it is written. A pair whose changes differ, or that cannot
// be compared (landinggit.ErrNotComparable: logged once), is not read
// again, but one whose commits the remote seemed not to serve
// (landinggit.ErrNotServed, an inference) is read again after
// DeliveredByNotServedRetry, logged each time; a read that failed otherwise,
// or a refused write, writes nothing and is tried again after
// DeliveredByRetry. Memory only, per repository: a restart compares each
// pair once more.
func (c *Controller) LandingDeliveredByPass(ctx context.Context, repo string, s landing.State) error {
	g, ok := c.DrillGit[repo].(ChangeReader)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, DeliveredByBudget)
	defer cancel()
	pairs, err := store.LandingDeliveredByCandidates(ctx, c.DB, repo, s)
	if err != nil {
		return err
	}
	now := c.clock()
	keys := make([]string, len(pairs))
	for i, p := range pairs {
		keys[i] = repo + "/" + p.Root + "/" + p.ByOp + "/" + p.OldA + "/" + p.NewA
	}
	c.deliveredBy.keep(repo, keys)
	// Every pass also records what the comparisons settled (R5:5.5-18),
	// the passes with nothing to compare included, so a record whose write
	// failed is written on a later one.
	cmp := c.compareOneDeliveredBy(ctx, g, repo, pairs, keys, now)
	return errors.Join(cmp, c.reworkSeenPass(ctx, repo, pairs, keys, now))
}

// compareOneDeliveredBy compares the first due pair, at most one per pass:
// the same change writes landing_delivered_by; a different one, or one that
// cannot be compared, is settled with that result; one whose commits the
// remote seemed not to serve, a failed read or a refused write waits.
func (c *Controller) compareOneDeliveredBy(ctx context.Context, g ChangeReader, repo string, pairs []store.DeliveredByPair, keys []string, now time.Time) error {
	for i, p := range pairs {
		if !c.deliveredBy.due(keys[i], now) {
			continue
		}
		old, cur, err := g.Changes(ctx, p.OldA, p.NewA)
		if errors.Is(err, landinggit.ErrNotServed) {
			c.deliveredBy.retry(keys[i], now, DeliveredByNotServedRetry)
			c.logf("landing_delivered_by %s by %s in %s: not compared: %v; read again in %s", p.Root, p.By, repo, err, DeliveredByNotServedRetry)
			return nil
		}
		if errors.Is(err, landinggit.ErrNotComparable) {
			c.deliveredBy.settle(keys[i], "not_comparable")
			c.logf("landing_delivered_by %s by %s in %s: not compared, not read again: %v", p.Root, p.By, repo, err)
			return nil
		}
		if err != nil {
			c.deliveredBy.retry(keys[i], now, DeliveredByRetry)
			return fmt.Errorf("landing_delivered_by %s by %s: reading the changes: %w", p.Root, p.By, err)
		}
		if !landinggit.SameChange(old, cur) {
			c.deliveredBy.settle(keys[i], "different")
			return nil
		}
		err = store.RecordLandingDeliveredBy(ctx, c.DB, repo, store.DeliveredBy{Root: p.Root, Op: p.Op, By: p.By, ByOp: p.ByOp,
			OldA: p.OldA, NewA: p.NewA, PatchID: old.PatchID, DiffSHA256: old.DiffSHA256})
		if err != nil {
			c.deliveredBy.retry(keys[i], now, DeliveredByRetry)
		}
		return err
	}
	return nil
}

// reworkSeenPass records landing_rework_seen for each declined root whose
// listed pairs (its step's landed siblings now) are all settled not the same
// change, none waiting, failed or same (R5:5.5-18, DESIGN §5). The store
// looks the set up first and writes only when the facts still match; a
// stale snapshot or a failed write is tried on a later pass. A set found or
// written is not looked up again by this process.
func (c *Controller) reworkSeenPass(ctx context.Context, repo string, pairs []store.DeliveredByPair, keys []string, now time.Time) error {
	type group struct {
		root, op, oldA string
		sibs           []store.ReworkSibling
		settled        bool
	}
	var order []string
	groups := map[string]*group{}
	for i, p := range pairs {
		k := p.Root + "/" + p.Op + "/" + p.OldA
		gr, ok := groups[k]
		if !ok {
			gr = &group{root: p.Root, op: p.Op, oldA: p.OldA, settled: true}
			groups[k] = gr
			order = append(order, k)
		}
		why := c.deliveredBy.result(keys[i])
		if why == "" {
			gr.settled = false
		}
		gr.sibs = append(gr.sibs, store.ReworkSibling{By: p.By, ByOp: p.ByOp, NewA: p.NewA, Why: why})
	}
	var errs []error
	sets := map[string]bool{}
	for _, gr := range groups {
		sets[repo+"/"+gr.root+"/"+gr.op+"/"+store.ReworkSetKey(gr.sibs)] = true
	}
	c.deliveredBy.keepSets(repo, sets)
	for _, k := range order {
		gr := groups[k]
		if !gr.settled {
			continue
		}
		done := repo + "/" + gr.root + "/" + gr.op + "/" + store.ReworkSetKey(gr.sibs)
		if c.deliveredBy.recordedSet(done) || now.Before(c.deliveredBy.closedUntil(done)) {
			continue
		}
		_, status, err := store.RecordReworkSeen(ctx, c.DB, repo, gr.root, gr.op, gr.oldA, gr.sibs)
		if err != nil {
			errs = append(errs, fmt.Errorf("landing_rework_seen %s in %s: %w", gr.root, repo, err))
			continue
		}
		switch status {
		case "found", "written":
			c.deliveredBy.record(done)
		case "closed":
			// A closed step may open again (a standing condition lapsed,
			// a stop lifted): looked at again after a while, never
			// remembered as recorded.
			c.deliveredBy.closeFor(done, now.Add(DeliveredByRetry))
		}
	}
	return errors.Join(errs...)
}

// deliveredByMemo is what the backfill remembers per pair (repository,
// root, by_op and both A): settled without a delivered_by note, with the
// result (different or not_comparable); or a time before which a failed
// comparison is not tried again (a wait is no result). And the rework sets
// already found or written. Each repository's pass forgets only its own:
// the landing loop passes over every live repository in turn.
type deliveredByMemo struct {
	mu       sync.Mutex
	settled  map[string]string
	after    map[string]time.Time
	recorded map[string]bool
	closed   map[string]time.Time
}

// keep forgets repo's pairs no longer listed; keys are repo's, each
// prefixed "repo/".
func (m *deliveredByMemo) keep(repo string, keys []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	listed := map[string]bool{}
	for _, k := range keys {
		listed[k] = true
	}
	for k := range m.settled {
		if strings.HasPrefix(k, repo+"/") && !listed[k] {
			delete(m.settled, k)
		}
	}
	for k := range m.after {
		if strings.HasPrefix(k, repo+"/") && !listed[k] {
			delete(m.after, k)
		}
	}
}

func (m *deliveredByMemo) due(key string, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settled[key] == "" && !now.Before(m.after[key])
}

// settle records a pair's result: different or not_comparable.
func (m *deliveredByMemo) settle(key, why string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settled == nil {
		m.settled = map[string]string{}
	}
	m.settled[key] = why
}

// result is a pair's settled result, "" while it has none.
func (m *deliveredByMemo) result(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settled[key]
}

func (m *deliveredByMemo) recordedSet(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recorded[key]
}

// record remembers a rework set found or written.
func (m *deliveredByMemo) record(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recorded == nil {
		m.recorded = map[string]bool{}
	}
	m.recorded[key] = true
}

func (m *deliveredByMemo) retry(key string, now time.Time, wait time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.after == nil {
		m.after = map[string]time.Time{}
	}
	m.after[key] = now.Add(wait)
}

// keepSets forgets repo's rework sets the pass no longer lists, so the memo
// holds at most what the last pass listed; keys are prefixed "repo/".
func (m *deliveredByMemo) keepSets(repo string, listed map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.recorded {
		if strings.HasPrefix(k, repo+"/") && !listed[k] {
			delete(m.recorded, k)
		}
	}
	for k := range m.closed {
		if strings.HasPrefix(k, repo+"/") && !listed[k] {
			delete(m.closed, k)
		}
	}
}

// closeFor puts off a set whose step is closed until until.
func (m *deliveredByMemo) closeFor(key string, until time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed == nil {
		m.closed = map[string]time.Time{}
	}
	m.closed[key] = until
}

func (m *deliveredByMemo) closedUntil(key string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed[key]
}
