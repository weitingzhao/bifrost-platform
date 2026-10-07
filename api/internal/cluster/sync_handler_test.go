package cluster

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSyncKubeconfigDisabledIsNotSuccess(t *testing.T) {
	t.Setenv("PLATFORM_CLUSTER_SYNC_ENABLED", "")
	h := &Handler{svc: NewService(nil)}
	rec := httptest.NewRecorder()
	h.HandleSyncKubeconfig(rec, httptest.NewRequest(http.MethodPost, "/api/v1/cluster/sync-kubeconfig", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when sync is disabled; body = %s", rec.Code, rec.Body.String())
	}
}
