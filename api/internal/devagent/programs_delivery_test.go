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
}
