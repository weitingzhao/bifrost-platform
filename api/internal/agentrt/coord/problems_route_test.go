package coord_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; without the runtime's database the route says so and reads nothing.
func TestProblemsRouteWithoutTheDatabase(t *testing.T) {
	rec := httptest.NewRecorder()
	coord.ProblemsRoute{DB: func() eventlog.DB { return nil }}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/bus/problems", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "not connected") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
