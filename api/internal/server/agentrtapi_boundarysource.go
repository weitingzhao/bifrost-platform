package server

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	verificationports "github.com/weitingzhao/bifrost-platform/api/internal/verification/ports"
)

// GET /delivery is the delivery domain's (agentrt/deliveryapi, 2026-11-19);
// what stays here binds its boundaries view to the rules verification hands
// it (verification/ports.DeliveryBoundaries).

// boundaryRuleFrom projects one rule pair for the boundaries view; the
// caller has already selected the versions (no authority is established here).
func boundaryRuleFrom(id string, current, latest *verificationports.RuleVersion, now time.Time, b verificationports.Boundaries) deliveryview.BoundaryRule {
	var c, l *deliveryview.BoundaryVersion
	if current != nil {
		c = &deliveryview.BoundaryVersion{Document: current.Doc(), Version: current.Version, Effective: current.Effective, Expires: current.Expires}
	}
	if latest != nil {
		l = &deliveryview.BoundaryVersion{Document: latest.Doc(), DraftDocument: latest.Doc(), Version: latest.Version, Effective: latest.Effective, Expires: latest.Expires}
	}
	defaults := map[string]int{}
	for _, key := range b.Tuning {
		defaults[key.Name] = key.Default
	}
	return deliveryview.ProjectBoundaryRule(id, c, l, now, b.TuningKind, b.Loosen, b.Initial, defaults)
}

// boundarySource binds the boundaries view to the rule store, the rule
// registry, the landing waits and the release gate's cap. It calls the same
// CurrentRule parser as the consumers.
var boundarySource = func() deliveryview.BoundarySource {
	b := verificationports.DeliveryBoundaries()
	keys := []deliveryview.TuningKey{}
	for _, k := range b.Tuning {
		keys = append(keys, deliveryview.TuningKey{Name: k.Name, Default: k.Default, Min: k.Min, Ceiling: k.Ceiling, Unit: k.Unit, ReadBy: k.ReadBy})
	}
	return deliveryview.BoundarySource{
		RuleIDs: b.RuleIDs,
		Retired: b.Retired,
		Rule: func(ctx context.Context, tx pgx.Tx, id string, now time.Time) (deliveryview.BoundaryRule, map[string]int, error) {
			r, err := b.Rule(ctx, tx, id, now)
			if err != nil {
				return deliveryview.BoundaryRule{}, nil, err
			}
			return boundaryRuleFrom(id, r.Current, r.Latest, now, b), r.Tuning, nil
		},
		Tuning:            keys,
		WaitBoundaries:    b.WaitBoundaries,
		ComparisonFileCap: releasepolicy.ComparisonFileCap(),
	}
}()
