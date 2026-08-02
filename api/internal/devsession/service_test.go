package devsession

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestNewService_LocalUsesBdev(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("OPS_VIEWER_ENV", "")
	svc := NewService(&config.Config{}, nil)
	if svc.Mode() != ModeBdev {
		t.Fatalf("mode=%q want bdev", svc.Mode())
	}
	if svc.Env() != "dev" {
		t.Fatalf("env=%q want dev", svc.Env())
	}
}

func TestNewService_LocalIgnoresViewerEnvProdFromYAML(t *testing.T) {
	// ResolveViewerEnv ignores clusters.yaml viewer_env when not in-cluster.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("OPS_VIEWER_ENV", "")
	svc := NewService(&config.Config{
		Clusters: &config.ClustersFile{
			Clusters: []config.ClusterEntry{{ID: "c", ViewerEnv: "prod"}},
		},
	}, nil)
	if svc.Mode() != ModeBdev {
		t.Fatalf("local must stay bdev even if yaml pins prod; mode=%q", svc.Mode())
	}
}

func TestNewService_StgUsesK8s(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "stg")
	svc := NewService(&config.Config{}, nil)
	if svc.Mode() != ModeK8s {
		t.Fatalf("mode=%q want k8s", svc.Mode())
	}
	if svc.Env() != "stg" {
		t.Fatalf("env=%q want stg", svc.Env())
	}
}

func TestNewService_ProdUsesK8s(t *testing.T) {
	t.Setenv("OPS_VIEWER_ENV", "prod")
	svc := NewService(&config.Config{}, nil)
	if svc.Mode() != ModeK8s {
		t.Fatalf("mode=%q want k8s", svc.Mode())
	}
}
