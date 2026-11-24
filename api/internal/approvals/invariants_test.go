package approvals

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// The approval service's invariants, written out (main coordinator and duty,
// 2026-11-15: with no second reader, the author proves a change safe with
// facts the machine checks). Each is a named test below. This file is the
// author's to change (its diff is a change's reach); what must be refused
// however the service changes is in mustrefuse/, which only the Owner changes.
//
//  (I1, I2: an agent cannot approve, reject or resolve; withdraw is the
//  requester's alone: these moved to the must-refuse set in mustrefuse/,
//  which the author cannot change; they are not repeated here.)
//  I3  The command an Owner approved is the command that runs: params that no
//      longer hash to params_hash are never started.
//  I4  Withdrawing and superseding only ever make cards fewer: neither starts
//      a card, a withdrawn or superseded card can no longer be claimed, and a
//      card an executor has claimed can be neither.
//  I5  A card cannot be superseded by a different requester.
//  I6  An owner_run_command card says what changes and who can do what new
//      (create_check tests); a tier X request is refused whatever it says.
//
// Origin: the three-questions review of the gates, 2026-11-15; #562, #564,
// #566 for I4/I5, #557 for I3's reason to exist.

// I3: the stored command is changed after approval; the executor is not given it.
func TestInvariantAnApprovedCommandThatChangedIsNeverStarted(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec := approveOwnerRun(t, svc)
	if err := svc.store.update(func(doc *file) error {
		found, _ := doc.find(rec.ID)
		found.Params["command"] = "kubectl -n cicd delete configmap something-else"
		doc.put(found)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "o", ID: rec.ID})
	if out.Status == http.StatusOK {
		t.Fatalf("a changed command was handed to an executor: %v", out.Body)
	}
	if got, _ := svc.get(rec.ID); got.Status == StatusRunning || (got.Execution != nil && got.Execution.LeaseID != "") {
		t.Fatalf("record = %s lease %v", got.Status, got.Execution)
	}
}

// I4: for every state a card can be in, a withdrawal never makes it more
// runnable, and only a pending or unclaimed-approved card changes.
func TestInvariantWithdrawNeverStartsAnything(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	pending := pendingCard(t, svc)
	approved := approveOwnerRun(t, svc)
	running, _ := claimed(t, svc)
	for name, id := range map[string]string{"pending": pending.ID, "approved": approved.ID, "running": running.ID} {
		out := svc.withdraw(id, "s", "x")
		got, _ := svc.get(id)
		switch name {
		case "running":
			if out.Status != http.StatusConflict || got.Status != StatusRunning {
				t.Fatalf("running card: %d status %s", out.Status, got.Status)
			}
		default:
			if out.Status != http.StatusOK || got.Status != StatusRejected {
				t.Fatalf("%s card: %d status %s", name, out.Status, got.Status)
			}
		}
		if got.Status != StatusRunning && got.Execution != nil && got.Execution.LeaseID != "" && name != "running" {
			t.Fatalf("%s card holds a lease after withdrawal", name)
		}
	}
	// And the second withdrawal of a withdrawn card does nothing more.
	if out := svc.withdraw(pending.ID, "s", "again"); out.Status != http.StatusConflict {
		t.Fatalf("withdrawing twice = %d", out.Status)
	}
}

// I4/I5: a new card of the same subject takes the place of its own requester's
// unrun card only; another requester's card, or a claimed one, still blocks.
func TestInvariantSupersedeOnlyReplacesTheRequestersOwnUnrunCard(t *testing.T) {
	newSvc := func() *Service {
		svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
		svc.SetOpenKey(func(action string, params map[string]any) string {
			if action == "ib_mode" {
				return "node:" + params["mode"].(string)
			}
			return ""
		})
		return svc
	}
	mk := func(svc *Service, who string) createResult {
		return svc.create(context.Background(), who, "ib_mode", "r", "", map[string]any{"mode": "n1"})
	}
	svc := newSvc()
	first := mk(svc, "s1")
	second := mk(svc, "s1")
	if second.Status != http.StatusCreated {
		t.Fatalf("replacement = %d %v", second.Status, second.Body)
	}
	old, _ := svc.get(first.Approval.ID)
	if old.Status != StatusRejected || !strings.HasPrefix(old.RejectReason, supersededPrefix) {
		t.Fatalf("old card = %s %q", old.Status, old.RejectReason)
	}
	if list, _, _ := svc.list("pending"); len(list) != 1 {
		t.Fatalf("pending after replacement = %d", len(list))
	}
	// Negative control: another requester's card is not replaced.
	svc = newSvc()
	mk(svc, "s1")
	if other := mk(svc, "s2"); other.Status != http.StatusConflict {
		t.Fatalf("another requester's replacement = %d %v", other.Status, other.Body)
	}
}
