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
