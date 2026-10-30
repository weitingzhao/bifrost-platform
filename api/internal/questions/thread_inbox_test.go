package questions

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func TestThreadInboxAnswersRetryAndRestart(t *testing.T) {
	t.Parallel()
	for _, ruled := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner", true: "rule"}[ruled], func(t *testing.T) {
			t.Parallel()
			s := testSvc(t)
			r := ask(t, s, "agentrt", "thread-inbox-question-key", map[string]any{
				"requester_thread": "local_question (W-31)",
				"options":          []map[string]string{{"label": "Proceed"}, {"label": "Wait"}}, "recommended": 1})
			if r.Code != 201 {
				t.Fatalf("create: %+v", r)
			}
			id := r.Body.(map[string]any)["id"].(string)
			answer := func() Result {
				if ruled {
					return s.AnswerAsRule(context.Background(), "agentrt", "rule card-auto-answer v1", id, "a", s.clock().Add(time.Hour), "")
				}
				return s.Answer(context.Background(), "console", "owner", "admin", id, []byte(`{"option":"a","channel":"console"}`))
			}
			if r := answer(); r.Code != 200 {
				t.Fatalf("answer: %+v", r)
			}
			if err := s.retryThreadInbox(context.Background(), func(context.Context, string, string, string) error {
				return errors.New("sender failed")
			}); err == nil {
				t.Fatal("sender failure hidden")
			}
			q, ok, err := s.Lookup(id)
			if err != nil || !ok || q.Status != StatusAnswered || q.Answer.Option != "a" {
				t.Fatalf("answer changed after sender failure: %+v %v", q, err)
			}
			if r := answer(); r.Code != 200 && !(ruled && r.Code == 409) {
				t.Fatalf("answer replay: %+v", r)
			}
			restarted := New(s.store.path, nil)
			var bodies []string
			send := dedupeInboxSender(func(_ context.Context, key, to, body string) error {
				if to != "thread:local_question" {
					t.Errorf("recipient=%s", to)
				}
				bodies = append(bodies, body)
				return nil
			})
			for i := 0; i < 2; i++ {
				if err := restarted.retryThreadInbox(context.Background(), send); err != nil {
					t.Fatal(err)
				}
			}
			if len(bodies) != 1 || !strings.Contains(bodies[0], "Option: a — Proceed") ||
				!strings.Contains(bodies[0], "An answer is not an approval.") ||
				!strings.Contains(bodies[0], "Channel: "+q.Answer.Channel) || !strings.Contains(bodies[0], "Answered by: "+q.Answer.By) || !strings.Contains(bodies[0], "#questions?id="+id) {
				t.Fatalf("bodies=%v", bodies)
			}
		})
	}
}

func TestThreadInboxIdleSweepBackfillsAndSkipsLabels(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "questions"), nil)
	for i, thread := range []string{"", "a label without an id", "local_a / local_b", "https://example.test/#local_a", "session-1 (see https://example.test/?next=thread:local_a)", "local_a"} {
		if err := s.store.update(func(doc *document) error {
			doc.Questions = append(doc.Questions, Question{ID: "q_" + strings.Repeat(string(rune('a'+i)), 16),
				Number: i + 1, Status: StatusAnswered, RequesterThread: thread, Answer: &Answer{Option: "a", By: "owner", At: time.Now()}})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	mail := make(chan string, 4)
	s.SetThreadInbox(dedupeInboxSender(func(_ context.Context, key, to, body string) error { mail <- to; return nil }))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safego.Go("test-question-inbox-loop", func() { defer close(done); s.RunSweep(ctx, time.Millisecond) })
	t.Cleanup(func() { cancel(); <-done })
	select {
	case to := <-mail:
		if to != "thread:local_a" {
			t.Fatalf("to=%s", to)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("idle sweep did not deliver")
	}
	cancel()
	<-done
	s.RetryThreadInbox(context.Background())
	if len(mail) != 0 {
		t.Fatal("duplicate or unparseable delivery")
	}
}

func TestThreadInboxPrunedQuestionsAreNotDelivered(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "questions"), nil)
	s.store.keepOverride, s.store.keep = true, 1
	if err := s.store.update(func(doc *document) error {
		doc.asOf = time.Now()
		doc.Questions = []Question{{ID: "q_aaaaaaaaaaaaaaaa", Number: 1, Status: StatusAnswered, RequesterThread: "local_a",
			Answer: &Answer{Option: "a", By: "owner", At: doc.asOf.Add(-8 * 24 * time.Hour)}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.store.keep = 0
	if err := s.store.update(func(doc *document) error { doc.asOf = time.Now(); return nil }); err != nil {
		t.Fatal(err)
	}
	doc, err := s.load()
	if err != nil || len(doc.Questions) != 0 {
		t.Fatalf("pruned document: %+v %v", doc, err)
	}
	var sent int
	if err := s.retryThreadInbox(context.Background(), func(context.Context, string, string, string) error { sent++; return nil }); err != nil || sent != 0 {
		t.Fatalf("pruned delivery=%d %v", sent, err)
	}
	doc, err = s.load()
	if err != nil {
		t.Fatalf("pruned record=%+v %v", doc, err)
	}
}

func TestThreadInboxDoesNotChangePublicQuestion(t *testing.T) {
	t.Parallel()
	q := Question{ID: "q_aaaaaaaaaaaaaaaa", Status: StatusAnswered, RequesterThread: "local_a", Answer: &Answer{By: "owner"}}
	doc := document{Questions: []Question{q}}
	raw, err := json.Marshal(doc.Questions[0].public(""))
	if err != nil || strings.Contains(string(raw), "thread_inbox") {
		t.Fatalf("private delivery metadata in public question: %s %v", raw, err)
	}
}
