package approvals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// The approval service's invariants, written out (main coordinator and duty,
// 2026-11-15: with no second reader, the author proves a change safe with
// facts the machine checks). Each is a named test below. Changing this list
// is the one change left to the Owner.
//
//  I1  An agent (operator token) can neither approve nor reject nor resolve a card.
//  I2  A card can be withdrawn only by the requester that built it.
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
// #566 for I2/I4/I5, #557 for I3's reason to exist.

func invariantRouter(t *testing.T) (http.Handler, *Service) {
	t.Helper()
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(authPath, []byte(`
tokens:
  - {name: operator, role: operator, token: operator-test-token}
  - {name: admin, role: admin, token: admin-test-token}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(filepath.Join(dir, "approvals"), nil)
	r := chi.NewRouter()
	Mount(r, auth, svc)
	return r, svc
}

func call(h http.Handler, method, path, body, token, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if session != "" {
		req.Header.Set(SessionHeader, session)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// I1.
func TestInvariantAnAgentCannotApproveRejectOrResolve(t *testing.T) {
	h, svc := invariantRouter(t)
	a := pendingCard(t, svc)
	for _, p := range []string{"approve", "reject", "resolve"} {
		rec := call(h, http.MethodPost, "/approvals/"+a.ID+"/"+p, `{"reason":"r","channel":"chat"}`, "operator-test-token", "agent-session")
		if rec.Code < 400 {
			t.Fatalf("operator %s = %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if got, _ := svc.get(a.ID); got.Status != StatusPending {
		t.Fatalf("status after the agent's attempts = %s", got.Status)
	}
	// Positive control: the same route is open to the admin token.
	if rec := call(h, http.MethodPost, "/approvals/"+a.ID+"/reject", `{"reason":"r"}`, "admin-test-token", ""); rec.Code != http.StatusOK {
		t.Fatalf("admin reject = %d %s", rec.Code, rec.Body.String())
	}
}

// I2, over HTTP: the requester is the session header, not a body field.
func TestInvariantWithdrawIsTheRequestersAloneOverHTTP(t *testing.T) {
	h, svc := invariantRouter(t)
	c := svc.create(context.Background(), "s1", "owner_run_command", "x\n改了什么：a\n批了以后谁因此能做什么新事：b", "", map[string]any{"command": "true", "reason": "r"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	if rec := call(h, http.MethodPost, "/approvals/"+c.Approval.ID+"/withdraw", `{"reason":"mine","requester":"s1"}`, "operator-test-token", "s2"); rec.Code != http.StatusForbidden {
		t.Fatalf("another session = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h, http.MethodPost, "/approvals/"+c.Approval.ID+"/withdraw", `{"reason":"mine"}`, "operator-test-token", "s1"); rec.Code != http.StatusOK {
		t.Fatalf("the requester = %d %s", rec.Code, rec.Body.String())
	}
}

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
			if action == "cordon_node" {
				return "node:" + params["name"].(string)
			}
			return ""
		})
		return svc
	}
	mk := func(svc *Service, who string) createResult {
		return svc.create(context.Background(), who, "cordon_node", "r", "", map[string]any{"name": "n1"})
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
