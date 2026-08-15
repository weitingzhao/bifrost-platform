//go:build integration

package cluster

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// Run with: go test -tags=integration ./internal/cluster -run TestExecSQLOnPrimaryLive -count=1
func TestExecSQLOnPrimaryLive(t *testing.T) {
	home, _ := os.UserHomeDir()
	kube := filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	if _, err := os.Stat(kube); err != nil {
		t.Skip("kubeconfig missing")
	}
	t.Setenv("PLATFORM_KUBECONFIG", kube)
	// Force client-go path (in-cluster platform-api has no kubectl).
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	svc := NewService(&config.ClusterEntry{ID: "live", KubeconfigEnv: "PLATFORM_KUBECONFIG"})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	out, err := svc.ExecSQLOnPrimary(ctx, "bifrost_golden_source", "SELECT count(*) FROM data_ops.ingest_freshness")
	if err != nil {
		t.Fatalf("ExecSQLOnPrimary: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("empty output")
	}
	t.Logf("freshness rows: %s", strings.TrimSpace(out))
}
