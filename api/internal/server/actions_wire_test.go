package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

func TestDirectCDStaysForbiddenWithoutExecutorMarker(t *testing.T) {
	var hits int
	next := func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !fromExecutor(r.Context()) {
			t.Errorf("handler ran without the executor marker")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
	s := &Server{}
	guarded := s.guard("cordon_node", next)

	direct := cordonRequest(t)
	direct.Header.Set("X-Approval-Executor", "1")
	direct = direct.WithContext(context.WithValue(direct.Context(), "executor", true))
	rec := httptest.NewRecorder()
	guarded(rec, direct)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "approval required") || hits != 0 {
		t.Fatalf("direct = %d hits=%d %s", rec.Code, hits, rec.Body.String())
	}

	out, err := invokeAction(context.Background(), guarded, http.MethodPost,
		"/api/v1/cluster/nodes/node-a/cordon", map[string]string{"name": "node-a"}, nil)
	if err != nil || hits != 1 {
		t.Fatalf("internal = %#v err=%v hits=%d", out, err, hits)
	}

	again := httptest.NewRecorder()
	guarded(again, cordonRequest(t))
	if again.Code != http.StatusForbidden || hits != 1 {
		t.Fatalf("second direct = %d hits=%d %s", again.Code, hits, again.Body.String())
	}
}

func TestDaemonScaleUpStillReachesHandler(t *testing.T) {
	actions.SetDaemonReplicas(func(context.Context, string) (int32, bool) {
		return 0, false
	})
	var hits int
	next := func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"d10"}`))
	}
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/workloads/scale", strings.NewReader(`{"namespace":"bifrost-dev","kind":"Deployment","name":"daemon","replicas":2}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.guard("scale_deployment", next)(rec, req)
	if strings.Contains(rec.Body.String(), "approval required") || hits != 1 {
		t.Fatalf("daemon scale-up = %d hits=%d %s", rec.Code, hits, rec.Body.String())
	}
}

func cordonRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/node-a/cordon", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "node-a")
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
