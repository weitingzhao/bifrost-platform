package relay_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/drill"
	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// 2026-11-16: bus step 1 (RULES.md §7): the relay's and the gate's reading
// of the ledger is eventlog.EventsAfter's, through the wiring: an event gate-git
// appends wakes a listener at its commit (eventlog.Append's pg_notify) and is
// read after the horizon taken before it, as an envelope of its type.
func TestBusReadsTheLedgerThroughEventsAfter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	ledger := &relay.PGLedger{DB: func() relay.TxBeginner { return pool }, EventsAfter: relay.EventsAfterOf(eventlog.EventFilter{HiddenPrefixes: []string{drill.StreamPrefix}})}
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
	// (eventlog.EventsAfter's horizon): other tests' databases share the server.
	// Origin: 2026-11-18, ci-platform-2gvsm: 10 s was shorter than a transaction
	// another package's test holds on purpose (migrate's
	// TestUpgradeTo0010StatementTimeoutRollsBack ran 30.7 s beside it, with a 37 s
	// statement timeout), so the event stayed behind the horizon and this failed.
	// Reproduced by holding a transaction open on the server for 25 s.
	var rows []relay.Row
	var next relay.Cursor
	for deadline := time.Now().Add(60 * time.Second); len(rows) == 0 && err == nil && time.Now().Before(deadline); {
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
