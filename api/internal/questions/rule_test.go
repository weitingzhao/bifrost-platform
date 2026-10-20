package questions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	far := time.Now().Add(time.Hour)
	open, err := s.RuleCards("agentrt")
	if err != nil || len(open) != 2 {
		t.Fatalf("open of agentrt: %d %v", len(open), err)
	}
	for name, c := range map[string]struct {
		requester, ref, option, why string
		code                        int
	}{
		"another option":             {"agentrt", card, "b", "", http.StatusConflict},
		"no recommendation":          {"agentrt", bare, "a", "", http.StatusConflict},
		"why but no such option":     {"agentrt", bare, "d", "the card recommends nothing", http.StatusConflict},
		"why but blank":              {"agentrt", bare, "b", "  ", http.StatusConflict},
		"another requester":          {"agentrt", other, "a", "", http.StatusForbidden},
		"another requester, and why": {"agentrt", other, "b", "the card recommends nothing", http.StatusForbidden},
		"as another session":         {"thread-a", card, "c", "", http.StatusForbidden},
		"no such question":           {"agentrt", "q_0000000000000000", "a", "", http.StatusNotFound},
	} {
		if r := s.AnswerAsRule(ctx, c.requester, "rule card-auto-answer v1", c.ref, c.option, far, c.why); r.Code != c.code {
			t.Errorf("%s: %d %v; want %d", name, r.Code, r.Body, c.code)
		}
	}
	if r := s.AnswerAsRule(ctx, "agentrt", "rule card-auto-answer v1", card, "c", far, ""); r.Code != http.StatusOK {
		t.Fatalf("the rule's answer: %d %v", r.Code, r.Body)
	}
	q, ok, err := s.Lookup(card)
	if err != nil || !ok || q.Status != StatusAnswered || q.Answer.Option != "c" || q.Answer.By != "rule card-auto-answer v1" ||
		q.Answer.Channel != RuleChannel || q.Answer.Approval {
		t.Fatalf("the answer: %+v %+v %v %v", q, q.Answer, ok, err)
	}
	if r := s.AnswerAsRule(ctx, "agentrt", "rule card-auto-answer v1", card, "c", far, ""); r.Code != http.StatusConflict {
		t.Fatalf("a second answer: %d %v", r.Code, r.Body)
	}
	if open, err = s.RuleCards("agentrt"); err != nil || len(open) != 2 || open[0].ID != card || open[1].ID != bare {
		t.Fatalf("open after the answer: %+v %v", open, err)
	}
	// The Owner answers a thread's question on the console as before.
	raw, _ := json.Marshal(map[string]any{"option": "a", "channel": "console"})
	if r := s.Answer(ctx, "owner-console", "admin", "admin", other, raw); r.Code != http.StatusOK {
		t.Fatalf("the Owner's answer: %d %v", r.Code, r.Body)
	}
}

// Review 442 A2, B2: a rule's answer is written only while the rule holds and
// its caller still waits; one too late writes nothing. The rule's answer
// stays listed until the runtime acknowledges it (B1).
func TestAnswerAsRuleOnlyInTime(t *testing.T) {
	s := testSvc(t)
	opts := []map[string]string{{"label": "Give it one more round"}, {"label": "Do not do this task"}}
	r := ask(t, s, "agentrt", "k-card-recommends-a", map[string]any{"options": opts, "recommended": 1})
	card := r.Body.(map[string]any)["id"].(string)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if r := s.AnswerAsRule(gone, "agentrt", "rule card-auto-answer v1", card, "a", time.Now().Add(time.Hour), ""); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("after its caller gave up: %d %v", r.Code, r.Body)
	}
	if r := s.AnswerAsRule(context.Background(), "agentrt", "rule card-auto-answer v1", card, "a", time.Now().Add(-time.Second), ""); r.Code != http.StatusConflict {
		t.Fatalf("after the rule expired: %d %v", r.Code, r.Body)
	}
	if q, _, _ := s.Lookup(card); q.Status != StatusOpen {
		t.Fatalf("a late answer was written: %+v", q)
	}
	if r := s.AnswerAsRule(context.Background(), "agentrt", "rule card-auto-answer v1", card, "a", time.Now().Add(time.Hour), ""); r.Code != http.StatusOK {
		t.Fatalf("in time: %d %v", r.Code, r.Body)
	}
	if listed, err := s.RuleCards("agentrt"); err != nil || len(listed) != 1 || listed[0].Status != StatusAnswered {
		t.Fatalf("the rule's answer before the runtime read it: %+v %v", listed, err)
	}
	if r := s.Ack(context.Background(), "agentrt", "agentrt", "operator", card, nil); r.Code != http.StatusOK {
		t.Fatalf("ack: %d %v", r.Code, r.Body)
	}
	if listed, err := s.RuleCards("agentrt"); err != nil || len(listed) != 0 {
		t.Fatalf("after the runtime read it: %+v %v", listed, err)
	}
}

