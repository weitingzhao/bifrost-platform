package agentrtapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The plan import end to end (CUT-plan-import-2026-10-19.md): a round of the
// ledger parsed, imported by the store, read back through the routes the
// Console and the board read.

const (
	fixtureLedger = "../planimport/testdata/ledger.md"
	fixtureBoard  = "../planimport/testdata/ledger-board.json"
)

// The fixture's steps with a ★ the board does not see (after 「（补强）」).
var fixtureStars = []string{"2.1-4", "2.3-1", "2.3-2", "2.4-2"}

func seeded(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	must(t, err)
	return db.Pool(t, db.Runtime)
}

func ledger(t *testing.T, path string) planimport.Request {
	t.Helper()
	text, err := os.ReadFile(path)
	must(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	must(t, err)
	return req
}

// get reads a route as the viewer and returns its body.
func get(t *testing.T, s *httptest.Server, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.URL+"/api/v1/agentrt/"+path, nil)
	must(t, err)
	req.Header.Set("Authorization", "Bearer "+tokens["viewer"])
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	must(t, err)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, resp.StatusCode, body)
	}
	return body
}

func TestPlanImportMatchesLedger(t *testing.T) {
	checkImport(t, fixtureLedger, fixtureBoard, fixtureStars)
}

// The real ledger, when it is named (see planimport's
// TestParseRealLedgerMatchesBoard for the three variables).
func TestPlanImportRealLedger(t *testing.T) {
	path, board := os.Getenv("AGENTRT_PLAN_LEDGER"), os.Getenv("AGENTRT_PLAN_BOARD")
	if path == "" || board == "" {
		t.Skip("set AGENTRT_PLAN_LEDGER and AGENTRT_PLAN_BOARD to import the real ledger")
	}
	var stars []string
	if s := os.Getenv("AGENTRT_PLAN_EXTRA_STARS"); s != "" {
		stars = strings.Split(s, ",")
	}
	checkImport(t, path, board, stars)
}

