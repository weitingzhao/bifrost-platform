package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Traefik IngressRoute — Trade edge is trade-gateway CR, not a per-NS nginx Deployment.
var traefikIngressRouteGVR = schema.GroupVersionResource{
	Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes",
}

const tradeGatewayIngressRoute = "trade-gateway"

type ServiceDependencyView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail,omitempty"`
}

type ServiceDomainView struct {
	ID           string                  `json:"id"`
	Label        string                  `json:"label"`
	Status       string                  `json:"status"`
	Reachability probe.Reachability      `json:"reachability"`
	Summary      string                  `json:"summary"`
	Dependencies []ServiceDependencyView `json:"dependencies"`
}

type ServiceReadinessResponse struct {
	ClusterID    string             `json:"cluster_id"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string               `json:"detail"`
	Domains      []ServiceDomainView  `json:"domains"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

type readinessSnapshot struct {
	nodes         []NodeView
	clusterCaps   map[string]ClusterCapabilityView
	nodeCoverage  map[string]CapabilityCoverageView
	pools         map[string]placement.PoolView
	deployments   map[string]appsv1.Deployment
	ingressRoutes map[string]bool // "namespace/name" → present
}

func (s *Service) ServiceReadiness(ctx context.Context) ServiceReadinessResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	snap, snapErr := s.buildReadinessSnapshot(ctx)
	domains := evaluateServiceDomains(snap)

	reach, detail := aggregateServiceReadiness(domains)
	if snapErr != nil && reach == probe.ReachOK {
		reach = probe.ReachDegraded
		detail = snapErr.Error()
	}

	return ServiceReadinessResponse{
		ClusterID:    base.ClusterID,
		Reachability: reach,
		Detail:       detail,
		Domains:      domains,
		GeneratedAt:  now,
	}
}

func (s *Service) buildReadinessSnapshot(ctx context.Context) (readinessSnapshot, error) {
	nodesResp := s.Nodes(ctx)
	clusterCaps, _, _ := s.probeClusterCapabilities(ctx)
	coverage := buildNodeCoverage(nodesResp.Nodes, NodeCapabilityCatalog())
	placementResp := s.Placement(ctx)

	capMap := make(map[string]ClusterCapabilityView, len(clusterCaps))
	for _, c := range clusterCaps {
		capMap[c.ID] = c
	}
	covMap := make(map[string]CapabilityCoverageView, len(coverage))
	for _, c := range coverage {
		covMap[c.ID] = c
	}
	poolMap := make(map[string]placement.PoolView, len(placementResp.Pools))
	for _, p := range placementResp.Pools {
		poolMap[p.ID] = p
	}

	deployments, depErr := s.loadReadinessDeployments(ctx)
	ingressRoutes, irErr := s.loadTradeGatewayIngressRoutes(ctx)
	snap := readinessSnapshot{
		nodes:         nodesResp.Nodes,
		clusterCaps:   capMap,
		nodeCoverage:  covMap,
		pools:         poolMap,
		deployments:   deployments,
		ingressRoutes: ingressRoutes,
	}
	if depErr != nil {
		return snap, depErr
	}
	if irErr != nil {
		return snap, irErr
	}
	return snap, nil
}

func (s *Service) loadReadinessDeployments(ctx context.Context) (map[string]appsv1.Deployment, error) {
	clientset, _, err := s.buildClient()
	if err != nil {
		return nil, err
	}
	namespaces := []string{
		"bifrost-stg", "bifrost-dev", "bifrost-prod",
		"cicd", "cnpg-system", "data", "data-warehouse", "ai", "tekton-pipelines",
		"kube-system", // Traefik ingress controller
	}
	out := make(map[string]appsv1.Deployment)
	var firstErr error
	for _, ns := range namespaces {
		list, listErr := clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if listErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("namespace %s: %w", ns, listErr)
			}
			continue
		}
		for _, d := range list.Items {
			out[ns+"/"+d.Name] = d
		}
	}
	return out, firstErr
}

