//bifrost:owner owner

package server

import (
	"net/http"
	"testing"
)

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
