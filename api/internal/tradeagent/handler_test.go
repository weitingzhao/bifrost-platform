package tradeagent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
