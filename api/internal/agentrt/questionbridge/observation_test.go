package questionbridge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Moved from the server's agentrt_external_records_test.go (decoupling item 5,
// A5): an expired open card is not read as open again.
func TestExternalRecordQuestionObservationNeverReopensExpired(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "prod") // the cards model the PROD approval home, as the questions package's tests do
	svc := questions.New(filepath.Join(t.TempDir(), "cards"), nil)
	raw := []byte(`{"idempotency_key":"0123456789abcdef","title":"Choose","urgency":"Now","meanwhile":"Wait"}`)
	res := svc.Create(context.Background(), "thread-a", "reporter", "reporter", raw)
	if res.Code >= 300 {
		t.Fatalf("create: %+v", res)
	}
	items, err := svc.Observations()
	if err != nil || len(items) != 1 {
		t.Fatalf("observations: %+v %v", items, err)
	}
	q := items[0]
	now := q.ExpiresAt.Add(time.Second)
	q.Status = "expired"
	terminal, ok := Observation(q, now)
	if !ok || terminal.Status != "expired" {
		t.Fatalf("terminal: %+v %v", terminal, ok)
	}
	q.Status = "open"
	if _, ok := Observation(q, now); ok {
		t.Fatal("stale open card could reverse terminal observation")
	}
	q.Status = "withdrawn"
	if f, ok := Observation(q, now); !ok || f.Status != "withdrawn" {
		t.Fatal("closed card lost")
	}
}

// Moved from the server's agentrt_sources_test.go (decoupling item 5, A5):
// what a card becomes as a seen fact. An answered card with no option, no
// answerer or no time is not a fact; free text with no option reads as "none".
func TestFactOfAnAnsweredCard(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	card := func(status string, answer map[string]any) questions.Question {
		m := map[string]any{"id": "q", "status": status, "requester": questions.RuntimeSession}
		if answer != nil {
			m["answer"] = answer
		}
		raw, _ := json.Marshal(m)
		var q questions.Question
		if err := json.Unmarshal(raw, &q); err != nil {
			t.Fatal(err)
		}
		return q
	}
	ans := func(option, text, by string, when time.Time) map[string]any {
		return map[string]any{"option": option, "text": text, "by": by, "at": when}
	}
	if f, ok := Fact(card("open", nil)); !ok || f.Status != "open" || f.Option != "" {
		t.Fatalf("open: %+v %v", f, ok)
	}
	f, ok := Fact(card("answered", ans("a", "go", "owner", at)))
	if !ok || f.Option != "a" || f.Text != "go" || f.AnsweredBy != "owner" || f.AnsweredAt != "2026-10-14T17:00:00Z" {
		t.Fatalf("answered: %+v %v", f, ok)
	}
	if f, ok = Fact(card("answered", ans("", "just text", "owner", at))); !ok || f.Option != "none" || f.Text != "just text" {
		t.Fatalf("textual: %+v %v", f, ok)
	}
	for name, a := range map[string]map[string]any{
		"empty": ans("", "", "owner", at), "nobody": ans("a", "", "", at), "untimed": ans("a", "", "owner", time.Time{}),
	} {
		if _, ok := Fact(card("answered", a)); ok {
			t.Fatalf("%s answer became a fact", name)
		}
	}
}
