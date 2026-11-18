package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
)

// Seed is actor.Seed: the runtime's first actors are the base's. The platform
// starts through this name.
func Seed(ctx context.Context, conn interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}) (bool, error) {
	return actor.Seed(ctx, conn)
}
