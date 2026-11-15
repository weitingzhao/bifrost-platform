package approvals

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Origin: cards #562, #564, #566 (2026-11-14). A card the requester built
// wrong, or that became moot, could only be ended by the Owner rejecting it;
// #562 was approved anyway and ran a second release of the same commit.
// Owner 2026-11-15: the requester takes its own card back.

func pendingCard(t *testing.T, svc *Service) Approval {
	t.Helper()
	c := svc.create(context.Background(), "s", "owner_run_command", "delete a ConfigMap", "", map[string]any{
		"command": "kubectl -n cicd delete configmap x", "reason": "TD-290 remove retired ConfigMap\nmore",
	})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	return c.Approval
}

// Positive control: the requester withdraws a pending card; it ends rejected,
// with the withdrawal as the reason.
func TestRequesterWithdrawsPendingCard(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	a := pendingCard(t, svc)
	out := svc.withdraw(a.ID, "s", "built with the wrong revision")
	if out.Status != http.StatusOK || out.Body["status"] != StatusRejected || out.Body["withdrawn"] != true {
		t.Fatalf("withdraw = %d %v", out.Status, out.Body)
	}
	got, _ := svc.get(a.ID)
	if got.Status != StatusRejected || !strings.HasPrefix(got.RejectReason, withdrawnPrefix) {
		t.Fatalf("record = %s %q", got.Status, got.RejectReason)
	}
	// The Owner can no longer approve it.
	if ap := svc.approveWith(context.Background(), a.ID, withLine(a, approveInput{Channel: "console"})); ap.Status == http.StatusAccepted {
		t.Fatalf("approved a withdrawn card: %v", ap.Body)
	}
}

// Negative control (the claim of "only the requester"): another session cannot.
func TestOnlyTheRequesterCanWithdraw(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	a := pendingCard(t, svc)
	out := svc.withdraw(a.ID, "someone-else", "no")
	if out.Status != http.StatusForbidden {
		t.Fatalf("other requester = %d %v", out.Status, out.Body)
	}
	if got, _ := svc.get(a.ID); got.Status != StatusPending {
		t.Fatalf("status after refused withdrawal = %s", got.Status)
	}
}

// An approved card nobody has claimed yet (the #562 case) can be withdrawn,
// and then no executor can claim it.
func TestApprovedUnclaimedCardCanBeWithdrawn(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec := approveOwnerRun(t, svc)
	out := svc.withdraw(rec.ID, "s", "moot")
	if out.Status != http.StatusOK {
		t.Fatalf("withdraw approved = %d %v", out.Status, out.Body)
	}
	if cl := svc.claim(context.Background(), "admin", claimInput{ExecutorID: "o", ID: rec.ID}); cl.Status == http.StatusOK {
		t.Fatalf("claimed a withdrawn card: %v", cl.Body)
	}
}

// A claimed card is too late: it may already be running.
func TestClaimedCardCannotBeWithdrawn(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, _ := claimed(t, svc)
	out := svc.withdraw(rec.ID, "s", "too late")
	if out.Status != http.StatusConflict {
		t.Fatalf("withdraw claimed = %d %v", out.Status, out.Body)
	}
}

// Missing pieces fail closed.
func TestWithdrawNeedsReasonAndKnownCard(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	a := pendingCard(t, svc)
	if out := svc.withdraw(a.ID, "s", "  "); out.Status != http.StatusBadRequest {
		t.Fatalf("empty reason = %d", out.Status)
	}
	if out := svc.withdraw("appr_nope", "s", "x"); out.Status != http.StatusNotFound {
		t.Fatalf("unknown card = %d", out.Status)
	}
	if out := svc.withdraw(a.ID, "", "x"); out.Status != http.StatusBadRequest {
		t.Fatalf("no requester = %d", out.Status)
	}
}
