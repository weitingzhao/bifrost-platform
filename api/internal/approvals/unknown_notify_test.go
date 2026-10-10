package approvals

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

func TestCreateTimeoutNotifiesUnknown(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("sweep_failed_backups", func(context.Context, map[string]any) (any, error) {
		return nil, actions.Uncertain("create timed out; outcome needs checking")
	})
	c := svc.create(context.Background(), "owner", "sweep_failed_backups", "sweep", "", nil)
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	r := chi.NewRouter()
	r.Post("/approvals/{id}/approve", svc.HandleApprove)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/approvals/"+c.Approval.ID+"/approve", bytes.NewReader(echoApproveBody(c.Approval, "console", nil))))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"unknown"`) {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	ds := deliveriesOf(t, svc, c.Approval.ID)
	got := f.got()
	if len(got) != 1 || !strings.Contains(got[0].Title, "unknown") || !strings.Contains(got[0].Message, "needs checking") {
		t.Fatalf("pushes = %+v", got)
	}
	if strings.Contains(got[0].Message, "Executor lost: no result after its lease lapsed") {
		t.Fatalf("push hid the create reason: %+v", got)
	}
	if len(ds) != 1 || ds[0].Kind != "unknown" {
		t.Fatalf("deliveries = %+v", ds)
	}
}
