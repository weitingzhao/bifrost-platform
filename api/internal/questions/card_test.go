package questions

import (
	"net/http"
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
	if res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"owner_duty": "rule", "task_id": "t_01M4XW5Z7SC6YN6KEN3JXF8CFV", "options": twoOptions()}); res.Code < 400 {
		t.Fatalf("a reused key with another task = %d %v", res.Code, res.Body)
	}
	// a plain question still has neither
	if res := ask(t, s, "thread-b", "2222222222222222", nil); res.Code != http.StatusCreated {
		t.Fatalf("plain = %d %v", res.Code, res.Body)
	}
	if q, _ := byNumber(t, s, "3"); q.OwnerDuty != "" || q.TaskID != "" {
		t.Fatalf("plain question = %+v", q)
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
