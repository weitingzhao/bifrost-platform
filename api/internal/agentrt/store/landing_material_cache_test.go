package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func cacheFixture() (grantDB, GrantSources) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	now := DecisionAt()
	d := grantDB{opID: "land:task:1", latest: true, enabled: true,
		op:     landing.Operation{A: a, M0: a, R: b, Task: "task", Attempt: "attempt", Phase: "awaiting_grant", PreparedAt: now.Add(-time.Minute)},
		facts:  landing.Facts{Repo: "bifrost-platform", Tenant: "dev", Task: "task", Attempt: "attempt", Base: b, A: a, ExecutorPin: "pin"},
		rule:   GrantRule{ID: "landing:dev", Version: 1, SHA256: strings.Repeat("c", 64)},
		review: GrantReview{Task: "review"}, ciKey: &GrantCIKey{Repo: "bifrost-platform", Revision: b, Base: a}}
	d.facts.Rule = &rules.Rule{Header: rules.Header{ID: d.rule.ID, Version: 1, Effective: now.Add(-time.Hour), Expires: now.Add(time.Hour)}, Kind: rules.KindLanding,
		Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", Allow: []string{"**"}}},
			Limits: rules.LandingLimits{MaxFiles: 1, MaxChangedLines: 1, MaxLandingsPerDay: 10}}}
	src := GrantSources{Cache: &GrantMaterialCache{}, Main: func(context.Context) (string, error) { return a, nil }}
	src.Facts = LandingSource{
		Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
			return true, []rules.LandingChange{{Kind: "M", OldPath: "console/src/pages/View.tsx", NewPath: "console/src/pages/View.tsx", OldMode: "100644", NewMode: "100644", Added: 2}}, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			return landing.MainHistory{}, nil
		},
		GrantPatch: func(context.Context, string, string, string) (GrantPatch, error) {
			return GrantPatch{SHA256: "digest", Shown: "patch", Size: 5, Match: true}, nil
		},
		GrantCI: func(context.Context, GrantCIKey, time.Time) (GrantCIEvidence, error) {
			return GrantCIEvidence{Pipeline: "ci", Run: "run", UID: "uid", Candidate: b, Status: "Succeeded", CreatedAt: now}, nil
		},
	}
	return d, src
}

func TestMaterialCacheKeys(t *testing.T) {
	t.Parallel()
	d, _ := cacheFixture()
	base := grantMaterialKey(d, false)
	for name, edit := range map[string]func(*grantDB){
		"op": func(d *grantDB) { d.opID += "2" }, "repo": func(d *grantDB) { d.facts.Repo += "2" },
		"task": func(d *grantDB) { d.facts.Task += "2" }, "attempt": func(d *grantDB) { d.facts.Attempt += "2" },
		"base": func(d *grantDB) { d.facts.Base += "2" }, "A": func(d *grantDB) { d.facts.A += "2" },
		"op A": func(d *grantDB) { d.op.A += "2" }, "M0": func(d *grantDB) { d.op.M0 += "2" },
		"R": func(d *grantDB) { d.op.R += "2" }, "rule ID": func(d *grantDB) { d.rule.ID += "2" },
		"rule version": func(d *grantDB) { d.rule.Version++ }, "rule hash": func(d *grantDB) { d.rule.SHA256 += "2" },
		"checks": func(*grantDB) {}, "stg": func(*grantDB) {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cache := &GrantMaterialCache{}
			calls := 0
			work := func(context.Context) (grantExt, error) { calls++; return grantExt{}, nil }
			key := base
			for range 2 {
				if _, err := cache.read(context.Background(), key, work); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("same key computed %d times", calls)
			}
			next := d
			edit(&next)
			key = grantMaterialKey(next, name == "stg")
			if name == "checks" {
				key.Checks++
			} // a binary's checks constant cannot change in-process
			if _, err := cache.read(context.Background(), key, work); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("changed %s did not recompute", name)
			}
		})
	}
}

