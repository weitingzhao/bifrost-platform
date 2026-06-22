package cluster

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestDomainStatusFromDepsReady(t *testing.T) {
	status, reach, _ := domainStatusFromDeps([]ServiceDependencyView{
		{Reachability: probe.ReachOK},
		{Reachability: probe.ReachOK},
	})
	if status != "ready" || reach != probe.ReachOK {
		t.Fatalf("got %s %s", status, reach)
	}
}

func TestDomainStatusFromDepsStandby(t *testing.T) {
	status, _, summary := domainStatusFromDeps([]ServiceDependencyView{
		{Reachability: probe.ReachOK},
		{Reachability: probe.ReachDegraded, Detail: "scaled to zero (standby)"},
	})
	if status != "standby" {
		t.Fatalf("got %s summary=%s", status, summary)
	}
}

func TestEvalDatabaseDomainWithCaps(t *testing.T) {
	snap := readinessSnapshot{
		clusterCaps: map[string]ClusterCapabilityView{
			"storage-class-local-path": {ID: "storage-class-local-path", Reachability: probe.ReachOK, Detail: "ok"},
			"storage-class-nfs-hot":    {ID: "storage-class-nfs-hot", Reachability: probe.ReachOK, Detail: "ok"},
			"storage-class-nfs-cold":   {ID: "storage-class-nfs-cold", Reachability: probe.ReachOK, Detail: "ok"},
			"nfs-provisioner-hot":      {ID: "nfs-provisioner-hot", Reachability: probe.ReachOK, Detail: "ok"},
			"nfs-provisioner-cold":     {ID: "nfs-provisioner-cold", Reachability: probe.ReachOK, Detail: "ok"},
		},
		nodeCoverage: map[string]CapabilityCoverageView{
			"nfs-client":     {ID: "nfs-client", NodesReady: 3, NodesTotal: 3, Reachability: probe.ReachOK},
			"postgres-role":  {ID: "postgres-role", NodesReady: 1, NodesTotal: 1, Reachability: probe.ReachOK},
		},
		nodes: []NodeView{
			{Name: "n1", Architecture: "amd64", Status: "Ready", Reachability: probe.ReachOK},
		},
		pools: map[string]placement.PoolView{
			"amd64_general": {ID: "amd64_general", NodesReady: 1, NodesTotal: 1, Status: placement.PoolStatusLive},
		},
		deployments: map[string]appsv1.Deployment{
			cnpgOperatorNS + "/" + cnpgOperatorDeploy: {
				ObjectMeta: metav1.ObjectMeta{Name: cnpgOperatorDeploy, Namespace: cnpgOperatorNS},
				Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
			},
		},
	}
	d := evalDatabaseDomain(snap)
	if d.ID != "database" {
		t.Fatalf("id: %s", d.ID)
	}
	if d.Status != "partial" {
		t.Fatalf("expected partial without minio/embedded cutover, got %s", d.Status)
	}
}

func int32Ptr(v int32) *int32 { return &v }

func TestEvalApplicationsDomainAmd64OnlyNoArm64(t *testing.T) {
	snap := readinessSnapshot{
		nodes: []NodeView{
			{Name: "n1", Architecture: "amd64", Status: "Ready", Reachability: probe.ReachOK},
			{Name: "n2", Architecture: "amd64", Status: "Ready", Reachability: probe.ReachOK},
		},
		pools: map[string]placement.PoolView{
			"arm64_edge": {ID: "arm64_edge", NodesReady: 0, NodesTotal: 0, Status: placement.PoolStatusLive},
		},
		deployments: map[string]appsv1.Deployment{
			"bifrost-stg/nginx": {
				ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "bifrost-stg"},
				Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
			},
			"bifrost-stg/frontend": {
				ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "bifrost-stg"},
				Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
			},
		},
	}
	d := evalApplicationsDomain(snap)
	if strings.Contains(d.Summary, "arm64") {
		t.Fatalf("optional arm64 pool must not block domain summary: %s", d.Summary)
	}
	var arm64Dep *ServiceDependencyView
	for i := range d.Dependencies {
		if d.Dependencies[i].ID == "pool-arm64_edge" {
			arm64Dep = &d.Dependencies[i]
			break
		}
	}
	if arm64Dep == nil {
		t.Fatal("missing arm64 optional dep")
	}
	if arm64Dep.Reachability != probe.ReachOK {
		t.Fatalf("optional arm64 pool should be ok when absent, got %s", arm64Dep.Reachability)
	}
}

func TestServiceReadinessMissingKubeconfig(t *testing.T) {
	t.Setenv("PLATFORM_KUBECONFIG", t.TempDir()+"/missing.yaml")
	svc := NewService(nil)
	resp := svc.ServiceReadiness(t.Context())
	if len(resp.Domains) != 7 {
		t.Fatalf("domains: %d", len(resp.Domains))
	}
}
