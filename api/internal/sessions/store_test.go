package sessions

import (
	"path/filepath"
	"testing"
)

func TestCreateGetValidate(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	store := NewStore(configDir)
	// Override via PLATFORM_DATA_DIR already used by NewStore — configDir/../data/sessions
	// NewStore joins configDir/../data — ensure path exists by using explicit env
	t.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	store = NewStore(configDir)

	rec, err := store.Create(CreateRequest{
		ProgramID: "prog-1",
		PhaseID:   "P1",
		LaneID:    "console-api",
		Pack:      "# pack\nsession",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.SessionID == "" || rec.PackHash == "" {
		t.Fatalf("missing id/hash: %+v", rec)
	}
	got, ok, err := store.Get(rec.SessionID)
	if err != nil || !ok || got.ProgramID != "prog-1" {
		t.Fatalf("get: ok=%v err=%v got=%+v", ok, err, got)
	}
	if err := store.ValidateProgressHook(rec.SessionID, "prog-1", "P1"); err != nil {
		t.Fatalf("validate ok: %v", err)
	}
	if err := store.ValidateProgressHook(rec.SessionID, "prog-1", "P2"); err == nil {
		t.Fatal("expected phase mismatch")
	}
	if err := store.ValidateProgressHook("", "prog-1", "P1"); err == nil {
		t.Fatal("expected empty session_id to be rejected")
	}
}
