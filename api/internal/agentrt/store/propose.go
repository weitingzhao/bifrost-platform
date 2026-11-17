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

// ErrInvalid: the request breaks a rule of its own (a field missing or out
// of range, a writer that may not write); nothing was written.
var ErrInvalid = eventlog.ErrInvalid

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// The writer of a request, its checks and a thread's registration are the
// actor package's (the base every domain's write routes call); these are its
// names as this package has always written them.
type Writer = actor.Writer

var ErrNotYours = actor.ErrNotYours

const ThreadsPerDay = actor.ThreadsPerDay

func ThreadActor(session string) (string, error) { return actor.ThreadActor(session) }

func holdsSessionTx(ctx context.Context, tx pgx.Tx, w Writer) error {
	return actor.HoldsSessionTx(ctx, tx, w)
}

func kindOf(a string) string { return actor.KindOf(a) }

func CheckWriter(ctx context.Context, tx pgx.Tx, w Writer) (bool, error) {
	return actor.CheckWriter(ctx, tx, w)
}

func ensureWriter(ctx context.Context, tx pgx.Tx, w Writer, bounded bool) error {
	return actor.Ensure(ctx, tx, w, bounded)
}

func ActorKind(ctx context.Context, tx pgx.Tx, a string) (string, error) {
	return actor.ActorKind(ctx, tx, a)
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
