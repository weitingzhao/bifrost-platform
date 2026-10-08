package agentdeploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoadsLastJob(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.json")
	t.Setenv("PLATFORM_AGENT_DEPLOY_LAST", path)
	raw := `{"id":"job-1","status":"done","remote":"vision@192.168.10.50","role":"primary","started_at":"2026-10-01T00:00:00Z","log":""}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	last := NewStore().Last()
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
