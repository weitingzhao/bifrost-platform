package instruments

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; no Gitea is a 503 that says so, and a read answers the report.
func TestRouteAnswersTheReport(t *testing.T) {
	get := func(rt Route) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/governance/instruments", nil))
		return rec
	}
	if rec := get(Route{Service: func() *Service { return nil }}); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no service: %d", rec.Code)
	}
	svc := New(newFake())
	rec := get(Route{Service: func() *Service { return svc }})
	var report Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("%d %v: %s", rec.Code, err, rec.Body)
	}
}
