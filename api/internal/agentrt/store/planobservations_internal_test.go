//bifrost:owner plan

package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

func TestPV2DependencyWithdrawsSameSnapshot(t *testing.T) {
	t.Parallel()
	// The dependent's own success remains historical evidence, but its current
	// validity follows the prerequisite, without waiting for node write order.
	a := planview.Acceptance{ID: "acceptance", Milestone: "accepted"}
	d := &planview.Document{Root: "P", Version: 1, Tenant: "dev", Items: []planview.Item{{Node: "first", Acceptance: []planview.Acceptance{a}}, {Node: "second", Acceptance: []planview.Acceptance{a}, Dependencies: []planview.Edge{{Root: "P", Node: "first", Milestone: "accepted", RequiredFor: "accepted", Relation: "blocks"}}}}}
	s := &planFactsSnapshot{Now: time.Now(), Docs: map[string]*planview.Document{"P": d}, Observations: map[string]planObservation{}}
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
		s := &planFactsSnapshot{Now: now, Observations: map[string]planObservation{b.Digest(): old}, Passes: map[string]planPassSeen{}}
		s.applyPass("P", planPassSeen{Fresh: tt.fresh, At: now.Add(-time.Minute)})
		if v, status, _ := s.reading(b); v != tt.value || status != tt.status {
			t.Fatalf("%s: %v %s", tt.name, v, status)
		}
	}
}

// The coordinator, 2026-11-14: a binding whose source is retired reads
// Unknown, whatever its last observation said; the same answer under a
// binding that names no task still reads.
func TestPV2RetiredBindingReadsUnknown(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, tt := range []struct {
		name string
		b    planview.Binding
		want plan.Tri
	}{
		{"git without a task", planview.Binding{Root: "P", Step: "P:1-1", ID: "g", Version: 1, Source: "git"}, plan.True},
		{"git with a task", planview.Binding{Root: "P", Step: "P:1-1", ID: "t", Version: 1, Source: "git", Task: "t_1", Attempt: "a_1", Mission: "M-1", Round: 1}, plan.Unknown},
		{"verification", planview.Binding{Root: "P", Step: "P:1-1", ID: "v", Version: 1, Source: "verification", Task: "t_1", Attempt: "a_1", Mission: "M-1", Round: 1}, plan.Unknown},
		{"deployment", planview.Binding{Root: "P", Step: "P:1-1", ID: "d", Version: 1, Source: "deployment", Task: "t_1", Attempt: "a_1", Mission: "M-1", Round: 1}, plan.Unknown},
	} {
		s := &planFactsSnapshot{Now: now, Observations: map[string]planObservation{
			tt.b.Digest(): {Root: "P", Digest: tt.b.Digest(), Status: "Fresh", Value: "true", LastSuccess: timeText(now.Add(-time.Minute))}}}
		if v, _, _ := s.reading(tt.b); v != tt.want {
			t.Errorf("%s reads %v; want %v", tt.name, v, tt.want)
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
