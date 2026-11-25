package gate

// Shared reads (bus step 4, ISSUE-74). Before this, each open stream and each long poll read
// the ledger on its own: every event woke every waiter and each read once,
// and every waiter re-read on its own timer while nothing happened. Reads
// grew with the number of open Consoles. Now:
//
//   - the same read at the same moment (same cursor, limit and types: every
//     Console tab woken by one event) goes to the ledger once and its answer
//     is handed to all of them;
//   - one follower per process watches the ledger for anything new and wakes
//     the waiters; a waiter no longer re-reads on its own timer, so an idle
//     waiter costs no read at all.
//
// The routes' contract is unchanged: the same query gives the same answer,
// read from the ledger; NATS still only wakes sooner.

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// readTimeout bounds one shared ledger read: it runs apart from any one
// waiter, so a waiter that leaves does not cancel the read for the others.
const readTimeout = 10 * time.Second

type shared struct {
	flight    singleflight.Group
	following atomic.Bool
	// answers between two wakes: every waiter woken by one wake asks the
	// same thing, so one read answers them all. A new wake starts afresh, so
	// an answer is never handed out after the wake that could change it.
	mu      sync.Mutex
	gen     uint64
	answers map[string]afterResult
}

func (s *shared) cached(gen uint64, key string) (afterResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return afterResult{}, false
	}
	a, ok := s.answers[key]
	return a, ok
}

func (s *shared) keep(gen uint64, key string, a afterResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen < s.gen {
		return
	}
	if gen > s.gen || s.answers == nil {
		s.gen, s.answers = gen, map[string]afterResult{}
	}
	s.answers[key] = a
}

// after is Ledger.After, read once for every waiter asking the same thing at
// the same moment. The rows are shared: callers only read them.
func (g *Gate) after(ctx context.Context, cursor relay.Cursor, limit int, types []string) ([]relay.Row, relay.Cursor, error) {
	sorted := slices.Clone(types)
	slices.Sort(sorted)
	key := cursor.String() + "|" + strconv.Itoa(limit) + "|" + strings.Join(sorted, ",")
	// Answers are kept only between wakes, and only while the follower runs:
	// then every new event is followed by a wake (NATS or the follower), and
	// no waiter re-reads on a timer hoping to find what no wake announced.
	cache := g.Wake != nil && g.shared.following.Load()
	var gen uint64
	if cache {
		gen = g.Wake.Gen()
		if a, ok := g.shared.cached(gen, key); ok {
			return a.rows, a.next, nil
		}
	}
	ch := g.shared.flight.DoChan(strconv.FormatUint(gen, 10)+"|"+key, func() (any, error) {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readTimeout)
		defer cancel()
		rows, next, err := g.Ledger.After(rctx, cursor, limit, types)
		if err == nil && cache {
			g.shared.keep(gen, key, afterResult{rows, next})
		}
		return afterResult{rows, next}, err
	})
	select {
	case <-ctx.Done():
		return nil, cursor, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, cursor, r.Err
		}
		a := r.Val.(afterResult)
		return a.rows, a.next, nil
	}
}

type afterResult struct {
	rows []relay.Row
	next relay.Cursor
}

// Follow watches the ledger for new events, one read per Poll (every 16 s
// with NATS up, as its safety net; every 2 s without), and wakes the
// waiters when there is one. While it runs, waiters do not re-read on their
// own timers. It needs Wake; it returns when ctx ends.
func (g *Gate) Follow(ctx context.Context) {
	if g.Wake == nil {
		return
	}
	var cursor relay.Cursor
	for {
		c, err := g.Ledger.Horizon(ctx)
		if err == nil {
			cursor = c
			break
		}
		g.logf("bus gate follower: horizon: %v", err)
		if !sleepCtx(ctx, g.poll()) {
			return
		}
	}
	g.shared.following.Store(true)
	defer g.shared.following.Store(false)
	types := g.Tenant.Types()
	for sleepCtx(ctx, g.poll()) {
		for {
			// Straight from the ledger: the follower is what finds the event
			// no wake has announced yet, so it never takes a kept answer.
			rctx, cancel := context.WithTimeout(ctx, readTimeout)
			rows, next, err := g.Ledger.After(rctx, cursor, MaxLimit, types)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					g.logf("bus gate follower: %v", err)
				}
				break
			}
			if cursor.Less(next) {
				cursor = next
			}
			if len(rows) > 0 {
				g.Wake.Notify()
			}
			if len(rows) < MaxLimit {
				break
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
