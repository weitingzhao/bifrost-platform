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
	status, reach, summary := domainStatusFromDeps([]ServiceDependencyView{
		{Reachability: probe.ReachOK},
		{Reachability: probe.ReachDegraded, Detail: "scaled to zero (standby)"},
	})
	if status != "standby" {
		t.Fatalf("got %s summary=%s", status, summary)
	}
	if reach != probe.ReachOK {
		t.Fatalf("standby reachability must be ok, got %s", reach)
	}
}

func TestEvalGPUDomainStandbyNoDemand(t *testing.T) {
	zero := int32(0)
	snap := readinessSnapshot{
		nodes: []NodeView{
			{
				Name: "gpu-server", Status: "NotReady", Reachability: probe.ReachUnknown,
				ElasticMode: "standby",
				Capabilities: []NodeCapabilityView{{ID: "gpu-pool"}, {ID: "warehouse"}},
			},
		},
		pools: map[string]placement.PoolView{
			"gpu": {ID: "gpu", NodesReady: 0, NodesTotal: 1, Status: placement.PoolStatusDegraded},
		},
		deployments: map[string]appsv1.Deployment{
			"ai/ollama": {
				ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "ai"},
				Status:     appsv1.DeploymentStatus{Replicas: 0, ReadyReplicas: 0},
				Spec:       appsv1.DeploymentSpec{Replicas: &zero},
			},
		},
	}
	d := evalGPUDomain(snap)
	if d.Status != "standby" {
		t.Fatalf("expected standby, got %s (%s)", d.Status, d.Summary)
	}
	if d.Reachability != probe.ReachOK {
		t.Fatalf("standby reachability must be ok, got %s", d.Reachability)
	}
	if !strings.Contains(d.Summary, "Standby") && !strings.Contains(d.Summary, "no demand") {
		t.Fatalf("summary should mention standby/no demand: %s", d.Summary)
	}
}

func TestEvalGPUDomainStandbyWithDemand(t *testing.T) {
	one := int32(1)
	snap := readinessSnapshot{
		nodes: []NodeView{
			{
				Name: "gpu-server", Status: "NotReady", Reachability: probe.ReachUnknown,
				ElasticMode: "standby",
				Capabilities: []NodeCapabilityView{{ID: "gpu-pool"}},
			},
		},
		pools: map[string]placement.PoolView{
			"gpu": {ID: "gpu", NodesReady: 0, NodesTotal: 1, Status: placement.PoolStatusDegraded},
		},
		deployments: map[string]appsv1.Deployment{
			"ai/ollama": {
				ObjectMeta: metav1.ObjectMeta{Name: "ollama", Namespace: "ai"},
				Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 0},
				Spec:       appsv1.DeploymentSpec{Replicas: &one},
			},
		},
	}
	d := evalGPUDomain(snap)
	if d.Status != "unavailable" {
		t.Fatalf("expected unavailable when demand + standby, got %s (%s)", d.Status, d.Summary)
	}
	if d.Reachability != probe.ReachFail {
		t.Fatalf("expected fail reachability, got %s", d.Reachability)
	}
}

func TestEvalWarehouseDomainStandbyNoDemand(t *testing.T) {
	zero := int32(0)
	snap := readinessSnapshot{
		nodes: []NodeView{
			{
				Name: "gpu-server", Status: "NotReady", Reachability: probe.ReachUnknown,
				ElasticMode: "standby",
				Capabilities: []NodeCapabilityView{{ID: "gpu-pool"}, {ID: "warehouse"}},
			},
		},
		nodeCoverage: map[string]CapabilityCoverageView{
			"warehouse": {
				ID: "warehouse", NodesReady: 0, NodesTotal: 1,
				NodeNames: []string{"gpu-server"}, Reachability: probe.ReachDegraded,
				GapReason: "warehouse: 1 node(s) labeled but none Ready",
			},
		},
		pools: map[string]placement.PoolView{
			"gpu": {ID: "gpu", NodesReady: 0, NodesTotal: 1, Status: placement.PoolStatusDegraded},
		},
		deployments: map[string]appsv1.Deployment{
			"data-warehouse/minio": {
				ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: "data-warehouse"},
				Status:     appsv1.DeploymentStatus{Replicas: 0, ReadyReplicas: 0},
				Spec:       appsv1.DeploymentSpec{Replicas: &zero},
			},
		},
	}
	d := evalWarehouseDomain(snap)
	if d.Status != "standby" || d.Reachability != probe.ReachOK {
		t.Fatalf("expected standby+ok, got %s %s (%s)", d.Status, d.Reachability, d.Summary)
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

func TestEvalApplicationsDomainNoArm64Pool(t *testing.T) {
	snap := applicationsReadySnapshot()
	d := evalApplicationsDomain(snap)
	if strings.Contains(d.Summary, "arm64") {
		t.Fatalf("applications summary must not mention arm64: %s", d.Summary)
	}
	for _, dep := range d.Dependencies {
		if dep.ID == "pool-arm64_edge" || strings.Contains(strings.ToLower(dep.Label), "arm64") {
			t.Fatalf("arm64 edge pool removed from catalog; unexpected dep: %+v", dep)
		}
	}
}

func applicationsReadySnapshot() readinessSnapshot {
	one := int32(1)
	readyAPI := func(ns, name string) appsv1.Deployment {
		return appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
		}
	}
	deploys := map[string]appsv1.Deployment{
		"kube-system/traefik": readyAPI("kube-system", "traefik"),
	}
	routes := map[string]bool{}
	for _, ns := range []string{"bifrost-stg", "bifrost-dev", "bifrost-prod"} {
		deploys[ns+"/frontend"] = readyAPI(ns, "frontend")
		routes[ns+"/"+tradeGatewayIngressRoute] = true
		for _, api := range []string{
			"api-monitor", "api-ops", "api-trading", "api-strategy", "api-portfolio",
			"api-market", "api-research", "api-massive", "api-docs",
		} {
			deploys[ns+"/"+api] = readyAPI(ns, api)
		}
	}
	return readinessSnapshot{
		nodes: []NodeView{
			{Name: "n1", Architecture: "amd64", Status: "Ready", Reachability: probe.ReachOK},
			{Name: "n2", Architecture: "amd64", Status: "Ready", Reachability: probe.ReachOK},
		},
		pools: map[string]placement.PoolView{
			"amd64_general": {ID: "amd64_general", NodesReady: 2, NodesTotal: 2, Status: placement.PoolStatusLive},
		},
		deployments:   deploys,
		ingressRoutes: routes,
	}
}

