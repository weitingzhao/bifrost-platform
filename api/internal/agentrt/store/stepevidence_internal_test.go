package store

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

const evRoot = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"

// foldOf is a landing fold holding the given operations of evRoot.
func foldOf(ops map[string]landing.Operation) landing.State {
	s := landing.State{Operations: map[string]landing.Operation{}}
	for id, o := range ops {
		o.Task = evRoot
		s.Operations[id] = o
	}
	return s
}

// Where one root's landing stands, by its latest operation.
func TestRootLanding(t *testing.T) {
	t.Parallel()
	op := func(n int, phase, outcome string) landing.Operation {
		return landing.Operation{ClaimNumber: n, Phase: phase, Outcome: outcome}
	}
	for name, c := range map[string]struct {
		ops     map[string]landing.Operation
		handoff bool
		want    int
		wantOp  string
	}{
		"never claimed, in the handoff":      {nil, true, rootPending, ""},
		"never claimed, outside the handoff": {nil, false, rootNone, ""},
		"claimed":                            {map[string]landing.Operation{"o1": op(1, "claimed", "")}, false, rootPending, "o1"},
		"branch pushed":                      {map[string]landing.Operation{"o1": op(1, "branch_pushed", "")}, false, rootPending, "o1"},
		"parked":                             {map[string]landing.Operation{"o1": op(1, "parked", "")}, false, rootPending, "o1"},
		"awaiting a grant":                   {map[string]landing.Operation{"o1": op(1, "awaiting_grant", "")}, false, rootPending, "o1"},
		"main intent":                        {map[string]landing.Operation{"o1": op(1, "main_intent", "")}, false, rootUnknown, "o1"},
		"main result ok":                     {map[string]landing.Operation{"o1": op(1, "main_result", "ok")}, false, rootUnknown, "o1"},
		"main result unknown":                {map[string]landing.Operation{"o1": op(1, "main_result", "unknown")}, false, rootUnknown, "o1"},
		"main result non-ff, replay due":     {map[string]landing.Operation{"o1": op(1, "main_result", "non_ff")}, false, rootPending, "o1"},
		"discarded, replay due":              {map[string]landing.Operation{"o1": op(1, "discarded", "main_changed")}, false, rootPending, "o1"},
		"failed":                             {map[string]landing.Operation{"o1": op(1, "failed", "stale")}, false, rootFailed, "o1"},
		"failed in main":                     {map[string]landing.Operation{"o1": op(1, "failed", "in_main")}, false, rootFailed, "o1"},
		"landed":                             {map[string]landing.Operation{"o1": {ClaimNumber: 1, Phase: "landed", EverLanded: true, R: "r1"}}, false, rootLanded, "o1"},
		"landed, then reverted":              {map[string]landing.Operation{"o1": {ClaimNumber: 1, Phase: "reverted", EverLanded: true, Reverted: true}}, false, rootFailed, "o1"},
		"rehearsal only":                     {map[string]landing.Operation{"o1": {ClaimNumber: 1, Phase: "rehearsed", DryRun: true}}, true, rootNone, "o1"},
		"failure retried, retry not taken":   {map[string]landing.Operation{"o1": {ClaimNumber: 1, Phase: "failed", Outcome: "bound", Retried: true}}, false, rootPending, "o1"},
		"failure retried, retry landed": {map[string]landing.Operation{"o1": {ClaimNumber: 1, Phase: "failed", Outcome: "bound", Retried: true},
			"o2": {ClaimNumber: 2, Phase: "landed", EverLanded: true, R: "r2"}}, false, rootLanded, "o2"},
	} {
		got, gotOp, o := rootLanding(foldOf(c.ops), evRoot, c.handoff)
		if got != c.want || gotOp != c.wantOp {
			t.Errorf("%s: %d %q, want %d %q", name, got, gotOp, c.want, c.wantOp)
		}
		if got == rootLanded && o.R == "" {
			t.Errorf("%s: landed without its R", name)
		}
	}
}

// step_landed over the roots: a no outweighs an unknown; nothing that lands
// is no; a root with no real landing is a no, so a landed one cannot mask
// it (review 630).
func TestLandingValue(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		statuses []int
		want     plan.Tri
	}{
		{nil, plan.False},
		{[]int{rootNone}, plan.False},
		{[]int{rootLanded}, plan.True},
		{[]int{rootLanded, rootNone}, plan.False},
		{[]int{rootLanded, rootLanded}, plan.True},
		{[]int{rootLanded, rootUnknown}, plan.Unknown},
		{[]int{rootUnknown}, plan.Unknown},
		{[]int{rootLanded, rootPending}, plan.False},
		{[]int{rootUnknown, rootFailed}, plan.False},
		{[]int{rootLanded, rootFailed}, plan.False},
		// R5:5.6-1: a declined root whose change landed from another root
		// counts neither way; a root truly landed is still needed.
		{[]int{rootDelivered, rootLanded}, plan.True},
		{[]int{rootDelivered}, plan.False},
		{[]int{rootDelivered, rootDelivered}, plan.False},
		{[]int{rootDelivered, rootLanded, rootFailed}, plan.False},
		{[]int{rootDelivered, rootLanded, rootUnknown}, plan.Unknown},
	} {
		if got := landingValue(c.statuses); got != c.want {
			t.Errorf("%v: %s, want %s", c.statuses, got, c.want)
		}
	}
}

// Whether B5 verified one landed member on STG.
func TestStgSettled(t *testing.T) {
	t.Parallel()
	m := StgMemberKey{Root: evRoot, Op: "land:" + evRoot + ":1", R: "r1"}
	state := func(phase, to string, admitted, covered, withheld bool) StgState {
		s := newStgState()
		s.Batches["b1"] = &StgBatch{Phase: phase}
		if admitted {
			s.Members[m] = StgMemberState{Batch: "b1", To: to}
		}
		if covered {
			s.Covers[StgCoverKey{Evidence: "cluster/cicd/run", Member: m}] = StgEvent{Seq: 9}
		}
		if withheld {
			s.Withheld[m] = StgEvent{Seq: 7}
		}
		return s
	}
	for name, c := range map[string]struct {
		s    StgState
		want plan.Tri
	}{
		"never admitted":                  {state("CLOSED", "", false, false, false), plan.False},
		"in an open batch":                {state("SNAP", "", true, false, false), plan.Unknown},
		"delivered, batch not settled":    {state("VERIFIED", "delivered", true, false, false), plan.Unknown},
		"delivered, batch settled":        {state("CLOSED", "delivered", true, false, false), plan.True},
		"covered, batch settled":          {state("CLOSED", "covered", true, true, false), plan.True},
		"covered, never admitted":         {state("CLOSED", "", false, true, false), plan.True},
		"released, batch settled":         {state("CLOSED", "released", true, false, false), plan.False},
		"released, then covered":          {state("CLOSED", "released", true, true, false), plan.True},
		"exhausted":                       {state("CLOSED", "exhausted", true, false, false), plan.False},
		"disposed":                        {state("CLOSED", "disposed", true, false, false), plan.False},
		"withheld":                        {state("CLOSED", "", false, false, true), plan.False},
		"withheld, though in a batch":     {state("SNAP", "", true, false, true), plan.False},
		"admitted to a batch not in fold": {func() StgState { s := state("CLOSED", "", true, false, false); delete(s.Batches, "b1"); return s }(), plan.Unknown},
	} {
		if got := stgSettled(c.s, m); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}
