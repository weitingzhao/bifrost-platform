package questions

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TD-390: the runtime's card-auto-answer rule answers only the runtime's own
// open questions, only with the option a question recommends, and as itself:
// the answer names the rule and the rule channel, never the Owner. OpenOf
// lists a requester's open questions and nothing else.
func TestAnswerAsRule(t *testing.T) {
	s := testSvc(t)
	ctx := context.Background()
	opts := []map[string]string{{"label": "Give it one more round"}, {"label": "Do not do this task"}, {"label": "Close mission M-3"}}
	id := func(r Result) string {
		t.Helper()
		if r.Code != http.StatusCreated {
			t.Fatalf("create: %d %v", r.Code, r.Body)
		}
		return r.Body.(map[string]any)["id"].(string)
	}
	card := id(ask(t, s, "agentrt", "k-card-recommends-c", map[string]any{"options": opts, "recommended": 3}))
	bare := id(ask(t, s, "agentrt", "k-card-recommends-none", map[string]any{"title": "Another card", "options": opts}))
	other := id(ask(t, s, "thread-a", "k-thread-card-recommends", map[string]any{"title": "A thread's", "options": opts, "recommended": 1}))

	open, err := s.OpenOf("agentrt")
	if err != nil || len(open) != 2 {
		t.Fatalf("open of agentrt: %d %v", len(open), err)
	}
	for name, c := range map[string]struct {
		requester, ref, option string
		code                   int
	}{
		"another option":     {"agentrt", card, "b", http.StatusConflict},
		"no recommendation":  {"agentrt", bare, "a", http.StatusConflict},
		"another requester":  {"agentrt", other, "a", http.StatusForbidden},
		"as another session": {"thread-a", card, "c", http.StatusForbidden},
		"no such question":   {"agentrt", "q_0000000000000000", "a", http.StatusNotFound},
	} {
		if r := s.AnswerAsRule(ctx, c.requester, "rule card-auto-answer v1", c.ref, c.option); r.Code != c.code {
			t.Errorf("%s: %d %v; want %d", name, r.Code, r.Body, c.code)
		}
	}
	if r := s.AnswerAsRule(ctx, "agentrt", "rule card-auto-answer v1", card, "c"); r.Code != http.StatusOK {
		t.Fatalf("the rule's answer: %d %v", r.Code, r.Body)
	}
	q, ok, err := s.Lookup(card)
	if err != nil || !ok || q.Status != StatusAnswered || q.Answer.Option != "c" || q.Answer.By != "rule card-auto-answer v1" ||
		q.Answer.Channel != RuleChannel || q.Answer.Approval {
		t.Fatalf("the answer: %+v %+v %v %v", q, q.Answer, ok, err)
	}
	if r := s.AnswerAsRule(ctx, "agentrt", "rule card-auto-answer v1", card, "c"); r.Code != http.StatusConflict {
		t.Fatalf("a second answer: %d %v", r.Code, r.Body)
	}
	if open, err = s.OpenOf("agentrt"); err != nil || len(open) != 1 || open[0].ID != bare {
		t.Fatalf("open after the answer: %+v %v", open, err)
	}
	// The Owner answers a thread's question on the console as before.
	raw, _ := json.Marshal(map[string]any{"option": "a", "channel": "console"})
	if r := s.Answer(ctx, "owner-console", "admin", "admin", other, raw); r.Code != http.StatusOK {
		t.Fatalf("the Owner's answer: %d %v", r.Code, r.Body)
	}
}
