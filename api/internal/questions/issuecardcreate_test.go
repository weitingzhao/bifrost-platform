package questions

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// W-31 BI-1 slice 2: a business issue's number names a card only when the
// agent runtime asks it (DESIGN-crew-intake §5.3: the runtime's template,
// no text of the issue); a thread's ask_owner, or any other session, may
// not open one, whatever else it writes on it.
func TestOnlyTheRuntimeAsksABusinessIssuesCard(t *testing.T) {
	s := testSvc(t)
	opts := []map[string]string{{"label": "p1"}, {"label": "p2"}, {"label": "p3"}}
	for _, session := range []string{"thread-a", "owner-console", "agentrt-x"} {
		res := ask(t, s, session, "k-bi-card-"+strings.ReplaceAll(session, "-", "x")+"1", map[string]any{"work_id": "BI-3", "options": opts})
		if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.(map[string]any)["error"].(string), "agent runtime only") {
			t.Fatalf("%s opened a BI- card: %d %v", session, res.Code, res.Body)
		}
	}
	for _, work := range []string{"BI-0", "BI-03", "bi-3", "BI-", "BI-3x"} {
		res := ask(t, s, RuntimeSession, "k-bi-card-shape-"+strings.ReplaceAll(work, "-", "x"), map[string]any{"work_id": work, "options": opts})
		if res.Code != http.StatusBadRequest || res.Body.(map[string]any)["error"] != "work_id is invalid" {
			t.Fatalf("work id %q was taken: %d %v", work, res.Code, res.Body)
		}
	}
	id := mustCreate(t, s, RuntimeSession, "k-bi-card-runtime-1", map[string]any{"work_id": "BI-3", "options": opts})["id"].(string)
	q, ok, err := s.Lookup(id)
	if err != nil || !ok || q.WorkID != "BI-3" || !BusinessIssueCard(q) || q.AllowText {
		t.Fatalf("the runtime's card: %+v %v %v", q, ok, err)
	}
	// Its answer is an option and nothing else (slice 1b holds).
	if r := s.Answer(context.Background(), "owner-console", "admin", "admin", id, mustJSON(t, map[string]any{"option": "none", "text": "x"})); r.Code != http.StatusBadRequest {
		t.Fatalf("none with text on a BI card: %d %v", r.Code, r.Body)
	}
	if r := s.Answer(context.Background(), "owner-console", "admin", "admin", id, mustJSON(t, map[string]any{"option": "b"})); r.Code != http.StatusOK {
		t.Fatalf("an option on a BI card: %d %v", r.Code, r.Body)
	}
	// Other work ids are as before, from any session.
	if r := ask(t, s, "thread-a", "k-bi-card-w31-card-1", map[string]any{"work_id": "W-31", "options": opts}); r.Code != http.StatusCreated {
		t.Fatalf("a W- card: %d %v", r.Code, r.Body)
	}
}
