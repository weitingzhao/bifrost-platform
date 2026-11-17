//bifrost:owner plan

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

func TestPV2ObservationVersionsAndStaleness(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	owner := db.Connect(t, db.Owner)
	if _, err := Seed(ctx, owner); err != nil {
		t.Fatal(err)
	}
	rt := db.Connect(t, db.Runtime)
	for _, n := range []PlanNode{{ID: "P", Kind: "round", Label: "P", Title: "P", Root: "P"}, {ID: "P:1", Parent: "P", Kind: "item", Label: "1", Title: "1", Root: "P"}, {ID: "P:1-1", Parent: "P:1", Kind: "step", Label: "1-1", Title: "step", StepKind: "agent", Root: "P"}} {
		if err := InTx(ctx, rt, func(tx pgx.Tx) error { _, e := CreatePlanNode(ctx, tx, "controller", n); return e }); err != nil {
			t.Fatal(err)
		}
	}
	b := planview.Binding{Root: "P", Step: "P:1-1", ID: "b", Version: 1}
	n := planObservation{V: 1, Kind: "agentrt.plan_observation/v1", Root: b.Root, Step: b.Step, Binding: b.ID, Version: 1, Digest: b.Digest(), Source: observationKey(b), SourceVersion: 2, Status: "Fresh", Value: "true", Evidence: []string{"event:1"}, LastSuccess: timeText(time.Now().Add(-30 * time.Minute)), Reason: "verified", Changed: true}
	record := func(n planObservation) (bool, error) {
		var did bool
		e := InTx(ctx, rt, func(tx pgx.Tx) error { var e error; did, e = recordPlanObservation(ctx, tx, n); return e })
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
	if _, e := record(changed); !errors.Is(e, ErrConflict) {
		t.Fatalf("conflicting source version: %v", e)
	}
	changed.SourceVersion = 1
	if _, e := record(changed); e != nil {
		t.Fatal(e)
	}
	var snap *planFactsSnapshot
	if e := InTx(ctx, rt, func(tx pgx.Tx) error { var e error; snap, e = readPlanFacts(ctx, tx); return e }); e != nil {
		t.Fatal(e)
	}
	if snap.Observations[b.Digest()].Value != "true" {
		t.Fatal("late older version overwrote newer fact")
	}
	if v, status, _ := snap.reading(b); v != plan.Unknown || status != "Stale" {
		t.Fatalf("expired success = %v %s", v, status)
	}
	body, _ := jsonObject(n)
	e := InTx(ctx, rt, func(tx pgx.Tx) error {
		_, e := Append(ctx, tx, Event{Stream: observationStream(b.Root), Type: "note", Actor: "owner", PlanNode: b.Step, Body: map[string]any{"text": "fake success", "plan_observation": body}})
		return e
	})
	if e == nil {
		t.Fatal("non-controller observation accepted")
	}
}
