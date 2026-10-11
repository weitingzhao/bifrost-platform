package questions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Frozen shapes for slice 1B. Each file is one request or one response.
// Regenerate with UPDATE_CONTRACT=1 after an intentional contract change.
func TestFrozenContract(t *testing.T) {
	dir := filepath.Join("testdata", "contract")
	shapes := contractShapes(t)
	if os.Getenv("UPDATE_CONTRACT") == "1" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, v := range shapes {
			if err := os.WriteFile(filepath.Join(dir, name), pretty(t, v), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, v := range shapes {
		t.Run(name, func(t *testing.T) {
			got := pretty(t, v)
			want, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("missing golden %s (UPDATE_CONTRACT=1 writes it): %v", name, err)
			}
			if string(got) != string(want) {
				t.Fatalf("golden %s\n got %s\nwant %s", name, got, want)
			}
		})
	}
}

func contractShapes(t *testing.T) map[string]any {
	t.Helper()
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	req := map[string]any{
		"idempotency_key":  "0123456789abcdef",
		"title":            "Where is the quota?",
		"body":             "The thread is blocked on the number.",
		"options":          []any{map[string]any{"label": "Ship"}, map[string]any{"label": "Wait", "detail": "one day"}},
		"recommended":      2,
		"allow_text":       false,
		"urgency":          "Before tonight",
		"meanwhile":        "The thread waits",
		"work_id":          "W-65",
		"requester_thread": "quota thread",
		"link":             "https://example.com/docs",
		"link_text":        "Docs",
		"ttl_hours":        24,
	}
	s := contractSvc(t, start)
	created := s.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, req))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %v", created.Code, created.Body)
	}
	replay := s.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, req))
	openQ := s.Get(context.Background(), "thread-a", "reporter", "reporter", "Q12")
	other := s.Get(context.Background(), "thread-b", "viewer", "viewer", "12")
	answered := answer(t, s, "q_0123456789abcdef", map[string]any{"option": "b", "channel": "console"})
	again := answer(t, s, "Q12", map[string]any{"option": "a", "channel": "console"})
	answeredQ := s.Get(context.Background(), "thread-a", "reporter", "reporter", "Q12")
	inbox := s.List(context.Background(), "thread-a", "reporter", "reporter", "", true)

	withdrawnSvc := contractSvc(t, start)
	wreq := cloneMap(req)
	wreq["title"] = "Withdraw me"
	wd := withdrawnSvc.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, wreq))
	if wd.Code != http.StatusCreated {
		t.Fatal(wd.Body)
	}
	forbidden := withdrawnSvc.Withdraw(context.Background(), "thread-b", "operator", "operator", "Q12", mustJSON(t, map[string]any{"reason": "no"}))
	w := withdrawnSvc.Withdraw(context.Background(), "thread-a", "reporter", "reporter", "Q12", mustJSON(t, map[string]any{"reason": "asked in thread"}))
	late := answer(t, withdrawnSvc, "Q12", map[string]any{"option": "b", "channel": "console"})

	expSvc := contractSvc(t, start)
	ereq := cloneMap(req)
	ereq["title"] = "Expires"
	ereq["ttl_hours"] = 1
	if res := expSvc.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, ereq)); res.Code != http.StatusCreated {
		t.Fatal(res.Body)
	}
	expSvc.SetClock(func() time.Time { return start.Add(time.Hour) })
	expiredAnswer := answer(t, expSvc, "Q12", map[string]any{"option": "b", "channel": "console"})
	expiredQ := expSvc.Get(context.Background(), "thread-a", "reporter", "reporter", "q12")

	missing := s.Get(context.Background(), "thread-a", "reporter", "reporter", "q_0000000000000000")
	title := s.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, map[string]any{
		"idempotency_key": "abcdef0123456789", "title": repeat('a', 299), "urgency": "u", "meanwhile": "m",
	}))
	unknown := s.Create(context.Background(), "thread-a", "reporter", "reporter", []byte(`{"idempotency_key":"abcdef0123456789","title":"Hello","urgency":"u","meanwhile":"m","action":"ship"}`))
	reused := s.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, merge(req, map[string]any{"title": "Different title"})))
	unreadDup := s.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, merge(req, map[string]any{"idempotency_key": "fedcba9876543210"})))
	dupSvc := contractSvc(t, start)
	if res := dupSvc.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, req)); res.Code != http.StatusCreated {
		t.Fatal(res.Body)
	}
	dup := dupSvc.Create(context.Background(), "thread-a", "reporter", "reporter", mustJSON(t, merge(req, map[string]any{"idempotency_key": "fedcba9876543210"})))

	h, _ := newHTTP(t)
	unauth := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "viewer-test-token", "thread-a")
	noSession := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "reporter-test-token", "")
	tooBig := call(t, h, http.MethodPost, "/api/v1/questions", `{"title":"`+repeat('a', maxBodyBytes)+`"}`, "reporter-test-token", "thread-a")

	quota := testSvc(t)
	for i := 1; i <= 3; i++ {
		mustCreate(t, quota, "thread-a", keyN(i), map[string]any{"title": "open " + itoa(i)})
	}
	fourth := ask(t, quota, "thread-a", keyN(4), map[string]any{"title": "open 4"})

	return map[string]any{
		"create-request.json":                req,
		"create-response.json":               envelope(created.Code, created.Body),
		"replay-response.json":               envelope(replay.Code, replay.Body),
		"question-open.json":                 envelope(openQ.Code, openQ.Body),
		"question-open-other-session.json":   envelope(other.Code, other.Body),
		"answer-request.json":                map[string]any{"option": "b", "channel": "console"},
		"answer-response.json":               envelope(answered.Code, answered.Body),
		"question-answered.json":             envelope(answeredQ.Code, answeredQ.Body),
		"inbox.json":                         envelope(inbox.Code, inbox.Body),
		"question-withdrawn.json":            envelope(w.Code, w.Body),
		"question-expired.json":              envelope(expiredQ.Code, expiredQ.Body),
		"error-title.json":                   envelope(title.Code, title.Body),
		"error-unknown-field.json":           envelope(unknown.Code, unknown.Body),
		"error-session.json":                 envelope(noSession.Code, decodeBody(t, noSession.Body.Bytes())),
		"error-unauthorized.json":            envelope(unauth.Code, decodeBody(t, unauth.Body.Bytes())),
		"error-forbidden.json":               envelope(forbidden.Code, forbidden.Body),
		"error-not-found.json":               envelope(missing.Code, missing.Body),
		"error-not-open.json":                envelope(late.Code, late.Body),
		"error-expired.json":                 envelope(expiredAnswer.Code, expiredAnswer.Body),
		"error-already-answered.json":        envelope(again.Code, again.Body),
		"error-idempotency.json":             envelope(reused.Code, reused.Body),
		"error-duplicate.json":               envelope(dup.Code, dup.Body),
		"error-already-answered-unread.json": envelope(unreadDup.Code, unreadDup.Body),
		"error-body-too-large.json":          envelope(tooBig.Code, decodeBody(t, tooBig.Body.Bytes())),
		"error-quota-requester.json":         envelope(fourth.Code, fourth.Body),
	}
}

func contractSvc(t *testing.T, start time.Time) *Service {
	t.Helper()
	s := testSvc(t)
	s.SetClock(func() time.Time { return start })
	s.newID = func() (string, error) { return "q_0123456789abcdef", nil }
	if err := s.store.update(func(doc *document) error {
		doc.LastNumber = 11
		doc.asOf = start
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func envelope(status int, body any) map[string]any {
	return map[string]any{"status": status, "body": body}
}

func pretty(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeBody(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func merge(base, over map[string]any) map[string]any {
	out := cloneMap(base)
	for k, v := range over {
		out[k] = v
	}
	return out
}
