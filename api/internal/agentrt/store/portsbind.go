package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
)

// The plan code's ports (planports.go) whose providers are packages already
// outside this one are bound here, at the package's start: this file is the
// store's wiring and not the plan's, and it is the one place of the store
// that names both sides. A provider still in this package binds its own
// port from its own file (envelope.go, recorderimport.go).

func init() {
	BindPlanExternalWork(ExternalWorkPorts{
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
		ActivityBuckets: workregistration.ActivityBuckets,
	})
	BindPlanCoordination(CoordinationPorts{AlertClaim: coord.AlertClaim})
}
