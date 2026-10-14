package agentrtapi_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func mayList(out map[string]any) []string {
	var l []string
	for _, v := range out["may"].([]any) {
		l = append(l, v.(string))
	}
	return l
}

// whoami: who the caller is, who its writes are recorded as, what it may do.
func TestWhoami(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	for _, c := range []struct {
		who, session, actor, kind, problem string
		may                                []string
	}{
		{"operator", "s1", "thread:s1", "thread (registered on its first write)", "", []string{"read", "propose", "upload", "stop", "drain"}},
		{"operator", "", "", "", "X-Bifrost-Session", []string{"read", "propose", "upload", "stop", "drain"}},
		{"agentd:h1", "", "host:h1", "host", "", []string{"read", "claim", "upload", "stop"}},
		{"admin", "", "owner", "owner", "", []string{"read", "propose", "upload", "stop", "drain", "lift"}},
		{"seat:chief_of_staff", "", "seat:chief_of_staff", "seat", "", []string{"read", "propose", "upload", "stop"}},
		{"seat:ghost", "", "seat:ghost", "", "not registered", []string{"read", "propose", "upload", "stop"}},
		{"viewer", "s9", "thread:s9", "thread (registered on its first write)", "", []string{"read", "stop"}},
	} {
		var header []string
		if c.session != "" {
			header = []string{session, c.session}
		}
		code, out := call(t, s, c.who, "GET", "whoami", nil, header...)
		problem, _ := out["actor_error"].(string)
		if code != http.StatusOK || out["actor"] != nilIfEmpty(c.actor) || out["actor_kind"] != nilIfEmpty(c.kind) ||
			(c.problem == "") != (problem == "") || !strings.Contains(problem, c.problem) || !slices.Equal(mayList(out), c.may) {
			t.Errorf("%s/%s: %d %v", c.who, c.session, code, out)
		}
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Controls: anyone sets, the Owner lifts; a pause stops claims and its lift
// lets them go on; a drain is a control on the host; bad controls write
// nothing.
func TestControlsAndHosts(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	claimBody := map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}}}

	code, hosts := call(t, s, "viewer", "GET", "hosts", nil)
	if h1 := hostOf(t, hosts, "h1"); code != http.StatusOK || h1["online"] != false || h1["poll_age_s"] != nil {
		t.Fatalf("a host that never polled: %d %v", code, hosts)
	}
	pause := map[string]any{"idempotency_key": "p1", "scope": "global", "kind": "pause", "reason": "test"}
	code, set := call(t, s, "viewer", "POST", "controls", pause, session, "s1")
	if code != http.StatusOK || set["kind"] != "pause" {
		t.Fatalf("a viewer pauses: %d %v", code, set)
	}
	if got, again := call(t, s, "viewer", "POST", "controls", pause, session, "s1"); got != http.StatusOK || again["control_id"] != set["control_id"] {
		t.Fatalf("a retry: %d %v", got, again)
	}
	var by string
	must(t, pool.QueryRow(ctx, `SELECT set_by_actor_id FROM agentrt.control WHERE control_id = $1`, set["control_id"]).Scan(&by))
	if by != "thread:s1" {
		t.Fatalf("set by %s", by)
	}
	if got, _ := call(t, s, "agentd:h1", "POST", "claim", claimBody); got != http.StatusNoContent {
		t.Fatalf("a claim under a global pause: %d", got)
	}
	id := set["control_id"].(string)
	for _, who := range []string{"operator", "viewer", "agentd:h1"} {
		if got, _ := call(t, s, who, "DELETE", "controls/"+id, nil, session, "s1"); got != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("%s lifted a control: %d", who, got)
		}
	}
	code, listed := call(t, s, "viewer", "GET", "controls", nil)
	if code != http.StatusOK || len(listed["controls"].([]any)) != 1 {
		t.Fatalf("controls in force: %d %v", code, listed)
	}
	if got, out := call(t, s, "admin", "DELETE", "controls/"+id, nil); got != http.StatusOK || out["cleared"] != true || out["cleared_by"] != "owner" {
		t.Fatalf("the Owner lifts: %d %v", got, out)
	}
	if got, out := call(t, s, "admin", "DELETE", "controls/"+id, nil); got != http.StatusOK || out["cleared"] != false {
		t.Fatalf("lifted twice: %d %v", got, out)
	}
	if got, _ := call(t, s, "admin", "DELETE", "controls/c_nope", nil); got != http.StatusNotFound {
		t.Fatalf("a control there is not: %d", got)
	}
	if got, out := call(t, s, "viewer", "GET", "controls?all=1", nil); got != http.StatusOK || len(out["controls"].([]any)) != 1 {
		t.Fatalf("every control: %d %v", got, out)
	} else if got, out := call(t, s, "viewer", "GET", "controls", nil); got != http.StatusOK || len(out["controls"].([]any)) != 0 {
		t.Fatalf("controls in force after the lift: %d %v", got, out)
	}

	// A drain: only operators and the Owner; the host takes no new work.
	drain := map[string]any{"idempotency_key": "d1", "reason": "maintenance"}
	if got, _ := call(t, s, "viewer", "POST", "hosts/h1/drain", drain, session, "s1"); got != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Fatalf("a viewer drained: %d", got)
	}
	if got, out := call(t, s, "operator", "POST", "hosts/nope/drain", drain, session, "s1"); got != http.StatusUnprocessableEntity {
		t.Fatalf("a host there is not: %d %v", got, out)
	}
	code, drained := call(t, s, "operator", "POST", "hosts/h1/drain", drain, session, "s1")
	if code != http.StatusOK || drained["kind"] != "drain" || drained["scope_ref"] != "h1" {
		t.Fatalf("drain: %d %v", code, drained)
	}
	if got, _ := call(t, s, "agentd:h1", "POST", "claim", claimBody); got != http.StatusNoContent {
		t.Fatalf("a drained host claimed: %d", got)
	}
	_, hosts = call(t, s, "viewer", "GET", "hosts", nil)
	h1 := hostOf(t, hosts, "h1")
	if h1["online"] != true || len(h1["controls"].([]any)) != 1 {
		t.Fatalf("h1 after a poll and a drain: %v", h1)
	}
	if got, _ := call(t, s, "admin", "DELETE", "controls/"+drained["control_id"].(string), nil); got != http.StatusOK {
		t.Fatalf("lift the drain: %d", got)
	}
	if got, claim := call(t, s, "agentd:h1", "POST", "claim", claimBody); got != http.StatusOK || claim["task_id"] != task {
		t.Fatalf("a claim after the lift: %d %v", got, claim)
	}
	_, hosts = call(t, s, "viewer", "GET", "hosts", nil)
	if h1 = hostOf(t, hosts, "h1"); h1["live_attempts"] != float64(1) {
		t.Fatalf("h1 with its attempt: %v", h1)
	}

	// An agentd stops its own work, recorded as its host.
	code, own := call(t, s, "agentd:h1", "POST", "controls", map[string]any{"idempotency_key": "s1", "scope": "task", "scope_ref": task,
		"kind": "stop", "reason": "the executor misbehaves"})
	if code != http.StatusOK {
		t.Fatalf("agentd stops: %d %v", code, own)
	}
	must(t, pool.QueryRow(ctx, `SELECT set_by_actor_id FROM agentrt.control WHERE control_id = $1`, own["control_id"]).Scan(&by))
	if by != "host:h1" {
		t.Fatalf("set by %s", by)
	}

	var before int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.control`).Scan(&before))
	for name, body := range map[string]map[string]any{
		"no key":         {"scope": "global", "kind": "stop", "reason": "x"},
		"no reason":      {"idempotency_key": "b1", "scope": "global", "kind": "stop"},
		"bad scope":      {"idempotency_key": "b2", "scope": "planet", "kind": "stop", "reason": "x"},
		"drain a task":   {"idempotency_key": "b3", "scope": "task", "scope_ref": task, "kind": "drain", "reason": "x"},
		"global ref":     {"idempotency_key": "b4", "scope": "global", "scope_ref": "x", "kind": "stop", "reason": "x"},
		"no ref":         {"idempotency_key": "b5", "scope": "mission", "kind": "stop", "reason": "x"},
		"missing task":   {"idempotency_key": "b6", "scope": "task", "scope_ref": "t_nope", "kind": "stop", "reason": "x"},
		"missing host":   {"idempotency_key": "b7", "scope": "host", "scope_ref": "h9", "kind": "pause", "reason": "x"},
		"missing schema": {"idempotency_key": "b8", "scope": "mission", "scope_ref": "M-9", "kind": "pause", "reason": "x"},
	} {
		if got, out := call(t, s, "operator", "POST", "controls", body, session, "s1"); got != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %v", name, got, out)
		}
	}
	var after int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.control`).Scan(&after))
	if after != before {
		t.Fatalf("refused controls wrote %d", after-before)
	}
}

