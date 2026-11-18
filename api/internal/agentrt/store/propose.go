package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Shared writer checks and the remaining proposal/batch vocabulary.

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", eventlog.ErrInvalid, fmt.Sprintf(format, args...))
}

// Writer is actor.Writer: the store functions below still take it by this name.
type Writer = actor.Writer

func ensureWriter(ctx context.Context, tx pgx.Tx, w Writer, bounded bool) error {
	return actor.Ensure(ctx, tx, w, bounded)
}

// What only this package knows, registered with the actor package: the
// day's limit of new threads where the tuning rule sets it. No holder of an
// agent's token is registered, so the actor package refuses every agent.
func init() {
	actor.RegisterThreadLimit(func(ctx context.Context, tx pgx.Tx) (int, error) {
		tune, err := tuningIn(ctx, tx)
		if err != nil {
			return 0, err
		}
		return tune.Int(rules.TuneThreadsPerDay), nil
	})
}
