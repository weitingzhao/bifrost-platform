package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

var epochForSettle = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// C2: settleS5 never ends an operation awaiting a grant (the protocol would
// refuse failed{S5_*} there and the slot would stay held); it reports the
// broken invariant instead of touching the store (no DB is wired here).
func TestS5SettleRefusesAwaitingGrant(t *testing.T) {
	c := &Controller{}
	o := landing.Operation{Phase: "awaiting_grant", S5: landing.S5State{Required: true}}
	for _, why := range []string{"S5_BEHAVIOR", "S5_CI_IDENTITY", "S5_PREPARATION_ONLY"} {
		a, err := c.settleS5(context.Background(), o, "land:t_01ARZ3NDEKTSV4RRFFQ69G5FAV:1", why, landing.S5CIResult{Status: "Failed"}, false)
		if err == nil || !strings.Contains(err.Error(), "awaiting a grant") || a.Kind != "wait" {
			t.Fatal(why, a, err)
		}
	}
	// The protocol side of the same invariant.
	s := landing.State{Operations: map[string]landing.Operation{"op": {Phase: "awaiting_grant", Task: "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"}}}
	if err := s.Check(landing.Entry{At: epochForSettle, Record: landing.Record{"v": landing.S5Version, "kind": "failed", "op": "op", "why": "S5_BEHAVIOR"}}); err == nil {
		t.Fatal("failed{S5_*} accepted from awaiting_grant")
	}
}

// C1: S5_PREPARATION_ONLY waits only while no terminal result exists; other
// wait-class codes keep their rule. These cases end before any store call.
func TestS5SettleWaitsOnlyWithoutTerminalResult(t *testing.T) {
	c := &Controller{}
	o := landing.Operation{Phase: "branch_pushed"}
	for _, ci := range []landing.S5CIResult{{}, {Status: "Missing"}, {Status: "Running"}} {
		for _, why := range []string{"S5_PREPARATION_ONLY", "S5_EVIDENCE_MISSING", "S5_BINDING", "S5_GENERATION_STALE", "S5_REVIEW_MISSING", "window"} {
			if a, err := c.settleS5(context.Background(), o, "op", why, ci, false); err != nil || a.Kind != "wait" {
				t.Fatal(why, ci.Status, a, err)
			}
		}
	}
}
