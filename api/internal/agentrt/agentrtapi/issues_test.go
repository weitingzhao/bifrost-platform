package agentrtapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The issue routes over HTTP (W-31 BI-1 slice 2, DESIGN-crew-intake §2,
// §5.2, §5.3). The markers stand for her words and the crew's texts: they
// are read on the issue routes by the issue's readers and nowhere else.

var issueMarkers = []string{"HERWORDS-9d41", "READING-9d41", "PROPOSAL-9d41", "TITLE-9d41", "BRIEF-9d41", "REPLY-9d41", "ANSWER-9d41",
	"NOTE-9d41", "DESIGN-9d41", "COMMIT-9d41"}

func noIssueMarkers(t *testing.T, what string, v any) {
	t.Helper()
	doc, err := json.Marshal(v)
	must(t, err)
	for _, m := range issueMarkers {
		if strings.Contains(string(doc), m) {
			t.Fatalf("%s carries %s: %s", what, m, doc)
		}
	}
}

// A request's writers, old roles as on the base commit (writer() and
// actorOf() unchanged, R2-A1) and an intake token only by intakeWriter().
func TestWritersOfEachRole(t *testing.T) {
	for _, c := range []struct {
		p                  actuation.Principal
		writer, actor, in string
	}{
		{actuation.Principal{Name: "admin", Role: actuation.RoleAdmin}, "owner", "owner", ""},
		{actuation.Principal{Name: "operator", Role: actuation.RoleOperator}, "thread:s-1", "thread:s-1", ""},
		{actuation.Principal{Name: "trade-satellite", Role: actuation.RoleOperator}, "thread:s-1", "thread:s-1", ""},
		{actuation.Principal{Name: "reporter", Role: actuation.RoleReporter}, "thread:s-1", "thread:s-1", ""},
		{actuation.Principal{Name: "viewer", Role: actuation.RoleViewer}, "thread:s-1", "thread:s-1", ""},
		{actuation.Principal{Name: "seat:chief_of_staff", Role: actuation.RoleReporter}, "seat:chief_of_staff", "seat:chief_of_staff", ""},
		{actuation.Principal{Name: "agentd:h1", Role: actuation.RoleAgentd}, "thread:s-1", "host:h1", ""},
		{actuation.Principal{Name: "intake:dots-p2", Role: actuation.RoleIntake}, "", "", "agent:dots-p2"},
	} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(agentrtapi.SessionHeader, "s-1")
		r = r.WithContext(actuation.WithPrincipal(r.Context(), c.p))
		w, a, i, werr, aerr, ierr := agentrtapi.WritersForTest(r)
		if (c.writer == "") != (werr != nil) || (c.actor == "") != (aerr != nil) || (c.in == "") != (ierr != nil) ||
			w.Actor != c.writer && c.writer != "" || a.Actor != c.actor && c.actor != "" || i.Actor != c.in && c.in != "" {
			t.Fatalf("%+v: writer %q %v, actor %q %v, intake %q %v", c.p, w.Actor, werr, a.Actor, aerr, i.Actor, ierr)
		}
	}
}

