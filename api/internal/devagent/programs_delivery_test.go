package devagent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/operatequeue"
)

func TestHandleApprovePostCompletionInjectsOperateQueue(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })

	store := NewFileStore(configDir)
	pending := []PostCompletionItem{{
		ID: "pc-1", ProgramID: "test-program", Title: "Operate handoff",
		Description: "Follow-up ops work", Status: "pending_review", CreatedAt: "2026-07-07T00:00:00Z",
		SourceLaneID: "delivery", OperateLane: "governance", HandoffKind: "recurring_setup",
		Reason: "Own the recurring check", AgentTaskID: "ops",
		AcceptanceCriteria: []string{"Schedule is owned"}, VerificationSteps: []string{"Run once"},
		RiskLevel: "medium", Owner: "platform", DueAt: "2026-08-01T00:00:00Z",
	}}
	if err := store.SavePendingPostCompletion(pending); err != nil {
		t.Fatal(err)
	}

	h := &Handler{store: store}
	h.operateQueue = operatequeue.NewHandler(configDir, actuation.NewAuditLog(""))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/programs/post-completion/pc-1/approve", bytes.NewReader([]byte(`{}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("itemId", "pc-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.HandleApprovePostCompletionItem(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "approved" {
		t.Fatalf("status = %v", resp["status"])
	}
	queueRaw, ok := resp["operate_queue_item"].(map[string]any)
	if !ok {
		t.Fatalf("missing operate_queue_item: %+v", resp)
	}
	if queueRaw["status"] != "open" || queueRaw["program_id"] != "test-program" {
		t.Fatalf("queue item = %+v", queueRaw)
	}

	list, err := h.operateQueue.Store().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 1 || list.Open[0].PendingID != "pc-1" {
		t.Fatalf("store open = %+v", list.Open)
	}
	got := list.Open[0]
	if got.SourceLaneID != "delivery" || got.OperateLane != "governance" ||
		got.HandoffKind != "recurring_setup" || got.AgentTaskID != "ops" ||
		len(got.AcceptanceCriteria) != 1 || len(got.VerificationSteps) != 1 {
		t.Fatalf("structured fields not injected: %+v", got)
	}
}

func TestRejectDoesNotInjectQueue(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })
	store := NewFileStore(configDir)
	if err := store.SavePendingPostCompletion([]PostCompletionItem{{
		ID: "reject-1", ProgramID: "p", Title: "Reject", Status: "pending_review",
		CreatedAt: "2026-07-07T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{store: store, operateQueue: operatequeue.NewHandler(configDir, actuation.NewAuditLog(""))}
	req := requestWithParam(http.MethodPost, "/reject", `{"reason":"Not an operational responsibility"}`, "itemId", "reject-1")
	rec := httptest.NewRecorder()
	h.HandleRejectPostCompletionItem(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	list, err := h.operateQueue.Store().List()
	if err != nil || len(list.Open) != 0 {
		t.Fatalf("reject injected queue: %+v err=%v", list.Open, err)
	}
	items, _ := store.LoadPendingPostCompletion()
	if items[0].Status != "rejected" {
		t.Fatalf("status=%s", items[0].Status)
	}
}

func TestNoHandoffDistinctFromNotAssessed(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })
	h := &Handler{
		store: NewFileStore(configDir),
		runtimes: map[string]*programRuntime{
			"p": {
				blueprint: &ProgramBlueprint{ID: "p", Title: "Program"},
				state: &ProgramStateRecord{
					ProgramID: "p", PostCompletion: &PostCompletionState{AssessmentStatus: "not_assessed"},
				},
			},
		},
	}
	req := requestWithParam(http.MethodPost, "/no-handoff", `{"reason":"Pure UI change"}`, "programId", "p")
	rec := httptest.NewRecorder()
	h.HandleNoPostCompletionHandoff(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := h.runtimes["p"].state.PostCompletion; got.AssessmentStatus != "no_handoff" ||
		got.NoHandoffReason != "Pure UI change" {
		t.Fatalf("assessment=%+v", got)
	}
}

func TestLegacyHandoffBlueprintNormalizesForStructuredQueue(t *testing.T) {
	item := normalizeHandoffBlueprint(OperateQueueItemBlueprint{
		ID: "legacy", Title: "Legacy follow-up", Description: "Keep observing",
	}, "delivery", "")
	if err := validateHandoffBlueprint(item); err != nil {
		t.Fatalf("legacy handoff should remain readable/submittable: %v", err)
	}
	if item.OperateLane != "governance" || item.HandoffKind != "one_off" ||
		item.RiskLevel != "low" || len(item.AcceptanceCriteria) == 0 ||
		len(item.VerificationSteps) == 0 {
		t.Fatalf("legacy defaults missing: %+v", item)
	}
}

func TestQueueLifecycleWritesProgramAssessmentAndEvidence(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { os.Unsetenv("PLATFORM_DATA_DIR") })
	store := NewFileStore(configDir)
	if err := store.SavePendingPostCompletion([]PostCompletionItem{{
		ID: "pending", ProgramID: "p", Title: "Handoff", Status: "approved",
		CreatedAt: "2026-07-07T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{
		store: store,
		runtimes: map[string]*programRuntime{
			"p": {
				blueprint: &ProgramBlueprint{ID: "p", Title: "Program"},
				state:     &ProgramStateRecord{ProgramID: "p", PostCompletion: &PostCompletionState{}},
			},
		},
	}
	h.OnOperateQueueExecution(operatequeue.Item{
		ProgramID: "p", PendingID: "pending", ExecutionJobID: "job-1",
		UpdatedAt: "2026-07-08T00:00:00Z",
	})
	if got := h.runtimes["p"].state.PostCompletion.AssessmentStatus; got != "in_operate" {
		t.Fatalf("assessment after execution=%s", got)
	}
	h.OnOperateQueueClosed(operatequeue.Item{
		ProgramID: "p", PendingID: "pending", ExecutionJobID: "job-1",
		CompletionEvidence: []string{"operator: verified"}, ClosedAt: "2026-07-09T00:00:00Z",
	})
	items, err := store.LoadPendingPostCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != "closed" || items[0].ExecutionJobID != "job-1" ||
		len(items[0].CompletionEvidence) != 1 {
		t.Fatalf("closed item=%+v", items[0])
	}
	if got := h.runtimes["p"].state.PostCompletion.AssessmentStatus; got != "closed" {
		t.Fatalf("assessment after close=%s", got)
	}
}

func requestWithParam(method, path, body, key, value string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
