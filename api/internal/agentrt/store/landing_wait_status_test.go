package store

import (
	"maps"
	"strings"
	"testing"
	"time"
)

func TestC4WaitingStatusDoesNotInventOwnerWorkOrGrant(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("a", 40)
	for _, tc := range []struct {
		status, main, shown string
		todo                bool
	}{
		{"pending", "", "request_main_unknown", false},
		{"pending", head, "awaiting_owner", true},
		{"pending", strings.Repeat("b", 40), "request_stale", false},
		{"approved", head, "approval_observed", false},
		{"running", head, "approval_observed", false},
		{"unknown", head, "awaiting_request", false},
		{"expired", head, "awaiting_request", false},
	} {
		t.Run(tc.status+tc.main, func(t *testing.T) {
			t.Parallel()
			w := LandingWaitStatus{Phase: "awaiting_grant", M0: head, ApprovalStatus: tc.status, Shown: tc.shown}
			w.ObserveMain(tc.main)
			if w.Shown != tc.shown || w.OwnerTodo != tc.todo {
				t.Fatal(w)
			}
		})
	}
	w := LandingWaitStatus{Phase: "awaiting_grant", M0: head, ApprovalStatus: "pending"}
	w.ObserveMain(head)
	if !w.OwnerTodo {
		t.Fatal("control")
	}
	w.ObserveMain("")
	if w.OwnerTodo || w.MainKnown {
		t.Fatal("read error retained approval readiness")
	}
}

func TestC4RequestSchemaIsClosed(t *testing.T) {
	t.Parallel()
	r := LandingGrantRequest{Op: "land:1", ApprovalID: "approval_1", ApprovalNumber: 1, Status: "rejected", ParamsHash: strings.Repeat("a", 64), SeenAt: time.Now()}.body()
	if why := badLandingGrantRequest(r); why != "" {
		t.Fatal(why)
	}
	for name, mutate := range map[string]func(map[string]any){
		"extra":   func(m map[string]any) { m["grant_id"] = "fake" },
		"missing": func(m map[string]any) { delete(m, "op") },
		"status":  func(m map[string]any) { m["status"] = "revoked" },
		"time":    func(m map[string]any) { m["seen_at"] = "yesterday" },
		"number":  func(m map[string]any) { m["approval_number"] = 0 },
		"version": func(m map[string]any) { m["v"] = "2" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := maps.Clone(r)
			mutate(m)
			if badLandingGrantRequest(m) == "" {
				t.Fatal(m)
			}
		})
	}
}
