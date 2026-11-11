package server

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

func TestWindowRuntimeStream(t *testing.T) {
	t.Parallel()
	db := agentrttest.Migrated(t)
	cfg, err := pgxpool.ParseConfig(db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	// A single connection catches borrowing another connection while a stream
	// is locked, the failure mode of the previous attempt's database tests.
	cfg.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = store.Seed(ctx, pool); err != nil {
		t.Fatal(err)
	}
	event := delivery.WindowEvent{Namespace: "window-test", Who: "operator@host", Action: "expired"}
	for i := 0; i < 2; i++ {
		if err = appendWindowEvent(ctx, pool, event, `{"release_window":{"action":"expired"}}`); err != nil {
			t.Fatal(err)
		}
	}
	event.Action = "refused"
	for i := 0; i < 2; i++ {
		if err = appendWindowEvent(ctx, pool, event, `{"release_window":{"action":"refused"}}`); err != nil {
			t.Fatal(err)
		}
	}
	count, err := store.VerifyStream(ctx, pool, "release-window:window-test")
	if err != nil || count != 3 {
		t.Fatalf("events=%d error=%v", count, err)
	}
}
