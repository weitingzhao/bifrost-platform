package cluster

import (
	"testing"
)

func TestEnsureMetricsServerDisabled(t *testing.T) {
	t.Setenv("PLATFORM_METRICS_SERVER_ENABLED", "0")
	svc := NewService(nil)
	resp, err := svc.EnsureMetricsServer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("expected OK=false when disabled")
	}
	if resp.Action != "ensure-metrics-server" {
		t.Fatalf("action: got %q", resp.Action)
	}
}

func TestDefaultMetricsServerScriptResolves(t *testing.T) {
	path := ResolveInfraScript("", "install-metrics-server.sh")
	if path == "" {
		t.Fatal("expected non-empty script path")
	}
}