func (s *Service) loadTradeGatewayIngressRoutes(ctx context.Context) (map[string]bool, error) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	return listTradeGatewayIngressRoutes(ctx, dyn)
}

func listTradeGatewayIngressRoutes(ctx context.Context, dyn dynamic.Interface) (map[string]bool, error) {
	out := make(map[string]bool)
	namespaces := []string{"bifrost-stg", "bifrost-dev", "bifrost-prod"}
	var firstErr error
	for _, ns := range namespaces {
		obj, getErr := dyn.Resource(traefikIngressRouteGVR).Namespace(ns).Get(ctx, tradeGatewayIngressRoute, metav1.GetOptions{})
		if getErr != nil {
			// NotFound → domain dep gap; other errors → snapshot-level degraded.
			if !apierrors.IsNotFound(getErr) && firstErr == nil {
				firstErr = fmt.Errorf("ingressroute %s/%s: %w", ns, tradeGatewayIngressRoute, getErr)
			}
			continue
		}
		if obj != nil {
			out[ns+"/"+tradeGatewayIngressRoute] = true
		}
	}
	return out, firstErr
}

func evaluateServiceDomains(snap readinessSnapshot) []ServiceDomainView {
	return []ServiceDomainView{
		evalDatabaseDomain(snap),
		evalRedisDomain(snap),
		evalGPUDomain(snap),
		evalWarehouseDomain(snap),
		evalWorkersDomain(snap),
		evalApplicationsDomain(snap),
		evalCICDDomain(snap),
	}
}

func evalDatabaseDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		clusterCapDep(snap, "storage-class-local-path", "StorageClass local-path (PGDATA)"),
		clusterCapDep(snap, "storage-class-nfs-hot", "StorageClass nfs-hot (backups)"),
		clusterCapDep(snap, "storage-class-nfs-cold", "StorageClass nfs-cold"),
		clusterCapDep(snap, "nfs-provisioner-hot", "NFS provisioner (hot)"),
		clusterCapDep(snap, "nfs-provisioner-cold", "NFS provisioner (cold)"),
		nodeCovDep(snap, "nfs-client", "NFS client nodes"),
		nodeCovDep(snap, "postgres-role", "PostgreSQL host nodes"),
		schedulableArchDep(snap, "amd64", "Schedulable amd64 nodes"),
	}
	deps = append(deps, deploymentDep(snap, cnpgOperatorNS, cnpgOperatorDeploy, "CloudNativePG operator"))
	deps = append(deps, deploymentDep(snap, cnpgNamespace, "minio", "MinIO backup target"))

	embedded := activeEmbeddedPostgresDep(snap)
	if embedded != nil {
		deps = append(deps, *embedded)
	}

	domain := finalizeDomain("database", "PostgreSQL", deps, "CloudNativePG HA @ data NS · local-path PGDATA · nfs-hot backups")
	if embedded != nil && domain.Status == "ready" {
		domain.Status = "partial"
		domain.Reachability = probe.ReachDegraded
		domain.Summary = "CNPG infra ready · embedded postgres still serving apps (cutover pending)"
	} else if embedded == nil && domain.Status == "ready" {
		domain.Summary = "CNPG HA ready · STG on data NS · dev/prod cutover pending"
	}
	return domain
}

func activeEmbeddedPostgresDep(snap readinessSnapshot) *ServiceDependencyView {
	refs := []deployRef{
		{"bifrost-stg", "postgres"},
		{"bifrost-dev", "postgres"},
		{"bifrost-prod", "postgres"},
	}
	namespaces := make([]string, 0, len(refs))
	for _, ref := range refs {
		key := ref.namespace + "/" + ref.name
		d, ok := snap.deployments[key]
		if !ok {
			continue
		}
		desired := d.Status.Replicas
		if desired == 0 && d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if desired == 0 {
			continue
		}
		namespaces = append(namespaces, ref.namespace)
	}
	if len(namespaces) == 0 {
		return nil
	}
	return &ServiceDependencyView{
		ID:           "legacy-embedded-postgres",
		Label:        "Embedded postgres (app namespaces)",
		Reachability: probe.ReachDegraded,
		Detail:       fmt.Sprintf("active in %s — cutover to bifrost-postgres-rw.data.svc", strings.Join(namespaces, ", ")),
	}
}

func evalRedisDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		schedulableArchDep(snap, "amd64", "Schedulable amd64 nodes"),
		clusterCapDep(snap, "storage-class-local-path", "StorageClass local-path (Redis PVC)"),
		clusterCapDep(snap, "storage-class-nfs-hot", "StorageClass nfs-hot (RDB backup)"),
	}
	for _, spec := range redisTargetCatalog {
		deps = append(deps, redisTargetDep(snap, spec.name, spec.role, spec.environment))
	}
	for _, ns := range []string{"bifrost-stg", "bifrost-dev", "bifrost-prod"} {
		if dep := embeddedRedisDep(snap, ns); dep != nil {
			deps = append(deps, *dep)
		}
	}

	domain := finalizeDomain("redis", "Redis", deps, "Bitnami live/queue @ data NS · per-env isolation")
	targetsReady := 0
	embeddedActive := 0
	for _, d := range deps {
		if strings.HasPrefix(d.ID, "redis-target-") && d.Reachability == probe.ReachOK {
			targetsReady++
		}
		if strings.HasPrefix(d.ID, "embedded-redis-") {
			embeddedActive++
		}
	}
	if targetsReady == len(redisTargetCatalog) && embeddedActive == 0 {
		return domain
	}
	if domain.Status == "ready" {
		domain.Status = "partial"
		domain.Reachability = probe.ReachDegraded
	}
	if embeddedActive > 0 && targetsReady == 0 {
		domain.Summary = fmt.Sprintf("Embedded redis in %d env(s) · phase ⑥ live/queue @ data NS pending", embeddedActive)
	} else if targetsReady > 0 {
		domain.Summary = fmt.Sprintf("%d/%d data NS targets · %d embedded active", targetsReady, len(redisTargetCatalog), embeddedActive)
	} else {
		domain.Summary = "Embedded redis only · Bitnami split not deployed (phase ⑥)"
	}
	return domain
}

func evalGPUDomain(snap readinessSnapshot) ServiceDomainView {
	gpuNodes := filterNodes(snap.nodes, func(n NodeView) bool {
		return hasCapabilityID(n, "gpu-pool")
	})
	demand := deploymentDesiredReplicas(snap, "ai", "ollama") > 0
	deps := []ServiceDependencyView{
		elasticPoolDep(snap, "gpu", "GPU node pool", gpuNodes, demand),
	}
	if len(gpuNodes) == 0 {
		deps = append(deps, ServiceDependencyView{
			ID: "gpu-nodes", Label: "GPU nodes",
			Reachability: probe.ReachDegraded, Detail: "no workload=gpu node registered",
		})
	} else {
		deps = append(deps, gpuNodesDep(gpuNodes, demand))
	}
	wl := deploymentDep(snap, "ai", "ollama", "Ollama (ai)")
	if wl.Reachability == probe.ReachDegraded && strings.Contains(wl.Detail, "not deployed") {
		wl.Detail = "scaled to zero or not deployed (make gpu-ollama-up)"
	}
	deps = append(deps, wl)
	domain := finalizeDomain("gpu", "GPU / AI", deps, "Elastic compute on gpu-server")
	return applyElasticDomainVerdict(domain, gpuNodes, demand, "Standby — no demand", "Compute needed but GPU node offline")
}

