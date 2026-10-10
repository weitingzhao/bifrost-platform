package approvals

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func approvalRoutes(svc *Service) http.Handler {
	r := chi.NewRouter()
	r.Post("/approvals/{id}/heartbeat", svc.HandleHeartbeat)
	r.Post("/approvals/{id}/result", svc.HandleResult)
	return r
}

func postApproval(h http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The handlers enforce lease expiry themselves. Nothing here calls get, which
// would sweep before the write under test.
func TestLeaseExpiryInsideTheWrite(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)
	stored, ok := svc.find(rec.ID)
	if !ok || stored.Execution == nil {
		t.Fatal("claimed record missing")
	}
	expires := stored.Execution.LeaseExpiresAt
	h := approvalRoutes(svc)

	inside := expires.Add(time.Minute)
	if !inside.After(expires) || inside.After(expires.Add(unknownGrace)) {
		t.Fatalf("fixture clock %s is not inside the grace after %s", inside, expires)
	}
	svc.SetClock(func() time.Time { return inside })
	hb := postApproval(h, "/approvals/"+rec.ID+"/heartbeat", `{"lease_id":"`+lease+`"}`)
	if hb.Code != http.StatusOK {
		t.Fatalf("renew inside grace = %d %s", hb.Code, hb.Body.String())
	}
	renewed, _ := svc.find(rec.ID)
	if renewed.Status != StatusRunning || !renewed.Execution.LeaseExpiresAt.Equal(inside.Add(leaseFor)) {
		t.Fatalf("renewed lease = %s %s", renewed.Status, renewed.Execution.LeaseExpiresAt)
	}

	past := renewed.Execution.LeaseExpiresAt.Add(unknownGrace + time.Second)
	svc.SetClock(func() time.Time { return past })
	lapsed := postApproval(h, "/approvals/"+rec.ID+"/heartbeat", `{"lease_id":"`+lease+`"}`)
	if lapsed.Code != http.StatusConflict {
		t.Fatalf("renew past grace = %d %s", lapsed.Code, lapsed.Body.String())
	}
	unknown, _ := svc.find(rec.ID)
	if unknown.Status != StatusUnknown {
		t.Fatalf("status after heartbeat = %s, want unknown without a sweep", unknown.Status)
	}

	refused := postApproval(h, "/approvals/"+rec.ID+"/result", `{"lease_id":"`+lease+`","started":false,"refusal":"kube API unreachable"}`)
	if refused.Code != http.StatusConflict {
		t.Fatalf("started=false past grace = %d %s", refused.Code, refused.Body.String())
	}
	still, _ := svc.find(rec.ID)
	if still.Status != StatusUnknown {
		t.Fatalf("refusal requeued the lapsed run: %s", still.Status)
	}

	late := postApproval(h, "/approvals/"+rec.ID+"/result", `{"lease_id":"`+lease+`","exit_code":0}`)
	if late.Code != http.StatusOK || !bytes.Contains(late.Body.Bytes(), []byte(`"late_result":true`)) || !bytes.Contains(late.Body.Bytes(), []byte(`"status":"executed"`)) {
		t.Fatalf("late result = %d %s", late.Code, late.Body.String())
	}
	again := postApproval(h, "/approvals/"+rec.ID+"/result", `{"lease_id":"`+lease+`","exit_code":0}`)
	if again.Code != http.StatusConflict {
		t.Fatalf("second late result = %d %s", again.Code, again.Body.String())
	}
}

func TestRenewStoreErrorIsHTTP500(t *testing.T) {
	svc, _, backend := twoPods(t)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)
	backend.failUpdate = errors.New("disk full")
	h := approvalRoutes(svc)
	hb := postApproval(h, "/approvals/"+rec.ID+"/heartbeat", `{"lease_id":"`+lease+`"}`)
	if hb.Code != http.StatusInternalServerError || !bytes.Contains(hb.Body.Bytes(), []byte("disk full")) {
		t.Fatalf("heartbeat = %d %s", hb.Code, hb.Body.String())
	}
	got, _ := svc.find(rec.ID)
	if got.Status != StatusRunning || !got.Execution.LeaseExpiresAt.Equal(base.Add(leaseFor)) {
		t.Fatalf("failed renew changed the record: %s %s", got.Status, got.Execution.LeaseExpiresAt)
	}
}