func TestMaterialCacheSingleflightAndBounds(t *testing.T) {
	t.Parallel()
	cache := &GrantMaterialCache{}
	started, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int32
	work := func(ctx context.Context) (grantExt, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return grantExt{patch: GrantPatch{Shown: "same"}}, nil
		case <-ctx.Done():
			return grantExt{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		safego.Go("test.material.reader", func() { defer wg.Done(); _, err := cache.read(ctx, materialKey{Op: "one"}, work); results <- err })
	}
	<-started
	// A second key uses the remaining slot; a third must answer immediately.
	wg.Add(1)
	safego.Go("test.material.second", func() { defer wg.Done(); _, _ = cache.read(ctx, materialKey{Op: "two"}, work) })
	<-started
	before := time.Now()
	if _, err := cache.read(ctx, materialKey{Op: "three"}, work); !errors.Is(err, ErrMaterialBusy) {
		t.Fatalf("overload: %v", err)
	}
	if time.Since(before) >= materialWaitLimit {
		t.Fatal("capacity waited on work")
	}
	// Cancelled clients do not cancel or multiply the bounded background work.
	cancel()
	wg.Wait()
	close(release)
	if _, err := cache.read(context.Background(), materialKey{Op: "one"}, work); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d computations for two keys", calls.Load())
	}
}

