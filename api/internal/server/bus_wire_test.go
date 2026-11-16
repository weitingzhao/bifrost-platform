package server

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// 2026-11-16: bus step 1 (RULES.md §7): the relay's and the gate's reading
// of the ledger is store.EventsAfter's, through the wiring: an event gate-git
// appends wakes a listener at its commit (eventlog.Append's pg_notify) and is
// read after the horizon taken before it, as an envelope of its type.
func TestBusReadsTheLedgerThroughEventsAfter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	ledger := &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: busEventsAfter}
	from, err := ledger.Horizon(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wake := relay.Listen(ctx, pool, t.Logf)
	select {
	case <-wake: // the LISTEN is in place
	case <-time.After(10 * time.Second):
		t.Fatal("the listener did not start")
	}
	commit := strings.Repeat("c", 40)
	if ok, aerr := bushook.Advance(ctx, pool, "bifrost-platform", commit, "", bushook.ChangePushed); aerr != nil || !ok {
		t.Fatalf("appended %v %v", ok, aerr)
	}
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("no wake at the commit")
	}
	// An event shows once every older transaction of the cluster has ended
	// (store.EventsAfter's horizon): other tests' databases share the server.
	var rows []relay.Row
	var next relay.Cursor
	for deadline := time.Now().Add(10 * time.Second); len(rows) == 0 && err == nil && time.Now().Before(deadline); {
		if rows, next, err = ledger.After(ctx, from, 100, []string{bushook.Type}); len(rows) == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if err != nil || len(rows) != 1 || !rows[0].Cursor().Less(next) {
		t.Fatalf("rows %+v next %s %v", rows, next, err)
	}
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := relay.Build(rows[0], all[relay.Tenant].Channels[bushook.Type], relay.Tenant, "dev")
	if err != nil || e.Subject != "bifrost-platform@"+commit || e.Actor != bushook.Actor {
		t.Fatalf("envelope %+v %v", e, err)
	}
	if row, found, err := ledger.Event(ctx, rows[0].EventID); err != nil || !found || row.Cursor() != rows[0].Cursor() {
		t.Fatalf("by id: %+v %v %v", row, found, err)
	}
	if rows, _, err := ledger.After(ctx, next, 100, []string{bushook.Type}); err != nil || len(rows) != 0 {
		t.Fatalf("read again after next: %d %v", len(rows), err)
	}
}

// 2026-11-16: bus step 1: the bus's kick runs head-release's own pass, one
// at a time with the controller's loop; with the switch off nothing is wrapped.
func TestHeadReleaseKickRunsTheSamePass(t *testing.T) {
	t.Parallel()
	var b busWiring
	if b.serialHeadRelease(nil) != nil {
		t.Fatal("an off switch became a pass")
	}
	var runs, inside atomic.Int32
	pass := b.serialHeadRelease(func(context.Context) error {
		if inside.Add(1) > 1 {
			t.Error("two passes at once")
		}
		time.Sleep(50 * time.Millisecond)
		inside.Add(-1)
		runs.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.runKicks(ctx)
	b.kickHeadRelease()
	if err := pass(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 2 {
		t.Fatalf("%d passes", runs.Load())
	}
}
