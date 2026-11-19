package agentrtapi

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// GET /delivery is the delivery domain's (agentrt/deliveryapi, 2026-11-19);
// what stays here binds its boundaries view to the rule store (verification).

// BoundaryRuleFrom projects one stored rule pair for the boundaries view; the
// caller has already selected the versions (no authority is established here).
func BoundaryRuleFrom(id string, current, latest *store.StoredRule, now time.Time) deliveryview.BoundaryRule {
	var c, l *deliveryview.BoundaryVersion
	if current != nil {
		c = &deliveryview.BoundaryVersion{Document: current.Doc(), Version: current.Version, Effective: current.Effective, Expires: current.Expires}
	}
	if latest != nil {
		l = &deliveryview.BoundaryVersion{Document: latest.Doc(), DraftDocument: latest.Doc(), Version: latest.Version, Effective: latest.Effective, Expires: latest.Expires}
	}
	defaults := map[string]int{}
	for _, key := range rules.TuningKeys() {
		defaults[key.Name] = key.Default
	}
	return deliveryview.ProjectBoundaryRule(id, c, l, now, rules.KindTuning, rules.Loosen, rules.Initial, defaults)
}

// boundarySource binds the boundaries view to the rule store, the rule
// registry, the landing waits and the release gate's cap. It calls the same
// CurrentRule parser as the consumers.
var boundarySource = func() deliveryview.BoundarySource {
	keys := []deliveryview.TuningKey{}
	for _, k := range rules.TuningKeys() {
		keys = append(keys, deliveryview.TuningKey{Name: k.Name, Default: k.Default, Min: k.Min, Ceiling: k.Ceiling, Unit: k.Unit, ReadBy: k.ReadBy})
	}
	return deliveryview.BoundarySource{
		RuleIDs: store.RuleIDs,
		Retired: rules.Retired,
		Rule: func(ctx context.Context, tx pgx.Tx, id string, now time.Time) (deliveryview.BoundaryRule, map[string]int, error) {
			current, err := store.CurrentRule(ctx, tx, id, now)
			if err != nil {
				return deliveryview.BoundaryRule{}, nil, err
			}
			latest, err := store.LatestRule(ctx, tx, id)
			if err != nil {
				return deliveryview.BoundaryRule{}, nil, err
			}
			var values map[string]int
			if current != nil && current.Tuning != nil {
				values = current.Tuning.Values
				if values == nil {
					values = map[string]int{}
				}
			}
			return BoundaryRuleFrom(id, current, latest, now), values, nil
		},
		Tuning:            keys,
		WaitBoundaries:    landing.WaitBoundaries(),
		ComparisonFileCap: releasepolicy.ComparisonFileCap(),
	}
}()
