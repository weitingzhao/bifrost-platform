package questions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
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

	nuls := strings.Repeat("\x00", 300)
	if encodedBytes(nuls) != 1802 {
		t.Fatalf("300 NUL characters encode to %d bytes, want 1802", encodedBytes(nuls))
	}
	worst := worstQuestion()
	n, err = recordBytes(worst)
	if err != nil {
		t.Fatal(err)
	}
	if n > maxRecordBytes {
		t.Fatalf("worst-case record = %d bytes, maxRecordBytes = %d", n, maxRecordBytes)
	}
	raw, err := marshalJSON(worst)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte{0}) || bytes.Contains(raw, []byte(`\u0000`)) {
		t.Fatal("a control character survived into the stored record")
	}
	for _, field := range worstFields(worst) {
		if encodedBytes(field.value) > field.limit {
			t.Fatalf("%s encodes to %d bytes, cap %d", field.name, encodedBytes(field.value), field.limit)
		}
		if !strings.Contains(field.value, "\"") || !strings.Contains(field.value, "\\") || !strings.Contains(field.value, "\U0001F600") {
			t.Fatalf("%s = %q, want a quote, a backslash, and a non-BMP character", field.name, field.value)
		}
	}
}

type cappedField struct {
	name  string
	value string
	limit int
}

func worstFields(q Question) []cappedField {
	out := []cappedField{
		{"requester_principal", q.RequesterPrincipal, principalLimit},
		{"requester_thread", q.RequesterThread, threadLimit},
		{"title", q.Title, titleLimit},
		{"body", q.Body, bodyLimit},
		{"urgency", q.Urgency, lineLimit},
		{"meanwhile", q.Meanwhile, lineLimit},
		{"link", q.Link, linkLimit},
		{"link_text", q.LinkText, linkTextLimit},
		{"answer.text", q.Answer.Text, bodyLimit},
		{"answer.by", q.Answer.By, principalLimit},
		{"answer.read.principal", q.Answer.Read.Principal, principalLimit},
		{"withdrawal.principal", q.Withdrawal.Principal, principalLimit},
		{"withdrawal.reason", q.Withdrawal.Reason, lineLimit},
	}
	for _, o := range q.Options {
		out = append(out,
			cappedField{"label", o.Label, labelLimit},
			cappedField{"detail", o.Detail, detailLimit},
		)
	}
	for _, d := range q.Deliveries {
		out = append(out,
			cappedField{"delivery.kind", d.Kind, 32},
			cappedField{"delivery.channel", d.Channel, 16},
			cappedField{"delivery.target", d.Target, 64},
			cappedField{"delivery.result", d.Result, 16},
			cappedField{"delivery.error", d.Error, 300},
		)
	}
	return out
}

func worstQuestion() Question {
	text := func(limit int) string {
		return clipEncoded(dirty(limit), limit)
	}
	principal := clipPrincipal(strings.Repeat("\x00", 300) + dirty(principalLimit))
	at := time.Date(2026, 10, 11, 6, 1, 0, 0, time.UTC)
	var relay []notice.Delivery
	for i := 0; i < maxDeliveries; i++ {
		relay = append(relay, notice.Delivery{
			Kind:    strings.Repeat("\x00", 300) + dirty(32),
			Channel: dirty(16),
			Target:  dirty(64),
			Result:  dirty(16),
			Error:   strings.Repeat("\x00", 300) + dirty(300),
			At:      at,
		})
	}
	return Question{
		ID: "q_0123456789abcdef", Number: 12, Status: StatusAnswered,
		Requester: repeat('a', sessionLimit), RequesterPrincipal: principal,
		RequesterThread: text(threadLimit), WorkID: "W-65",
		Title: text(titleLimit), Body: text(bodyLimit),
		Options: []Option{
			{Key: "a", Label: text(labelLimit), Detail: text(detailLimit)},
			{Key: "b", Label: text(labelLimit), Detail: text(detailLimit)},
			{Key: "c", Label: text(labelLimit), Detail: text(detailLimit)},
			{Key: "d", Label: text(labelLimit), Detail: text(detailLimit)},
		},
		Recommended: "b", AllowText: true,
		Urgency: text(lineLimit), Meanwhile: text(lineLimit),
		Link: text(linkLimit), LinkText: text(linkTextLimit),
		CreatedAt:      time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC),
		ExpiresAt:      time.Date(2026, 10, 12, 6, 0, 0, 0, time.UTC),
		IdempotencyKey: repeat('k', keyMax), RequestFP: repeat('e', 64), QuestionFP: repeat('f', 64),
		Answer: &Answer{
			Option: "none", Text: text(bodyLimit), By: principal,
			At: time.Date(2026, 10, 11, 7, 0, 0, 0, time.UTC), Channel: "console", Approval: false,
			Read: &ReadReceipt{At: time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC), Session: repeat('a', sessionLimit), Principal: principal},
		},
		Withdrawal: &Withdrawal{
			Session: repeat('a', sessionLimit), Principal: principal, Reason: text(lineLimit),
			At: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC),
		},
		Deliveries: clipDeliveries(relay, at),
	}
}