func TestEvalApplicationsDomainTraefikGatewayReady(t *testing.T) {
	d := evalApplicationsDomain(applicationsReadySnapshot())
	if d.Status != "ready" || d.Reachability != probe.ReachOK {
		t.Fatalf("expected ready/ok with Traefik + trade-gateway + frontend + APIs, got %s %s (%s)", d.Status, d.Reachability, d.Summary)
	}
	for _, dep := range d.Dependencies {
		if strings.Contains(strings.ToLower(dep.Label), "nginx") {
			t.Fatalf("nginx must not be an applications dependency: %+v", dep)
		}
	}
	var gateway, traefik *ServiceDependencyView
	for i := range d.Dependencies {
		switch d.Dependencies[i].ID {
		case "ingressroute-bifrost-stg-trade-gateway":
			gateway = &d.Dependencies[i]
		case "workload-kube-system-traefik":
			traefik = &d.Dependencies[i]
		}
	}
	if gateway == nil || gateway.Reachability != probe.ReachOK {
		t.Fatalf("expected trade-gateway IngressRoute ok, got %+v", gateway)
	}
	if traefik == nil || traefik.Reachability != probe.ReachOK {
		t.Fatalf("expected Traefik controller ok, got %+v", traefik)
	}
}

func TestEvalApplicationsDomainMissingGatewayDegraded(t *testing.T) {
	snap := applicationsReadySnapshot()
	delete(snap.ingressRoutes, "bifrost-dev/"+tradeGatewayIngressRoute)
	d := evalApplicationsDomain(snap)
	if d.Status != "partial" || d.Reachability != probe.ReachDegraded {
		t.Fatalf("missing trade-gateway should partial/degraded, got %s %s (%s)", d.Status, d.Reachability, d.Summary)
	}
	found := false
	for _, dep := range d.Dependencies {
		if dep.ID == "ingressroute-bifrost-dev-trade-gateway" {
			found = true
			if dep.Reachability != probe.ReachDegraded {
				t.Fatalf("dev gateway dep should be degraded, got %s", dep.Reachability)
			}
		}
		if dep.ID == "workload-bifrost-dev-nginx" || strings.Contains(dep.Label, "Ingress nginx") {
			t.Fatalf("legacy nginx dep must not appear: %+v", dep)
		}
	}
	if !found {
		t.Fatal("missing ingressroute-bifrost-dev-trade-gateway dep")
	}
}

func TestAggregateServiceReadinessStandbyOnly(t *testing.T) {
	domains := []ServiceDomainView{
		{ID: "database", Status: "ready", Reachability: probe.ReachOK},
		{ID: "workers", Status: "standby", Reachability: probe.ReachOK},
		{ID: "warehouse", Status: "standby", Reachability: probe.ReachOK},
	}
	reach, detail := aggregateServiceReadiness(domains)
	if reach != probe.ReachOK {
		t.Fatalf("standby-only domains should not block aggregate readiness: %s %s", reach, detail)
	}
	if !strings.Contains(detail, "standby") {
		t.Fatalf("detail should mention standby: %s", detail)
	}
}

func TestEvalWorkersDomainDaemonStandby(t *testing.T) {
	replicas := int32(0)
	snap := readinessSnapshot{
		pools: map[string]placement.PoolView{
			"amd64_general": {ID: "amd64_general", NodesReady: 2, NodesTotal: 2, Status: placement.PoolStatusLive},
		},
		nodeCoverage: map[string]CapabilityCoverageView{
			"nfs-client": {ID: "nfs-client", NodesReady: 2, NodesTotal: 2, Reachability: probe.ReachOK},
		},
		deployments: map[string]appsv1.Deployment{
			"bifrost-stg/daemon": {
				ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "bifrost-stg"},
				Status:     appsv1.DeploymentStatus{Replicas: 0, ReadyReplicas: 0},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			},
		},
	}
	d := evalWorkersDomain(snap)
	if d.Status != "standby" {
		t.Fatalf("expected standby when daemon scaled to zero, got %s (%s)", d.Status, d.Summary)
	}
}

func TestDependencyIsStandby(t *testing.T) {
	if !dependencyIsStandby(ServiceDependencyView{Reachability: probe.ReachDegraded, Detail: "scaled to zero (standby)"}) {
		t.Fatal("expected standby")
	}
	if !dependencyIsStandby(ServiceDependencyView{Reachability: probe.ReachOK, Detail: "standby — no demand"}) {
		t.Fatal("expected ReachOK standby detail to count as standby")
	}
	if dependencyIsStandby(ServiceDependencyView{Reachability: probe.ReachDegraded, Detail: "not deployed in bifrost-stg"}) {
		t.Fatal("not deployed should not be standby")
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