func TestMaterialCacheConcurrentReadersShareTheResult(t *testing.T) {
	t.Parallel()
	cache := &GrantMaterialCache{}
	entered, release := make(chan struct{}, 16), make(chan struct{})
	var calls atomic.Int32
	work := func(context.Context) (grantExt, error) {
		calls.Add(1)
		<-release
		return grantExt{patch: GrantPatch{Shown: "shared patch"}}, nil
	}
	type result struct {
		x   grantExt
		err error
	}
	results := make(chan result, 16)
	for range 16 {
		safego.Go("test.material.singleflight", func() {
			entered <- struct{}{}
			x, err := cache.read(context.Background(), materialKey{}, work)
			results <- result{x, err}
		})
	}
	for range 16 {
		<-entered
	}
	close(release)
	for range 16 {
		r := <-results
		if r.err != nil || r.x.patch.Shown != "shared patch" {
			t.Fatalf("shared result: %+v %v", r.x, r.err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d computations for one key", calls.Load())
	}
}

func TestMaterialCachePendingExpiryAndErrors(t *testing.T) {
	t.Parallel()
	cache := &GrantMaterialCache{}
	release := make(chan struct{})
	if _, err := cache.read(context.Background(), materialKey{}, func(context.Context) (grantExt, error) {
		<-release
		return grantExt{changes: []rules.LandingChange{{NewPath: "original"}}}, nil
	}); !errors.Is(err, ErrMaterialPending) {
		t.Fatalf("slow computation: %v", err)
	}
	close(release)
	work := func(context.Context) (grantExt, error) { return grantExt{}, errors.New("broken Git") }
	x, err := cache.read(context.Background(), materialKey{}, work)
	if err != nil {
		t.Fatal(err)
	}
	x.changes[0].NewPath = "mutated"
	x, err = cache.read(context.Background(), materialKey{}, work)
	if err != nil || x.changes[0].NewPath != "original" {
		t.Fatal("cache aliased caller", err)
	}
	cache.mu.Lock()
	cache.expire(time.Now().Add(materialTTL))
	cache.mu.Unlock()
	if _, err := cache.read(context.Background(), materialKey{}, work); err == nil {
		t.Fatal("expired evidence reused")
	}
	if _, err := cache.read(context.Background(), materialKey{}, func(context.Context) (grantExt, error) { return grantExt{}, nil }); err == nil {
		t.Fatal("failure completion lost before backoff expired")
	}
	cache.mu.Lock()
	cache.expire(time.Now().Add(materialFailureTTL))
	cache.mu.Unlock()
	if _, err := cache.read(context.Background(), materialKey{}, func(context.Context) (grantExt, error) { return grantExt{}, nil }); err != nil {
		t.Fatal("failure prevented recovery after backoff", err)
	}
}

func TestMaterialCacheEvictsWithinMemoryAndEntryLimits(t *testing.T) {
	t.Parallel()
	cache := &GrantMaterialCache{}
	work := func(context.Context) (grantExt, error) { return grantExt{patch: GrantPatch{Shown: "patch"}}, nil }
	for i := range materialMaxEntries + 1 {
		if _, err := cache.read(context.Background(), materialKey{Op: fmt.Sprint(i)}, work); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.entries) != materialMaxEntries {
		t.Fatal("entry bound", len(cache.entries))
	}
	large := func(context.Context) (grantExt, error) {
		return grantExt{changes: []rules.LandingChange{{AddedText: strings.Repeat("x", materialMaxBytes/2)}}}, nil
	}
	for _, op := range []string{"large1", "large2"} {
		if _, err := cache.read(context.Background(), materialKey{Op: op}, large); err != nil {
			t.Fatal(err)
		}
	}
	if cache.bytes > materialMaxBytes || len(cache.entries) != 1 {
		t.Fatal("byte bound", cache.bytes, len(cache.entries))
	}
}

func TestMaterialCacheOversizedEvidenceDoesNotRecomputeForever(t *testing.T) {
	t.Parallel()
	cache := &GrantMaterialCache{}
	calls := 0
	work := func(context.Context) (grantExt, error) {
		calls++
		return grantExt{changes: []rules.LandingChange{{AddedText: strings.Repeat("x", materialMaxBytes+1)}}}, nil
	}
	for range 2 {
		if _, err := cache.read(context.Background(), materialKey{}, work); !errors.Is(err, ErrMaterialTooLarge) {
			t.Fatal(err)
		}
	}
	if calls != 1 || cache.bytes != 0 {
		t.Fatal("oversized payload retained or recomputed", calls, cache.bytes)
	}
}

func TestMaterialCacheRefreshesLiveEvidence(t *testing.T) {
	t.Parallel()
	d, src := cacheFixture()
	result, patch, ci := src.Facts.Result, src.Facts.GrantPatch, src.Facts.GrantCI
	var gitReads, ciReads int
	src.Facts.Result = func(ctx context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		gitReads++
		return result(ctx, repo, task, attempt, base, a)
	}
	src.Facts.GrantPatch = func(ctx context.Context, a, m0, r string) (GrantPatch, error) {
		gitReads++
		return patch(ctx, a, m0, r)
	}
	src.Facts.GrantCI = func(ctx context.Context, key GrantCIKey, at time.Time) (GrantCIEvidence, error) {
		ciReads++
		return ci(ctx, key, at)
	}
	read := func() (*LandingGrantMaterial, error) {
		return readGrantMaterial(context.Background(), src, false, func() (grantDB, error) { return d, nil })
	}
	d.grants = []*grantView{{LandingGrant: LandingGrant{Op: d.opID, A: d.op.A, R: d.op.R, M0: d.op.M0}, exp: DecisionAt().Add(time.Hour)}}
	first, err := read()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Problems) != 0 {
		t.Fatalf("invalid control: %+v", first.Problems)
	}
	if len(first.Grants) != 1 || !first.Grants[0].Valid {
		t.Fatal("invalid grant control", first.Grants)
	}
	d.grants[0].RevokedAt = DecisionAt()
	d.latest, d.enabled, d.facts.ReservedToday = false, false, 3
	d.op.Phase = "landed"
	d.acceptance = []GrantAcceptance{{VerificationID: "changed", Result: "fail"}}
	d.review.Artifact = "new-review"
	src.Facts.GrantCI = func(context.Context, GrantCIKey, time.Time) (GrantCIEvidence, error) {
		ciReads++
		return GrantCIEvidence{Status: "Failed"}, nil
	}
	second, err := read()
	if err != nil {
		t.Fatal(err)
	}
	if gitReads != 2 || ciReads != 2 {
		t.Fatalf("Git reads=%d CI reads=%d", gitReads, ciReads)
	}
	if second.Latest || second.GrantEnabled || second.Phase != "landed" || second.QuotaReserved != 3 || second.MaterialDigest == first.MaterialDigest || second.Candidate.Review.Artifact != "new-review" {
		t.Fatalf("live facts cached: %+v", second)
	}
	if !slices.Contains(second.Problems, "ci_not_green") {
		t.Fatal(second.Problems)
	}
	if len(second.Grants) != 1 || second.Grants[0].Valid || !second.Grants[0].Revoked {
		t.Fatal("grant status cached", second.Grants)
	}
}

func TestMaterialCacheRejectsBindingChangedDuringRead(t *testing.T) {
	t.Parallel()
	d, src := cacheFixture()
	reads := 0
	_, err := readGrantMaterial(context.Background(), src, false, func() (grantDB, error) {
		reads++
		if reads == 2 {
			d.op.R = strings.Repeat("f", 40)
		}
		return d, nil
	})
	if !errors.Is(err, ErrMaterialPending) {
		t.Fatalf("changed R joined to cached Git: %v", err)
	}
}
