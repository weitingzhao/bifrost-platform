package lineage

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Gitea pulls its GitHub mirrors every 8 hours, and only deliver runs ask it to
// fetch sooner, so a repo nobody released could be hours behind. Before a scan
// the service asks Gitea to fetch every mirror, at most once per mirrorEvery,
// and waits up to mirrorSettle for the fetches to finish.
const (
	mirrorEvery  = 2 * time.Minute
	mirrorSettle = 10 * time.Second
	mirrorPoll   = time.Second
)

// MirrorSync reports the mirror fetch this build asked for (absent when the
// last one was under mirrorEvery ago).
type MirrorSync struct {
	RequestedAt time.Time `json:"requested_at"`
	// Settled: every requested mirror finished fetching within the wait.
	Settled bool `json:"settled"`
	// Pending lists mirrors still fetching when the scan started (it read their
	// previous state).
	Pending []string `json:"pending,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

type mirrorThrottle struct {
	mu   sync.Mutex
	last time.Time
}

// due reports whether a sync may start now, and claims it if so.
func (m *mirrorThrottle) due(now time.Time, every time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.last.IsZero() && now.Sub(m.last) < every {
		return false
	}
	m.last = now
	return true
}

// fetched: Gitea stamps mirror_updated in whole seconds when a fetch ends.
func fetched(r giteaRepo, since time.Time) bool {
	return !r.MirrorUpdated.Before(since.Truncate(time.Second))
}

// refreshMirrors asks Gitea to fetch every mirror and returns the repo list as
// it stands when they are done (or when the wait runs out). It never fails the
// build: an error leaves the repos as they were and is reported in MirrorSync.
func (s *Service) refreshMirrors(ctx context.Context, g *gitea, repos []giteaRepo) ([]giteaRepo, *MirrorSync) {
	now := s.clock()
	if !s.mirrors.due(now, s.mirrorEvery) {
		return repos, nil
	}
	ms := &MirrorSync{RequestedAt: now}
	var asked []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, repoConcurrency)
	for _, r := range repos {
		if !r.Mirror {
			continue
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer safego.Recover("lineage.mirrorSync")
			sem <- struct{}{}
			defer func() { <-sem }()
			err := g.mirrorSync(ctx, name)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				ms.Errors = append(ms.Errors, err.Error())
				return
			}
			asked = append(asked, name)
		}(r.Name)
	}
	wg.Wait()
	sort.Strings(asked)
	sort.Strings(ms.Errors)
	if len(asked) == 0 {
		ms.Settled = len(ms.Errors) == 0
		return repos, ms
	}

	deadline := now.Add(s.mirrorSettle)
	for {
		t := time.NewTimer(s.mirrorPoll)
		select {
		case <-ctx.Done():
			t.Stop()
			ms.Pending = asked
			return repos, ms
		case <-t.C:
		}
		fresh, err := g.repos(ctx)
		if err != nil {
			ms.Errors = append(ms.Errors, "list repos: "+err.Error())
			ms.Pending = asked
			return repos, ms
		}
		byName := make(map[string]giteaRepo, len(fresh))
		for _, r := range fresh {
			byName[r.Name] = r
		}
		var pending []string
		for _, name := range asked {
			if !fetched(byName[name], now) {
				pending = append(pending, name)
			}
		}
		if len(pending) == 0 || !s.clock().Before(deadline) {
			ms.Pending = pending
			ms.Settled = len(pending) == 0 && len(ms.Errors) == 0
			return fresh, ms
		}
	}
}
