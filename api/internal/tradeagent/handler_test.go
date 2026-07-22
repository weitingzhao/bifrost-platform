package tradeagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDomains(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trade-agent/domains", nil)
	rec := httptest.NewRecorder()
	h.HandleDomains(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Domains     []DomainView `json:"domains"`
		DomainCount int          `json:"domain_count"`
		Mode        string       `json:"mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.DomainCount != len(Domains()) || payload.Mode != "read_only" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHandleCatalog(t *testing.T) {
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trade-agent/catalog", nil)
	rec := httptest.NewRecorder()
	h.HandleCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp CatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ServerName != ServerName || len(resp.Tools) == 0 {
		t.Fatalf("resp = %+v", resp)
	}
}
