package operatequeue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAddAndList(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })

	store := NewStore(configDir)
	item, err := store.Add(Item{
		ID: "q-1", ProgramID: "wave-3b", Title: "Ship operate queue", Status: StatusOpen,
		CreatedAt: "2026-07-07T00:00:00Z", Source: SourceManual,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if item.ID != "q-1" {
		t.Fatalf("item id = %q", item.ID)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Open) != 1 || list.Open[0].ProgramID != "wave-3b" {
		t.Fatalf("open items = %+v", list.Open)
	}
	if list.RecentClosed == nil {
		t.Fatal("expected empty recent_closed slice")
	}
}

func TestStoreIdempotentPendingID(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })

	store := NewStore(configDir)
	first := NewItemFromApproval(ApprovalInjectParams{
		PendingID: "pending-1", ProgramID: "p1", Title: "Handoff", ApprovedBy: "owner",
	})
	saved, err := store.Add(first)
	if err != nil {
		t.Fatal(err)
	}
	dup := NewItemFromApproval(ApprovalInjectParams{
		PendingID: "pending-1", ProgramID: "p1", Title: "Handoff again", ApprovedBy: "owner",
	})
	again, err := store.Add(dup)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != saved.ID {
		t.Fatalf("expected idempotent pending_id, got %q vs %q", again.ID, saved.ID)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 1 {
		t.Fatalf("expected 1 open item, got %d", len(list.Open))
	}
}

func TestNewItemFromManualValidation(t *testing.T) {
	if _, err := NewItemFromManual(EnqueueRequest{}); err == nil {
		t.Fatal("expected validation error")
	}
	item, err := NewItemFromManual(EnqueueRequest{
		ProgramID: "manual-program", Title: "Manual enqueue", Lane: "governance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Lane != "governance" || item.Source != SourceManual {
		t.Fatalf("unexpected item: %+v", item)
	}
	if _, err := NewItemFromManual(EnqueueRequest{
		ProgramID: "p", Title: "x", Lane: "invalid-lane",
	}); err == nil {
		t.Fatal("expected invalid lane error")
	}
}
