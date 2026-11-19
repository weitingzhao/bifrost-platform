package planstore_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planwiring"
)

// This test writes plan observations through the ledger, so it needs the
// seed and the vocabulary package store declares (the last case is refused
// by the check store's vocabulary runs before a note is written). It is
// therefore a test outside the package, which may import store, and reaches
// the package's own names through export_test.go. It was a test inside
// package store, with the same body.
func TestPV2ObservationVersionsAndStaleness(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	owner := db.Connect(t, db.Owner)
	if _, err := actor.Seed(ctx, owner); err != nil {
		t.Fatal(err)
	}
	rt := db.Connect(t, db.Runtime)
	for _, n := range []planstore.PlanNode{{ID: "P", Kind: "round", Label: "P", Title: "P", Root: "P"}, {ID: "P:1", Parent: "P", Kind: "item", Label: "1", Title: "1", Root: "P"}, {ID: "P:1-1", Parent: "P:1", Kind: "step", Label: "1-1", Title: "step", StepKind: "agent", Root: "P"}} {
		if err := eventlog.InTx(ctx, rt, func(tx pgx.Tx) error { _, e := planstore.CreatePlanNode(ctx, tx, "controller", n); return e }); err != nil {
			t.Fatal(err)
		}
	}
	b := planview.Binding{Root: "P", Step: "P:1-1", ID: "b", Version: 1}
	n := planstore.PlanObservationForTest{V: 1, Kind: "agentrt.plan_observation/v1", Root: b.Root, Step: b.Step, Binding: b.ID, Version: 1, Digest: b.Digest(), Source: planstore.ObservationKeyForTest(b), SourceVersion: 2, Status: "Fresh", Value: "true", Evidence: []string{"event:1"}, LastSuccess: planstore.TimeTextForTest(time.Now().Add(-30 * time.Minute)), Reason: "verified", Changed: true}
	record := func(n planstore.PlanObservationForTest) (bool, error) {
		var did bool
		e := eventlog.InTx(ctx, rt, func(tx pgx.Tx) error {
			var e error
			did, e = planstore.RecordPlanObservationForTest(ctx, tx, n)
			return e
		})
		return did, e
	}
	if did, e := record(n); e != nil || !did {
		t.Fatalf("initial %v %v", did, e)
	}
	if did, e := record(n); e != nil || did {
		t.Fatalf("retry %v %v", did, e)
	}
	changed := n
	changed.Value = "false"
	if _, e := record(changed); !errors.Is(e, actor.ErrConflict) {
		t.Fatalf("conflicting source version: %v", e)
	}
	changed.SourceVersion = 1
	if _, e := record(changed); e != nil {
		t.Fatal(e)
	}
	var snap *planstore.PlanFactsSnapshotForTest
	if e := eventlog.InTx(ctx, rt, func(tx pgx.Tx) error { var e error; snap, e = planstore.ReadPlanFactsForTest(ctx, tx); return e }); e != nil {
		t.Fatal(e)
	}
	if snap.Observations[b.Digest()].Value != "true" {
		t.Fatal("late older version overwrote newer fact")
	}
	if v, status, _ := planstore.ReadingForTest(snap, b); v != plan.Unknown || status != "Stale" {
		t.Fatalf("expired success = %v %s", v, status)
	}
	body, _ := planstore.JSONObjectForTest(n)
	e := eventlog.InTx(ctx, rt, func(tx pgx.Tx) error {
		_, e := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.ObservationStreamForTest(b.Root), Type: "note", Actor: "owner", PlanNode: b.Step, Body: map[string]any{"text": "fake success", "plan_observation": body}})
		return e
	})
	if e == nil {
		t.Fatal("non-controller observation accepted")
	}
}

// TestMain binds the plan code's ports by name: linking planwiring binds
// nothing (decoupling item 5, 2026-11-18; the tests used to be bound only
// because they linked it).
func TestMain(m *testing.M) {
	planwiring.Bind()
	os.Exit(m.Run())
}
