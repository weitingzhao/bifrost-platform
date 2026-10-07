package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TD-203 / TD-208: the SSH console and the husbandry sync (which starts
// full-auto remediation) answered anyone who could reach the port.
func TestShellAndRemediationRoutesNeedAToken(t *testing.T) {
	srv, err := New(newTestConfig(t))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	router := srv.Router()
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/console/ws-ticket?node=node-a"},
		{http.MethodPost, "/api/v1/checklist/husbandry-sync"},
		{http.MethodGet, "/api/v1/console/ws?node=node-a"},
		{http.MethodGet, "/api/v1/cluster/workloads/pods/data/bifrost-postgres-1/logs"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Origin", "http://127.0.0.1:5180")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d, want 401", c.method, c.path, rec.Code)
		}
	}
}
