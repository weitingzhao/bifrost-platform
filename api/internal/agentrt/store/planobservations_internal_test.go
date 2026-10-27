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
		b := planview.Binding{V: 1, Kind: planview.BindingKind, ID: d.Items[i].Node, Version: 1, Tenant: "dev", Root: "P", Node: d.Items[i].Node, Step: "P:" + d.Items[i].Node, Acceptance: a.ID, AcceptanceSHA256: planview.AcceptanceDigest(a), Atom: "external_verified", Source: "owner_signoff"}
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
	o.LastSuccess = timeText(s.Now.Add(-30 * time.Minute))
	s.Observations[bs[0].Digest()] = o
	if got := s.milestone("P", "second", "accepted", map[string]bool{}); got != plan.Unknown {
		t.Fatalf("stale prerequisite was accepted: %v", got)
	}
}

// PV2 review 2026-10-26: an unchanged answer is not rewritten; the pass that
// read it again keeps it fresh, and a binding the pass could not read ages to
// Stale.
func TestPV2PassKeepsUnchangedAnswerFresh(t *testing.T) {
	t.Parallel()
	b := planview.Binding{Root: "P", Step: "P:1-1", ID: "b", Version: 1}
	now := time.Now()
	old := planObservation{Root: "P", Digest: b.Digest(), Status: "Fresh", Value: "true", LastSuccess: timeText(now.Add(-30 * time.Minute))}
	for _, tt := range []struct {
		name   string
		fresh  map[string]time.Time
		value  plan.Tri
		status string
	}{
		{"read again", map[string]time.Time{b.Digest(): now.Add(-time.Minute)}, plan.True, "Fresh"},
		// PV2 re-review B1: the latest pass could not read it; it keeps the
		// time of the read before, a minute ago, and stays Fresh.
		{"failed after a recent read", map[string]time.Time{b.Digest(): now.Add(-2 * time.Minute)}, plan.True, "Fresh"},
		{"not read for 30 minutes", map[string]time.Time{}, plan.Unknown, "Stale"},
	} {
		s := &planFactsSnapshot{Now: now, Observations: map[string]planObservation{b.Digest(): old}, Current: map[string]bool{}, Passes: map[string]planPassSeen{}}
		s.applyPass("P", planPassSeen{Fresh: tt.fresh, At: now.Add(-time.Minute)})
		if v, status, _ := s.reading(b); v != tt.value || status != tt.status {
			t.Fatalf("%s: %v %s", tt.name, v, status)
		}
	}
}

// PV2 review 2026-10-26 S1: adopting a view keeps a step's other conditions.
func TestPV2ComposeBoundRule(t *testing.T) {
	t.Parallel()
	atom := func(id string, v int) any {
		return map[string]any{"main_effective": map[string]any{"binding": id, "version": v}}
	}
	for _, tt := range []struct {
		name, old string
		atoms     []any
		want      string
		fails     bool
	}{
		{"empty step", `{}`, []any{atom("b", 1)}, `{"all":[{"main_effective":{"binding":"b","version":1}}]}`, false},
		{"keeps signoff", `{"owner_signoff":"x"}`, []any{atom("b", 1)}, `{"all":[{"owner_signoff":"x"},{"main_effective":{"binding":"b","version":1}}]}`, false},
		{"replaces old version", `{"all":[{"owner_signoff":"x"},{"main_effective":{"binding":"b","version":1}}]}`, []any{atom("b", 2)}, `{"all":[{"owner_signoff":"x"},{"main_effective":{"binding":"b","version":2}}]}`, false},
		{"binding dropped", `{"all":[{"owner_signoff":"x"},{"main_effective":{"binding":"b","version":1}}]}`, nil, `{"all":[{"owner_signoff":"x"}]}`, false},
		{"only bound, dropped", `{"all":[{"main_effective":{"binding":"b","version":1}}]}`, nil, `{}`, false},
		{"bound under any", `{"any":[{"owner_signoff":"x"},{"main_effective":{"binding":"b","version":1}}]}`, []any{atom("b", 2)}, "", true},
	} {
		got, err := composeBoundRule([]byte(tt.old), tt.atoms)
		if (err != nil) != tt.fails || (!tt.fails && string(got) != tt.want) {
			t.Fatalf("%s: %s %v", tt.name, got, err)
		}
	}
}
