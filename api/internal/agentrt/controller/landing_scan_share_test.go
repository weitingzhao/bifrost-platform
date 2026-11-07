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