func checkImport(t *testing.T, path, board string, stars []string) {
	pool := seeded(t)
	req := ledger(t, path)
	want := planimport.FromRequest(req)
	var done, active, steps int
	for _, n := range req.Nodes {
		if n.Kind == "step" {
			steps++
		}
		switch n.State {
		case planimport.StateDone:
			done++
		case planimport.StateActive:
			active++
		}
	}
	res, err := store.ImportPlan(ctx, pool, "controller", req)
	must(t, err)
	if !res.Imported || res.Nodes != 1+len(req.Nodes) || res.Done != done || res.Active != active || res.EventID == 0 {
		t.Fatalf("import = %+v; want %d nodes, %d done, %d active", res, 1+len(req.Nodes), done, active)
	}
	s := server(t, pool)

	// GET /plan: the round step by step as the ledger has it, and as the
	// board reads it.
	tree := get(t, s, "plan?root=R2")
	got, err := planimport.FromPlan(tree, "R2")
	must(t, err)
	if d := planimport.Diff(want, got); len(d) > 0 {
		t.Fatalf("GET /plan differs from the ledger:\n%s", strings.Join(d, "\n"))
	}
	boardDoc, err := os.ReadFile(board)
	must(t, err)
	var boardView planimport.View
	must(t, json.Unmarshal(boardDoc, &boardView))
	extra := planimport.BoardMissesStars(boardView, &got)
	if d := planimport.Diff(boardView, got); len(d) > 0 || !slices.Equal(extra, stars) {
		t.Fatalf("GET /plan differs from the board's reading (★ it misses: %v, want %v):\n%s", extra, stars, strings.Join(d, "\n"))
	}

	// Each node's state names the event it came from.
	var answer struct {
		Nodes []struct {
			ID     string          `json:"plan_node_id"`
			Kind   string          `json:"node_kind"`
			State  *string         `json:"state"`
			WorkID *string         `json:"work_id"`
			Done   json.RawMessage `json:"done_rule_json"`
			Event  struct {
				Type string         `json:"event_type"`
				Body map[string]any `json:"body"`
			} `json:"state_event"`
		} `json:"nodes"`
	}
	must(t, json.Unmarshal(tree, &answer))
	firstDone := ""
	for _, n := range answer.Nodes {
		cause, _ := n.Event.Body["cause"].(string)
		state := ""
		if n.State != nil {
			state = *n.State
		}
		var ok bool
		switch state {
		case "done":
			ok = n.Event.Type == "plan_node_state_changed" && cause == "done_rule_manual" && string(n.Done) == `{"manual": "`+planimport.ManualAtom(n.ID)+`"}`
			if firstDone == "" {
				firstDone = n.ID
			}
		case "active":
			ok = n.Event.Type == "plan_node_state_changed" && cause == "legacy_imported" && string(n.Done) == "{}"
		default:
			ok = n.Event.Type == "plan_node_created" && string(n.Done) == "{}"
		}
		if !ok || n.WorkID == nil || *n.WorkID != req.Root.WorkID {
			t.Errorf("%s (%s %q): state event %s %v, done rule %s, work id %v", n.ID, n.Kind, state, n.Event.Type, n.Event.Body, n.Done, n.WorkID)
		}
	}

	// A done step in full: its evidence note, the ledger line in it.
	var node struct {
		Events []struct {
			Type string         `json:"event_type"`
			Body map[string]any `json:"body"`
		} `json:"events"`
	}
	must(t, json.Unmarshal(get(t, s, "plan/"+firstDone), &node))
	var evidence []any
	for _, e := range node.Events {
		if e.Type == "note" && e.Body["manual"] == planimport.ManualAtom(firstDone) {
			evidence, _ = e.Body["evidence"].([]any)
		}
	}
	if len(evidence) != 2 || evidence[1] != res.Request {
		t.Fatalf("%s: evidence %v; want its own and the request %s", firstDone, evidence, res.Request)
	}
	var proof struct {
		Node     string `json:"plan_node_id"`
		Request  string `json:"request_artifact_id"`
		Evidence struct {
			Line string `json:"line"`
		} `json:"evidence"`
	}
	must(t, json.Unmarshal(get(t, s, "artifacts/"+evidence[0].(string)), &proof))
	line := ""
	for _, n := range req.Nodes {
		if n.ID == firstDone {
			line = n.Evidence.Line
		}
	}
	if proof.Node != firstDone || proof.Request != res.Request || proof.Evidence.Line != line {
		t.Fatalf("evidence of %s = %+v", firstDone, proof)
	}
	var stored planimport.Request
	must(t, json.Unmarshal(get(t, s, "artifacts/"+res.Request), &stored))
	if stored.Source.SHA256 != req.Source.SHA256 || len(stored.Nodes) != len(req.Nodes) {
		t.Fatal("the stored request is not the one imported")
	}

	// GET /progress counts as the board does; the imported done steps are
	// manual.
	var progress struct {
		Plan []struct {
			ID    string `json:"plan_node_id"`
			Count struct {
				Done, Total, Manual int
			} `json:"count"`
			Items []struct {
				Label string `json:"label"`
				Count struct {
					Done, Total int
				} `json:"count"`
			} `json:"items"`
		} `json:"plan"`
	}
	must(t, json.Unmarshal(get(t, s, "progress"), &progress))
	if len(progress.Plan) != 1 || progress.Plan[0].ID != "R2" {
		t.Fatalf("progress plan = %+v", progress.Plan)
	}
	r := progress.Plan[0]
	if r.Count.Done != done || r.Count.Total != steps || r.Count.Manual != done || len(r.Items) != len(want.Items) {
		t.Fatalf("progress R2 = %+v; want %d/%d, %d manual", r.Count, done, steps, done)
	}
	for i, it := range want.Items {
		d := 0
		for _, st := range it.Steps {
			if st.State == "done" {
				d++
			}
		}
		if r.Items[i].Label != it.Key || r.Items[i].Count.Done != d || r.Items[i].Count.Total != len(it.Steps) {
			t.Errorf("progress item %s = %+v; want %d/%d", it.Key, r.Items[i], d, len(it.Steps))
		}
	}

	// The board's when, by the read chain of CUT §6: the plan stream's
	// legacy_imported event names the request, read once; each step still in
	// its imported state shows the date its line was marked, not the import's
	// time (review 310 A6).
	reqID := importHead(t, s, "R2")
	if reqID != res.Request {
		t.Fatalf("import head names %s, the import stored %s", reqID, res.Request)
	}
	var named planimport.Request
	must(t, json.Unmarshal(get(t, s, "artifacts/"+reqID), &named))
	shown, err := planimport.FromPlanWithRequest(tree, "R2", &named)
	must(t, err)
	marked := map[string]string{}
	for _, n := range req.Nodes {
		if n.Kind == "step" {
			marked[n.Label] = n.Evidence.MarkedAt
		}
	}
	for _, it := range shown.Items {
		for _, st := range it.Steps {
			if st.When != marked[st.ID] {
				t.Errorf("step %s shows %q beside it, want the line's %q", st.ID, st.When, marked[st.ID])
			}
		}
	}

	// The plan's stream is one unbroken chain.
	if _, err := store.VerifyStream(ctx, pool, store.PlanStream("R2")); err != nil {
		t.Fatal(err)
	}
}