func evalWarehouseDomain(snap readinessSnapshot) ServiceDomainView {
	whNodes := filterNodes(snap.nodes, func(n NodeView) bool {
		return hasCapabilityID(n, "warehouse") || hasCapabilityID(n, "gpu-pool")
	})
	demand := deploymentDesiredReplicas(snap, "data-warehouse", "minio") > 0
	deps := []ServiceDependencyView{
		elasticNodeCovDep(snap, "warehouse", "Warehouse nodes", demand),
		elasticPoolDep(snap, "gpu", "GPU / warehouse host", whNodes, demand),
	}
	wl := deploymentDep(snap, "data-warehouse", "minio", "MinIO (data-warehouse)")
	if wl.Reachability == probe.ReachDegraded && (strings.Contains(wl.Detail, "standby") || strings.Contains(wl.Detail, "scaled to zero")) {
		wl.Detail = "scaled to zero — make gpu-warehouse-up to start"
	}
	deps = append(deps, wl)
	domain := finalizeDomain("warehouse", "Data warehouse", deps, "MinIO object store on gpu-server")
	return applyElasticDomainVerdict(domain, whNodes, demand, "Standby — no demand", "Compute needed but warehouse node offline")
}

func evalWorkersDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		poolDep(snap, "amd64_general", "amd64 general pool"),
		nodeCovDep(snap, "nfs-client", "NFS client nodes"),
	}
	wl := firstReadyDeployment(snap, []deployRef{
		{"bifrost-stg", "daemon"},
		{"bifrost-dev", "daemon"},
		{"bifrost-prod", "daemon"},
	})
	if wl != nil {
		deps = append(deps, *wl)
	} else {
		deps = append(deps, ServiceDependencyView{
			ID: "workload-daemon", Label: "Trading daemon",
			Reachability: probe.ReachDegraded, Detail: "not deployed",
		})
	}
	domain := finalizeDomain("workers", "General workers", deps, "Daemon · data pipelines")
	if domain.Status == "partial" || domain.Status == "standby" {
		onlyStandby := true
		for _, d := range domain.Dependencies {
			if d.Reachability == probe.ReachOK {
				continue
			}
			if !dependencyIsStandby(d) {
				onlyStandby = false
				break
			}
		}
		if onlyStandby {
			domain.Status = "standby"
			domain.Reachability = probe.ReachOK
			domain.Summary = "Worker pool ready · trading daemon scaled to zero (observe mode / D10)"
		}
	}
	return domain
}

func evalApplicationsDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		schedulableAnyDep(snap, "Schedulable nodes"),
		deploymentDep(snap, "kube-system", "traefik", "Traefik ingress controller"),
	}
	// Edge is Traefik IngressRoute trade-gateway per Trade NS (not per-NS nginx).
	for _, env := range []struct{ ns, label string }{
		{"bifrost-stg", "stg"},
		{"bifrost-dev", "dev"},
		{"bifrost-prod", "prod"},
	} {
		deps = append(deps, ingressRouteDep(snap, env.ns, tradeGatewayIngressRoute, "Trade gateway ("+env.label+")"))
		deps = append(deps, deploymentDep(snap, env.ns, "frontend", "Trade frontend ("+env.label+")"))
		apiReady, apiDetail := countReadyDeployments(snap, env.ns, "api-")
		deps = append(deps, ServiceDependencyView{
			ID: "apis-" + env.label, Label: "FastAPI services (" + env.label + ")",
			Reachability: apiReadyReach(apiReady),
			Detail:       apiDetail,
		})
	}
	return finalizeDomain("applications", "General applications", deps, "amd64 Trade stack · Traefik trade-gateway · frontend · 4 API pods (Phase B)")
}

func ingressRouteDep(snap readinessSnapshot, ns, name, label string) ServiceDependencyView {
	key := ns + "/" + name
	id := "ingressroute-" + ns + "-" + name
	if snap.ingressRoutes != nil && snap.ingressRoutes[key] {
		return ServiceDependencyView{
			ID: id, Label: label,
			Reachability: probe.ReachOK,
			Detail:       "IngressRoute " + name + " present",
		}
	}
	return ServiceDependencyView{
		ID: id, Label: label,
		Reachability: probe.ReachDegraded,
		Detail:       "IngressRoute " + name + " missing in " + ns,
	}
}

func evalCICDDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		poolDep(snap, "amd64_ci", "amd64 CI pool (Kaniko)"),
		poolDep(snap, "amd64_general", "amd64 general pool"),
	}
	deps = append(deps, deploymentDep(snap, "cicd", "gitea", "Gitea"))
	deps = append(deps, deploymentDep(snap, "cicd", "registry", "Container registry"))
	tekton := findDeploymentByPrefix(snap, "tekton-pipelines", "tekton")
	if tekton == nil {
		tekton = findDeploymentByPrefix(snap, "cicd", "tekton")
	}
	if tekton != nil {
		reach, detail := deploymentReach(tekton)
		deps = append(deps, ServiceDependencyView{
			ID: "tekton", Label: "Tekton controller",
			Reachability: reach, Detail: detail,
		})
	} else {
		deps = append(deps, ServiceDependencyView{
			ID: "tekton", Label: "Tekton controller",
			Reachability: probe.ReachDegraded, Detail: "not found in tekton-pipelines or cicd",
		})
	}
	return finalizeDomain("cicd", "CI/CD", deps, "Gitea · Registry · Tekton builds")
}

type deployRef struct {
	namespace string
	name      string
}

func finalizeDomain(id, label string, deps []ServiceDependencyView, purpose string) ServiceDomainView {
	status, reach, summary := domainStatusFromDeps(deps)
	if summary == "" {
		summary = purpose
	}
	return ServiceDomainView{
		ID: id, Label: label, Status: status, Reachability: reach, Summary: summary, Dependencies: deps,
	}
}

func dependencyIsStandby(d ServiceDependencyView) bool {
	lower := strings.ToLower(d.Detail)
	standbyText := strings.Contains(lower, "standby") || strings.Contains(lower, "scaled to zero")
	if !standbyText {
		return false
	}
	// Standby deps may be ReachOK (expected off) or ReachDegraded (legacy scaled-to-zero).
	return d.Reachability == probe.ReachOK || d.Reachability == probe.ReachDegraded || d.Reachability == probe.ReachUnknown
}

func domainStatusFromDeps(deps []ServiceDependencyView) (status string, reach probe.Reachability, summary string) {
	fail := 0
	realDegraded := 0
	standby := 0
	var gaps []string
	for _, d := range deps {
		if dependencyIsStandby(d) {
			standby++
			continue
		}
		switch d.Reachability {
		case probe.ReachFail:
			fail++
			gaps = append(gaps, d.Label)
		case probe.ReachDegraded:
			realDegraded++
		}
	}
	switch {
	case fail > 0:
		return "unavailable", probe.ReachFail, fmt.Sprintf("Blocked: %s", strings.Join(gaps, ", "))
	case realDegraded > 0:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d dependency gap(s)", realDegraded)
	case standby > 0:
		// Product formula: standby + no demand → status standby + reachability ok.
		return "standby", probe.ReachOK, "Standby — no demand"
	default:
		return "ready", probe.ReachOK, "All dependencies satisfied"
	}
}

func aggregateServiceReadiness(domains []ServiceDomainView) (probe.Reachability, string) {
	fail := 0
	degraded := 0
	standby := 0
	for _, d := range domains {
		switch d.Status {
		case "standby":
			standby++
		case "unavailable":
			fail++
		default:
			switch d.Reachability {
			case probe.ReachFail:
				fail++
			case probe.ReachDegraded:
				degraded++
			}
		}
	}
	switch {
	case fail > 0:
		return probe.ReachDegraded, fmt.Sprintf("%d domain(s) unavailable", fail)
	case degraded > 0:
		return probe.ReachDegraded, fmt.Sprintf("%d domain(s) partial", degraded)
	case standby > 0:
		return probe.ReachOK, fmt.Sprintf("%d domain(s) on standby (scaled to zero)", standby)
	default:
		return probe.ReachOK, "all service domains ready"
	}
}

