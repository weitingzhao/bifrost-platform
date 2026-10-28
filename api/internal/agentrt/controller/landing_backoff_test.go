package controller

import (
	"slices"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// TD-447 4: a refusal that may pass waits 1, 2, 4, then 5 minutes; a claim or
// "changed" clears it; a task leaving the list is forgotten; any move of the
// stream's operations or retries forgets every wait.
func TestDrillBackoff(t *testing.T) {
	var b drillBackoff
	at := time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC)
	g := drillGeneration(landing.State{})
	if got := b.due([]string{"t1", "t2"}, g, at); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Fatal(got)
	}
	for i, wait := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		b.record("t1", false, at)
		if got := b.due([]string{"t1", "t2"}, g, at.Add(wait-time.Second)); !slices.Equal(got, []string{"t2"}) {
			t.Fatalf("try %d: still waiting want [t2], got %v", i+1, got)
		}
		if got := b.due([]string{"t1", "t2"}, g, at.Add(wait)); !slices.Equal(got, []string{"t1", "t2"}) {
			t.Fatalf("try %d: due after %v, got %v", i+1, wait, got)
		}
	}
	b.record("t1", true, at)
	if got := b.due([]string{"t1"}, g, at); !slices.Equal(got, []string{"t1"}) {
		t.Fatal("a claim did not clear the wait", got)
	}
	b.record("t1", false, at)
	b.due([]string{"t2"}, g, at)
	if got := b.due([]string{"t1"}, g, at); !slices.Equal(got, []string{"t1"}) {
		t.Fatal("a task that left the list kept its wait", got)
	}
	b.record("t1", false, at)
	moved := drillGeneration(landing.State{Operations: map[string]landing.Operation{"land:x:1": {Phase: "claimed"}}})
	if got := b.due([]string{"t1"}, moved, at); !slices.Equal(got, []string{"t1"}) {
		t.Fatal("a moved stream kept the waits", got)
	}
}

// The generation sees what frees a slot or changes a retry with the same
// number of operations and retries (review 653 R653-3), and only that.
func TestDrillGeneration(t *testing.T) {
	ops := func(phase, outcome string) map[string]landing.Operation {
		return map[string]landing.Operation{"land:a:1": {Phase: phase, Outcome: outcome}, "land:b:1": {Phase: "claimed"}, "land:c:1": {Phase: "failed", Outcome: "bound"}}
	}
	retry := func(after, consumed string, at time.Time) map[string]landing.Retry {
		return map[string]landing.Retry{"a": {AfterOp: after, ConsumedBy: consumed, At: at}}
	}
	at := time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC)
	base := drillGeneration(landing.State{Operations: ops("branch_pushed", ""), Retries: retry("land:a:1", "", at)})
	for name, s := range map[string]landing.State{
		"parked":            {Operations: ops("parked", ""), Retries: retry("land:a:1", "", at)},
		"failed":            {Operations: ops("failed", "ci_red"), Retries: retry("land:a:1", "", at)},
		"rehearsed":         {Operations: ops("rehearsed", ""), Retries: retry("land:a:1", "", at)},
		"retry consumed":    {Operations: ops("branch_pushed", ""), Retries: retry("land:a:1", "land:a:2", at)},
		"retry after other": {Operations: ops("branch_pushed", ""), Retries: retry("land:a:2", "", at)},
		"retry again":       {Operations: ops("branch_pushed", ""), Retries: retry("land:a:1", "", at.Add(time.Hour))},
		"outcome only":      {Operations: ops("branch_pushed", "window"), Retries: retry("land:a:1", "", at)},
	} {
		if drillGeneration(s) == base {
			t.Errorf("%s: the generation did not move", name)
		}
	}
	for range 20 {
		if drillGeneration(landing.State{Operations: ops("branch_pushed", ""), Retries: retry("land:a:1", "", at)}) != base {
			t.Fatal("the same stream gave another generation")
		}
	}
	// Built in another order, with two retries: the same generation.
	two := map[string]landing.Retry{"a": {AfterOp: "land:a:1", At: at}, "b": {AfterOp: "land:b:1", At: at}}
	reversed := map[string]landing.Retry{}
	reversed["b"], reversed["a"] = two["b"], two["a"]
	if drillGeneration(landing.State{Retries: two}) != drillGeneration(landing.State{Retries: reversed}) {
		t.Fatal("insertion order changed the generation")
	}
	// A wait set under one generation is due at once when only an outcome moved.
	var b drillBackoff
	b.due([]string{"t1"}, base, at)
	b.record("t1", false, at)
	moved := drillGeneration(landing.State{Operations: ops("branch_pushed", "window"), Retries: retry("land:a:1", "", at)})
	if got := b.due([]string{"t1"}, moved, at); len(got) != 1 {
		t.Fatal("an outcome change kept the wait", got)
	}
}
