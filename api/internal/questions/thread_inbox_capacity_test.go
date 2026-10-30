package questions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThreadInboxAnswerIsNotApproval(t *testing.T) {
	t.Parallel()
	for _, channel := range []string{"console", "rule"} {
		doc := document{Questions: []Question{{ID: "q_aaaaaaaaaaaaaaaa", Number: 1, Status: StatusAnswered,
			RequesterThread: "local_review", Options: []Option{{Key: "a", Label: "Proceed"}},
			Answer: &Answer{Option: "a", By: "owner", Channel: channel}}}}
		body := questionConclusion(doc.Questions[0])
		if !strings.Contains(body, "Option: a — Proceed") || !strings.Contains(body, "Channel: "+channel) {
			t.Fatal("answer content control failed")
		}
		if !strings.Contains(body, "An answer is not an approval.") {
			t.Errorf("%s answer lacks the authorization boundary", channel)
		}
	}
}

// Retain the original 727 near-capacity input; orphaned legacy delivery data
// must not affect the answer or be delivered without a business source.
func TestThreadInboxBacklogDoesNotBlockAnswer(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	q := Question{ID: "q_aaaaaaaaaaaaaaaa", Number: 1, Status: StatusOpen,
		Requester: "review", RequesterThread: "local_review", Title: "Proceed?",
		Options: []Option{{Key: "a", Label: "Proceed"}}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	doc := legacyInboxDocument{document: document{Questions: []Question{q}}, Inbox: map[string]legacyInboxEntry{}}
	entry := legacyInboxEntry{Record: "q_0000000000000000", To: "thread:local_review", Body: strings.Repeat("x", 400)}
	// Fill with bounded per-message entries, then trim the last entry to leave
	// room for an ordinary answer but not the additional inbox entry.
	for i := 0; i < 1000; i++ {
		e := entry
		e.Record = fmt.Sprintf("q_%016x", i)
		e.Order = uint64(i + 1)
		doc.Inbox["question:"+e.Record+":answered"] = e
	}
	for {
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= maxDocBytes-300 {
			extra := len(raw) - (maxDocBytes - 300)
			for i := 0; extra > 0; i++ {
				key := fmt.Sprintf("question:q_%016x:answered", i)
				e := doc.Inbox[key]
				n := min(extra, len(e.Body)-1)
				e.Body = e.Body[:len(e.Body)-n]
				doc.Inbox[key] = e
				extra -= n
			}
			break
		}
		i := len(doc.Inbox)
		e := entry
		e.Record = fmt.Sprintf("q_%016x", i)
		e.Order = uint64(i + 1)
		doc.Inbox["question:"+e.Record+":answered"] = e
	}
	for _, withRecipient := range []bool{false, true} {
		t.Run(fmt.Sprint(withRecipient), func(t *testing.T) {
			// Serial: both cases reuse the prepared document.
			if withRecipient {
				doc.Questions[0].RequesterThread = "local_review"
			} else {
				doc.Questions[0].RequesterThread = ""
			}
			raw, _ := json.Marshal(doc)
			path := filepath.Join(t.TempDir(), "questions")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			svc := New(path, nil)
			svc.SetClock(func() time.Time { return now })
			res := svc.Answer(context.Background(), "console", "owner", "admin", q.ID, []byte(`{"option":"a","channel":"console"}`))
			saved, found, err := svc.Lookup(q.ID)
			t.Logf("withRecipient=%t queued=%d inputBytes=%d code=%d status=%s found=%t err=%v body=%v", withRecipient, len(doc.Inbox), len(raw), res.Code, saved.Status, found, err, res.Body)
			if res.Code != 200 || !found || err != nil || saved.Status != StatusAnswered {
				t.Fatal("delivery backlog changed the answer or its retention")
			}
			for i := 0; i < 3; i++ {
				if err := svc.retryThreadInbox(context.Background(), func(context.Context, string, string, string) error { return errors.New("PG unavailable") }); (err != nil) != withRecipient {
					t.Fatal("outage did not follow the retained source")
				}
			}
			restarted := New(path, nil)
			mail := map[string]string{}
			send := dedupeInboxSender(func(_ context.Context, key, to, body string) error {
				if to != "thread:local_review" {
					t.Errorf("recipient=%q", to)
				}
				if _, ok := mail[key]; ok {
					t.Errorf("duplicate after restart: %s", key)
				}
				mail[key] = body
				return nil
			})
			for i := 0; i < 2; i++ {
				if err := restarted.retryThreadInbox(context.Background(), send); err != nil {
					t.Fatal(err)
				}
			}
			want := 0
			if withRecipient {
				want++
			}
			if len(mail) != want {
				t.Fatalf("recovered=%d want=%d", len(mail), want)
			}
			if withRecipient && !strings.Contains(mail["question:"+q.ID+":answered"], "An answer is not an approval.") {
				t.Fatal("new answer missing its non-authorization boundary")
			}

		})
	}
}

func TestThreadInboxScanCannotBlockBusiness(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "questions"), nil)
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })
	doc := document{Questions: []Question{{ID: "q_aaaaaaaaaaaaaaaa", Number: 1, Status: StatusAnswered,
		RequesterThread: "local_review", Answer: &Answer{Option: "a", At: now, By: "owner"}}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	s.store.docLimit = len(raw) + 1 // business fits; even a done receipt does not
	if err := os.WriteFile(s.store.path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var body string
		if err := s.retryThreadInbox(context.Background(), func(_ context.Context, _, _ string, text string) error { body = text; return nil }); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "An answer is not an approval.") {
			t.Fatal("scan did not reconstruct the retained source")
		}
		saved, found, err := s.Lookup(doc.Questions[0].ID)
		if err != nil || !found || saved.Status != StatusAnswered {
			t.Fatal("scan consumed the answer's capacity")
		}
		stored, err := os.ReadFile(s.store.path)
		if err != nil || len(stored) > s.store.docLimit {
			t.Fatalf("budget exceeded: %d %v", len(stored), err)
		}
	}
}

