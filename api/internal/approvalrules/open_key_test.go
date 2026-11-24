package approvalrules

import (
	"testing"
)

// Origin: cards #575 and #576 (2026-11-15): one session asked the same
// command twice and both stayed pending, because an execution card had no
// "same subject" key, so the supersede rule never applied; #562/#564/#566 the
// same for releases.
func TestApprovalOpenKeyNamesTheSubjectOfAnExecutionOrReleaseCard(t *testing.T) {
	a := OpenKey("owner_run_command", map[string]any{"command": "kubectl get pods", "reason": "one"})
	b := OpenKey("owner_run_command", map[string]any{"command": "kubectl get pods", "reason": "two"})
	c := OpenKey("owner_run_command", map[string]any{"command": "kubectl get nodes"})
	if a == "" || a != b || a == c {
		t.Fatalf("the same command, another reason, and another command: %q %q %q", a, b, c)
	}
	p1 := OpenKey("start_pipeline_run", map[string]any{"name": "bifrost-deliver-platform-prod", "revision": "a"})
	p2 := OpenKey("start_pipeline_run", map[string]any{"name": "bifrost-deliver-platform-prod", "revision": "b"})
	p3 := OpenKey("start_pipeline_run", map[string]any{"name": "bifrost-deliver-stg"})
	if p1 == "" || p1 != p2 || p1 == p3 {
		t.Fatalf("one pipeline, two revisions, and another pipeline: %q %q %q", p1, p2, p3)
	}
	// A request that names no subject has no key, so it is never blocked or replaced.
	for _, act := range []string{"owner_run_command", "start_pipeline_run"} {
		if k := OpenKey(act, map[string]any{}); k != "" {
			t.Fatalf("%s with no subject has key %q", act, k)
		}
	}
	if k := OpenKey("ib_mode", map[string]any{"mode": "n1"}); k != "" {
		t.Fatalf("another action has key %q", k)
	}
}

// One open agentrt_rule_approve request per rule, whatever version it
// names; landing grants keep their own key.
func TestRuleApproveOpenKey(t *testing.T) {
	a := OpenKey(RuleApproveAction, map[string]any{"rule_id": "concurrency", "rule_version": 2})
	b := OpenKey(RuleApproveAction, map[string]any{"rule_id": "concurrency", "rule_version": 3})
	if a == "" || a != b {
		t.Fatalf("keys %q and %q", a, b)
	}
	if c := OpenKey(RuleApproveAction, map[string]any{"rule_id": "budget:dev"}); c == a {
		t.Fatal("two rules share a key")
	}
	if k := OpenKey("mark_missions_no_merge", map[string]any{"rule_id": "concurrency"}); k != "" {
		t.Fatalf("another action has key %q", k)
	}
}
