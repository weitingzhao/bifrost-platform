package controller

import (
	"context"
	"errors"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/slots"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// SlotsEvery is how often the controller looks at the hosts' slots (R5
// phase 2 step 3b, store/hostslots.go); a host's count moves at most once
// every interval_s of the concurrency rule's slots block.
const SlotsEvery = 30 * time.Second

// Slots is one pass of elastic executor slots: under a concurrency rule with
// a slots block, each active host that takes ordinary work is given the
// count its rule's policy decides from the host's headroom (package slots),
// clamped to the rule's min and per_host. A host gets its first count at
// once; after that it is decided again once its last move is interval_s old,
// and at once when its count is outside the rule's range or the policy is
// fixed (a fixed count follows the rule as soon as it changes). A decision
// that moves the count is recorded (host_slots_set and
// capacity_json.effective_slots, store.SetHostSlots); one that holds it
// writes nothing. Without a rule, or with one without a block, nothing is
// written: the claim then gives per_host, as before. It returns how many
// moves it recorded.
func (c *Controller) Slots(ctx context.Context) (int, error) {
	rule, hosts, now, err := store.SlotHosts(ctx, c.DB)
	if err != nil || rule == nil {
		return 0, err
	}
	cfg := slots.ConfigOf(rule.Rule)
	if !cfg.Elastic {
		return 0, nil
	}
	interval := time.Duration(cfg.Params.IntervalS) * time.Second
	moved := 0
	var errs []error
	for _, h := range hosts {
		recorded := h.Effective >= 0
		inRange := h.Effective == slots.Clamp(h.Effective, cfg.Params.Min, cfg.Params.Max)
		if recorded && inRange && cfg.Policy != rules.SlotPolicyFixed && !h.LastSet.IsZero() && now.Sub(h.LastSet) < interval {
			continue
		}
		d, err := slots.Decide(cfg, h.Effective, h.Signals)
		if err != nil {
			return moved, err
		}
		if recorded && !d.Moved() {
			continue
		}
		wrote, err := store.SetHostSlots(ctx, c.DB, h.Host, d, h.Effective)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if wrote {
			moved++
			c.logf("agentrt slots: host %s %d -> %d (%s): %s", h.Host, d.Old, d.New, d.Policy, d.Reason)
		}
	}
	return moved, errors.Join(errs...)
}
