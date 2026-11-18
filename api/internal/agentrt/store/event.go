package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// What this package's tests reach eventlog and actor through. They stay here
// because nothing else outside those packages uses these names: a test of
// another package calling them there directly is what
// TestNoNewNameIsExportedForAnotherPackagesTests refuses. They go when those
// tests do (TD-582).

type ChainBreak = eventlog.ChainBreak

func VerifyStream(ctx context.Context, db interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, stream string) (int64, error) {
	return eventlog.VerifyStream(ctx, db, stream)
}

func WithEventClock(ctx context.Context, clock func() time.Time) context.Context {
	return eventlog.WithEventClock(ctx, clock)
}

// testHook, set only by tests (SetHook), is actor.TestHook.
var testHook = &actor.TestHook
