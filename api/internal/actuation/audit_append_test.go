package actuation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleAppendHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	log := NewAuditLog(path)
	auth := &AuthService{
		principals: map[string]Principal{
			"satellite-token": {Name: "trade-satellite", Role: RoleOperator},
		},
	}
	handler := auth.Require(RoleOperator)(http.HandlerFunc(log.HandleAppend))

	body, _ := json.Marshal(map[string]string{
		"actor":  "operator",
		"action": "market_ingest_restart",
		"target": "ib_ingestor",
		"status": "success",
		"detail": "command_id=abc ip=10.0.0.1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/append", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer satellite-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["id"] == "" || resp["at"] == nil {
		t.Fatalf("expected id and at in response: %+v", resp)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.records) != 1 {
		t.Fatalf("records: got %d want 1", len(log.records))
	}
	if log.records[0].Action != "market_ingest_restart" || log.records[0].Actor != "operator" {
		t.Fatalf("unexpected record: %+v", log.records[0])
	}
}

func TestHandleAppendAuthDenied(t *testing.T) {
	log := NewAuditLog("")
	auth := &AuthService{
		principals: map[string]Principal{
			"viewer-token": {Name: "viewer", Role: RoleViewer},
		},
	}
	handler := auth.Require(RoleOperator)(http.HandlerFunc(log.HandleAppend))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/append", bytes.NewReader([]byte(`{"action":"a","target":"b","status":"ok"}`)))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleAppendMalformedBody(t *testing.T) {
	log := NewAuditLog("")
	auth := &AuthService{
		principals: map[string]Principal{
			"op": {Name: "operator", Role: RoleOperator},
		},
	}
	handler := auth.Require(RoleOperator)(http.HandlerFunc(log.HandleAppend))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/append", bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Authorization", "Bearer op")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleAppendMissingRequiredFields(t *testing.T) {
	log := NewAuditLog("")
	auth := &AuthService{
		principals: map[string]Principal{
			"op": {Name: "operator", Role: RoleOperator},
		},
	}
	handler := auth.Require(RoleOperator)(http.HandlerFunc(log.HandleAppend))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/append", bytes.NewReader([]byte(`{"action":"only-action"}`)))
	req.Header.Set("Authorization", "Bearer op")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleAppendUsesPrincipalWhenActorEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	log := NewAuditLog(path)
	auth := &AuthService{
		principals: map[string]Principal{
			"satellite-token": {Name: "trade-satellite-dev", Role: RoleOperator},
		},
	}
	handler := auth.Require(RoleOperator)(http.HandlerFunc(log.HandleAppend))

	body, _ := json.Marshal(map[string]string{
		"action": "ops_shutdown",
		"target": "process",
		"status": "scheduled",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/append", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer satellite-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.records[0].Actor != "trade-satellite-dev" {
		t.Fatalf("actor: got %q want trade-satellite-dev", log.records[0].Actor)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected audit file to be written")
	}
}
