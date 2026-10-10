package approvals

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func TestClaimAuditRedactsExecutorID(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.json")
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(auditPath))
	rec := approveOwnerRun(t, svc)
	h := chi.NewRouter()
	h.Post("/approvals/claim", svc.HandleClaim)
	req := httptest.NewRequest(http.MethodPost, "/approvals/claim", strings.NewReader(`{"executor_id":"token=SYNTHETIC_SECRET","id":"`+rec.ID+`"}`))
	req = req.WithContext(actuation.WithPrincipal(req.Context(), actuation.Principal{Name: "owner", Role: actuation.RoleAdmin}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", rr.Code, rr.Body.String())
	}
	body, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "SYNTHETIC_SECRET") || !strings.Contains(string(body), "token=[redacted]") {
		t.Fatalf("audit = %s", body)
	}
}
