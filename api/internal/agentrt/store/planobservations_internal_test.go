package store

import (
	"context"
	"encoding/json"
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
	n := planObservation{V: 1, Kind: "agentrt.plan_observation/v1", Root: b.Root, Step: b.Step, Binding: b.ID, Version: 1, Digest: b.Digest(), Source: observationKey(b), SourceVersion: 2, Status: "Fresh", Value: "true", Evidence: []string{"event:1"}, LastSuccess: timeText(time.Now().Add(-2 * time.Minute)), Reason: "verified", Changed: true}
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

func TestPV2DependencyWithdrawsSameSnapshot(t *testing.T) {
	t.Parallel()
	// The dependent's own success remains historical evidence, but its current
	// validity follows the prerequisite, without waiting for node write order.
	a := planview.Acceptance{ID: "acceptance", Milestone: "accepted"}
	d := &planview.Document{Root: "P", Version: 1, Tenant: "dev", Items: []planview.Item{{Node: "first", Acceptance: []planview.Acceptance{a}}, {Node: "second", Acceptance: []planview.Acceptance{a}, Dependencies: []planview.Edge{{Root: "P", Node: "first", Milestone: "accepted", RequiredFor: "accepted", Relation: "blocks"}}}}}
	s := &planFactsSnapshot{Now: time.Now(), Docs: map[string]*planview.Document{"P": d}, Observations: map[string]planObservation{}, Current: map[string]bool{}}
	// Use schema-valid bindings even though the deliberately small document
	// only exercises the evaluator.
	var bs []planview.Binding
	for i := range d.Items {
		b := planview.Binding{V: 1, Kind: planview.BindingKind, ID: d.Items[i].Node, Version: 1, Tenant: "dev", Root: "P", Node: d.Items[i].Node, Step: "P:" + d.Items[i].Node, Acceptance: a.ID, AcceptanceSHA256: planview.AcceptanceDigest(a), Atom: "external_verified", Source: "owner_signoff", Evidence: []string{"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
		raw, _ := json.Marshal(b)
		d.Items[i].Bindings = []json.RawMessage{raw}
		bs = append(bs, b)
		s.Observations[b.Digest()] = planObservation{Digest: b.Digest(), Status: "Fresh", Value: "true", LastSuccess: timeText(s.Now)}
	}
	if got := s.milestone("P", "second", "accepted", map[string]bool{}); got != plan.True {
		t.Fatal(got)
	}
	o := s.Observations[bs[0].Digest()]
	o.Value = "false"
	s.Observations[bs[0].Digest()] = o
	if got := s.milestone("P", "second", "accepted", map[string]bool{}); got != plan.False {
		t.Fatalf("revert did not block dependent: %v", got)
	}
	o.Value = "true"
	o.LastSuccess = timeText(s.Now.Add(-2 * time.Minute))
	s.Observations[bs[0].Digest()] = o
	if got := s.milestone("P", "second", "accepted", map[string]bool{}); got != plan.Unknown {
		t.Fatalf("stale prerequisite was accepted: %v", got)
	}
}
