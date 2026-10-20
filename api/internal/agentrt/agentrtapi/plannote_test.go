package agentrtapi_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The architecture table through the interface (W-31 2.5 G2 A): imported
// beside round 2 on one decision card each, its rows on GET /progress with
// their ticks and latest notes, and POST /plan/{id}/notes.

// archRowDoc is an architecture row as GET /progress gives it.
type archRowDoc struct {
	PlanNodeID string `json:"plan_node_id"`
	Title      string `json:"title"`
	Tick       string `json:"tick"`
	Reason     string `json:"reason"`
	Note       *struct {
		Text string `json:"text"`
		At   string `json:"at"`
		By   string `json:"by"`
	} `json:"note"`
}

type archProgress struct {
	Plan []struct {
		PlanNodeID string       `json:"plan_node_id"`
		Kind       string       `json:"kind"`
		Rows       []archRowDoc `json:"rows"`
	} `json:"plan"`
}

func TestArchTableThroughTheInterface(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	propose := func(key string, req planimport.Request) {
		t.Helper()
		if code, out := call(t, s, "reporter", "POST", "plan/imports", importBody(key, req), session, "s1"); code != 200 || out["question_id"] == nil {
			t.Fatalf("import %s = %d %v", req.Root.ID, code, out)
		}
		if n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || n != 1 {
			t.Fatalf("decide %s: %d %v", req.Root.ID, n, err)
		}
	}
	propose("r2", ledger(t, fixtureLedger))
	text, err := os.ReadFile(fixtureLedger)
	must(t, err)
	spec := strings.Repeat("ab", 32)
	arch, err := planimport.ParseArch(string(text), []planimport.ArchRow{
		{Label: "release-guard", Rule: []byte(`{"gates_passed":[{"test":"T05","spec_sha256":"` + spec + `"}],"env":"prod"}`)},
		{Label: "state-db"},
	}, planimport.LedgerOptions{File: "WORK.md"})
	must(t, err)
	before := len(cards)
	propose("arch", arch)
	if c := cards[before]; !strings.HasPrefix(c.Title, "Import plan ARCH: ") ||
		!strings.Contains(c.Body, "2 rows: 1 with gate tests") || !strings.Contains(c.Body, "2 rows get what their line says") {
		t.Fatalf("the table's card = %+v", c)
	}
	var roots struct {
		Roots []struct {
			ID string `json:"plan_node_id"`
		} `json:"roots"`
	}
	mustJSON(t, get(t, s, "plan"), &roots)
	if len(roots.Roots) != 2 || roots.Roots[0].ID != "ARCH" || roots.Roots[1].ID != "R2" {
		t.Fatalf("roots = %+v", roots)
	}
	rows := func() map[string]archRowDoc {
		t.Helper()
		var p archProgress
		mustJSON(t, get(t, s, "progress"), &p)
		for _, r := range p.Plan {
			if r.Kind == "arch_root" {
				byID := map[string]archRowDoc{}
				for _, row := range r.Rows {
					byID[row.PlanNodeID] = row
				}
				return byID
			}
		}
		t.Fatal("no architecture root on /progress")
		return nil
	}
	got := rows()
	guard, state := got["ARCH:release-guard"], got["ARCH:state-db"]
	if guard.Tick != "unknown" || !strings.Contains(guard.Reason, "what prod runs is not recorded yet") || guard.Title != "发布护栏" ||
		guard.Note == nil || guard.Note.Text != "在正式环境" || guard.Note.By != "controller" {
		t.Fatalf("release-guard = %+v", guard)
	}
	if state.Tick != "unknown" || state.Reason != store.ReasonNoGate || state.Note == nil || state.Note.Text != "两个环境都在用" {
		t.Fatalf("state-db = %+v", state)
	}

	note := func(key, text string) map[string]any { return map[string]any{"idempotency_key": key, "text": text} }
	// Who may write one: those who propose plan changes.
	for _, who := range []string{"viewer", "agentd:h1", ""} {
		if code, _ := call(t, s, who, "POST", "plan/ARCH:state-db/notes", note("n0", "x"), session, "s1"); code != http.StatusUnauthorized &&
			code != http.StatusForbidden {
			t.Fatalf("%q wrote a note: %d", who, code)
		}
	}
	if code, out := call(t, s, "operator", "POST", "plan/ARCH:state-db/notes", note("n0", "x")); code != http.StatusUnprocessableEntity {
		t.Fatalf("a thread without a session: %d %v", code, out)
	}
	code, first := call(t, s, "operator", "POST", "plan/ARCH:state-db/notes", note("n1", "PROD rebuild drill still owed"), session, "night")
	if code != http.StatusOK || first["event_id"] == nil || first["by"] != "thread:night" || first["plan_node_id"] != "ARCH:state-db" {
		t.Fatalf("a note = %d %v", code, first)
	}
	if code, again := call(t, s, "operator", "POST", "plan/ARCH:state-db/notes", note("n1", "PROD rebuild drill still owed"), session, "night"); code != 200 ||
		again["event_id"] != first["event_id"] {
		t.Fatalf("a retry = %d %v", code, again)
	}
	if code, out := call(t, s, "operator", "POST", "plan/ARCH:state-db/notes", note("n1", "another text"), session, "night"); code != http.StatusConflict ||
		out["error"] != "key_conflict" {
		t.Fatalf("a reused key = %d %v", code, out)
	}
	if n := rows()["ARCH:state-db"].Note; n == nil || n.Text != "PROD rebuild drill still owed" || n.By != "thread:night" {
		t.Fatalf("the latest note = %+v", n)
	}
	if code, out := call(t, s, "admin", "POST", "plan/ARCH:state-db/notes", note("n2", "done on PROD"), session, "ignored"); code != 200 || out["by"] != "owner" {
		t.Fatalf("the Owner's note = %d %v", code, out)
	}
	if n := rows()["ARCH:state-db"].Note; n == nil || n.Text != "done on PROD" || n.By != "owner" {
		t.Fatalf("the latest note after the Owner's = %+v", n)
	}

	for name, c := range map[string]struct {
		path string
		body any
		code int
	}{
		"no such node":   {"plan/ARCH:nothing/notes", note("e1", "x"), http.StatusNotFound},
		"a step":         {"plan/R2:2.0-1/notes", note("e2", "x"), http.StatusUnprocessableEntity},
		"a round":        {"plan/R2/notes", note("e3", "x"), http.StatusUnprocessableEntity},
		"the table":      {"plan/ARCH/notes", note("e4", "x"), http.StatusUnprocessableEntity},
		"empty":          {"plan/ARCH:state-db/notes", note("e5", ""), http.StatusUnprocessableEntity},
		"no key":         {"plan/ARCH:state-db/notes", note("", "x"), http.StatusUnprocessableEntity},
		"a local path":   {"plan/ARCH:state-db/notes", note("e6", "see /home/agent/notes"), http.StatusUnprocessableEntity},
		"too long":       {"plan/ARCH:state-db/notes", note("e7", strings.Repeat("a", store.MaxNoteText+1)), http.StatusUnprocessableEntity},
		"an extra field": {"plan/ARCH:state-db/notes", map[string]any{"idempotency_key": "e8", "text": "x", "manual": "y"}, http.StatusBadRequest},
	} {
		if code, out := call(t, s, "operator", "POST", c.path, c.body, session, "night"); code != c.code {
			t.Errorf("%s: %d %v; want %d", name, code, out, c.code)
		}
	}
	var notes int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND plan_node_id LIKE 'ARCH%'`).Scan(&notes))
	// The two first notes of the import, then the thread's and the Owner's.
	if notes != 4 {
		t.Fatalf("%d notes on the table; want 4", notes)
	}
	if _, err := store.VerifyStream(ctx, pool, "plan:ARCH"); err != nil {
		t.Fatal(fmt.Errorf("plan:ARCH: %w", err))
	}
}
