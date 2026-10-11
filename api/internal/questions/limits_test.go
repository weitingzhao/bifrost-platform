package questions

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

func TestCapacityDropsOrdinaryAndRefusesWhenProtected(t *testing.T) {
	s := testSvc(t)
	var ids []string
	for i := 1; i <= 3; i++ {
		created := mustCreate(t, s, "thread-a", keyN(i), map[string]any{"title": "t" + itoa(i)})
		ids = append(ids, created["id"].(string))
		if res := s.Withdraw(context.Background(), "thread-a", "reporter", "reporter", created["id"].(string), mustJSON(t, map[string]any{"reason": "old"})); res.Code != http.StatusOK {
			t.Fatal(res.Body)
		}
	}
	before := readRaw(t, s)
	s.store.docLimit = len(before)
	created := mustCreate(t, s, "thread-b", keyN(9), map[string]any{"title": "new"})
	if created["status"] != StatusOpen {
		t.Fatal(created)
	}
	if hasID(t, s, ids[0]) {
		t.Fatal("oldest terminal was kept")
	}

	s2 := testSvc(t)
	kept := mustCreate(t, s2, "thread-a", keyN(1), nil)
	raw := readRaw(t, s2)
	s2.store.docLimit = len(raw)
	got := ask(t, s2, "thread-b", keyN(2), nil)
	if got.Code != http.StatusInsufficientStorage {
		t.Fatalf("over capacity = %d %v", got.Code, got.Body)
	}
	if after := readRaw(t, s2); after != raw {
		t.Fatal("over-capacity create changed the document")
	}
	if !hasID(t, s2, kept["id"].(string)) {
		t.Fatal("protected question was dropped")
	}
}

func TestMaxRecordFits(t *testing.T) {
	q := Question{
		ID: "q_0123456789abcdef", Number: 12, Status: StatusAnswered,
		Requester: repeat('a', sessionLimit), RequesterPrincipal: repeat('b', principalLimit),
		RequesterThread: fill(threadLimit), WorkID: "W-65",
		Title: fill(titleLimit), Body: fill(bodyLimit),
		Options: []Option{
			{Key: "a", Label: fill(labelLimit), Detail: fill(detailLimit)},
			{Key: "b", Label: fill(labelLimit), Detail: fill(detailLimit)},
			{Key: "c", Label: fill(labelLimit), Detail: fill(detailLimit)},
			{Key: "d", Label: fill(labelLimit), Detail: fill(detailLimit)},
		},
		Recommended: "b", AllowText: true,
		Urgency: fill(lineLimit), Meanwhile: fill(lineLimit),
		Link: "https://example.com/" + fill(linkLimit-len("https://example.com/")), LinkText: fill(linkTextLimit),
		CreatedAt:      time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC),
		IdempotencyKey: repeat('k', keyMax), RequestFP: repeat('e', 64), QuestionFP: repeat('f', 64),
		Answer: &Answer{
			Option: "none", Text: fill(bodyLimit), By: repeat('b', principalLimit),
			At: time.Date(2026, 10, 11, 7, 0, 0, 0, time.UTC), Channel: "console", Approval: false,
			Read: &ReadReceipt{At: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC), Session: repeat('a', sessionLimit), Principal: repeat('b', principalLimit)},
		},
		Deliveries: []Delivery{
			{Kind: repeat('k', 32), Channel: repeat('c', 16), Target: repeat('t', 64), Result: repeat('r', 16), Error: fill(300), At: time.Date(2026, 10, 11, 6, 1, 0, 0, time.UTC)},
			{Kind: repeat('k', 32), Channel: repeat('c', 16), Target: repeat('t', 64), Result: repeat('r', 16), Error: fill(300), At: time.Date(2026, 10, 11, 6, 1, 0, 0, time.UTC)},
			{Kind: repeat('k', 32), Channel: repeat('c', 16), Target: repeat('t', 64), Result: repeat('r', 16), Error: fill(300), At: time.Date(2026, 10, 11, 6, 1, 0, 0, time.UTC)},
			{Kind: repeat('k', 32), Channel: repeat('c', 16), Target: repeat('t', 64), Result: repeat('r', 16), Error: fill(300), At: time.Date(2026, 10, 11, 6, 1, 0, 0, time.UTC)},
		},
	}
	n, err := recordBytes(q)
	if err != nil {
		t.Fatal(err)
	}
	if n > maxRecordBytes {
		t.Fatalf("max record = %d bytes, maxRecordBytes = %d", n, maxRecordBytes)
	}
}