func hostOf(t *testing.T, hosts map[string]any, id string) map[string]any {
	t.Helper()
	for _, h := range hosts["hosts"].([]any) {
		if m := h.(map[string]any); m["host_id"] == id {
			return m
		}
	}
	t.Fatalf("no host %s in %v", id, hosts)
	return nil
}

// The plan: the roots, a subtree depth first with each step's state, the
// event it came from and its tasks, and one node in full.
func TestPlanReads(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	code, roots := call(t, s, "viewer", "GET", "plan", nil)
	if code != http.StatusOK || len(roots["roots"].([]any)) != 1 || roots["roots"].([]any)[0].(map[string]any)["plan_node_id"] != "R2" {
		t.Fatalf("roots: %d %v", code, roots)
	}
	code, tree := call(t, s, "viewer", "GET", "plan?root=R2", nil)
	if code != http.StatusOK {
		t.Fatalf("tree: %d %v", code, tree)
	}
	var order []string
	for _, n := range tree["nodes"].([]any) {
		m := n.(map[string]any)
		order = append(order, fmt.Sprint(m["plan_node_id"], "@", m["depth"]))
		if m["plan_node_id"] == "R2:1-1" {
			ev, _ := m["state_event"].(map[string]any)
			counts, _ := m["task_counts"].(map[string]any)
			if m["state"] != "planned" || ev["event_type"] != "plan_node_created" || counts["ready"] != float64(1) {
				t.Errorf("the step: %v", m)
			}
		}
	}
	if fmt.Sprint(order) != "[R2@0 R2:1@1 R2:1-1@2]" {
		t.Fatalf("order %v", order)
	}
	if got, _ := call(t, s, "viewer", "GET", "plan?root=R9", nil); got != http.StatusNotFound {
		t.Fatalf("a root there is not: %d", got)
	}
	code, node := call(t, s, "viewer", "GET", "plan/R2:1-1", nil)
	if code != http.StatusOK {
		t.Fatalf("node: %d %v", code, node)
	}
	tasks := node["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["task_id"] != task || len(node["events"].([]any)) == 0 {
		t.Fatalf("node: %v", node)
	}
	code, item := call(t, s, "viewer", "GET", "plan/R2:1", nil)
	if children := item["children"].([]any); code != http.StatusOK || len(children) != 1 || children[0].(map[string]any)["plan_node_id"] != "R2:1-1" {
		t.Fatalf("item: %d %v", code, item)
	}
	if got, _ := call(t, s, "viewer", "GET", "plan/R9", nil); got != http.StatusNotFound {
		t.Fatalf("a node there is not: %d", got)
	}
}
