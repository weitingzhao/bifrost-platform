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
			c.deliveredBy.differ(keys[i])
			c.logf("landing_delivered_by %s by %s in %s: not compared, not read again: %v", p.Root, p.By, repo, err)
			return nil
		}
		if err != nil {
			c.deliveredBy.retry(keys[i], now, DeliveredByRetry)
			return fmt.Errorf("landing_delivered_by %s by %s: reading the changes: %w", p.Root, p.By, err)
		}
		if !landinggit.SameChange(old, cur) {
			c.deliveredBy.differ(keys[i])
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

// deliveredByMemo is what the backfill remembers per pair (repository,
// root, by_op and both A): different, settled without a note (not the same
// change, or not comparable); or a time before which a failed comparison is
// not tried again. Each repository's pass forgets only its own pairs: the
// landing loop passes over every live repository in turn.
type deliveredByMemo struct {
	mu        sync.Mutex
	different map[string]bool
	after     map[string]time.Time
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
	for k := range m.different {
		if strings.HasPrefix(k, repo+"/") && !listed[k] {
			delete(m.different, k)
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
	return !m.different[key] && !now.Before(m.after[key])
}

func (m *deliveredByMemo) differ(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.different == nil {
		m.different = map[string]bool{}
	}
	m.different[key] = true
}

func (m *deliveredByMemo) retry(key string, now time.Time, wait time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.after == nil {
		m.after = map[string]time.Time{}
	}
	m.after[key] = now.Add(wait)
}