func clusterCapDep(snap readinessSnapshot, id, label string) ServiceDependencyView {
	c, ok := snap.clusterCaps[id]
	if !ok {
		return ServiceDependencyView{ID: id, Label: label, Reachability: probe.ReachFail, Detail: "not probed"}
	}
	return ServiceDependencyView{ID: id, Label: label, Reachability: c.Reachability, Detail: c.Detail}
}

func nodeCovDep(snap readinessSnapshot, id, label string) ServiceDependencyView {
	c, ok := snap.nodeCoverage[id]
	if !ok {
		return ServiceDependencyView{ID: id, Label: label, Reachability: probe.ReachDegraded, Detail: "not in catalog"}
	}
	detail := c.GapReason
	if c.NodesReady > 0 {
		detail = fmt.Sprintf("%d/%d ready: %s", c.NodesReady, c.NodesTotal, strings.Join(c.NodeNames, ", "))
	}
	return ServiceDependencyView{ID: id, Label: label, Reachability: c.Reachability, Detail: detail}
}

func poolDep(snap readinessSnapshot, id, label string) ServiceDependencyView {
	p, ok := snap.pools[id]
	if !ok {
		return ServiceDependencyView{ID: "pool-" + id, Label: label, Reachability: probe.ReachFail, Detail: "pool not found"}
	}
	reach := probe.ReachOK
	detail := fmt.Sprintf("%d/%d ready", p.NodesReady, p.NodesTotal)
	switch p.Status {
	case placement.PoolStatusPlanned:
		reach = probe.ReachDegraded
		detail = "pool planned — no live nodes"
	case placement.PoolStatusDegraded:
		reach = probe.ReachDegraded
		detail = fmt.Sprintf("pool degraded — %d/%d ready", p.NodesReady, p.NodesTotal)
	}
	if p.NodesReady == 0 && p.Status != placement.PoolStatusPlanned {
		reach = probe.ReachFail
	}
	return ServiceDependencyView{ID: "pool-" + id, Label: label, Reachability: reach, Detail: detail}
}

func schedulableArchDep(snap readinessSnapshot, arch, label string) ServiceDependencyView {
	ready, total := schedulableArch(snap.nodes, arch)
	reach := probe.ReachOK
	detail := fmt.Sprintf("%d/%d schedulable", ready, total)
	if ready == 0 {
		reach = probe.ReachFail
		detail = fmt.Sprintf("no schedulable %s nodes", arch)
	}
	return ServiceDependencyView{ID: "schedulable-" + arch, Label: label, Reachability: reach, Detail: detail}
}

func schedulableAnyDep(snap readinessSnapshot, label string) ServiceDependencyView {
	ready, total := schedulableAny(snap.nodes)
	reach := probe.ReachOK
	detail := fmt.Sprintf("%d/%d schedulable", ready, total)
	if ready == 0 {
		reach = probe.ReachFail
		detail = "no schedulable nodes"
	}
	return ServiceDependencyView{ID: "schedulable-any", Label: label, Reachability: reach, Detail: detail}
}

func schedulableArch(nodes []NodeView, arch string) (ready, total int) {
	for _, n := range nodes {
		if n.Architecture != arch {
			continue
		}
		total++
		if n.Unschedulable || n.Status != "Ready" || n.Reachability != probe.ReachOK {
			continue
		}
		ready++
	}
	return ready, total
}

func schedulableAny(nodes []NodeView) (ready, total int) {
	for _, n := range nodes {
		total++
		if n.Unschedulable || n.Status != "Ready" || n.Reachability != probe.ReachOK {
			continue
		}
		ready++
	}
	return ready, total
}