// importHead pages through the plan's stream for its import head, as the
// board does (planimport.ImportHead).
func importHead(t *testing.T, s *httptest.Server, root string) string {
	t.Helper()
	var after int64
	for pages := 0; pages < 100; pages++ {
		id, next, err := planimport.ImportHead(get(t, s, fmt.Sprintf("events?stream=plan:%s&after_seq=%d", root, after)))
		must(t, err)
		if id != "" {
			return id
		}
		if next == 0 {
			t.Fatalf("plan %s has no import head", root)
		}
		after = next
	}
	t.Fatal("no import head in 100 pages")
	return ""
}

// The import head need not be the stream's first event: under the
// recommended entry (CUT §4 A) the proposal and its decision come first, and
// a stream can hold more than a page (500) before it (review 311 A2).
func TestPlanImportHeadAfterOtherEvents(t *testing.T) {
	pool := seeded(t)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		for i := 0; i < 501; i++ {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("R2"), Type: "note", Actor: "controller",
				Body: map[string]any{"text": fmt.Sprintf("before the import %d", i)}}); err != nil {
				return err
			}
		}
		return nil
	}))
	res, err := store.ImportPlan(ctx, pool, "controller", ledger(t, fixtureLedger))
	must(t, err)
	s := server(t, pool)
	if got := importHead(t, s, "R2"); got != res.Request {
		t.Fatalf("import head %s, want %s", got, res.Request)
	}
	if _, next, err := planimport.ImportHead(get(t, s, "events?stream=plan:R9")); err != nil || next != 0 {
		t.Fatalf("a stream that does not exist: next %d, %v", next, err)
	}
}

