package selfhealth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestResolveViewerEnv_EnvOverride(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "prod")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	got := ResolveViewerEnv(nil)
	if got != "prod" {
		t.Fatalf("got %q want prod", got)
	}
}

func TestResolveViewerEnv_ClusterFallback_InCluster(t *testing.T) {
	_ = os.Unsetenv("OPS_VIEWER_ENV")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	cfg := &config.Config{
		Clusters: &config.ClustersFile{
			Clusters: []config.ClusterEntry{{ID: "c", ViewerEnv: "prod"}},
		},
	}
	got := ResolveViewerEnv(cfg)
	if got != "prod" {
		t.Fatalf("got %q want prod", got)
	}
}

func TestResolveViewerEnv_LocalIgnoresClusterYAMLProd(t *testing.T) {
	_ = os.Unsetenv("OPS_VIEWER_ENV")
	_ = os.Unsetenv("KUBERNETES_SERVICE_HOST")
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	configDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config"))
	clusters, path, err := config.LoadClusters(configDir)
	if err != nil {
		t.Fatalf("LoadClusters(%s): %v", configDir, err)
	}
	cfg := &config.Config{Clusters: clusters, ClustersPath: path}
	entry := cfg.DefaultCluster()
	if entry == nil {
		t.Fatal("DefaultCluster nil")
	}
	if entry.ViewerEnv != "prod" {
		t.Fatalf("clusters.yaml DefaultCluster viewer_env=%q want prod (path=%s)", entry.ViewerEnv, path)
	}
	got := ResolveViewerEnv(cfg)
	if got != "dev" {
		t.Fatalf("local ResolveViewerEnv got %q want dev (must ignore yaml prod seat)", got)
	}
}

func TestResolveViewerEnv_ClustersYAMLProd_InCluster(t *testing.T) {
	_ = os.Unsetenv("OPS_VIEWER_ENV")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	configDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "config"))
	clusters, path, err := config.LoadClusters(configDir)
	if err != nil {
		t.Fatalf("LoadClusters(%s): %v", configDir, err)
	}
	cfg := &config.Config{Clusters: clusters, ClustersPath: path}
	got := ResolveViewerEnv(cfg)
	if got != "prod" {
		t.Fatalf("in-cluster ResolveViewerEnv from clusters.yaml got %q want prod", got)
	}
}

func TestResolveViewerEnv_DefaultDev(t *testing.T) {
	_ = os.Unsetenv("OPS_VIEWER_ENV")
	_ = os.Unsetenv("KUBERNETES_SERVICE_HOST")
	got := ResolveViewerEnv(&config.Config{})
	if got != "dev" {
		t.Fatalf("got %q want dev", got)
	}
}

func TestNormalizeViewerEnv(t *testing.T) {
	if normalizeViewerEnv("DEV-LOCAL") != "dev-local" {
		t.Fatal("dev-local")
	}
	if normalizeViewerEnv("nope") != "dev" {
		t.Fatal("fallback")
	}
}
