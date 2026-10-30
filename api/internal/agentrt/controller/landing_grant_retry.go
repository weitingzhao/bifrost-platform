package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ready probes only the last temporary refusal before opening a workspace.
// Current capacity, including quota, handles blockers whose entire lifetime
// falls between scans, and replacements that keep the repository full.
// A successful probe schedules the complete gate; it authorizes nothing.
func (g *grantTries) ready(key string, clock func() time.Time, probe func(string) (bool, error)) (bool, error) {
	g.mu.Lock()
	reason := g.waiting[key]
	g.mu.Unlock()
	if reason == "" {
		return g.due(key, clock()), nil
	}
	ready, err := probe(reason)
	if err != nil || ready {
		g.mu.Lock()
		delete(g.waiting, key)
		// Start the try (or the ordinary error backoff) after the read,
		// so a slow failure does not spend its own wait.
		g.last[key] = clock()
		g.mu.Unlock()
	}
	return ready && err == nil, err
}

// record changes scheduling only. Other refusals, errors and rule rechecks
// without a grant retain the ten-minute interval. No protocol/event changes.
func (g *grantTries) record(key string, d landing.Decision, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.waiting, key)
	if err == nil && !d.Allowed && (d.Reason == "limit" || d.Reason == "window") {
		if g.waiting == nil {
			g.waiting = map[string]string{}
		}
		g.waiting[key] = d.Reason
	}
}

func grantCapacityAvailable(f landing.Facts, at time.Time) bool {
	if f.Rule == nil || f.Rule.Landing == nil || at.Before(f.Rule.Effective) || !at.Before(f.Rule.Expires) {
		return false
	}
	if _, ok := f.Rule.Landing.Repo(f.Repo); !ok {
		return false
	}
	return f.ReservedToday < f.Rule.Landing.Limits.MaxLandingsPerDay &&
		f.OtherInFlight < f.Rule.Landing.EffectiveLimits(f.Repo).MaxInFlightPerRepo
}

// A window is read at most once for waiting grants per repository/pass.
// The final gate uses its own fresh read, never this cached observation.
type grantWindowProbe struct {
	read, blocked bool
	err           error
}

func (w *grantWindowProbe) ready(ctx context.Context, read func(context.Context, string) (bool, error), repo string) (bool, error) {
	if !w.read {
		w.read = true
		if read == nil {
			w.err = fmt.Errorf("landing window source unavailable")
		} else {
			w.blocked, w.err = read(ctx, repo)
		}
	}
	return !w.blocked && w.err == nil, w.err
}