func dirty(limit int) string {
	var b strings.Builder
	b.WriteString("\x00\x01\x07\x1f\u007f\u0080\u009f\u202e\u200b\u2028\u2029\ufeff")
	unit := "\"\\\U0001F600"
	for b.Len() < limit*4+len(unit) {
		b.WriteString(unit)
	}
	return b.String()
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

func TestNotifyStoreFailureDoesNotAuditUncommittedExpiry(t *testing.T) {
	root := t.TempDir()
	b := &failUpdateBackend{data: map[string][]byte{}, failAt: 3}
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	s := New(filepath.Join(root, "questions"), actuation.NewAuditLog(filepath.Join(root, "audit.json")))
	s.post = acceptPost()
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	jumped := false
	s.SetClock(func() time.Time {
		if jumped {
			return start.Add(2 * time.Hour)
		}
		return start
	})
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", map[string]any{"ttl_hours": 1})
	id := created["id"].(string)
	if b.updates != 2 {
		t.Fatalf("updates before the failing notify = %d", b.updates)
	}
	before := append([]byte(nil), b.data["questions"]...)
	s.post = func(context.Context, notice.Message) ([]notice.Delivery, error) {
		jumped = true
		return []notice.Delivery{{
			Channel: "ntfy", Target: "phone", Result: notice.ResultAccepted,
			Error: strings.Repeat("\x00", 300),
		}}, nil
	}
	s.sendCreated(context.Background(), caller{
		session: "thread-a", principal: "reporter", role: "reporter", op: "notify",
	}, Question{ID: id, Number: 1, Title: "Where is the quota?"})

	if !bytes.Equal(b.data["questions"], before) {
		t.Fatal("failed update stored the delivery")
	}
	doc, err := parse(b.data["questions"])
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Questions) != 1 || doc.Questions[0].Status != StatusOpen {
		t.Fatalf("status after failed update = %+v", doc.Questions)
	}
	if doc.Questions[0].Deliveries[0].Error != "" || doc.Questions[0].Deliveries[0].Result != notice.ResultAccepted {
		t.Fatalf("delivery changed: %+v", doc.Questions[0].Deliveries)
	}
	recs := auditRecords(t, b.data["audit.json"])
	stores, expires, notifies := 0, 0, 0
	for _, rec := range recs {
		switch rec.Action {
		case "question.store":
			stores++
			if rec.Status != "error" || !strings.Contains(rec.Detail, "op=notify") || rec.Target != id {
				t.Fatalf("store audit = %+v", rec)
			}
		case "question.expire":
			expires++
		case "question.notify":
			notifies++
		}
	}
	if stores != 1 || expires != 0 || notifies != 1 {
		t.Fatalf("store=%d expire=%d notify=%d, want 1, 0, 1", stores, expires, notifies)
	}

	s.Sweep()
	doc, err = parse(b.data["questions"])
	if err != nil {
		t.Fatal(err)
	}
	if doc.Questions[0].Status != StatusExpired {
		t.Fatalf("sweep after the failed update left status %s", doc.Questions[0].Status)
	}
	if !auditHasAction(auditRecords(t, b.data["audit.json"]), "question.expire", id) {
		t.Fatal("committed expiry was not audited")
	}
}

type auditRec struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func auditRecords(t *testing.T, raw []byte) []auditRec {
	t.Helper()
	var recs []auditRec
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatal(err)
	}
	return recs
}

func auditHasAction(recs []auditRec, action, id string) bool {
	for _, rec := range recs {
		if rec.Action == action && rec.Target == id {
			return true
		}
	}
	return false
}

type failUpdateBackend struct {
	mu      sync.Mutex
	data    map[string][]byte
	updates int
	failAt  int
}

func (b *failUpdateBackend) Read(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), d...), nil
}

func (b *failUpdateBackend) Write(_ context.Context, key string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = append([]byte(nil), data...)
	return nil
}

func (b *failUpdateBackend) Update(_ context.Context, key string, mutate func([]byte) ([]byte, error)) error {
	b.mu.Lock()
	b.updates++
	n := b.updates
	old := append([]byte(nil), b.data[key]...)
	b.mu.Unlock()
	next, err := mutate(old)
	if err != nil {
		return err
	}
	if n == b.failAt {
		return errors.New("update failed")
	}
	b.mu.Lock()
	b.data[key] = append([]byte(nil), next...)
	b.mu.Unlock()
	return nil
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
