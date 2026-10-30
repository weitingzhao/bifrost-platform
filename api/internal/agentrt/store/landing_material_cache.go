package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

var (
	ErrMaterialPending  = errors.New("landing material is being computed; retry shortly")
	ErrMaterialBusy     = errors.New("landing material capacity is busy; retry shortly")
	ErrMaterialTooLarge = errors.New("landing Git evidence exceeds the material cache capacity")
)

const (
	materialTTL        = 10 * time.Minute
	materialFailureTTL = 5 * time.Second
	materialWorkLimit  = 2 * time.Minute
	materialWaitLimit  = time.Second
	materialMaxEntries = 64
	materialMaxBytes   = 32 << 20
	materialWorkers    = 2
)

// The cache contains only Result(base,A,task,attempt,repo) and
// GrantPatch(op.A,M0,R), including the full patch's digest. Neither reads
// mutable main content: main is fetched only to obtain the named objects.
// Every argument is keyed, together with the operation, rule/checks version
// and STG choice. All other material inputs (review, acceptance, CI, quota,
// grants, latest, rule expiry) are freshly composed, never cached.
type materialKey struct {
	Op, Repo, Task, Attempt, Base, A, OpA, M0, R string
	Rule                                         GrantRule
	Checks                                       int64
	STG                                          bool
}

func grantMaterialKey(d grantDB, stg bool) materialKey {
	return materialKey{Op: d.opID, Repo: d.facts.Repo, Task: d.facts.Task, Attempt: d.facts.Attempt,
		Base: d.facts.Base, A: d.facts.A, OpA: d.op.A, M0: d.op.M0, R: d.op.R,
		Rule: d.rule, Checks: rules.LandingChecksVersion, STG: stg}
}

type materialEntry struct {
	value   grantExt
	err     error
	expires time.Time
	bytes   int
}

type materialFlight struct {
	done chan struct{}
	x    grantExt
	err  error
}

// GrantMaterialCache is a bounded, expiring process-local cache. Its zero
// value is ready. Do not share it between runtimes or Git sources.
// Misses survive HTTP cancellation, with their own deadline. Requests wait
// at most one second; overload is retriable, never an unbounded work queue.
type GrantMaterialCache struct {
	mu      sync.Mutex
	entries map[materialKey]materialEntry
	flights map[materialKey]*materialFlight
	bytes   int
}

func (c *GrantMaterialCache) read(ctx context.Context, key materialKey, work func(context.Context) (grantExt, error)) (grantExt, error) {
	if err := ctx.Err(); err != nil {
		return grantExt{}, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[materialKey]materialEntry)
		c.flights = make(map[materialKey]*materialFlight)
	}
	c.expire(time.Now())
	if entry, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return cloneGrantGit(entry.value), entry.err
	}
	f := c.flights[key]
	if f == nil {
		if len(c.flights) >= materialWorkers {
			c.mu.Unlock()
			return grantExt{}, ErrMaterialBusy
		}
		f = &materialFlight{done: make(chan struct{}), err: errors.New("landing material computation interrupted")}
		c.flights[key] = f
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), materialWorkLimit)
		safego.Go("landing.material", func() {
			defer cancel()
			defer c.finish(key, f)
			f.x, f.err = work(workCtx)
			if f.err == nil {
				f.err = workCtx.Err()
			}
		})
	}
	c.mu.Unlock()
	timer := time.NewTimer(materialWaitLimit)
	defer timer.Stop()
	select {
	case <-f.done:
		return cloneGrantGit(f.x), f.err
	case <-ctx.Done():
		return grantExt{}, ctx.Err()
	case <-timer.C:
		return grantExt{}, ErrMaterialPending
	}
}

func cloneGrantGit(x grantExt) grantExt {
	return grantExt{single: x.single, changes: slices.Clone(x.changes), patch: x.patch}
}

func (c *GrantMaterialCache) finish(key materialKey, f *materialFlight) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(f.done)
	delete(c.flights, key)
	if f.err != nil {
		// The HTTP waiter may already have returned pending. Keep a short
		// failure completion so its retry sees the error and backs off. Never
		// retain partial Git evidence, including after a deadline or panic.
		f.x = grantExt{}
		if len(f.err.Error()) > 4096 {
			f.err = errors.New(strings.Clone(f.err.Error()[:4096]))
		}
		c.remember(key, materialEntry{err: f.err, expires: time.Now().Add(materialFailureTTL), bytes: len(f.err.Error())})
		return
	}
	x := cloneGrantGit(f.x)
	size := len(x.patch.Shown) + len(x.patch.SHA256)
	for _, ch := range x.changes {
		size += 128 + len(ch.OldPath) + len(ch.NewPath) + len(ch.AddedText) + len(ch.Kind) + len(ch.OldMode) + len(ch.NewMode)
	}
	if size > materialMaxBytes {
		// Retain only this deterministic refusal, not the oversized payload.
		// Otherwise every retry of a >1s computation would start it again and
		// the caller would see material_pending forever.
		x, f.x, f.err, size = grantExt{}, grantExt{}, ErrMaterialTooLarge, 0
	}
	c.remember(key, materialEntry{value: x, err: f.err, expires: time.Now().Add(materialTTL), bytes: size})
}

// Both successful evidence and failure completions share the entry/byte
// bounds; failures have their own short lifetime and carry no evidence.
// Called with c.mu held.
func (c *GrantMaterialCache) remember(key materialKey, entry materialEntry) {
	c.expire(time.Now())
	for len(c.entries) >= materialMaxEntries || c.bytes+entry.bytes > materialMaxBytes {
		var oldest materialKey
		var at time.Time
		for k, e := range c.entries {
			if at.IsZero() || e.expires.Before(at) {
				oldest, at = k, e.expires
			}
		}
		c.bytes -= c.entries[oldest].bytes
		delete(c.entries, oldest)
	}
	c.entries[key] = entry
	c.bytes += entry.bytes
}

func (c *GrantMaterialCache) expire(now time.Time) {
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			c.bytes -= e.bytes
			delete(c.entries, k)
		}
	}
}

func readGrantMaterial(ctx context.Context, src GrantSources, stg bool, readDB func() (grantDB, error)) (*LandingGrantMaterial, error) {
	d, err := readDB()
	if err != nil {
		return nil, err
	}
	if !src.complete() {
		return nil, errors.New("landing grant evidence unavailable: no Git or CI reader in this process")
	}
	key := grantMaterialKey(d, stg)
	var x grantExt
	if src.Cache == nil {
		x, err = readGrantGit(ctx, src, d)
	} else {
		x, err = src.Cache.read(ctx, key, func(ctx context.Context) (grantExt, error) { return readGrantGit(ctx, src, d) })
	}
	if err != nil {
		return nil, err
	}
	// Even a first read may have waited on Git. Refresh the candidate binding
	// and live state before publishing it. Never join old Git to a new key.
	d, err = readDB()
	if err != nil {
		return nil, err
	}
	if key != grantMaterialKey(d, stg) {
		return nil, ErrMaterialPending
	}
	x.ci, err = grantCIRead(ctx, src.Facts, d.ciKey, d.op)
	if err != nil {
		return nil, err
	}
	return composeMaterial(d, x, stg, DecisionAt())
}
