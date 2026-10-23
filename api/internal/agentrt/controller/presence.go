package controller

import (
	"context"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// PresenceEvery is how often the controller judges the hosts and executors
// (W-31 round 3, 3.5; CUT-online-status-2026-10-23 option A): a host is
// offline 30 seconds after its last poll (store.HostLostAfter), so a host
// that stops is recorded and told within 35 seconds, and the Console, which
// reads every 5 seconds, says it within 40.
const PresenceEvery = 5 * time.Second

// PlatformProbeLimit bounds a pass's asking whether the platform takes
// agentd's calls; no answer within it is no.
const PlatformProbeLimit = 3 * time.Second

// platformSeen is what this controller has seen of the platform taking
// agentd's calls: whether it did at the last pass, and since when it has
// again. It is held in memory: a controller started afresh has seen nothing,
// and its own start is the earliest a host's silence counts from.
type platformSeen struct {
	mu   sync.Mutex
	up   bool
	back time.Time
}

// see records whether the platform takes calls at now; it returns since when
// it has again (false while it does not), and whether that changed.
func (p *platformSeen) see(up bool, now time.Time) (time.Time, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turned := up != p.up
	if !up {
		p.up = false
		return time.Time{}, false, turned
	}
	if !p.up {
		p.up, p.back = true, now
	}
	return p.back, true, turned
}

// Presence is one pass of the presence loop (store.Presence): each host gone
// offline or back, each executor become unavailable or available again, is
// recorded once and told to the Owner in his conversation with the chief of
// staff. It reads at Now when one is set, else at the database's clock (the
// clock GET /progress reads). It first asks whether the platform takes
// agentd's calls now (PlatformUp): while it does not, a host's silence is
// the platform's doing and nothing is recorded or told; once it does again,
// a host's silence counts from no earlier than that moment, nor than this
// controller's start (review 526 A1b, 528 A1b). It returns how many turns it
// recorded.
func (c *Controller) Presence(ctx context.Context) (int, error) {
	var now time.Time
	if c.Now != nil {
		now = c.Now()
	}
	since := c.Started
	if c.PlatformUp != nil {
		pctx, cancel := context.WithTimeout(ctx, PlatformProbeLimit)
		up := c.PlatformUp(pctx)
		cancel()
		seen := now
		if seen.IsZero() {
			seen = time.Now()
		}
		back, ok, turned := c.platform.see(up, seen)
		switch {
		case turned && ok:
			c.logf("agentrt-presence: the platform takes agentd's calls; hosts' silence counts from %s", back.UTC().Format(time.RFC3339))
		case turned:
			c.logf("agentrt-presence: the platform does not take agentd's calls (the API's /health does not say agentrt_ready); nothing is told of the hosts until it does")
		}
		if !ok {
			return 0, nil
		}
		if back.After(since) {
			since = back
		}
	}
	return store.Presence(ctx, c.DB, now, since)
}