// The way through, an intake client's and the crew's, over HTTP, and who may
// not: the readers list, the intake token on nothing but its own, the
// generic reads never showing an issue's stream or text.
func TestIssueRoutesOverHTTP(t *testing.T) {
	pool, _ := world(t)
	seat := issueFixture(t, pool)
	s := server(t, pool)
	cards = nil
	sess := func(caller string) []string { return []string{agentrtapi.SessionHeader, "s-" + strings.ReplaceAll(caller, ":", "-")} }
	do := func(caller, method, path string, body any, want int) map[string]any {
		t.Helper()
		code, out := call(t, s, caller, method, path, body, sess(caller)...)
		if code != want {
			t.Fatalf("%s %s as %q: %d %v, want %d", method, path, caller, code, out, want)
		}
		return out
	}
	now := time.Now().UTC().Format(time.RFC3339)
	out := do("intake:dots-p2", "POST", "intake/issues", map[string]any{"idempotency_key": "k-1", "product": "screening", "kind": "complaint",
		"on_behalf_of": "person:p2", "title": "TITLE-9d41", "person_words": []any{map[string]any{"text": "HERWORDS-9d41", "said_at": now}},
		"agent_reading": "READING-9d41", "agent_proposal": "PROPOSAL-9d41"}, http.StatusOK)
	id := out["issue_id"].(string)
	if out["state"] != "clarifying" || out["maybe_duplicate_of"] == nil {
		t.Fatalf("submitted: %v", out)
	}
	// It speaks for p2 only, in its products.
	do("intake:dots-p2", "POST", "intake/issues", map[string]any{"idempotency_key": "k-2", "product": "screening", "kind": "complaint",
		"on_behalf_of": store.OwnerPerson, "title": "x", "person_words": []any{map[string]any{"text": "x"}}}, http.StatusUnprocessableEntity)
	do("intake:dots-p2", "POST", "intake/issues", map[string]any{"idempotency_key": "k-3", "product": "finance", "kind": "complaint",
		"on_behalf_of": "person:p2", "title": "x", "person_words": []any{map[string]any{"text": "x"}}}, http.StatusUnprocessableEntity)

	// The crew works it; the brief and a reply go back to her agent.
	brief := do("operator", "POST", "issues/"+id+"/brief", map[string]any{"idempotency_key": "c-1", "expected_version": 0,
		"text": "BRIEF-9d41"}, http.StatusOK)
	stale := do("operator", "POST", "issues/"+id+"/brief", map[string]any{"idempotency_key": "c-2", "expected_version": 0,
		"text": "another"}, http.StatusConflict)
	if stale["error"] != "stale" || stale["issue"].(map[string]any)["version"] != float64(1) {
		t.Fatalf("a stale write: %v", stale)
	}
	seen := do("intake:dots-p2", "GET", "intake/issues/"+id, nil, http.StatusOK)
	if seen["open_ask"].(map[string]any)["text"] != "BRIEF-9d41" || seen["open_ask"].(map[string]any)["purpose"] != "confirm_brief" {
		t.Fatalf("her agent's view: %v", seen)
	}
	do("intake:dots-p2", "POST", "intake/issues/"+id+"/responses", map[string]any{"idempotency_key": "k-4", "expected_version": 1,
		"in_reply_to": brief["ask_id"], "user_words": "ANSWER-9d41", "said_at": now, "user_verdict": "confirmed", "agent_note": "NOTE-9d41"}, http.StatusOK)
	do("operator", "POST", "issues/"+id+"/classification", map[string]any{"idempotency_key": "c-3", "expected_version": 2,
		"risk_class": "small", "category": "improvement", "seat_report": seat, "reason": "the seat judged it small"}, http.StatusOK)
	card := do("operator", "POST", "issues/"+id+"/owner-cards", map[string]any{"idempotency_key": "c-4", "expected_version": 3,
		"purpose": "priority"}, http.StatusOK)
	if card["question_id"] == nil || card["state"] != "owner_deciding" || card["waiting_on"] != "owner" {
		t.Fatalf("the Owner's card: %v", card)
	}
	do("operator", "POST", "issues/"+id+"/owner-cards", map[string]any{"idempotency_key": "c-4b", "expected_version": 4,
		"purpose": "priority", "title": "TITLE-9d41"}, http.StatusBadRequest) // no text is taken
	_, err := store.RecordOwnerEvidence(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, "o-1", store.OwnerEvidence{IssueID: id,
		Priority: "p1", BriefSHA256: card["brief_sha256"].(string)})
	must(t, err)
	taken := do("operator", "POST", "issues/"+id+"/take", map[string]any{"idempotency_key": "c-5", "expected_version": 5,
		"commitment": "COMMIT-9d41", "mission": map[string]any{"title": "fix the screen page", "goal": "it shows rows",
			"success": []string{"rows show"}, "budget": map[string]any{"max_attempts": 4, "max_wall_hours": 2}}}, http.StatusOK)
	mission := taken["mission_id"].(string)
	do("operator", "POST", "issues/"+id+"/replies", map[string]any{"idempotency_key": "c-6", "expected_version": 6, "purpose": "reply",
		"text": "REPLY-9d41"}, http.StatusOK)
	do("admin", "POST", "issues/"+id+"/design", map[string]any{"idempotency_key": "c-7", "expected_version": 7, "text": "DESIGN-9d41"},
		http.StatusConflict) // not while its work is live

	// Her agent reads its updates: the brief and the reply, never the
	// crew's own texts; then acknowledges.
	var updates []any
	next := ""
	for deadline := time.Now().Add(60 * time.Second); len(updates) < 5 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		body := map[string]any{"limit": 50}
		if next != "" {
			body["cursor"] = next
		}
		u := do("intake:dots-p2", "POST", "intake/updates", body, http.StatusOK)
		updates, next = append(updates, u["updates"].([]any)...), u["next_cursor"].(string)
	}
	doc, _ := json.Marshal(updates)
	for _, want := range []string{"BRIEF-9d41", "REPLY-9d41", `"type":"taken"`} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("the updates lack %s: %s", want, doc)
		}
	}
	for _, m := range []string{"COMMIT-9d41", "the seat judged"} {
		if strings.Contains(string(doc), m) {
			t.Fatalf("the updates carry %s", m)
		}
	}
	acked := do("intake:dots-p2", "POST", "intake/updates", map[string]any{"ack_cursor": next, "limit": 0}, http.StatusOK)
	if acked["acked"] != next {
		t.Fatalf("acknowledged: %v", acked)
	}
	do("intake:dots-p2", "POST", "intake/updates", map[string]any{"cursor": "x"}, http.StatusBadRequest)
	guide := do("intake:dots-p2", "GET", "intake/guide?product=finance", nil, http.StatusOK)
	if guide["acked"] != next || guide["on_behalf_of"] != "person:p2" || guide["product"].(map[string]any)["in_scope"] != false {
		t.Fatalf("the guide's half: %v", guide)
	}
	mine := do("intake:dots-p2", "GET", "intake/issues", nil, http.StatusOK)
	if len(mine["issues"].([]any)) != 1 {
		t.Fatalf("my issues: %v", mine)
	}

	// The readers read it all; nobody else reads it.
	full := do("admin", "GET", "issues/"+id, nil, http.StatusOK)
	doc, _ = json.Marshal(full)
	for _, m := range []string{"HERWORDS-9d41", "BRIEF-9d41", "COMMIT-9d41", "NOTE-9d41"} {
		if !strings.Contains(string(doc), m) {
			t.Fatalf("the full view lacks %s", m)
		}
	}
	do("operator", "GET", "issues?view=active", nil, http.StatusOK)
	for _, caller := range []string{"viewer", "reporter", "agentd:h1", "trade-satellite", "seat:chief_of_staff", "intake:dots-p2", ""} {
		for _, r := range []struct{ method, path string }{{"GET", "issues"}, {"GET", "issues/" + id}, {"POST", "issues/" + id + "/brief"}} {
			do(caller, r.method, r.path, map[string]any{}, http.StatusUnauthorized)
		}
	}
	for _, caller := range []string{"viewer", "operator", "admin", "agentd:h1", ""} {
		do(caller, "GET", "intake/issues/"+id, nil, http.StatusUnauthorized)
		do(caller, "POST", "intake/updates", map[string]any{}, http.StatusUnauthorized)
	}
	for _, r := range []struct{ method, path string }{{"GET", "whoami"}, {"GET", "events"}, {"GET", "artifacts"}, {"GET", "missions/" + mission},
		{"POST", "missions"}, {"POST", "controls"}, {"GET", "plan"}} {
		do("intake:dots-p2", r.method, r.path, map[string]any{}, http.StatusUnauthorized)
	}

	// The generic reads: no issue stream, no text, for any role.
	for _, caller := range []string{"viewer", "operator", "admin", "agentd:h1", "reporter"} {
		for _, stream := range []string{store.IssueStream(id), store.IntakeStream("agent:dots-p2"), store.PeopleStream} {
			do(caller, "GET", "events?stream="+stream, nil, http.StatusForbidden)
		}
		latest := do(caller, "GET", "events?limit=500", nil, http.StatusOK)
		after := do(caller, "GET", "events?after=0.0&limit=500", nil, http.StatusOK)
		for _, list := range []any{latest["events"], after["events"]} {
			for _, e := range list.([]any) {
				st := e.(map[string]any)["event_stream_id"].(string)
				if strings.HasPrefix(st, "issue:") || strings.HasPrefix(st, "intake:") {
					t.Fatalf("%s reads stream %s by /events", caller, st)
				}
			}
			noIssueMarkers(t, caller+"'s events", list)
		}
		for _, path := range []string{"artifacts?limit=100", "missions/" + mission, "tasks", "plan", "progress", "controls", "hosts"} {
			_, v := call(t, s, caller, "GET", path, nil)
			noIssueMarkers(t, caller+" GET "+path, v)
		}
	}
	noIssueMarkers(t, "the Owner's cards", cards)
	if len(cards) != 2 || cards[0].WorkID != id || cards[1].WorkID != id {
		t.Fatalf("the cards asked: %+v", cards)
	}
}