// Review 442 B3: a request may not name the runtime's own session to ask,
// withdraw or acknowledge: the rule takes the runtime's cards for its own.
func TestRuntimeSessionIsReserved(t *testing.T) {
	s := testSvc(t)
	for _, c := range []struct{ path, body string }{
		{"/questions", `{"idempotency_key":"k-forged-planner-question","title":"t","urgency":"u","meanwhile":"m"}`},
		{"/questions/q_0000000000000000/withdraw", `{"reason":"r"}`},
		{"/questions/q_0000000000000000/ack", ``},
	} {
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body))
		req.Header.Set(sessionHeader, RuntimeSession)
		w := httptest.NewRecorder()
		switch {
		case c.path == "/questions":
			s.HandleCreate(w, req)
		case strings.HasSuffix(c.path, "/withdraw"):
			s.HandleWithdraw(w, req)
		default:
			s.HandleAck(w, req)
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("%s as %s: %d %s", c.path, RuntimeSession, w.Code, w.Body.String())
		}
	}
}

// TD-393: a rule may take an option the question does not recommend only by
// saying why, and the answer keeps why as its text, on one line, bounded; it
// is still the rule's, on the rule channel.
func TestAnswerAsRuleSaysWhyItTakesAnother(t *testing.T) {
	s := testSvc(t)
	ctx := context.Background()
	opts := []map[string]string{{"label": "Start the mission"}, {"label": "Do not start it"}}
	r := ask(t, s, "agentrt", "k-mission-card-none", map[string]any{"title": "Mission: a walk", "options": opts})
	if r.Code != http.StatusCreated {
		t.Fatalf("create: %d %v", r.Code, r.Body)
	}
	card := r.Body.(map[string]any)["id"].(string)
	why := "the card recommends nothing:\n the rule takes the option that changes nothing " + strings.Repeat("x", 600)
	if r = s.AnswerAsRule(ctx, "agentrt", "rule card-auto-answer v3", card, "b", time.Now().Add(time.Hour), why); r.Code != http.StatusOK {
		t.Fatalf("the rule's answer: %d %v", r.Code, r.Body)
	}
	q, ok, err := s.Lookup(card)
	if err != nil || !ok || q.Answer.Option != "b" || q.Answer.By != "rule card-auto-answer v3" || q.Answer.Channel != RuleChannel ||
		!strings.HasPrefix(q.Answer.Text, "the card recommends nothing: the rule takes") || len(q.Answer.Text) > 500 || strings.Contains(q.Answer.Text, "\n") {
		t.Fatalf("the answer: %+v %v %v", q.Answer, ok, err)
	}
}

// TD-393: the runtime's cards that STG's rule answers are taken past the
// quotas, ten in a row, and leave the quotas of other requesters and of the
// runtime's own other cards as they were; nobody else may ask so.
func TestCreateForRuleTakesTheRuntimeCardsPastTheQuotas(t *testing.T) {
	s := testSvc(t)
	ctx := context.Background()
	for i := range 10 {
		raw := mustJSON(t, map[string]any{"idempotency_key": fmt.Sprintf("k-rule-card-%02d-padding", i), "title": fmt.Sprintf("Card %d", i),
			"urgency": "u", "meanwhile": "m"})
		if res := s.CreateForRule(ctx, RuntimeSession, RuntimeSession, "operator", raw); res.Code != http.StatusCreated {
			t.Fatalf("card %d: %d %v", i, res.Code, res.Body)
		}
	}
	raw := mustJSON(t, map[string]any{"idempotency_key": "k-thread-past-quota", "title": "A thread's", "urgency": "u", "meanwhile": "m"})
	if res := s.CreateForRule(ctx, "thread-a", "reporter", "reporter", raw); res.Code != http.StatusForbidden {
		t.Fatalf("a thread asked past the quotas: %d %v", res.Code, res.Body)
	}
	// Another requester's quota is untouched: the rule's cards left no
	// hourly marks.
	for i := range 3 {
		if res := ask(t, s, "thread-b", fmt.Sprintf("k-thread-b-%02d-padding", i), map[string]any{"title": fmt.Sprintf("B %d", i)}); res.Code != http.StatusCreated {
			t.Fatalf("thread-b's question %d: %d %v", i, res.Code, res.Body)
		}
	}
	// The runtime's own card through Create is held as before: ten of its
	// questions are open.
	if res := ask(t, s, RuntimeSession, "k-runtime-plain-padding", nil); res.Code != http.StatusTooManyRequests {
		t.Fatalf("a runtime card through Create: %d %v", res.Code, res.Body)
	}
}
