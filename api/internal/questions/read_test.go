package questions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListEmptyStoreCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "questions")
	s := New(path, nil)
	res := s.List(context.Background(), "thread-a", "viewer", "viewer", "all", false)
	if res.Code != 200 {
		t.Fatalf("list = %d %v", res.Code, res.Body)
	}
	s.Sweep()
	missing := s.Get(context.Background(), "thread-a", "viewer", "viewer", "q_0123456789abcdef")
	if missing.Code != 404 {
		t.Fatalf("get = %d %v", missing.Code, missing.Body)
	}
	for _, name := range []string{path, path + ".lock"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
}

func TestGetPersistsExpiry(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", map[string]any{"ttl_hours": 1})
	id := created["id"].(string)
	s.SetClock(func() time.Time { return start.Add(time.Hour) })
	got := s.Get(context.Background(), "thread-a", "reporter", "reporter", id)
	q, ok := got.Body.(Question)
	if !ok || q.Status != StatusExpired {
		t.Fatalf("get = %d %v", got.Code, got.Body)
	}
	doc := loadDoc(t, s)
	if len(doc.Questions) != 1 || doc.Questions[0].Status != StatusExpired {
		t.Fatalf("stored = %+v", doc.Questions)
	}
	if !auditHas(t, s, "question.expire", id) {
		t.Fatal("expiry was not audited")
	}
}