// counts is the number of plan nodes, events and artifacts.
func counts(t *testing.T, pool *pgxpool.Pool) [3]int {
	t.Helper()
	var c [3]int
	must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.plan_node), (SELECT count(*) FROM agentrt.event),
	  (SELECT count(*) FROM agentrt.artifact)`).Scan(&c[0], &c[1], &c[2]))
	return c
}

// Importing twice writes nothing the second time, even after the plan has
// moved on; a different request for the same root is refused whole.
func TestPlanImportTwice(t *testing.T) {
	pool := seeded(t)
	req := ledger(t, fixtureLedger)
	first, err := store.ImportPlan(ctx, pool, "controller", req)
	must(t, err)
	after := counts(t, pool)
	again, err := store.ImportPlan(ctx, pool, "controller", req)
	must(t, err)
	if again.Imported || again.EventID != first.EventID || again.Request != first.Request || counts(t, pool) != after {
		t.Fatalf("second import = %+v (first %+v); counts %v, were %v", again, first, counts(t, pool), after)
	}

	// A request that differs in one title: refused, nothing written.
	other := ledger(t, fixtureLedger)
	other.Nodes[1].Evidence.Line = strings.Replace(other.Nodes[1].Evidence.Line, other.Nodes[1].Title, other.Nodes[1].Title+"（改）", 1)
	other.Nodes[1].Title += "（改）"
	if _, oerr := store.ImportPlan(ctx, pool, "controller", other); !errors.Is(oerr, store.ErrInvalid) ||
		!strings.Contains(oerr.Error(), "already exists and was not imported from this request") {
		t.Fatalf("a different request: %v", oerr)
	}
	if counts(t, pool) != after {
		t.Fatal("a refused import wrote something")
	}

	// The plan moves on (a step added within the standing authorization);
	// the same request again still only says it was imported.
	out, err := store.ProposePlanChange(ctx, pool, store.Writer{Actor: "thread:local_test", Token: "reporter"}, "k1", "R2", "one more step",
		[]store.PlanChange{{Op: "add_node", Parent: "R2:2.1", Kind: "step", StepKind: "agent", Title: "new"}}, nil)
	must(t, err)
	if out["decision"] != "applied" {
		t.Fatalf("proposal = %v", out)
	}
	moved := counts(t, pool)
	again, err = store.ImportPlan(ctx, pool, "controller", req)
	must(t, err)
	if again.Imported || counts(t, pool) != moved {
		t.Fatalf("import after the plan moved = %+v", again)
	}
}

// After the import, the runtime numbers new nodes on from the imported ones:
// a step added to 2.1 is 2.1-6, an item added to the round is 2.5 (2.2b is
// not a number and is passed over).
func TestPlanImportNumberingContinues(t *testing.T) {
	pool := seeded(t)
	_, err := store.ImportPlan(ctx, pool, "controller", ledger(t, fixtureLedger))
	must(t, err)
	out, err := store.ProposePlanChange(ctx, pool, store.Writer{Actor: "thread:local_test", Token: "reporter"}, "k1", "R2", "one more step",
		[]store.PlanChange{{Op: "add_node", Parent: "R2:2.1", Kind: "step", StepKind: "agent", Title: "new step"}}, nil)
	must(t, err)
	if fmt.Sprint(out["nodes"]) != "[R2:2.1-6]" {
		t.Fatalf("step added = %v", out)
	}
	// An item is outside the standing authorization: the Owner's card, his
	// Apply, the controller's decision pass.
	ask := func(context.Context, store.Card) (string, error) { return "q_1", nil }
	out, err = store.ProposePlanChange(ctx, pool, store.Writer{Actor: "thread:local_test", Token: "reporter"}, "k2", "R2", "one more item",
		[]store.PlanChange{{Op: "add_node", Parent: "R2", Kind: "item", Title: "new item"}}, ask)
	must(t, err)
	if out["question_id"] != "q_1" {
		t.Fatalf("item proposal = %v", out)
	}
	answered := func(_ context.Context, id string) (store.Question, bool) {
		return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardApplyKey}, true
	}
	n, err := store.DecidePlanChanges(ctx, pool, answered, nil)
	must(t, err)
	if n != 1 {
		t.Fatalf("%d decided", n)
	}
	got, err := planimport.FromPlan(get(t, server(t, pool), "plan?root=R2"), "R2")
	must(t, err)
	var keys []string
	for _, it := range got.Items {
		keys = append(keys, it.Key)
	}
	if !slices.Equal(keys, []string{"2.0", "2.1", "2.2b", "2.3", "2.4", "2.5"}) || got.Items[1].Steps[5].ID != "2.1-6" {
		t.Fatalf("items %v, 2.1's last step %+v", keys, got.Items[1].Steps[len(got.Items[1].Steps)-1])
	}
}

// An import that fails part way leaves nothing: here a round already holds
// the root's place, which the first node's insert finds.
func TestPlanImportAllOrNothing(t *testing.T) {
	pool := seeded(t)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R9", Kind: "round", Label: "9", Title: "x", Ordinal: 2, Root: "R9"})
		return err
	}))
	before := counts(t, pool)
	if _, err := store.ImportPlan(ctx, pool, "controller", ledger(t, fixtureLedger)); err == nil {
		t.Fatal("an import over a taken root place succeeded")
	}
	if counts(t, pool) != before {
		t.Fatalf("a failed import left rows: %v, were %v", counts(t, pool), before)
	}
}
