package hermesinsight

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/hermesreadiness"
)

type stubReadiness struct {
	resp hermesreadiness.ReadinessResponse
}

func (s stubReadiness) Build(context.Context) hermesreadiness.ReadinessResponse {
	return s.resp
}

func testRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/hermes/insights", h.HandleList)
	r.Post("/hermes/run-first-task", h.HandleRunFirstTask)
	return r
}

func newTestHandler(t *testing.T, ready readinessProber) *Handler {
	t.Helper()
	h, err := NewHandlerWithOptions(HandlerOptions{
		StateDir:  t.TempDir(),
		Readiness: ready,
		Now:       func() time.Time { return time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHandleListEmpty(t *testing.T) {
	h := newTestHandler(t, stubReadiness{})
	rt := testRouter(h)
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hermes/insights", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 0 {
		t.Fatalf("total=%d", resp.Total)
	}
	if resp.Items == nil || len(resp.Items) != 0 {
		t.Fatalf("items=%v", resp.Items)
	}
}

func TestRunFirstTaskPersistsRow(t *testing.T) {
	h := newTestHandler(t, stubReadiness{resp: hermesreadiness.ReadinessResponse{Ready: true}})
	rt := testRouter(h)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hermes/run-first-task", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run RunFirstTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if !run.OK {
		t.Fatalf("ok=false err=%s", run.Error)
	}
	if run.Insight.ID == "" || run.Insight.Type != TypeFirstTask || run.Insight.Verdict != VerdictOK {
		t.Fatalf("insight=%+v", run.Insight)
	}
	if run.Insight.Source != SourceFirstTask {
		t.Fatalf("source=%s", run.Insight.Source)
	}
	if run.Error != "" {
		t.Fatalf("error=%q", run.Error)
	}
	if run.Insight.Summary == "" || !strings.Contains(run.Insight.Summary, "hermes-mission-health-l0") {
		t.Fatalf("summary=%s", run.Insight.Summary)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hermes/insights", nil))
	var listed ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Items) != 1 || listed.Items[0].ID != run.Insight.ID {
		t.Fatalf("listed=%+v", listed)
	}
}

func TestRunFirstTaskBlockedWhenHermesNotConfigured(t *testing.T) {
	t.Setenv("NOUS_HERMES_URL", "")
	t.Setenv("HERMES_LLM_KEY_CONFIGURED", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	h := newTestHandler(t, hermesreadiness.NewHandler())
	rt := testRouter(h)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hermes/run-first-task", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run RunFirstTaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.OK {
		t.Fatal("expected ok=false when Hermes not configured")
	}
	if run.Error == "" {
		t.Fatal("expected error explaining blocker")
	}
	if run.Insight.Verdict != VerdictBlocked {
		t.Fatalf("verdict=%s", run.Insight.Verdict)
	}
	if !strings.Contains(run.Error, "NOUS_HERMES_URL") && !strings.Contains(run.Insight.Summary, "NOUS_HERMES_URL") {
		t.Fatalf("blocker missing from error/summary: err=%s summary=%s", run.Error, run.Insight.Summary)
	}

	items, total := h.store.List(10)
	if total != 1 || items[0].Verdict != VerdictBlocked {
		t.Fatalf("store total=%d items=%v", total, items)
	}
}

func TestHandleListLimitClamp(t *testing.T) {
	h := newTestHandler(t, stubReadiness{})
	for i := 0; i < 8; i++ {
		if err := h.store.Append(HermesInsight{
			ID:      "n-" + strconv.Itoa(i),
			Type:    TypeFirstTask,
			Verdict: VerdictOK,
			Source:  SourceFirstTask,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rt := testRouter(h)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hermes/insights?limit=0", nil))
	var zero ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &zero); err != nil {
		t.Fatal(err)
	}
	if zero.Total != 8 || len(zero.Items) != 1 {
		t.Fatalf("limit=0 clamp → items=%d total=%d", len(zero.Items), zero.Total)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hermes/insights?limit=3", nil))
	var three ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &three); err != nil {
		t.Fatal(err)
	}
	if three.Total != 8 || len(three.Items) != 3 {
		t.Fatalf("limit=3 → items=%d total=%d", len(three.Items), three.Total)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hermes/insights?limit=999", nil))
	var wide ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &wide); err != nil {
		t.Fatal(err)
	}
	if wide.Total != 8 || len(wide.Items) != 8 {
		t.Fatalf("limit=999 clamp → items=%d total=%d", len(wide.Items), wide.Total)
	}
}
