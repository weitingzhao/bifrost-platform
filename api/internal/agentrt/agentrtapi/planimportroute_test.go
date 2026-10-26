package agentrtapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// POST /plan/imports (CUT-plan-import-2026-10-19.md §4 A): a plan's first
// import waits on one decision card and lands when the Owner answers it.

// answer is a question source that answers every card with option.
func answer(option string) func(context.Context, string) (store.Question, bool) {
	return func(_ context.Context, id string) (store.Question, bool) {
		return store.Question{ID: id, Status: store.QuestionAnswered, Option: option, Text: option, AnsweredBy: "owner",
			AnsweredAt: "2026-10-19T04:00:00Z"}, true
	}
}

func importBody(key string, req planimport.Request) map[string]any {
	return map[string]any{"idempotency_key": key, "reason": "round 2 into the runtime", "request": req}
}

func TestPlanImportThroughTheCard(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	req := ledger(t, fixtureLedger)
	before := len(cards)

	// Only a proposer may ask.
	if code, _ := call(t, s, "viewer", "POST", "plan/imports", importBody("k1", req), "X-Bifrost-Session", "s1"); code != http.StatusUnauthorized {
		t.Fatalf("a viewer's import = %d", code)
	}
	code, out := call(t, s, "proposer", "POST", "plan/imports", importBody("k1", req), "X-Bifrost-Session", "s1")
	if code != 200 || out["question_id"] == nil || out["decision"] != "" {
		t.Fatalf("import proposal = %d %v", code, out)
	}
	if len(cards) != before+1 {
		t.Fatalf("%d cards asked, want 1", len(cards)-before)
	}
	c := cards[len(cards)-1]
	if !strings.HasPrefix(c.Title, "Import plan R2: ") || c.OwnerDuty != "irreversible" || len(c.Options) != 2 ||
		c.Options[0] != store.ImportApply || !strings.Contains(c.Body, "9 done") {
		t.Fatalf("card = %+v", c)
	}
	q := out["question_id"].(string)

	// Nothing is written but the proposal and its request.
	if code, _ := call(t, s, "viewer", "GET", "plan?root=R2", nil); code != 404 {
		t.Fatalf("GET /plan before the decision = %d", code)
	}
	// The same key again, and another key for the same request: the same
	// proposal, no second card; another request waits behind it.
	if code, again := call(t, s, "proposer", "POST", "plan/imports", importBody("k1", req), "X-Bifrost-Session", "s1"); code != 200 || again["question_id"] != q {
		t.Fatalf("the same key again = %d %v", code, again)
	}
	if code, again := call(t, s, "proposer", "POST", "plan/imports", importBody("k2", req), "X-Bifrost-Session", "s1"); code != 200 || again["question_id"] != q {
		t.Fatalf("another key for the same request = %d %v", code, again)
	}
	other := ledger(t, fixtureLedger)
	other.Nodes[1].Evidence.Line = strings.Replace(other.Nodes[1].Evidence.Line, other.Nodes[1].Title, other.Nodes[1].Title+"（改）", 1)
	other.Nodes[1].Title += "（改）"
	if code, refused := call(t, s, "proposer", "POST", "plan/imports", importBody("k3", other), "X-Bifrost-Session", "s1"); code != 422 ||
		!strings.Contains(fmt.Sprint(refused), "waits on the Owner's card") {
		t.Fatalf("a second import while one waits = %d %v", code, refused)
	}
	if len(cards) != before+1 {
		t.Fatal("a retry asked another card")
	}

	// The Owner sees it among what he can act on.
	var progress struct {
		NeedsYou struct {
			Items []struct {
				ID     string         `json:"id"`
				Title  string         `json:"title"`
				Action map[string]any `json:"action"`
			} `json:"items"`
		} `json:"needs_you"`
	}
	mustJSON(t, get(t, s, "progress"), &progress)
	if len(progress.NeedsYou.Items) != 1 || progress.NeedsYou.Items[0].ID != "question:"+q ||
		!strings.HasPrefix(progress.NeedsYou.Items[0].Title, "Import plan R2") ||
		fmt.Sprint(progress.NeedsYou.Items[0].Action["options"]) != fmt.Sprint([]string{store.ImportApply, store.ImportRefuse}) {
		t.Fatalf("needs_you = %+v", progress.NeedsYou.Items)
	}

	// He answers Import: the controller lands it, its head caused by the
	// decision.
	n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil)
	must(t, err)
	if n != 1 {
		t.Fatalf("%d decided", n)
	}
	got, err := planimport.FromPlan(get(t, s, "plan?root=R2"), "R2")
	must(t, err)
	if d := planimport.Diff(planimport.FromRequest(req), got); len(d) > 0 {
		t.Fatalf("the imported plan differs from the ledger:\n%s", strings.Join(d, "\n"))
	}
	var causedBy, decided int64
	must(t, pool.QueryRow(ctx, `SELECT h.cause_event_id, d.event_id FROM agentrt.event h, agentrt.event d
	  WHERE h.event_type = 'legacy_imported' AND d.event_type = 'plan_change_decided' AND d.body_json->>'decision' = 'applied'`).Scan(&causedBy, &decided))
	if causedBy != decided {
		t.Fatalf("the import's head is caused by %d, the decision is %d", causedBy, decided)
	}
	if id := importHead(t, s, "R2"); id != out["request_artifact_id"] {
		t.Fatalf("import head %s, request %v", id, out["request_artifact_id"])
	}
	mustJSON(t, get(t, s, "progress"), &progress)
	if len(progress.NeedsYou.Items) != 0 {
		t.Fatalf("needs_you after the decision = %+v", progress.NeedsYou.Items)
	}

	// Afterwards the same request says it was imported; another is refused.
	if code, again := call(t, s, "proposer", "POST", "plan/imports", importBody("k4", req), "X-Bifrost-Session", "s1"); code != 200 || again["imported"] != false ||
		again["decision"] != "applied" {
		t.Fatalf("the request again after the import = %d %v", code, again)
	}
	if code, refused := call(t, s, "proposer", "POST", "plan/imports", importBody("k5", other), "X-Bifrost-Session", "s1"); code != 422 ||
		!strings.Contains(fmt.Sprint(refused), "already exists") {
		t.Fatalf("another request after the import = %d %v", code, refused)
	}
}

