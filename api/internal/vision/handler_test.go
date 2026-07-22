package vision

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// newTestHandlerConfig builds a config.Config pointed at a scratch temp dir
// and forces every cluster/kubeconfig-dependent path to fail fast instead of
// reaching the operator's real kubeconfig or network — keeping the V1 gate
// checks deterministic and hermetic.
func newTestHandlerConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_KUBECONFIG", filepath.Join(dir, "does-not-exist-kubeconfig.yaml"))
	t.Setenv("PLATFORM_VISION_V1_FRONTEND_ENV", filepath.Join(dir, "does-not-exist.env"))

	return &config.Config{
		ConfigPath: filepath.Join(configDir, "environments.yaml"),
		// No "dev" environment registered — checkDevMatrix short-circuits
		// with "dev environment not configured" instead of probing the network.
		Environments: []config.Environment{{ID: "prod", Label: "Prod"}},
	}
}

func TestHandleGetV1GateReturnsNotReadyWithoutCluster(t *testing.T) {
	cfg := newTestHandlerConfig(t)
	h := NewHandler(cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vision/v1/gate", nil)
	rec := httptest.NewRecorder()
	h.HandleGetV1Gate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var gate V1GateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &gate); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gate.Milestone != "V1" {
		t.Fatalf("Milestone = %q, want V1", gate.Milestone)
	}
	if gate.Ready {
		t.Fatal("expected gate to be not-ready without a reachable cluster/dev environment")
	}
	if gate.Result != "fail" {
		t.Fatalf("Result = %q, want fail", gate.Result)
	}
	if len(gate.Blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}
	if len(gate.Checks) == 0 {
		t.Fatal("expected checks to be populated")
	}
	if gate.SignedAt != nil {
		t.Fatalf("expected no signoff yet, got %+v", gate.SignedAt)
	}
}

func TestHandleRunV1GatePersistsGateRecord(t *testing.T) {
	cfg := newTestHandlerConfig(t)
	h := NewHandler(cfg, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vision/v1/gate", nil)
	rec := httptest.NewRecorder()
	h.HandleRunV1Gate(rec, req)

	// Not-ready gates are still a well-formed response (BadGateway would only
	// occur on a store write failure), so we expect 200 with ok=false.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp RunGateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.OK {
		t.Fatal("expected OK=false since required checks fail")
	}
	if resp.Action != "vision.v1-gate" || resp.Target != "vision-v1-dev-topology" {
		t.Fatalf("resp = %+v", resp)
	}

	// A second GET should now reflect the persisted gate record (rec.At set).
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/vision/v1/gate", nil)
	getRec := httptest.NewRecorder()
	h.HandleGetV1Gate(getRec, getReq)
	var gate V1GateResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &gate); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gate.At == nil {
		t.Fatal("expected gate.At to be set after RunV1Gate persisted a record")
	}
}

func TestHandleSignV1RejectsWhenGateNotReady(t *testing.T) {
	cfg := newTestHandlerConfig(t)
	h := NewHandler(cfg, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vision/v1/signoff", nil)
	rec := httptest.NewRecorder()
	h.HandleSignV1(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["ok"] != false {
		t.Fatalf("payload = %+v, want ok=false", payload)
	}
}