func gpuNodesDep(nodes []NodeView, demand bool) ServiceDependencyView {
	ready := 0
	names := make([]string, 0, len(nodes))
	standby := 0
	for _, n := range nodes {
		names = append(names, n.Name)
		if n.ElasticMode == "standby" || (n.Unschedulable && n.Status != "Ready") {
			standby++
			continue
		}
		if n.Status == "Ready" && n.Reachability == probe.ReachOK {
			ready++
		}
	}
	reach := probe.ReachOK
	detail := fmt.Sprintf("%d/%d active", ready, len(nodes))
	if ready == 0 && standby > 0 {
		if demand {
			reach = probe.ReachFail
			detail = fmt.Sprintf("Compute needed but node offline (%s)", strings.Join(names, ", "))
		} else {
			// Expected off — neutral reachability.
			reach = probe.ReachOK
			detail = fmt.Sprintf("standby — no demand (%s)", strings.Join(names, ", "))
		}
	} else if ready == 0 {
		reach = probe.ReachFail
		detail = "GPU nodes not ready"
	}
	return ServiceDependencyView{ID: "gpu-nodes", Label: "GPU nodes", Reachability: reach, Detail: detail}
}

func deploymentDesiredReplicas(snap readinessSnapshot, ns, name string) int32 {
	d, ok := snap.deployments[ns+"/"+name]
	if !ok {
		return 0
	}
	if d.Spec.Replicas != nil {
		return *d.Spec.Replicas
	}
	return d.Status.Replicas
}

func nodesAllElasticStandby(nodes []NodeView) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		if n.ElasticMode == "standby" {
			continue
		}
		// Any Ready/active node breaks the all-standby rollup.
		if n.Status == "Ready" && n.Reachability == probe.ReachOK {
			return false
		}
		if n.ElasticMode == "active" {
			return false
		}
		// Non-elastic NotReady is not "elastic standby".
		if n.ElasticMode == "" {
			return false
		}
	}
	return countElasticStandby(nodes) == len(nodes)
}

// elasticPoolDep — GPU/warehouse pools may be powered off with no demand (expected).
func elasticPoolDep(snap readinessSnapshot, id, label string, nodes []NodeView, demand bool) ServiceDependencyView {
	if nodesAllElasticStandby(nodes) || (len(nodes) > 0 && countElasticStandby(nodes) == len(nodes)) {
		if demand {
			return ServiceDependencyView{
				ID: "pool-" + id, Label: label,
				Reachability: probe.ReachFail,
				Detail:       "Compute needed but node offline",
			}
		}
		return ServiceDependencyView{
			ID: "pool-" + id, Label: label,
			Reachability: probe.ReachOK,
			Detail:       "standby — no demand",
		}
	}
	return poolDep(snap, id, label)
}

func countElasticStandby(nodes []NodeView) int {
	n := 0
	for _, node := range nodes {
		if node.ElasticMode == "standby" {
			n++
		}
	}
	return n
}

// elasticNodeCovDep — warehouse/gpu coverage when the only matching nodes are elastic standby.
func elasticNodeCovDep(snap readinessSnapshot, id, label string, demand bool) ServiceDependencyView {
	base := nodeCovDep(snap, id, label)
	if base.Reachability == probe.ReachOK {
		return base
	}
	c, ok := snap.nodeCoverage[id]
	if !ok || c.NodesTotal == 0 {
		return base
	}
	// All labeled nodes present but none Ready — treat as standby when no demand.
	if c.NodesReady == 0 && !demand {
		return ServiceDependencyView{
			ID: id, Label: label,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("standby — no demand (%s)", strings.Join(c.NodeNames, ", ")),
		}
	}
	if c.NodesReady == 0 && demand {
		return ServiceDependencyView{
			ID: id, Label: label,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("Compute needed but node offline (%s)", strings.Join(c.NodeNames, ", ")),
		}
	}
	return base
}

// applyElasticDomainVerdict — product formula for GPU/Warehouse elastic domains.
func applyElasticDomainVerdict(
	domain ServiceDomainView,
	nodes []NodeView,
	demand bool,
	standbySummary, demandSummary string,
) ServiceDomainView {
	standbyNodes := countElasticStandby(nodes) > 0 || nodesAllElasticStandby(nodes)
	if !standbyNodes && !demand {
		// Workload scaled to zero on a ready host still counts as standby.
		if domain.Status == "standby" {
			domain.Reachability = probe.ReachOK
			if domain.Summary == "" || strings.Contains(domain.Summary, "scaled to zero") {
				domain.Summary = standbySummary
			}
		}
		return domain
	}
	if standbyNodes && !demand {
		domain.Status = "standby"
		domain.Reachability = probe.ReachOK
		domain.Summary = standbySummary
		return domain
	}
	if standbyNodes && demand {
		domain.Status = "unavailable"
		domain.Reachability = probe.ReachFail
		domain.Summary = demandSummary
		return domain
	}
	return domain
}