// Refused, the import writes nothing and a new proposal may be made; a
// request the database refuses is refused before any card.
func TestPlanImportRefusedAndFailed(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	req := ledger(t, fixtureLedger)
	code, out := call(t, s, "proposer", "POST", "plan/imports", importBody("k1", req), "X-Bifrost-Session", "s1")
	if code != 200 {
		t.Fatalf("import proposal = %d %v", code, out)
	}
	n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardRefuseKey), nil)
	must(t, err)
	if n != 1 {
		t.Fatalf("%d decided", n)
	}
	if got, _ := call(t, s, "viewer", "GET", "plan?root=R2", nil); got != 404 {
		t.Fatalf("GET /plan after a refusal = %d", got)
	}
	before := len(cards)
	code, again := call(t, s, "proposer", "POST", "plan/imports", importBody("k2", req), "X-Bifrost-Session", "s1")
	if code != 200 || again["question_id"] == out["question_id"] || len(cards) != before+1 {
		t.Fatalf("a new proposal after a refusal = %d %v", code, again)
	}

	// The plan is made another way before the Owner answers: the decision
	// is recorded failed, with the reason, and nothing more is written.
	other := ledger(t, fixtureLedger)
	other.Nodes[1].Evidence.Line = strings.Replace(other.Nodes[1].Evidence.Line, other.Nodes[1].Title, other.Nodes[1].Title+"（改）", 1)
	other.Nodes[1].Title += "（改）"
	_, err = store.ImportPlan(ctx, pool, "controller", other)
	must(t, err)
	_, err = store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil)
	must(t, err)
	var decision, why string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'decision', COALESCE(body_json->>'error', '') FROM agentrt.event
	  WHERE event_type = 'plan_change_decided' ORDER BY event_id DESC LIMIT 1`).Scan(&decision, &why))
	if decision != "failed" || !strings.Contains(why, "already exists") {
		t.Fatalf("the late decision = %s %q", decision, why)
	}

	// A request the database refuses (a round already holds R2's place) is
	// refused when it is proposed, before any card.
	pool2 := seeded(t)
	s2 := server(t, pool2)
	must(t, store.InTx(ctx, pool2, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R9", Kind: "round", Label: "9", Title: "x", Ordinal: 2, Root: "R9"})
		return err
	}))
	before = len(cards)
	if code, refused := call(t, s2, "proposer", "POST", "plan/imports", importBody("k1", req), "X-Bifrost-Session", "s1"); code != 422 || len(cards) != before {
		t.Fatalf("a request the database refuses = %d %v, %d cards", code, refused, len(cards)-before)
	}
	var proposals int
	must(t, pool2.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_change_proposed'`).Scan(&proposals))
	if proposals != 0 {
		t.Fatalf("%d proposals written for a refused request", proposals)
	}
}

func mustJSON(t *testing.T, doc []byte, v any) {
	t.Helper()
	must(t, json.Unmarshal(doc, v))
}
