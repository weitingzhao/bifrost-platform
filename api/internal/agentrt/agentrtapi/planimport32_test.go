package agentrtapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// T32（经导入） (v5 §5.3, review 142 R4): T32's counter-examples a plan in
// WORK.md's format can carry, put through the import entry: each is refused
// with the constraint it breaks named, before any card, and leaves no
// proposal and no node. (What the format cannot carry, a task or a binding,
// is refused as a field the request does not have.) Importing the same
// request twice is TestPlanImportTwice.
func TestPlanImportCounterExamplesThroughTheRoute(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	nth := func(r *planimport.Request, kind string, n int) *planimport.Node {
		for i := range r.Nodes {
			if r.Nodes[i].Kind == kind {
				if n == 0 {
					return &r.Nodes[i]
				}
				n--
			}
		}
		t.Fatalf("the fixture has no %s %d", kind, n)
		return nil
	}
	counts := func() [5]int {
		var n [5]int
		for i, table := range []string{"artifact", "artifact_blob", "idempotency_key", "event", "plan_node"} {
			must(t, pool.QueryRow(ctx, "SELECT count(*) FROM agentrt."+table).Scan(&n[i]))
		}
		return n
	}
	for name, c := range map[string]struct {
		change func(*planimport.Request) any
		code   int
		want   string
	}{
		"a step under the round": {func(r *planimport.Request) any { nth(r, "step", 0).Parent = r.Root.ID; return r },
			http.StatusUnprocessableEntity, "a step sits under an item listed before it"},
		"an item under an item": {func(r *planimport.Request) any { nth(r, "item", 1).Parent = nth(r, "item", 0).ID; return r },
			http.StatusUnprocessableEntity, "an item sits under the root"},
		"a node of another kind": {func(r *planimport.Request) any { nth(r, "item", 1).Kind = "arch_row"; return r },
			http.StatusUnprocessableEntity, "an item or a step"},
		"a step renumbered": {func(r *planimport.Request) any { nth(r, "step", 1).Ordinal = 7; return r },
			http.StatusUnprocessableEntity, "steps are numbered"},
		"a step that says what its line does not": {func(r *planimport.Request) any { nth(r, "step", 0).Title = "另一个标题"; return r },
			http.StatusUnprocessableEntity, "does not say what its line says"},
		"a task on a step": {func(r *planimport.Request) any {
			b, err := json.Marshal(r)
			must(t, err)
			var doc map[string]any
			must(t, json.Unmarshal(b, &doc))
			step := doc["nodes"].([]any)[1].(map[string]any)
			step["tasks"] = []any{map[string]any{"title": "a task the import may not make"}}
			return doc
		}, http.StatusBadRequest, `unknown field "tasks"`},
	} {
		req := ledger(t, fixtureLedger)
		body := map[string]any{"idempotency_key": "k-" + name, "reason": "T32 through the import", "request": c.change(&req)}
		before, rows := len(cards), counts()
		code, out := call(t, s, "reporter", "POST", "plan/imports", body, "X-Bifrost-Session", "s1")
		msg, _ := out["message"].(string)
		if code != c.code || !strings.Contains(msg, c.want) {
			t.Errorf("%s: %d %v; want %d naming %q", name, code, out, c.code, c.want)
		}
		if len(cards) != before {
			t.Errorf("%s: %d cards asked", name, len(cards)-before)
		}
		// Nothing at all is left: no request stored, no key taken, no event,
		// no node (review 341 B4).
		if after := counts(); after != rows {
			t.Errorf("%s: rows %v before, %v after", name, rows, after)
		}
	}
	var proposals, nodes int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type IN ('plan_change_proposed', 'plan_node_created')
	  OR (event_type = 'legacy_imported' AND event_stream_id <> 'system')`).Scan(&proposals))
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id LIKE 'R2%'`).Scan(&nodes))
	if proposals != 0 || nodes != 0 {
		t.Fatalf("the refused imports left %d events and %d nodes", proposals, nodes)
	}
	if code, _ := call(t, s, "viewer", "GET", "plan?root=R2", nil); code != http.StatusNotFound {
		t.Fatalf("GET /plan after the refusals = %d", code)
	}
	// The counts see a proposal that is taken: the request is stored and its
	// key kept.
	rows := counts()
	if code, out := call(t, s, "reporter", "POST", "plan/imports", importBody("k-ok", ledger(t, fixtureLedger)), "X-Bifrost-Session", "s1"); code != http.StatusOK {
		t.Fatalf("the plan as it is: %d %v", code, out)
	}
	if after := counts(); after[0] == rows[0] || after[2] == rows[2] || after[3] == rows[3] {
		t.Fatalf("a taken proposal changed no count: %v, then %v", rows, after)
	}
}