func TestNotifyIsAtMostOnce(t *testing.T) {
	s := testSvc(t)
	var posts int
	var mu sync.Mutex
	var last notice.Message
	s.post = func(_ context.Context, msg notice.Message) ([]notice.Delivery, error) {
		mu.Lock()
		posts++
		last = msg
		mu.Unlock()
		return []notice.Delivery{{Channel: "ntfy", Target: "phone", Result: notice.ResultAccepted}}, nil
	}
	mustCreate(t, s, "thread-a", "0123456789abcdef", nil)
	if posts != 1 || len(last.Title) < len("Answer · Q") || last.Title[:len("Answer · Q")] != "Answer · Q" {
		t.Fatalf("title %q posts %d", last.Title, posts)
	}
	if !contains(last.Message, "Answering approves nothing.") {
		t.Fatalf("body %q", last.Message)
	}
	ask(t, s, "thread-a", "0123456789abcdef", nil)
	if posts != 1 {
		t.Fatalf("replay posted %d", posts)
	}
	if res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"title": "Different"}); res.Code != http.StatusConflict {
		t.Fatal(res.Body)
	}
	if posts != 1 {
		t.Fatal("rejected create posted")
	}
	id := mustCreate(t, s, "thread-b", "abcdef0123456789", nil)["id"].(string)
	if res := answer(t, s, id, nil); res.Code != http.StatusOK {
		t.Fatal(res.Body)
	}
	if res := s.Withdraw(context.Background(), "thread-b", "reporter", "reporter", id, mustJSON(t, map[string]any{"reason": "no"})); res.Code == http.StatusOK {
		t.Fatal("withdraw of an answered question succeeded")
	}
	s.Sweep()
	if posts != 2 {
		t.Fatalf("posts after answer/sweep = %d, want 2", posts)
	}

	crashed := testSvc(t)
	crashed.post = s.post
	crashed.beforeNotify = func() error { return errors.New("crash") }
	posts = 0
	mustCreate(t, crashed, "thread-a", "0123456789abcdef", nil)
	if posts != 0 {
		t.Fatalf("crash still posted %d", posts)
	}
	crashed.beforeNotify = nil
	ask(t, crashed, "thread-a", "0123456789abcdef", nil)
	if posts != 0 {
		t.Fatalf("replay after crash posted %d", posts)
	}
}

func TestNotifySurvivesOneConflict(t *testing.T) {
	root := t.TempDir()
	b := &conflictBackend{data: map[string][]byte{}}
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	s := New(filepath.Join(root, "questions"), nil)
	var posts int
	s.post = func(context.Context, notice.Message) ([]notice.Delivery, error) {
		posts++
		return []notice.Delivery{{Channel: "ntfy", Target: "phone", Result: notice.ResultAccepted}}, nil
	}
	res := ask(t, s, "thread-a", "0123456789abcdef", nil)
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %v", res.Code, res.Body)
	}
	if b.doubled != 1 || posts != 1 {
		t.Fatalf("conflicted updates %d posts %d", b.doubled, posts)
	}
	if res.Body.(map[string]any)["id"] == nil {
		t.Fatal(res.Body)
	}
}

func TestLinksAreRejected(t *testing.T) {
	s := testSvc(t)
	for _, link := range []string{
		"#approvals?id=appr_1",
		"#maintenance?id=appr_1",
		"#Questions",
		"http://ops.bifrost.lan/#approvals?id=appr_1",
		"http://ops.bifrost.lan/#maintenance?id=1",
		"https://ops.bifrost.lan/#questions?id=q_0123456789abcdef",
	} {
		res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"link": link})
		if res.Code != http.StatusBadRequest || !contains(res.Body.(map[string]any)["error"].(string), "link") {
			t.Fatalf("link %q = %d %v", link, res.Code, res.Body)
		}
	}
	if res := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"link": "https://example.com/docs", "link_text": "Docs"}); res.Code != http.StatusCreated {
		t.Fatalf("ordinary link = %d %v", res.Code, res.Body)
	}
}

type conflictBackend struct {
	mu      sync.Mutex
	data    map[string][]byte
	doubled int
}

func (b *conflictBackend) Read(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), d...), nil
}

func (b *conflictBackend) Write(_ context.Context, key string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = append([]byte(nil), data...)
	return nil
}

func (b *conflictBackend) Update(_ context.Context, key string, mutate func([]byte) ([]byte, error)) error {
	b.mu.Lock()
	old := append([]byte(nil), b.data[key]...)
	double := b.doubled == 0
	b.mu.Unlock()
	if double {
		if _, err := mutate(old); err != nil {
			return err
		}
		b.mu.Lock()
		b.doubled++
		b.mu.Unlock()
	}
	next, err := mutate(append([]byte(nil), old...))
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.data[key] = append([]byte(nil), next...)
	b.mu.Unlock()
	return nil
}
