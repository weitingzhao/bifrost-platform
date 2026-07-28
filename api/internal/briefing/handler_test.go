package briefing

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func newTestBriefingHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PLATFORM_BRIEFING_SESSION_RESULTS_PATH", filepath.Join(dir, "results.json"))
	t.Setenv("PLATFORM_SESSION_SNAPSHOT_PATH", filepath.Join(dir, "snapshot.json"))
	cfg := &config.Config{ConfigPath: filepath.Join(dir, "config", "environments.yaml")}
	return NewHandler(cfg, nil, actuation.NewAuditLog(""), nil, nil)
}

func TestHandleSessionPackReturnsCompactDefault(t *testing.T) {
	h := newTestBriefingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/briefing/session-pack", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionPack(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp PackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.PackSize != "compact" || resp.Pack == "" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandleSessionPackForwardsQueryParams(t *testing.T) {
	h := newTestBriefingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/briefing/session-pack?track=build&lane=governance&intent=review&pack=full&session_id=sess-1", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionPack(rec, req)

	var resp PackResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Track != "build" || resp.Lane != "governance" || resp.Intent != "review" || resp.PackSize != "full" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestStaleDataFreshnessDetail(t *testing.T) {
	lag := func(days float64) *float64 { return &days }

	reach, detail := staleDataFreshnessDetail(cluster.DataFreshnessResponse{
		Databases: []cluster.DataFreshnessDB{
			{Name: "bifrost_prod", LagVsProdDays: lag(99), Verdict: "reference"},
			{Name: "bifrost_dev", LagVsProdDays: lag(7), Verdict: "stale"},
			{Name: "bifrost_stg", LagVsProdDays: lag(2), Verdict: "fresh"},
		},
	})
	if reach != "stale-data" || !strings.Contains(detail, "bifrost_dev lag_vs_prod=7.0d (stale)") {
		t.Fatalf("stale freshness evidence = reach %q detail %q", reach, detail)
	}
	if strings.Contains(detail, "bifrost_prod") && strings.Contains(detail, "lag_vs_prod=99.0d") {
		t.Fatalf("prod reference must not be evidence: %q", detail)
	}

	reach, detail = staleDataFreshnessDetail(cluster.DataFreshnessResponse{
		Databases: []cluster.DataFreshnessDB{{Name: "bifrost_dev", LagVsProdDays: lag(6.9), Verdict: "aging"}},
	})
	if reach != "unknown" || detail != "" {
		t.Fatalf("freshness below stale threshold = reach %q detail %q", reach, detail)
	}
}

func TestHandleCloseSessionRequiresOutcome(t *testing.T) {
	h := newTestBriefingHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/briefing/session/close", bytes.NewBufferString(`{"summary":"no outcome"}`))
	rec := httptest.NewRecorder()
	h.HandleCloseSession(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCloseSessionAppendsAndListReflectsIt(t *testing.T) {
	h := newTestBriefingHandler(t)

	body := `{"outcome":"success","summary":"shipped feature X","track":"build","lane":"governance"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/briefing/session/close", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.HandleCloseSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var closeResp struct {
		OK     bool          `json:"ok"`
		Result SessionResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &closeResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !closeResp.OK || closeResp.Result.Outcome != "success" || closeResp.Result.Summary != "shipped feature X" {
		t.Fatalf("closeResp = %+v", closeResp)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/briefing/session/results", nil)
	listRec := httptest.NewRecorder()
	h.HandleListSessionResults(listRec, listReq)

	var listResp struct {
		Results []SessionResult `json:"results"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listResp.Results) != 1 || listResp.Results[0].ID != closeResp.Result.ID {
		t.Fatalf("listResp = %+v, want the just-closed session", listResp)
	}
}
