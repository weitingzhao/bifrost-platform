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

func TestStoreClose(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })

	store := NewStore(configDir)
	item, err := store.Add(Item{
		ID: "q-close", ProgramID: "p1", Title: "Resolve me", Status: StatusOpen,
		CreatedAt: "2026-07-07T00:00:00Z", Source: SourceManual,
	})
	if err != nil {
		t.Fatal(err)
	}

	closed, err := store.Close(item.ID, CloseRequest{
		CompletionEvidence: []string{"operator: verified manually"},
	}, false)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.Status != StatusClosed || closed.ClosedAt == "" {
		t.Fatalf("expected closed item, got %+v", closed)
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 0 {
		t.Fatalf("expected 0 open, got %d", len(list.Open))
	}
	if len(list.RecentClosed) != 1 {
		t.Fatalf("expected 1 recent_closed, got %d", len(list.RecentClosed))
	}

	again, err := store.Close(item.ID, CloseRequest{
		CompletionEvidence: []string{"operator: verified manually"},
	}, false)
	if err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if again.Status != StatusClosed {
		t.Fatalf("idempotent close status = %q", again.Status)
	}
}

func TestStoreDismissSkipsJobGates(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })

	store := NewStore(configDir)
	item, err := store.Add(Item{
		ID: "q-dismiss", ProgramID: "p1", Title: "Stale handoff", Status: StatusOpen,
		CreatedAt: "2026-07-07T00:00:00Z", Source: SourceManual,
		ExecutionJobID: "job-still-running",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close would fail without job done; Dismiss must succeed with evidence.
	if _, err := store.Close(item.ID, CloseRequest{
		CompletionEvidence: []string{"operator: tried close"},
	}, false); err == nil {
		t.Fatal("expected Close to require completed execution job")
	}

	closed, err := store.Dismiss(item.ID, DismissRequest{
		CompletionEvidence: []string{"operator: fleet already clean; handoff stale"},
		Reason:             "stale",
	})
	if err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if closed.Status != StatusClosed {
		t.Fatalf("expected closed, got %+v", closed)
	}
	if !hasEvidence(closed.CompletionEvidence, "dismiss:stale") {
		t.Fatalf("expected dismiss:stale tag, got %+v", closed.CompletionEvidence)
	}
}

func TestLegacyJSONLoadsWithDefaults(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	dataDir := filepath.Join(dir, "data")
	os.Setenv("PLATFORM_DATA_DIR", dataDir)
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })
	path := filepath.Join(dataDir, "operate", "queue.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"items":[{"id":"legacy","program_id":"p","lane":"release","title":"Legacy","status":"open","created_at":"2026-01-01T00:00:00Z"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := NewStore(configDir).List()
	if err != nil {
		t.Fatal(err)
	}
	got := list.Open[0]
	if got.OperateLane != "release" || got.HandoffKind != HandoffOneOff || got.RiskLevel != RiskLow {
		t.Fatalf("legacy normalization failed: %+v", got)
	}
}

func TestStructuredValidationRejectsInvalidEnumsAndTask(t *testing.T) {
	base := EnqueueOperateQueueRequestForTest()
	base.HandoffKind = "forever"
	if err := ValidateStructuredHandoff(base); err == nil {
		t.Fatal("expected invalid handoff_kind")
	}
	base = EnqueueOperateQueueRequestForTest()
	base.AgentTaskID = "not-in-catalog"
	if err := ValidateStructuredHandoff(base); err == nil {
		t.Fatal("expected invalid agent_task_id")
	}
}

func TestExecutionAndVerifiedClose(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })
	store := NewStore(configDir)
	item, err := store.Add(Item{
		ID: "execution", ProgramID: "p", Title: "Execute", Status: StatusOpen,
		HandoffKind: HandoffOneOff, CreatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err = store.RecordExecution(item.ID, "job-1")
	if err != nil || item.ExecutionJobID != "job-1" {
		t.Fatalf("RecordExecution: item=%+v err=%v", item, err)
	}
	req := CloseRequest{CompletionEvidence: []string{"job:job-1", "post_fix_verification:passed"}}
	if _, err := store.Close(item.ID, req, false); err == nil {
		t.Fatal("expected incomplete job rejection")
	}
	if _, err := store.Close(item.ID, req, true); err == nil {
		t.Fatal("expected post-fix verification rejection")
	}
	req.PostFixVerificationPassed = true
	closed, err := store.Close(item.ID, req, true)
	if err != nil || closed.ExecutionJobID != "job-1" || len(closed.CompletionEvidence) != 2 {
		t.Fatalf("verified close: item=%+v err=%v", closed, err)
	}
}

func TestRecurringSetupRequiresSetupEvidence(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })
	store := NewStore(configDir)
	item, err := store.Add(Item{
		ID: "recurring", ProgramID: "p", Title: "Schedule", Status: StatusOpen,
		HandoffKind: HandoffRecurringSetup, CreatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Close(item.ID, CloseRequest{CompletionEvidence: []string{"checked"}}, false); err == nil {
		t.Fatal("expected recurring evidence rejection")
	}
	if _, err := store.Close(item.ID, CloseRequest{CompletionEvidence: []string{"schedule: nightly-health verified"}}, false); err != nil {
		t.Fatalf("expected recurring close success: %v", err)
	}
}

func EnqueueOperateQueueRequestForTest() EnqueueRequest {
	return EnqueueRequest{
		ProgramID: "p", Title: "Handoff", OperateLane: "governance",
		HandoffKind: HandoffOneOff, Reason: "reason",
		AcceptanceCriteria: []string{"accepted"}, VerificationSteps: []string{"verified"},
		RiskLevel: RiskLow,
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
