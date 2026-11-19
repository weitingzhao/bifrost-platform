package cluster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureKubePrometheusStackDisabledByDefault(t *testing.T) {
	t.Setenv("PLATFORM_OBSERVABILITY_INSTALL_ENABLED", "")
	svc := NewService(nil)
	resp, err := svc.EnsureKubePrometheusStack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("expected OK=false when disabled")
	}
	if resp.Action != "ensure-kube-prometheus-stack" {
		t.Fatalf("action: got %q", resp.Action)
	}
}

func TestEnsureKubePrometheusStackRunsCustomScript(t *testing.T) {
	t.Setenv("PLATFORM_OBSERVABILITY_INSTALL_ENABLED", "1")
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "install-kube-prometheus-stack.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv("PLATFORM_OBSERVABILITY_INSTALL_SCRIPT", scriptPath)

	svc := NewService(nil)
	resp, err := svc.EnsureKubePrometheusStack()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatalf("expected OK=true, got message: %s", resp.Message)
	}
}

func TestHandleEnsureKubePrometheusStackDisabled(t *testing.T) {
	t.Setenv("PLATFORM_OBSERVABILITY_INSTALL_ENABLED", "0")
	h := &Handler{svc: NewService(nil)}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/addons/kube-prometheus-stack/ensure", nil)
	rr := httptest.NewRecorder()

	h.HandleEnsureKubePrometheusStack(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	var resp ActuationResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Action != "ensure-kube-prometheus-stack" {
		t.Fatalf("action: got %q", resp.Action)
	}
	if resp.OK {
		t.Fatal("expected OK=false")
	}
}
