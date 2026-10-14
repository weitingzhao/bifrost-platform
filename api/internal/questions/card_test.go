package questions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A decision card (design v5 §7 item 3) is a question that names which of
// the Owner's three duties it is and the agent runtime task it is about.
func TestDecisionCardFields(t *testing.T) {
	s := testSvc(t)
	const task = "t_01M4XW5Z7AC7CTA9P0YDGACGPG"
	res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"owner_duty": "rule", "task_id": task, "options": twoOptions()})
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %v", res.Code, res.Body)
	}
	q, ok := byNumber(t, s, "1")
	if !ok || q.OwnerDuty != "rule" || q.TaskID != task {
		t.Fatalf("stored = %+v %v", q, ok)
	}
	for _, bad := range []map[string]any{
		{"owner_duty": "approve"},
		{"owner_duty": 3},
		{"task_id": "t_Z1M4XW5Z7AC7CTA9P0YDGACGPG"},
		{"task_id": "T-1"},
	} {
		if res := ask(t, s, "thread-a", "fedcba9876543210", bad); res.Code != http.StatusBadRequest {
			t.Errorf("%v = %d %v", bad, res.Code, res.Body)
		}
	}
	// the same title about another task is another question, not a duplicate
	other := ask(t, s, "thread-a", "1111111111111111", map[string]any{"owner_duty": "rule", "task_id": "t_01M4XW5Z7SC6YN6KEN3JXF8CFV", "options": twoOptions()})
	if other.Code != http.StatusCreated {
		t.Fatalf("a card about another task = %d %v", other.Code, other.Body)
	}
	// a key reused with another task is a different request
	if res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"owner_duty": "rule", "task_id": "t_01M4XW5Z7SC6YN6KEN3JXF8CFV", "options": twoOptions()}); res.Code != http.StatusConflict ||
		!strings.Contains(fmt.Sprint(res.Body), "idempotency key reused with different content") {
		t.Fatalf("a reused key with another task = %d %v", res.Code, res.Body)
	}
	// the same key with the same task is the same request: replayed
	if res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"owner_duty": "rule", "task_id": task, "options": twoOptions()}); res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("a replay = %d %v", res.Code, res.Body)
	}
	// a plain question still has neither
	if res := ask(t, s, "thread-b", "2222222222222222", nil); res.Code != http.StatusCreated {
		t.Fatalf("plain = %d %v", res.Code, res.Body)
	}
	plain, ok := byNumber(t, s, "3")
	if !ok || plain.OwnerDuty != "" || plain.TaskID != "" {
		t.Fatalf("plain question = %+v %v", plain, ok)
	}
	if b, err := json.Marshal(plain); err != nil || strings.Contains(string(b), "owner_duty") || strings.Contains(string(b), "task_id") {
		t.Fatalf("a plain question shows the card fields: %s %v", b, err)
	}
	// an answer keeps them (it is still not an approval)
	if res := answer(t, s, "1", map[string]any{"option": "a", "channel": "console"}); res.Code != http.StatusOK {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	if q, ok = byNumber(t, s, "1"); !ok || q.OwnerDuty != "rule" || q.TaskID != task || q.Status != "answered" {
		t.Fatalf("after the answer = %+v %v", q, ok)
	}
}

func byNumber(t *testing.T, s *Service, ref string) (Question, bool) {
	t.Helper()
	doc, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	i, ok := doc.find(ref)
	if !ok {
		return Question{}, false
	}
	return doc.Questions[i], true
}
