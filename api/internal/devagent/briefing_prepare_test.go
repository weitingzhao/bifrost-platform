package devagent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleBriefingPrepare(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	h := &Handler{repoRoot: tmp}

	body := BriefingPrepareRequest{
		SessionPack: "# Bifrost Session\nsession_id: abc\n",
		SessionID:   "abc",
		ProgramID:   "prog-1",
		PhaseID:     "briefing",
		Lane:        "console-api",
		Intent:      "ops",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/briefing/prepare", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	h.HandleBriefingPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp BriefingPrepareResponse
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &resp); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if resp.Status != "ready" {
		t.Fatalf("status=%q", resp.Status)
	}
	if resp.Path != briefingPackRelPath {
		t.Fatalf("path=%q", resp.Path)
	}

	packBytes, err := os.ReadFile(filepath.Join(tmp, "data", "briefing", "active-pack.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(packBytes), "session_id: abc") {
		t.Fatalf("pack content missing session: %s", packBytes)
	}

	metaBytes, err := os.ReadFile(filepath.Join(tmp, "data", "briefing", "active-meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta BriefingActiveMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.SessionID != "abc" || meta.ProgramID != "prog-1" || meta.PhaseID != "briefing" {
		t.Fatalf("meta=%+v", meta)
	}
	if meta.Lane != "console-api" || meta.Intent != "ops" {
		t.Fatalf("meta lane/intent=%+v", meta)
	}
	if meta.PreparedAt == "" {
		t.Fatal("prepared_at empty")
	}
}

func TestHandleBriefingPrepareRequiresPack(t *testing.T) {
	t.Parallel()
	h := &Handler{repoRoot: t.TempDir()}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/briefing/prepare", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	h.HandleBriefingPrepare(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAtomicWriteFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "file.txt")
	if err := atomicWriteFile(path, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}