func deploymentDep(snap readinessSnapshot, ns, name, label string) ServiceDependencyView {
	key := ns + "/" + name
	d, ok := snap.deployments[key]
	if !ok {
		return ServiceDependencyView{
			ID: "workload-" + name, Label: label,
			Reachability: probe.ReachDegraded, Detail: "not deployed in " + ns,
		}
	}
	reach, detail := deploymentReach(&d)
	return ServiceDependencyView{
		ID: "workload-" + ns + "-" + name, Label: label, Reachability: reach, Detail: detail,
	}
}

func firstReadyDeployment(snap readinessSnapshot, refs []deployRef) *ServiceDependencyView {
	for _, ref := range refs {
		key := ref.namespace + "/" + ref.name
		d, ok := snap.deployments[key]
		if !ok {
			continue
		}
		reach, detail := deploymentReach(&d)
		dep := ServiceDependencyView{
			ID:           "workload-" + ref.name,
			Label:        ref.name + " (" + ref.namespace + ")",
			Reachability: reach,
			Detail:       detail,
		}
		return &dep
	}
	return nil
}

func deploymentReach(d *appsv1.Deployment) (probe.Reachability, string) {
	desired := d.Status.Replicas
	if desired == 0 && d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	if desired == 0 {
		return probe.ReachDegraded, "scaled to zero (standby)"
	}
	ready := d.Status.ReadyReplicas
	detail := fmt.Sprintf("ready %d/%d", ready, desired)
	switch {
	case ready >= desired:
		return probe.ReachOK, detail
	case ready > 0:
		return probe.ReachDegraded, detail
	default:
		return probe.ReachFail, detail
	}
}

func findDeploymentByPrefix(snap readinessSnapshot, namespace, prefix string) *appsv1.Deployment {
	var best *appsv1.Deployment
	for key, d := range snap.deployments {
		if !strings.HasPrefix(key, namespace+"/") {
			continue
		}
		if !strings.Contains(d.Name, prefix) {
			continue
		}
		if best == nil || d.Status.ReadyReplicas > best.Status.ReadyReplicas {
			copy := d
			best = &copy
		}
	}
	return best
}

func countReadyDeployments(snap readinessSnapshot, namespace, namePrefix string) (int, string) {
	total := 0
	ready := 0
	for key, d := range snap.deployments {
		if !strings.HasPrefix(key, namespace+"/") {
			continue
		}
		if !strings.HasPrefix(d.Name, namePrefix) {
			continue
		}
		total++
		desired := d.Status.Replicas
		if desired == 0 && d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if desired > 0 && d.Status.ReadyReplicas >= desired {
			ready++
		}
	}
	if total == 0 {
		return 0, "no api-* deployments in " + namespace
	}
	return ready, fmt.Sprintf("%d/%d api deployments ready", ready, total)
}

func apiReadyReach(ready int) probe.Reachability {
	// Phase B: 4 process pods (monitor/account/market/research).
	switch {
	case ready >= 4:
		return probe.ReachOK
	case ready > 0:
		return probe.ReachDegraded
	default:
		return probe.ReachFail
	}
}

func hasCapabilityID(n NodeView, id string) bool {
	for _, c := range n.Capabilities {
		if c.ID == id {
			return true
		}
	}
	return false
}

func filterNodes(nodes []NodeView, pred func(NodeView) bool) []NodeView {
	out := make([]NodeView, 0)
	for _, n := range nodes {
		if pred(n) {
			out = append(out, n)
		}
	}
	return out
}
