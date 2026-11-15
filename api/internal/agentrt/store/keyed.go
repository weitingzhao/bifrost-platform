package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
)

// Idempotency keys are the actor package's (a write's key and request,
// answered once and the same way on every retry).

func idempotentAs(ctx context.Context, db DB, w Writer, route, key string, request any,
	pre func(context.Context) error, op func(pgx.Tx) (map[string]any, error)) (map[string]any, error) {
	return actor.IdempotentAs(ctx, db, w, route, key, request, pre, op)
}
