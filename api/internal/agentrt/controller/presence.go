package controller

import (
	"context"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// PresenceEvery is how often the controller judges the hosts and executors
// (W-31 round 3, 3.5; CUT-online-status-2026-10-23 option A): a host is
// offline 30 seconds after its last poll (store.HostLostAfter), so a host
// that stops is recorded and told within 35 seconds, and the Console, which
// reads every 5 seconds, says it within 40.
const PresenceEvery = 5 * time.Second

// Presence is one pass of the presence loop (store.Presence): each host gone
// offline or back, each executor become unavailable or available again, is
// recorded once and told to the Owner in his conversation with the chief of
// staff. It reads at Now when one is set, else at the database's clock (the
// clock GET /progress reads). It returns how many turns it recorded.
func (c *Controller) Presence(ctx context.Context) (int, error) {
	var now time.Time
	if c.Now != nil {
		now = c.Now()
	}
	return store.Presence(ctx, c.DB, now)
}
