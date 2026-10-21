package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// What Progress says of the platform itself (TD-411): the controller loops
// failing the same way (loopalert.go) and the database's agentrt schema
// against the migrations this binary carries (migrate.Check), so the Console
// says "the database misses 0007 and 0008" where the Owner looks instead of a
// warning in the workers log. A schema that cannot be read is unknown, never
// a state that looks normal.

// controllerState reads the failing loops, then the schema. The schema is
// read last: a read that fails ends the transaction, and it is the one read
// here that may.
func controllerState(ctx context.Context, tx pgx.Tx) ([]map[string]any, any, error) {
	alerts, err := failingLoops(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	st, err := migrate.Check(ctx, tx)
	if err != nil {
		return alerts, map[string]any{"state": "unknown", "reason": "the schema's migrations could not be read: " + err.Error()}, nil
	}
	return alerts, st, nil
}
