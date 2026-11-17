//bifrost:owner owner

package server

import (
	"net/http"
	"testing"
)

// Origin: cards #575 and #576 (2026-11-15): one session asked the same
// command twice and both stayed pending, because an execution card had no
// "same subject" key, so the supersede rule never applied; #562/#564/#566 the
// same for releases.
func TestApprovalOpenKeyNamesTheSubjectOfAnExecutionOrReleaseCard(t *testing.T) {
	a := approvalOpenKey("owner_run_command", map[string]any{"command": "kubectl get pods", "reason": "one"})
	b := approvalOpenKey("owner_run_command", map[string]any{"command": "kubectl get pods", "reason": "two"})
	c := approvalOpenKey("owner_run_command", map[string]any{"command": "kubectl get nodes"})
	if a == "" || a != b || a == c {
		t.Fatalf("the same command, another reason, and another command: %q %q %q", a, b, c)
	}
	p1 := approvalOpenKey("start_pipeline_run", map[string]any{"name": "bifrost-deliver-platform-prod", "revision": "a"})
	p2 := approvalOpenKey("start_pipeline_run", map[string]any{"name": "bifrost-deliver-platform-prod", "revision": "b"})
	p3 := approvalOpenKey("start_pipeline_run", map[string]any{"name": "bifrost-deliver-stg"})
	if p1 == "" || p1 != p2 || p1 == p3 {
		t.Fatalf("one pipeline, two revisions, and another pipeline: %q %q %q", p1, p2, p3)
	}
	// A request that names no subject has no key, so it is never blocked or replaced.
	for _, act := range []string{"owner_run_command", "start_pipeline_run"} {
		if k := approvalOpenKey(act, map[string]any{}); k != "" {
			t.Fatalf("%s with no subject has key %q", act, k)
		}
	}
	if k := approvalOpenKey("cordon_node", map[string]any{"name": "n1"}); k != "" {
		t.Fatalf("another action has key %q", k)
	}
}

// The real path: one session asks the same command twice; the second card
// replaces the first (it ends rejected as superseded). Another requester's
// refusal is in approvals' own tests (a replacement is the requester's own).
func TestTheSameCommandAskedTwiceLeavesOneOpenCard(t *testing.T) {
	f := newOwnerExecFlow(t)
	card := func(why string) (int, map[string]any) {
		return f.call(http.MethodPost, "/api/v1/approvals", "operator", map[string]any{"action": "owner_run_command",
			"reason": why + "\n改了什么：测试\n批了以后谁因此能做什么新事：无",
			"params": map[string]any{"command": "echo same-command", "reason": "r"}})
	}
	status, first := card("one")
	if status != http.StatusCreated {
		t.Fatalf("first = %d %v", status, first)
	}
	if status, second := card("two"); status != http.StatusCreated {
		t.Fatalf("the replacing card = %d %v", status, second)
	}
	old := f.read(first["id"].(string))
	if old["status"] != "rejected" {
		t.Fatalf("the replaced card is %v", old["status"])
	}
}
