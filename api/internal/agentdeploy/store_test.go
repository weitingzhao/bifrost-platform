package agentdeploy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsLastJob(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.json")
	t.Setenv("PLATFORM_AGENT_DEPLOY_LAST", path)

	s1 := NewStore()
	job, ok := s1.Start("job-1", "vision@192.168.10.50", "primary")
	if !ok || job == nil {
		t.Fatal("expected start ok")
	}
	finished := s1.Finish(0, "")
	if finished == nil || finished.Status != "done" {
		t.Fatalf("expected done job, got %+v", finished)
	}

	s2 := NewStore()
	last := s2.Last()
	if last == nil {
		t.Fatal("expected last job loaded from disk")
	}
	if last.ID != "job-1" || last.Status != "done" || last.Role != "primary" {
		t.Fatalf("unexpected last: %+v", last)
	}
}

func TestStoreLoadLastIgnoresCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.json")
	t.Setenv("PLATFORM_AGENT_DEPLOY_LAST", path)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore()
	if s.Last() != nil {
		t.Fatal("expected nil last for corrupt file")
	}
}

func TestStoreFinishWithoutStart(t *testing.T) {
	s := NewStore()
	if got := s.Finish(0, ""); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	_ = time.Now() // keep time import used if we add timing tests later
}
