package controller

import (
	"context"
	"testing"
	"time"
)

// R5 phase 2 (PROD 2026-11-06): with dozens of pending operations an even split
// left each visit a few seconds and every one timed out. A visit now gets at
// least drillMinShare, the visits stop at half the budget, and the start rotates.
func TestDrillVisitsShareFairly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if b := drillVisitBudget(ctx); b < 44*time.Second || b > 45*time.Second {
		t.Fatalf("visit budget = %v, want half of 90s", b)
	}
	one, done := drillVisitShare(ctx, 30)
	deadline, _ := one.Deadline()
	done()
	if got := time.Until(deadline); got < drillMinShare-time.Second {
		t.Fatalf("a visit among 30 got %v, want at least %v", got, drillMinShare)
	}
	two, done2 := drillVisitShare(ctx, 1)
	deadline2, _ := two.Deadline()
	done2()
	if got := time.Until(deadline2); got < 80*time.Second {
		t.Fatalf("the last visit got %v, want what is left", got)
	}

	short, cancelShort := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShort()
	if drillHasShare(short) {
		t.Fatal("10s left is less than one visit's share")
	}
	if !drillHasShare(context.Background()) {
		t.Fatal("no deadline always has a share")
	}

	seen := map[int]bool{}
	for range 5 {
		seen[drillTurn(5)] = true
	}
	if len(seen) != 5 {
		t.Fatalf("five passes over five operations started at %v, want every one", seen)
	}
	if drillTurn(0) != 0 {
		t.Fatal("no operations start at 0")
	}
}

// TD-528 (PROD 2026-11-11, #442): a batch lead's share is split over the visits
// still due less its own followers still due after it; nothing else changes.
func TestDrillDivisorGivesALeadItsFollowersShare(t *testing.T) {
	t.Parallel()
	set := func(ids ...string) map[string]bool {
		m := map[string]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	seven := []string{"a", "b", "c", "d", "e", "f", "g"}
	for _, tc := range []struct {
		name      string
		pending   []string
		offset, k int
		followers map[string]bool
		want      int
	}{
		{"lone batch of 7, lead first", seven, 0, 0, set("b", "c", "d", "e", "f", "g"), 1},
		{"lone batch of 7, lead last", seven, 0, 6, set("a", "b", "c", "d", "e", "f"), 1},
		{"lead in the middle: followers before it are visits done", seven, 0, 3, set("a", "b", "e", "f"), 2},
		{"batch of 3 among 4 lone operations", seven, 0, 0, set("c", "e"), 5},
		{"no batch: an even split as before", seven, 0, 2, nil, 5},
		{"rotation: the followers after the lead in this pass's order", seven, 5, 0, set("g", "a"), 5},
	} {
		if got := drillDivisor(tc.pending, tc.offset, tc.k, tc.followers); got != tc.want {
			t.Errorf("%s: divisor %d, want %d", tc.name, got, tc.want)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 195*time.Second)
	defer cancel()
	one, done := drillWorkShare(ctx, drillDivisor(seven, 0, 0, set("b", "c", "d", "e", "f", "g")))
	defer done()
	if deadline, _ := one.Deadline(); time.Until(deadline) < 190*time.Second {
		t.Fatalf("a lone batch's lead got %v of 195s", time.Until(deadline))
	}
}
