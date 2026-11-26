package approvals

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The decision snapshot (CI line's way, 2026-11-15): a fixed batch of sample
// requests run through the real service, the whole outcome stored in
// testdata/decision_snapshot.txt. Whoever changes how the approval service
// decides changes the snapshot in the same commit, and the diff of that file
// is the plain list of what now comes out differently: which requests build a
// card, which are refused and why, what can be withdrawn or replaced. Origin:
// LAW-6 (2026-11-15): what reaches the Owner is a change to a
// rule, shown as what it changes. Update with UPDATE_SNAPSHOT=1.

const snapshotPath = "testdata/decision_snapshot.txt"

const goodReason = "x\n改了什么：一个对象\n批了以后谁因此能做什么新事：没有新能力"

func verdict(res createResult) string {
	if res.Status == http.StatusCreated {
		return "201 card pending"
	}
	msg, _ := res.Body["error"].(string)
	if len(msg) > 48 {
		msg = msg[:48]
	}
	return fmt.Sprintf("%d %s", res.Status, msg)
}

func snapshotLines(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	fresh := func() *Service { return New(filepath.Join(t.TempDir(), "approvals"), nil) }
	owner := func(svc *Service, who, command, reason string) createResult {
		return svc.create(ctx, who, "owner_run_command", reason, "", map[string]any{"command": command, "reason": "r"})
	}
	var out []string
	add := func(name, v string) { out = append(out, name+" => "+v) }

	svc := fresh()
	add("create: execution card with both lines", verdict(owner(svc, "s", "true", goodReason)))
	add("create: execution card with no lines", verdict(owner(svc, "s", "true", "plain")))
	add("create: execution card, only the first line", verdict(owner(svc, "s", "true", "x\n改了什么：a")))
	add("create: command uses --raw", verdict(owner(svc, "s", "kubectl get --raw /x", goodReason)))
	add("create: dry-run=client", verdict(owner(svc, "s", "kubectl apply -f x --dry-run=client", goodReason)))
	add("create: dry-run=server", verdict(owner(svc, "s", "kubectl apply -f x --dry-run=server", goodReason)))
	add("create: own kubeconfig", verdict(owner(svc, "s", "kubectl --kubeconfig k get pods", goodReason)))
	add("create: DROP not named in the reason", verdict(owner(svc, "s", "psql -c 'DROP TABLE t'", goodReason)))
	add("create: DROP named in the reason", verdict(owner(svc, "s", "psql -c 'DROP TABLE t'", goodReason+"\nDROP TABLE t: 16 empty tables")))
	add("create: drill with no lines", verdict(svc.createWith(ctx, createInput{Requester: "s", Action: "owner_run_command", Reason: "d",
		Params: map[string]any{"command": "true", "reason": "r"}, Drill: true})))
	add("create: unknown action", verdict(svc.create(ctx, "s", "no_such_action", "r", "", map[string]any{})))
	add("create: no reason", verdict(svc.create(ctx, "s", "ib_mode", "", "", map[string]any{"mode": "n"})))

	// Withdraw.
	svc = fresh()
	a := owner(svc, "s", "true", goodReason).Approval
	add("withdraw: another session", fmt.Sprint(svc.withdraw(a.ID, "other", "x").Status))
	add("withdraw: no reason", fmt.Sprint(svc.withdraw(a.ID, "s", " ").Status))
	add("withdraw: the requester, pending", fmt.Sprint(svc.withdraw(a.ID, "s", "x").Status))
	add("withdraw: again", fmt.Sprint(svc.withdraw(a.ID, "s", "x").Status))
	svc = fresh()
	ap := approveOwnerRun(t, svc)
	add("withdraw: approved, unclaimed", fmt.Sprint(svc.withdraw(ap.ID, "s", "x").Status))
	svc = fresh()
	run, _ := claimed(t, svc)
	add("withdraw: claimed and running", fmt.Sprint(svc.withdraw(run.ID, "s", "x").Status))

	// Supersede, on a subject with a one-open-card rule.
	key := func(action string, params map[string]any) string {
		if action == "ib_mode" {
			return "node:" + fmt.Sprint(params["mode"])
		}
		return ""
	}
	node := func(svc *Service, who string) createResult {
		return svc.create(ctx, who, "ib_mode", "r", "", map[string]any{"mode": "n1"})
	}
	svc = fresh()
	svc.SetOpenKey(key)
	node(svc, "s1")
	add("supersede: same requester replaces its pending card", verdict(node(svc, "s1")))
	svc = fresh()
	svc.SetOpenKey(key)
	node(svc, "s1")
	add("supersede: another requester is refused", verdict(node(svc, "s2")))

	// Roles over the approve/reject routes: see TestInvariantAnAgentCannotApproveRejectOrResolve.
	return out
}

func TestDecisionSnapshot(t *testing.T) {
	got := strings.Join(snapshotLines(t), "\n") + "\n"
	if os.Getenv("UPDATE_SNAPSHOT") == "1" {
		if err := os.WriteFile(snapshotPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("no snapshot (run with UPDATE_SNAPSHOT=1 and commit it): %v", err)
	}
	if string(want) != got {
		t.Fatalf("the approval service decides differently from the committed snapshot; if intended, rerun with UPDATE_SNAPSHOT=1 and commit the file.\n--- want\n%s--- got\n%s", want, got)
	}
}