func TestThreadInboxOutageAcrossRepeatedSourcePruning(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "questions")
	s := New(path, nil)
	s.store.keepOverride, s.store.keep = true, 1
	now := time.Now().UTC()
	for i := 0; i < 40; i++ {
		now = now.Add(8 * 24 * time.Hour)
		s.SetClock(func() time.Time { return now })
		q := Question{ID: fmt.Sprintf("q_%016x", i), Number: i + 1, Status: StatusOpen, Requester: "test",
			RequesterThread: "local_review", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Options: []Option{{Key: "a", Label: "Proceed"}}}
		if err := s.store.update(func(doc *document) error { doc.asOf = now; doc.Questions = append(doc.Questions, q); return nil }); err != nil {
			t.Fatal(err)
		}
		if r := s.Answer(context.Background(), "console", "owner", "admin", q.ID, []byte(`{"option":"a","channel":"console"}`)); r.Code != 200 {
			t.Fatalf("answer %d: %+v", i, r)
		}
		if err := s.retryThreadInbox(context.Background(), func(context.Context, string, string, string) error { return errors.New("PG offline") }); err == nil {
			t.Fatal("outage hidden")
		}
		if saved, found, err := s.Lookup(q.ID); err != nil || !found || saved.Status != StatusAnswered {
			t.Fatalf("latest answer lost: %d", i)
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) > 4096 {
			t.Fatalf("backlog grew business file: %d %v", len(raw), err)
		}
	}
	restarted := New(path, nil)
	mail := map[string]string{}
	for i := 0; i < 2; i++ {
		if err := restarted.retryThreadInbox(context.Background(), func(_ context.Context, key, to, body string) error {
			if old, ok := mail[key]; ok && old != body {
				t.Errorf("unstable conclusion %s", key)
			}
			if to != "thread:local_review" || !strings.Contains(body, "Option: a — Proceed") {
				t.Error("lost source content")
			}
			mail[key] = body
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(mail) != 2 {
		t.Fatalf("recovered %d; expected only the two retained answers", len(mail))
	}
}

func TestThreadInboxRebuildsLegacyRecipientFromSource(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"https://example.test/?next=local_victim", "session-1 (do not notify thread:local_victim)", "local_requester (TD-457)"} {
		s := New(filepath.Join(t.TempDir(), "questions"), nil)
		doc := legacyInboxDocument{document: document{Questions: []Question{{ID: "q_aaaaaaaaaaaaaaaa", Status: StatusAnswered, RequesterThread: label, Answer: &Answer{By: "owner"}}}}, Inbox: map[string]legacyInboxEntry{"question:q_aaaaaaaaaaaaaaaa:answered": {Record: "q_aaaaaaaaaaaaaaaa", To: "thread:local_victim", Body: "stale"}}}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.store.path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var calls int
		if err := s.retryThreadInbox(context.Background(), func(_ context.Context, key, to, body string) error {
			calls++
			if to != "thread:local_requester" || body == "stale" {
				t.Errorf("legacy misroute survived: to=%q body=%q", to, body)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if (calls == 1) != (label == "local_requester (TD-457)") {
			t.Fatalf("label=%q calls=%d", label, calls)
		}
	}
}
