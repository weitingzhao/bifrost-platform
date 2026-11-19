// Package planwiring binds the plan code's ports to their providers
// (decoupling item 5, S4, 2026-11-18; it was agentrt/store's portsbind.go).
// The plan code (planstore) may not import lineage, delivery, coordination or
// the Owner's questions, nor lineage the plan tree: each declares what it
// needs as a port and this package, which names both sides, binds them when
// Bind is called. A binary or a test that runs the plan code calls it; the
// rule ports are bound by agentrt/store, which keeps the rules and is linked
// from here.
package planwiring

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopalert"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliverymetrics" // declares its event types
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/strictjson"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// ExternalWorkPorts are the plan's ports onto the work records (the checks
// of a note and a description, the activity reads), as this package binds
// them. The assembly binds them again by name (server/agentrt_planwiring.go) so the
// binding is explicit where the runtime starts; binding twice is harmless.
func ExternalWorkPorts() planstore.ExternalWorkPorts {
	return planstore.ExternalWorkPorts{
		CheckNote: func(record any) error {
			var n externalwork.ExternalNote
			if err := strictjson.Value(record, &n); err != nil {
				return err
			}
			return n.Validate()
		},
		CheckDescription: func(raw []byte) error {
			_, err := externalwork.ParseExternalDescription(raw)
			return err
		},
		ActivityBuckets: workregistration.ActivityBuckets,
		RootActivity: func(ctx context.Context, tx pgx.Tx, root string, now time.Time, page planstore.RecentWorkPage) (planstore.RootActivity, error) {
			a, err := workregistration.ReadRootActivity(ctx, tx, root, now, workregistration.RecentPage(page), func(ctx context.Context, tx pgx.Tx, id string) (any, error) {
				return externalwork.ExternalDescriptionOf(ctx, tx, id)
			})
			if err != nil {
				return planstore.RootActivity{}, err
			}
			recent := make([]planreadings.RecentEvent, 0, len(a.Recent))
			for _, r := range a.Recent {
				recent = append(recent, planreadings.RecentEvent(r))
			}
			return planstore.RootActivity{Work: a.Work, Streams: a.Streams, Recent: recent, Registrants: a.Registrants}, nil
		},
		HasWork:              workregistration.HasWork,
		CollectorActorPrefix: actuation.CollectorActorPrefix,
	}
}

// rulePorts are the plan's ports onto the rule in force of each kind, as far
// as the plan code reads it, read through the rule store. A rule that does
// not read counts as none there too.
func rulePorts() planstore.RulePorts {
	return planstore.RulePorts{
		PlanRuleInForce: func(ctx context.Context, tx pgx.Tx) (*planstore.PlanRule, error) {
			r, err := store.InForceIn(ctx, tx, rules.KindPlan)
			if err != nil || r == nil {
				return nil, err
			}
			return &planstore.PlanRule{ID: r.ID, Version: r.Version, DoneRules: r.Plan.DoneRules}, nil
		},
		CardAutoKindsInForce: func(ctx context.Context, tx pgx.Tx) ([]string, error) {
			r, err := store.InForceIn(ctx, tx, rules.KindCardAuto)
			if err != nil || r == nil {
				return nil, err
			}
			return r.CardAuto.Kinds, nil
		},
	}
}

// Bind binds the plan code's ports that this package provides (including the rule
// ports, read through agentrt/store). The API binary calls it at its start
// (server/agentrt_planwiring.go); a test that runs the plan code calls it from
// its TestMain. Linking this package binds nothing any more (2026-11-18,
// decoupling item 5: the planstore tests used to be bound only because they
// linked it); an unbound port refuses. Binding twice is harmless.
func Bind() {
	planstore.BindPlanRules(rulePorts())
	planstore.BindPlanExternalWork(ExternalWorkPorts())
	externalwork.BindPlanTree(externalwork.PlanTreePorts{
		LockTree: planstore.LockTree,
		RootOf:   planstore.RootOf,
		DevTree:  planstore.DevTree,
		ViewTree: func() string { return planstore.ViewTree },
	})
	deliveryreadings.BindLineage(deliveryreadings.LineagePorts{
		CommitNaming: func(ctx context.Context, tx pgx.Tx, prefix string) (deliveryreadings.CommitNaming, error) {
			n, err := externalwork.NamingOfCommit(ctx, tx, prefix)
			return deliveryreadings.CommitNaming(n), err
		},
	})
	planstore.BindPlanLoops(planstore.LoopPorts{Failing: loopalert.FailingLoops})
	planstore.BindPlanRecorderImport(planstore.RecorderImportPorts{Decide: externalwork.DecideRecorderImport})
	planstore.BindPlanCoordination(planstore.CoordinationPorts{AlertClaim: coord.AlertClaim})
	planstore.BindPlanQuestions(planstore.QuestionPorts{BodyBytes: questions.BodyBytes, BodyLimit: questions.BodyLimit})
}
