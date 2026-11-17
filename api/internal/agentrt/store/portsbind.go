package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// The plan code's ports (planstore's planports.go) whose providers are
// packages outside this one are bound here, at the package's start, and so
// are the external work writes' ports onto the plan tree (externalwork's
// ports.go, provided by planstore), and the delivery read's port onto the
// lineage (deliveryreadings' LineagePorts, provided by externalwork): this file is the store's wiring and not
// the plan's, and it is the one place of the store that names both sides. A provider still in this package binds
// its own port from its own file (envelope.go, recorderimport.go,
// loopalert.go).

func init() {
	planstore.BindPlanExternalWork(planstore.ExternalWorkPorts{
		CheckNote: func(record any) error {
			var n externalwork.ExternalNote
			if err := strictViewValue(record, &n); err != nil {
				return err
			}
			return n.Validate()
		},
		CheckDescription: func(raw []byte) error {
			_, err := externalwork.ParseExternalDescription(raw)
			return err
		},
		Snapshot: func(ctx context.Context, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
			return workregistration.Snapshot(ctx, tx, root, now, func(ctx context.Context, tx pgx.Tx, id string) (any, error) {
				return externalwork.ExternalDescriptionOf(ctx, tx, id)
			})
		},
		ActivityBuckets:      workregistration.ActivityBuckets,
		CollectorActorPrefix: externalwork.CollectorActorPrefix,
	})
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
	planstore.BindPlanCoordination(planstore.CoordinationPorts{AlertClaim: coord.AlertClaim})
	planstore.BindPlanQuestions(planstore.QuestionPorts{BodyBytes: questions.BodyBytes})
}
