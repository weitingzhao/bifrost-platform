package agentrtapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of steps the Owner signs off (console/src/api/__fixtures__/agentrt-plan-signoffs.testdb.json):
// GET /plan?root=R2 once 2.4-1 is signed and before the controller's pass, with the answer to that sign-off, written
// when CONSOLE_PLAN_SIGNOFFS_FIXTURE_WRITE=1 (only when every assertion held); the Console's tests read it.
const consolePlanSignoffsFixture = "../../../../console/src/api/__fixtures__/agentrt-plan-signoffs.testdb.json"

// The Owner signs a step off (the Owner 2026-10-21 「加一个只给我用的「签收这
// 一步」路由和 Console 按钮」): POST /plan/{id}/signoff, the admin role only,
// records the owner_signoff its done rule waits on; the controller's next
// rules pass moves the step; GET /plan says who signed and when. The steps are
// round 2's, their rules set by a plan change the Owner applied.

type signedNode struct {
	ID       string `json:"plan_node_id"`
	State    string `json:"state"`
	Signoffs []struct {
		Ref     string  `json:"ref"`
		By      string  `json:"by"`
		At      string  `json:"at"`
		EventID float64 `json:"event_id"`
	} `json:"signoffs"`
}

func TestOwnerSignsAStepOff(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	if code, out := call(t, s, "proposer", "POST", "plan/imports", importBody("r2", ledger(t, fixtureLedger)), session, "s1"); code != 200 || out["question_id"] == nil {
		t.Fatalf("import = %d %v", code, out)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || n != 1 {
		t.Fatalf("decide the import: %d %v", n, err)
	}
	rule := func(node, doc string, bind ...map[string]string) map[string]any {
		return map[string]any{"op": "set_done_rule", "node": node, "rule": json.RawMessage(doc), "bind": bind}
	}
	changes := []map[string]any{
		rule("R2:2.4-1", `{"owner_signoff":"exam-2.4-1"}`),
		rule("R2:2.1-3", `{"any":[{"owner_signoff":"a"},{"owner_signoff":"b"}]}`),
		rule("R2:2.4-2", `{"approval_executed":16}`, map[string]string{"kind": "approval", "ref": "16"}),
		rule("R2:2.1-4", `{"owner_signoff":"d"}`),
		rule("R2:2.3-1", `{"owner_signoff":"late"}`),
		rule("R2:2.3-2", `{"owner_signoff":"race"}`),
		{"op": "drop_step", "node": "R2:2.1-4"},
	}
	code, out := call(t, s, "proposer", "POST", "plan/proposals",
		map[string]any{"idempotency_key": "signoffs", "root": "R2", "reason": "steps the Owner signs off", "changes": changes}, session, "s1")
	if code != 200 || out["question_id"] == nil {
		t.Fatalf("propose the rules = %d %v", code, out)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || n != 1 {
		t.Fatalf("decide the rules: %d %v", n, err)
	}
	nodes := func() map[string]signedNode {
		t.Helper()
		var doc struct {
			Nodes []signedNode `json:"nodes"`
		}
		mustJSON(t, get(t, s, "plan?root=R2"), &doc)
		byID := map[string]signedNode{}
		for _, n := range doc.Nodes {
			byID[n.ID] = n
		}
		return byID
	}
	signoffEvents := func(step string) int {
		t.Helper()
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'owner_signoff' AND plan_node_id = $1`, step).Scan(&n))
		return n
	}
	sign := func(who, step string, body any) (int, map[string]any) {
		t.Helper()
		return call(t, s, who, "POST", "plan/"+step+"/signoff", body)
	}

	// Only the Owner: every token an agent holds is refused, with or without a
	// session, and nothing is written.
	for _, who := range []string{"", "viewer", "reporter", "proposer", "operator", "seat:chief_of_staff", "agentd:h1"} {
		// Refused by the route's role (the platform's RequireAny: 401 for no token and for another role alike),
		// before the store's own check of the writer (403 not_owner).
		code, out := call(t, s, who, "POST", "plan/R2:2.4-1/signoff", map[string]any{}, session, "s1")
		if code != http.StatusUnauthorized || out["error"] != "admin token required" {
			t.Fatalf("%q signed a step off: %d %v", who, code, out)
		}
	}
	if n := signoffEvents("R2:2.4-1"); n != 0 || nodes()["R2:2.4-1"].Signoffs != nil {
		t.Fatalf("a refused sign-off wrote %d events", n)
	}

	// The Owner signs; the rule's one ref is the one signed.
	code, first := sign("admin", "R2:2.4-1", map[string]any{})
	if code != http.StatusOK || first["by"] != "owner" || first["ref"] != "exam-2.4-1" || first["already_signed"] != false ||
		first["plan_node_id"] != "R2:2.4-1" || first["event_id"] == nil || first["at"] == nil {
		t.Fatalf("the Owner's sign-off = %d %v", code, first)
	}
	// A second press, naming the ref or not, answers the first and writes nothing.
	for _, body := range []any{map[string]any{}, map[string]any{"ref": "exam-2.4-1"}} {
		code, again := sign("admin", "R2:2.4-1", body)
		if code != http.StatusOK || again["already_signed"] != true || again["event_id"] != first["event_id"] || again["at"] != first["at"] ||
			again["by"] != "owner" {
			t.Fatalf("a second sign-off = %d %v", code, again)
		}
	}
	if n := signoffEvents("R2:2.4-1"); n != 1 {
		t.Fatalf("%d sign-off events; want 1", n)
	}
	got := nodes()["R2:2.4-1"]
	if len(got.Signoffs) != 1 || got.Signoffs[0].Ref != "exam-2.4-1" || got.Signoffs[0].By != "owner" ||
		got.Signoffs[0].EventID != first["event_id"] || got.Signoffs[0].At == "" || got.State == "done" {
		t.Fatalf("GET /plan has %+v", got)
	}
	// A node with no sign-off has no signoffs key at all, not a null (review 485 C2).
	var raw struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
	}
	mustJSON(t, get(t, s, "plan?root=R2"), &raw)
	for _, n := range raw.Nodes {
		_, has := n["signoffs"]
		if id := strings.Trim(string(n["plan_node_id"]), `"`); has != (id == "R2:2.4-1") {
			t.Fatalf("%s: signoffs key %v", id, has)
		}
	}
	fixture := map[string]json.RawMessage{"plan": get(t, s, "plan?root=R2")}
	fixture["signed"], _ = json.Marshal(first)

	// The controller's next pass moves the signed step, and only it.
	c := &controller.Controller{DB: pool}
	if _, err := c.Rules(ctx); err != nil {
		t.Fatal(err)
	}
	if after := nodes(); after["R2:2.4-1"].State != "done" || after["R2:2.1-3"].State == "done" || after["R2:2.4-2"].State == "done" {
		t.Fatalf("after the rules pass: 2.4-1 %q, 2.1-3 %q, 2.4-2 %q", after["R2:2.4-1"].State, after["R2:2.1-3"].State, after["R2:2.4-2"].State)
	}
	// Signed and done: a press still answers the sign-off.
	if code, again := sign("admin", "R2:2.4-1", map[string]any{}); code != http.StatusOK || again["already_signed"] != true {
		t.Fatalf("a press on the done step = %d %v", code, again)
	}

	// A rule waiting on two sign-offs needs the ref named; the one named is signed.
	if code, out := sign("admin", "R2:2.1-3", map[string]any{}); code != http.StatusUnprocessableEntity ||
		!strings.Contains(out["message"].(string), "a, b") {
		t.Fatalf("no ref on two = %d %v", code, out)
	}
	if code, out := sign("admin", "R2:2.1-3", map[string]any{"ref": "a"}); code != http.StatusOK || out["ref"] != "a" || out["already_signed"] != false {
		t.Fatalf("ref a = %d %v", code, out)
	}
	if _, err := c.Rules(ctx); err != nil {
		t.Fatal(err)
	}
	if st := nodes()["R2:2.1-3"].State; st != "done" {
		t.Fatalf("2.1-3 after a of any(a, b) = %q", st)
	}
	// The other ref of a step done without it moves nothing: refused.
	if code, out := sign("admin", "R2:2.1-3", map[string]any{"ref": "b"}); code != http.StatusConflict || out["error"] != "state" {
		t.Fatalf("b on the done step = %d %v", code, out)
	}

	for name, c := range map[string]struct {
		step string
		body any
		code int
	}{
		"no such node":            {"R2:9.9-9", map[string]any{}, http.StatusNotFound},
		"a round":                 {"R2", map[string]any{}, http.StatusUnprocessableEntity},
		"an item":                 {"R2:2.4", map[string]any{}, http.StatusUnprocessableEntity},
		"no owner_signoff":        {"R2:2.4-2", map[string]any{}, http.StatusUnprocessableEntity},
		"an imported manual rule": {"R2:2.0-1", map[string]any{}, http.StatusUnprocessableEntity},
		"a ref it does not name":  {"R2:2.4-1", map[string]any{"ref": "exam-2.4-2"}, http.StatusUnprocessableEntity},
		"a dropped step":          {"R2:2.1-4", map[string]any{}, http.StatusConflict},
		"a step done before it":   {"R2:2.3-1", map[string]any{}, http.StatusConflict},
		"an extra field":          {"R2:2.4-1", map[string]any{"ref": "exam-2.4-1", "by": "someone"}, http.StatusBadRequest},
	} {
		if code, out := sign("admin", c.step, c.body); code != c.code {
			t.Errorf("%s: %d %v; want %d", name, code, out, c.code)
		}
	}

	// Presses at once (a double tap, two devices): one sign-off, every press answered with it.
	const presses = 8
	type answer struct {
		code    int
		already bool
		id      float64
		msg     string
	}
	answers := make(chan answer, presses)
	var wg sync.WaitGroup
	for range presses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, s.URL+"/api/v1/agentrt/plan/R2:2.3-2/signoff", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				answers <- answer{}
				return
			}
			req.Header.Set("Authorization", "Bearer "+tokens["admin"])
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				answers <- answer{}
				return
			}
			defer func() { _ = resp.Body.Close() }()
			var out map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&out)
			id, _ := out["event_id"].(float64)
			msg, _ := json.Marshal(out)
			answers <- answer{resp.StatusCode, out["already_signed"] == true, id, string(msg)}
		}()
	}
	wg.Wait()
	close(answers)
	fresh, ids := 0, map[float64]bool{}
	for a := range answers {
		if a.code != http.StatusOK {
			t.Fatalf("a press at once = %d %s", a.code, a.msg)
		}
		if !a.already {
			fresh++
		}
		ids[a.id] = true
	}
	if n := signoffEvents("R2:2.3-2"); fresh != 1 || len(ids) != 1 || n != 1 {
		t.Fatalf("%d presses at once: %d answered as the first, %d event ids, %d events", presses, fresh, len(ids), n)
	}

	for _, step := range []string{"R2:2.4-2", "R2:2.1-4", "R2:2.0-1", "R2:2.3-1"} {
		if n := signoffEvents(step); n != 0 {
			t.Errorf("a refused sign-off on %s wrote %d events", step, n)
		}
	}
	if os.Getenv("CONSOLE_PLAN_SIGNOFFS_FIXTURE_WRITE") == "1" && !t.Failed() {
		b, err := json.MarshalIndent(fixture, "", "  ")
		must(t, err)
		must(t, os.WriteFile(consolePlanSignoffsFixture, append(b, '\n'), 0o644))
	}
}
