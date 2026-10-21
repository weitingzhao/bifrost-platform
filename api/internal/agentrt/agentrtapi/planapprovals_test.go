package agentrtapi_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of steps done by requests (console/src/api/__fixtures__/agentrt-plan-approvals.testdb.json):
// GET /plan?root=R2 after done rules naming approvals were set through POST /plan/proposals, written when
// CONSOLE_PLAN_APPROVALS_FIXTURE_WRITE=1 (only when every assertion held); the Console's tests read it.
const consolePlanApprovalsFixture = "../../../../console/src/api/__fixtures__/agentrt-plan-approvals.testdb.json"

// A request says which step of the plan it is the approval for (W-31 2.5
// slice 5 gap 4, v4 §20.5 scenario 10): done rules naming approvals, an atom,
// nested in all beside a question, and two in any, set by a plan change the
// Owner applied, come back on GET /plan?root=R2 as they were proposed, on the
// steps they were set on; approval 16 waits on two steps.
func TestPlanStepsNameTheirApprovals(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	if code, out := call(t, s, "reporter", "POST", "plan/imports", importBody("r2", ledger(t, fixtureLedger)), session, "s1"); code != 200 || out["question_id"] == nil {
		t.Fatalf("import = %d %v", code, out)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || n != 1 {
		t.Fatalf("decide the import: %d %v", n, err)
	}
	bind := func(refs ...[2]string) []map[string]string {
		out := make([]map[string]string, 0, len(refs))
		for _, r := range refs {
			out = append(out, map[string]string{"kind": r[0], "ref": r[1]})
		}
		return out
	}
	rules := map[string]string{
		"R2:2.1-3": `{"approval_executed":16}`,
		"R2:2.4-1": `{"all":[{"approval_executed":17},{"question_answered":{"question":"Q1","option":"a"}}]}`,
		"R2:2.4-2": `{"any":[{"approval_executed":16},{"approval_executed":18}]}`,
	}
	changes := []map[string]any{
		{"op": "set_done_rule", "node": "R2:2.1-3", "rule": json.RawMessage(rules["R2:2.1-3"]), "bind": bind([2]string{"approval", "16"})},
		{"op": "set_done_rule", "node": "R2:2.4-1", "rule": json.RawMessage(rules["R2:2.4-1"]),
			"bind": bind([2]string{"approval", "17"}, [2]string{"question", "Q1"})},
		{"op": "set_done_rule", "node": "R2:2.4-2", "rule": json.RawMessage(rules["R2:2.4-2"]),
			"bind": bind([2]string{"approval", "16"}, [2]string{"approval", "18"})},
	}
	code, out := call(t, s, "reporter", "POST", "plan/proposals",
		map[string]any{"idempotency_key": "requests", "root": "R2", "reason": "steps done by their requests", "changes": changes}, session, "s1")
	if code != 200 || out["question_id"] == nil {
		t.Fatalf("propose the rules = %d %v", code, out)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || n != 1 {
		t.Fatalf("decide the rules: %d %v", n, err)
	}

	raw := get(t, s, "plan?root=R2")
	var doc struct {
		Root  string `json:"root"`
		Nodes []struct {
			ID   string          `json:"plan_node_id"`
			Kind string          `json:"node_kind"`
			Rule json.RawMessage `json:"done_rule_json"`
		} `json:"nodes"`
	}
	mustJSON(t, raw, &doc)
	compact := func(s string) string {
		var v any
		must(t, json.Unmarshal([]byte(s), &v))
		b, err := json.Marshal(v)
		must(t, err)
		return string(b)
	}
	seen := 0
	for _, n := range doc.Nodes {
		want, ok := rules[n.ID]
		if !ok {
			continue
		}
		seen++
		if n.Kind != "step" || compact(string(n.Rule)) != compact(want) {
			t.Errorf("%s: %s %s; want a step with %s", n.ID, n.Kind, n.Rule, want)
		}
	}
	if doc.Root != "R2" || seen != len(rules) {
		t.Fatalf("GET /plan?root=R2 is of %q and lists %d of the %d steps", doc.Root, seen, len(rules))
	}
	if os.Getenv("CONSOLE_PLAN_APPROVALS_FIXTURE_WRITE") == "1" && !t.Failed() {
		b, err := json.MarshalIndent(map[string]json.RawMessage{"plan": raw}, "", "  ")
		must(t, err)
		must(t, os.WriteFile(consolePlanApprovalsFixture, append(b, '\n'), 0o644))
	}
}
