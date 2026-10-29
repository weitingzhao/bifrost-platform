package store

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
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
			at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			state, err := landing.Fold(reviewWaitEntries(t, at.Add(-time.Hour), at))
			if err != nil {
				t.Fatal(err)
			}
			request := LandingGrantRequest{Op: "land:t_00000000000000000000000001:1", ApprovalID: "approval_1", ApprovalNumber: 1,
				Status: tc.status, ParamsHash: strings.Repeat("a", 64), SeenAt: at}
			w, err := readLandingWaitStatus(context.Background(), waitReviewTx{requests: []map[string]any{request.body()}}, state, request.Op, at)
			if err != nil {
				t.Fatal(err)
			}
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
