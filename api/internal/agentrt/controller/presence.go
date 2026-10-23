package controller

import (
	"context"
	"errors"
	"fmt"
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

// presencePassLimit bounds a whole presence pass: the probe, the reads and
// writes that decide whether hosts can be judged, and the judging (review
// 529 B-1). A pass past it fails, through the loop's alert, and the next one
// tries again.
var presencePassLimit = 20 * time.Second

// UnjudgedAfter is how long the platform may be seen not taking agentd's
// calls before the controller says host presence cannot be judged.
const UnjudgedAfter = 2 * time.Minute

// platformSeen is what this controller has seen of the platform taking
// agentd's calls: whether it did at the last pass, since when it has again,
// and since when it has not. It is held in memory: a controller started
// afresh has seen nothing, and its own start is the earliest a host's silence
// counts from.
type platformSeen struct {
	mu          sync.Mutex
	up, seen    bool
	back, since time.Time
	// probeNo is since when the probe has said no (zero while it says yes);
	// probeBack the last pass in which it said yes after an outage it saw.
	probeNo, probeBack time.Time
}

// probed records the probe's answer at now; it returns since when the probe
// has said no (zero while it says yes) and the last moment it said yes again
// after saying no. An outage the probe saw is an outage whatever the claims
// say (review 529 A-1): its end is a recovery point, so a host gets its 30 s
// from it.
func (p *platformSeen) probed(yes bool, now time.Time) (time.Time, time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ended := false
	switch {
	case yes && !p.probeNo.IsZero():
		p.probeNo, p.probeBack, ended = time.Time{}, now, true
	case !yes && p.probeNo.IsZero():
		p.probeNo = now
	}
	return p.probeNo, p.probeBack, ended
}

// see records whether the platform takes calls at now; it returns since when
// it has again, or since when it has not, whether it does, and whether that
// changed.
func (p *platformSeen) see(up bool, now time.Time) (time.Time, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turned := !p.seen || up != p.up
	first := !p.seen
	p.seen = true
	switch {
	case up && first:
		// Up when first seen: no recovery point, the controller's start is
		// the earliest a host's silence counts from.
		p.up, p.back = true, time.Time{}
	case up && turned:
		p.up, p.back = true, now
	case !up && turned:
		p.up, p.since = false, now
	}
	if up {
		return p.back, true, turned
	}
	return p.since, false, turned
}

// Presence is one pass of the presence loop (store.Presence): each host gone
// offline or back, each executor become unavailable or available again, is
// recorded once and told to the Owner in his conversation with the chief of
// staff. It reads at Now when one is set, else at the database's clock (the
// clock GET /progress reads).
//
// It first asks whether the platform takes agentd's calls now (review 528
// A1b): its probe of the API says so (PlatformUp), or some host claimed
// within HostLostAfter. While neither, a host's silence is the platform's
// doing: nothing is recorded or told of the hosts, and past UnjudgedAfter
// the Owner is told that host presence cannot be judged (once; again when it
// can). Once the platform takes calls again, a host's silence counts from no
// earlier than that moment, nor than the end of the last outage the probe
// saw (even one the claims bridged: review 529 A-1), nor than this
// controller's start. The whole pass runs within presencePassLimit. When only the
// claims say so, the hosts are judged as of the latest claim, the last
// moment the platform is known to have taken calls; and when a host claimed
// after the probe began to say no, the probe is wrong: the pass fails naming
// it, and the loop's alert (controller_failing) says so until it agrees
// again. It returns how many turns it recorded.
func (c *Controller) Presence(ctx context.Context) (int, error) {
	ctx, done := context.WithTimeout(ctx, presencePassLimit)
	defer done()
	var now time.Time
	if c.Now != nil {
		now = c.Now()
	}
	clock := now
	if clock.IsZero() {
		clock = time.Now()
	}
	since, judge := c.Started, now
	var wrong error
	if c.PlatformUp != nil {
		pctx, cancel := context.WithTimeout(ctx, PlatformProbeLimit)
		probe := c.PlatformUp(pctx)
		cancel()
		probeNo, probeBack, ended := c.platform.probed(probe == nil, clock)
		if ended {
			c.logf("agentrt-presence: the platform's probe answers again after an outage it saw; hosts' silence counts from %s", stamp(probeBack))
		}
		claimed := false
		var last time.Time
		if probe != nil {
			var err error
			if last, err = store.LatestClaim(ctx, c.DB); err != nil {
				return 0, err
			}
			claimed = !last.IsZero() && clock.Sub(last) <= store.HostLostAfter
		}
		at, up, turned := c.platform.see(probe == nil || claimed, clock)
		switch {
		case turned && up:
			c.logf("agentrt-presence: the platform takes agentd's calls; hosts' silence counts from %s", stamp(maxTime(at, c.Started)))
		case turned:
			c.logf("agentrt-presence: the platform does not take agentd's calls (%s: %v; no host claimed since %s); nothing is told of the hosts until it does",
				c.probeName(), probe, stamp(last))
		}
		if !up {
			if clock.Sub(at) < UnjudgedAfter {
				return 0, nil
			}
			_, err := store.PresenceUnjudged(ctx, c.DB, fmt.Sprintf("the platform has not answered its health probe (%s) since %s (the probe: %v), "+
				"and no host has claimed since %s.", c.probeName(), at.UTC().Format(time.RFC3339), probe, stamp(last)))
			return 0, err
		}
		if _, err := store.PresenceJudged(ctx, c.DB, fmt.Sprintf("the platform takes agentd's calls again since %s.",
			stamp(maxTime(at, c.Started)))); err != nil {
			return 0, err
		}
		since = maxTime(since, maxTime(at, probeBack))
		if probe != nil {
			judge = last
		}
		if probe != nil && last.After(probeNo) {
			wrong = fmt.Errorf("the platform's health probe (%s) says it does not take agentd's calls while hosts claim: %v", c.probeName(), probe)
		}
	}
	n, err := store.Presence(ctx, c.DB, judge, since)
	return n, errors.Join(err, wrong)
}

func (c *Controller) probeName() string {
	if c.PlatformProbe == "" {
		return "the platform's probe"
	}
	return c.PlatformProbe
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format(time.RFC3339)
}
