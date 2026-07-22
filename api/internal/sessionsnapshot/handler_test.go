package sessionsnapshot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestSnapshotHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("PLATFORM_SESSION_SNAPSHOT_PATH", filepath.Join(t.TempDir(), "latest.json"))
	return NewHandler()
}

func TestHandleLatestWithNoSnapshotYet(t *testing.T) {
	h := newTestSnapshotHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/session-snapshot/latest", nil)
	rec := httptest.NewRecorder()
	h.HandleLatest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["snapshot"] != nil {
		t.Fatalf("payload = %+v, want snapshot=nil", payload)
	}
}

func TestHandleSaveRejectsInvalidJSON(t *testing.T) {
	h := newTestSnapshotHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/session-snapshot", bytes.NewBufferString("not-json"))
	rec := httptest.NewRecorder()
	h.HandleSave(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSaveThenHandleLatestReturnsSavedPayload(t *testing.T) {
	h := newTestSnapshotHandler(t)

	saveReq := httptest.NewRequest(http.MethodPost, "/api/v1/session-snapshot", bytes.NewBufferString(`{"activeTab":"positions"}`))
	saveRec := httptest.NewRecorder()
	h.HandleSave(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("HandleSave status = %d, body=%s", saveRec.Code, saveRec.Body.String())
	}
	var saveResp map[string]any
	if err := json.Unmarshal(saveRec.Body.Bytes(), &saveResp); err != nil {
		t.Fatalf("unmarshal save: %v", err)
	}
	if saveResp["ok"] != true || saveResp["saved_at"] == "" {
		t.Fatalf("saveResp = %+v", saveResp)
	}

	latestReq := httptest.NewRequest(http.MethodGet, "/api/v1/session-snapshot/latest", nil)
	latestRec := httptest.NewRecorder()
	h.HandleLatest(latestRec, latestReq)

	var latestResp map[string]any
	if err := json.Unmarshal(latestRec.Body.Bytes(), &latestResp); err != nil {
		t.Fatalf("unmarshal latest: %v", err)
	}
	snapshot, ok := latestResp["snapshot"].(map[string]any)
	if !ok || snapshot["activeTab"] != "positions" {
		t.Fatalf("latestResp = %+v", latestResp)
	}
}
