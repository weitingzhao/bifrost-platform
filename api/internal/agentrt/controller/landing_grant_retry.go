package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

// grantCapacityAvailable judges f as the final gate of a granted landing
// does: the caller sets f.OtherInFlight to grantSlotHolders, so only an
// operation pushing to main holds the slot, not one still in CI or waiting
// for its card (landing.Operation.HoldsGrantedSlot, PROD 2026-11-06).
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

// grantSlotHolders counts the operations of s other than op that hold the
// slot against a granted landing's final gate (store.landingFactsGate with a
// usable grant). Batch mates are not subtracted: at worst the probe waits a
// pass longer than the gate would, never shorter.
func grantSlotHolders(s landing.State, op string) int {
	n := 0
	for id, o := range s.Operations {
		if id != op && o.Task != "" && o.HoldsGrantedSlot() {
			n++
		}
	}
	return n
}

// grantGeneration changes whenever an operation of the repository stream is
// added or moves, or a grant is recorded on it.
func grantGeneration(s landing.State) string {
	keys := make([]string, 0, len(s.Operations)+len(s.Grants))
	for id, o := range s.Operations {
		keys = append(keys, id+"="+o.Phase+"/"+o.Outcome)
	}
	for op, periods := range s.Grants {
		for _, p := range periods {
			keys = append(keys, op+"+"+p.Start.String())
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// observe forgets the ten-minute wait of every grant of repo's operations
// when the stream moved (an operation ended, parked, pushed to main; a grant
// was recorded), as drillBackoff does with drillGeneration: the next pass
// tries them again. A limit or window wait keeps its probe, which runs every
// pass anyway; the rule rechecks without a grant keep their interval.
func (g *grantTries) observe(repo string, s landing.State) {
	generation := grantGeneration(s)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.generation == nil {
		g.generation = map[string]string{}
	}
	if g.generation[repo] == generation {
		return
	}
	g.generation[repo] = generation
	for key := range g.last {
		op, grant, _ := strings.Cut(key, "/")
		if _, ok := s.Operations[op]; ok && !strings.HasPrefix(grant, "rule:") {
			delete(g.last, key)
		}
	}
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
