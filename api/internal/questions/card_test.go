package questions

import (
	"context"
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
	// the same key with the same task is the same request: the first answer,
	// replayed, and no new question (review 174 B3)
	before := count(t, s)
	replay := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"owner_duty": "rule", "task_id": task, "options": twoOptions()})
	body, _ := replay.Body.(map[string]any)
	if replay.Code != http.StatusOK || body["replayed"] != true || body["number"] != q.Number || body["id"] != q.ID || count(t, s) != before {
		t.Fatalf("a replay = %d %v (%d questions, was %d)", replay.Code, replay.Body, count(t, s), before)
	}
	// the same task with another duty is another question
	if res := ask(t, s, "thread-a", "3333333333333333", map[string]any{"owner_duty": "goal", "task_id": task, "options": twoOptions()}); res.Code != http.StatusCreated {
		t.Fatalf("another duty about the same task = %d %v", res.Code, res.Body)
	}
	// a plain question still has neither
	if res := ask(t, s, "thread-b", "2222222222222222", nil); res.Code != http.StatusCreated {
		t.Fatalf("plain = %d %v", res.Code, res.Body)
	}
	plain, ok := byNumber(t, s, "4")
	if !ok || plain.OwnerDuty != "" || plain.TaskID != "" {
		t.Fatalf("plain question = %+v %v", plain, ok)
	}
	if b, err := json.Marshal(plain); err != nil || strings.Contains(string(b), "owner_duty") || strings.Contains(string(b), "task_id") {
		t.Fatalf("a plain question shows the card fields: %s %v", b, err)
	}
	// what readers get: the card fields in GET and in the list
	for name, res := range map[string]Result{
		"get":  s.Get(context.Background(), "owner-console", "admin", "admin", "1"),
		"list": s.List(context.Background(), "owner-console", "admin", "admin", "all", false),
	} {
		b, err := json.Marshal(res.Body)
		if err != nil || res.Code != http.StatusOK || !strings.Contains(string(b), `"owner_duty":"rule"`) || !strings.Contains(string(b), `"task_id":"`+task+`"`) {
			t.Fatalf("%s = %d %s %v", name, res.Code, b, err)
		}
	}
	// an answer keeps them, and is still not an approval
	if res := answer(t, s, "1", map[string]any{"option": "a", "channel": "console"}); res.Code != http.StatusOK {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	if q, ok = byNumber(t, s, "1"); !ok || q.OwnerDuty != "rule" || q.TaskID != task || q.Status != "answered" || q.Answer == nil || q.Answer.Approval {
		t.Fatalf("after the answer = %+v %v", q, ok)
	}
}

func count(t *testing.T, s *Service) int {
	t.Helper()
	doc, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	return len(doc.Questions)
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
