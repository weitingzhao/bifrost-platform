package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// ExplainCache keeps each root's last explanation (R5:5.5-19 §5): a
// request never waits for a computation; one computation runs at a time in
// the whole process; a root's result is reused for 30 seconds; after a
// failure the root is not computed again for 60 seconds; at most 8 roots
// are kept, the least recently used going first. What it returns is always
// the last successful result, with when it was read and the last failure.
type ExplainCache struct {
	DB DB
	// Now and Compute are replaced in tests.
	Now     func() time.Time
	Compute func(ctx context.Context, db DB, root string) (map[string]any, time.Time, error)
	Logf    func(format string, args ...any)

	mu    sync.Mutex
	roots map[string]*explainEntry
	slot  chan struct{}   // one computation at a time
	queue []*explainEntry // entries waiting for the slot, once each; an evicted one leaves it
	clock int64           // use order, for least recently used
}

// Cache bounds (§5).
const (
	explainFresh   = 30 * time.Second
	explainBackoff = 60 * time.Second
	explainKeep    = 8
)

type explainEntry struct {
	root      string
	steps     map[string]any
	readAt    time.Time // last success
	attemptAt time.Time // last computation started
	lastErr   error
	failedAt  time.Time
	computing bool
	queued    bool
	used      int64
}

// Get is root's explain block for a plan-view response, and starts a
// computation when the result is missing or older than 30 seconds (and the
// root is not backing off after a failure).
func (c *ExplainCache) Get(root string) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.roots == nil {
		c.roots = map[string]*explainEntry{}
		c.slot = make(chan struct{}, 1)
	}
	now := c.now()
	e := c.roots[root]
	if e == nil {
		e = &explainEntry{root: root}
		c.roots[root] = e
		c.evict(root)
	}
	c.clock++
	e.used = c.clock
	stale := e.readAt.IsZero() || now.Sub(e.readAt) >= explainFresh
	backing := !e.failedAt.IsZero() && now.Sub(e.failedAt) < explainBackoff
	if stale && !backing && !e.computing && !e.queued {
		e.queued = true
		c.queue = append(c.queue, e)
		safego.Go("explain.cache", c.run)
	}
	return e.block()
}

// block is the contract's explain object for e.
func (e *explainEntry) block() map[string]any {
	out := map[string]any{"steps": map[string]any{}}
	switch {
	case !e.readAt.IsZero():
		out["status"], out["read_at"], out["steps"] = "ok", e.readAt.UTC().Format(time.RFC3339Nano), e.steps
	case e.lastErr != nil:
		out["status"], out["reason"] = "failed", explainReason(e.lastErr)
	default:
		out["status"] = "pending"
	}
	if !e.attemptAt.IsZero() {
		out["last_attempt_at"] = e.attemptAt.UTC().Format(time.RFC3339Nano)
	}
	if e.lastErr != nil {
		out["last_error"] = explainReason(e.lastErr)
	}
	return out
}

// explainReason is a failure as the contract names it; never the error's
// text, which may carry SQL or ids the page has no use for.
func explainReason(err error) string {
	var inc *ErrExplainIncomplete
	if errors.As(err, &inc) {
		return inc.Reason
	}
	return "error"
}

// run takes the slot and computes the queued roots one by one. A root
// queued while the holder lets the slot go is seen by the holder's last
// look or takes the free slot itself, so none is left waiting.
func (c *ExplainCache) run() {
	for {
		select {
		case c.slot <- struct{}{}:
		default:
			return // the slot's holder drains the queue
		}
		c.drain()
		<-c.slot
		c.mu.Lock()
		empty := len(c.queue) == 0
		c.mu.Unlock()
		if empty {
			return
		}
	}
}

// drain computes the queued roots while holding the slot.
func (c *ExplainCache) drain() {
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			c.mu.Unlock()
			return
		}
		e := c.queue[0]
		c.queue = c.queue[1:]
		if c.roots[e.root] != e || !e.queued { // evicted, or replaced, while it waited
			c.mu.Unlock()
			continue
		}
		e.queued, e.computing, e.attemptAt = false, true, c.now()
		c.mu.Unlock()

		steps, readAt, err := c.compute(e.root)

		c.mu.Lock()
		e.computing = false
		if err != nil {
			e.lastErr, e.failedAt = err, c.now()
			if c.Logf != nil {
				c.Logf("explain %s: %v", e.root, err)
			}
		} else {
			e.steps, e.readAt, e.lastErr, e.failedAt = steps, readAt, nil, time.Time{}
		}
		c.mu.Unlock()
	}
}

// evict drops the least recently used roots beyond explainKeep, never the
// one just added and never one being computed; a dropped root leaves the
// queue too, so the queue never holds more than the entries kept.
func (c *ExplainCache) evict(keep string) {
	for len(c.roots) > explainKeep {
		var victim string
		var oldest int64
		for r, e := range c.roots {
			if r == keep || e.computing {
				continue
			}
			if victim == "" || e.used < oldest {
				victim, oldest = r, e.used
			}
		}
		if victim == "" {
			return
		}
		gone := c.roots[victim]
		delete(c.roots, victim)
		c.queue = slices.DeleteFunc(c.queue, func(e *explainEntry) bool { return e == gone })
	}
}

// compute runs one computation; a panic is its failure, not the process's.
func (c *ExplainCache) compute(root string) (steps map[string]any, readAt time.Time, err error) {
	defer func() {
		if p := recover(); p != nil {
			steps, readAt, err = nil, time.Time{}, fmt.Errorf("explain: panic: %v", p)
		}
	}()
	compute := c.Compute
	if compute == nil {
		compute = ExplainRoot
	}
	return compute(context.Background(), c.DB, root)
}

func (c *ExplainCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
