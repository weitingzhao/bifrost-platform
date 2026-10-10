package approvals

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// A running lease past grace has not been swept. The result route stores
// unknown, answers 409, and still notifies. The same request again does not.
func TestResultRefusalPastGraceNotifiesUnknownOnce(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Second) })

	r := chi.NewRouter()
	r.Post("/approvals/{id}/result", svc.HandleResult)
	body := `{"lease_id":"` + lease + `","started":false,"refusal":"kube API unreachable"}`
	first := postApproval(r, "/approvals/"+rec.ID+"/result", body)
	if first.Code != http.StatusConflict {
		t.Fatalf("first result = %d %s", first.Code, first.Body.String())
	}
	got, ok := svc.find(rec.ID)
	if !ok || got.Status != StatusUnknown {
		t.Fatalf("stored status = %q ok=%v", got.Status, ok)
	}
	if unknownNotices(t, svc, rec.ID, f) != 1 {
		t.Fatal("expected one approval.unknown notification")
	}

	second := postApproval(r, "/approvals/"+rec.ID+"/result", body)
	if second.Code != http.StatusConflict {
		t.Fatalf("second result = %d %s", second.Code, second.Body.String())
	}
	if n := unknownNotices(t, svc, rec.ID, f); n != 1 {
		t.Fatalf("second request produced %d unknown notifications", n)
	}
}

func unknownNotices(t *testing.T, svc *Service, id string, f *fakeRelay) int {
	t.Helper()
	ds := deliveriesOf(t, svc, id)
	nDel, nPush := 0, 0
	for _, d := range ds {
		if d.Kind == "unknown" {
			nDel++
		}
	}
	for _, p := range f.got() {
		if strings.Contains(p.Title, "unknown") {
			nPush++
		}
	}
	if nDel != nPush {
		t.Fatalf("unknown deliveries=%d pushes=%d deliveries=%+v pushes=%+v", nDel, nPush, ds, f.got())
	}
	return nDel
}
