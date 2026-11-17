package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Traefik IngressRoute — Trade edge is trade-gateway CR, not a per-NS nginx Deployment.
var traefikIngressRouteGVR = schema.GroupVersionResource{
	Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes",
}

const tradeGatewayIngressRoute = "trade-gateway"

type ServiceDependencyView = resourceview.ServiceDependencyView
type ServiceDomainView = resourceview.ServiceDomainView

type ServiceReadinessResponse struct {
	ClusterID    string              `json:"cluster_id"`
	Reachability probe.Reachability  `json:"reachability"`
	Detail       string              `json:"detail"`
	Domains      []ServiceDomainView `json:"domains"`
	GeneratedAt  time.Time           `json:"generated_at"`
}

type readinessSnapshot struct {
	nodes         []NodeView
	clusterCaps   map[string]ClusterCapabilityView
	nodeCoverage  map[string]CapabilityCoverageView
	pools         map[string]PoolReading
	deployments   map[string]appsv1.Deployment
	ingressRoutes map[string]bool // "namespace/name" → present
	minio         minioBackend    // where Service data/minio points (in-cluster vs external)
	appEnvs       []config.AppEnv // application environments (environments.yaml)
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
	poolMap := make(map[string]PoolReading, len(placementResp.Pools))
	for _, p := range placementResp.Pools {
		poolMap[p.ID] = p
	}

	deployments, depErr := s.loadReadinessDeployments(ctx)
	ingressRoutes, irErr := s.loadTradeGatewayIngressRoutes(ctx)
	var minio minioBackend
	if clientset, _, err := s.buildClient(); err == nil {
		minio = s.resolveMinioBackend(ctx, clientset)
	}
	snap := readinessSnapshot{
		appEnvs:       s.appEnvs,
		minio:         minio,
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
	namespaces := append(s.AppNamespaces(),
		"cicd", "cnpg-system", "data", "data-warehouse", "ai", "tekton-pipelines",
		"kube-system", // Traefik ingress controller
	)
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
	return listTradeGatewayIngressRoutes(ctx, dyn, s.AppNamespaces())
}

func listTradeGatewayIngressRoutes(ctx context.Context, dyn dynamic.Interface, namespaces []string) (map[string]bool, error) {
	out := make(map[string]bool)
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
	deps = append(deps, minioDepLabeled(snap, "MinIO backup target"))

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
	refs := snap.appDeployRefs("postgres")
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
	for _, env := range snap.appEnvs {
		if dep := embeddedRedisDep(snap, env.Namespace); dep != nil {
			deps = append(deps, *dep)
		}
	}

	domain := finalizeDomain("redis", "Redis", deps, "Bitnami live @ data NS · Celery queue retired Wave 5")
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
	// Queue targets are standby/retired; live+embedded OK must not force degraded.
	if domain.Status == "ready" || domain.Reachability == probe.ReachOK {
		if embeddedActive > 0 {
			domain.Summary = fmt.Sprintf(
				"Embedded redis in %d env(s) · queue retired Wave 5 · Bitnami live optional",
				embeddedActive,
			)
		} else if targetsReady > 0 {
			domain.Summary = fmt.Sprintf(
				"%d/%d data NS targets · queue standby/retired",
				targetsReady, len(redisTargetCatalog),
			)
		}
		return domain
	}
	if domain.Status == "ready" {
		domain.Status = "partial"
		domain.Reachability = probe.ReachDegraded
	}
	if embeddedActive > 0 && targetsReady == 0 {
		domain.Summary = fmt.Sprintf("Embedded redis in %d env(s) · Bitnami live optional", embeddedActive)
	} else if targetsReady > 0 {
		domain.Summary = fmt.Sprintf("%d/%d data NS targets · %d embedded active", targetsReady, len(redisTargetCatalog), embeddedActive)
	} else {
		domain.Summary = "No Redis live coverage"
	}
	return domain
}

func evalGPUDomain(snap readinessSnapshot) ServiceDomainView {
	if computeReadings.GPUDomain == nil {
		return unboundDomain("gpu", "GPU / AI", "compute-readings", "Compute readings")
	}
	return computeReadings.GPUDomain(elasticInput(snap))
}

func evalWarehouseDomain(snap readinessSnapshot) ServiceDomainView {
	if computeReadings.WarehouseDomain == nil {
		return unboundDomain("warehouse", "Data warehouse", "compute-readings", "Compute readings")
	}
	return computeReadings.WarehouseDomain(elasticInput(snap))
}

// unboundDomain is a domain whose reader was not bound: not ready rather
// than unknown, since an unknown dependency counts as satisfied in
// domainStatusFromDeps and unbound must not read as ready.
func unboundDomain(id, label, depID, depLabel string) ServiceDomainView {
	return finalizeDomain(id, label, []ServiceDependencyView{{ID: depID, Label: depLabel,
		Reachability: probe.ReachFail, Detail: "no reader is bound"}}, "")
}

// elasticInput is what service readiness has gathered, in the shape compute's
// readings take (resourceview.ElasticSnapshot).
func elasticInput(snap readinessSnapshot) resourceview.ElasticSnapshot {
	nodes := make([]resourceview.ElasticNode, len(snap.nodes))
	for i, n := range snap.nodes {
		nodes[i] = resourceview.ElasticNode{Name: n.Name, Status: n.Status, ElasticMode: n.ElasticMode,
			Unschedulable: n.Unschedulable, Reachability: n.Reachability, CapabilityIDs: capabilityIDsFromView(n)}
	}
	coverage := make(map[string]resourceview.ElasticCoverage, len(snap.nodeCoverage))
	for id, c := range snap.nodeCoverage {
		coverage[id] = resourceview.ElasticCoverage{NodesReady: c.NodesReady, NodesTotal: c.NodesTotal, NodeNames: c.NodeNames}
	}
	return resourceview.ElasticSnapshot{
		Nodes:        nodes,
		NodeCoverage: coverage,
		DesiredReplicas: func(ns, name string) int32 {
			d, ok := snap.deployments[ns+"/"+name]
			if !ok {
				return 0
			}
			if d.Spec.Replicas != nil {
				return *d.Spec.Replicas
			}
			return d.Status.Replicas
		},
		PoolDep:       func(id, label string) ServiceDependencyView { return poolDep(snap, id, label) },
		NodeCovDep:    func(id, label string) ServiceDependencyView { return nodeCovDep(snap, id, label) },
		DeploymentDep: func(ns, name, label string) ServiceDependencyView { return deploymentDep(snap, ns, name, label) },
		Finalize:      finalizeDomain,
	}
}

func evalWorkersDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		poolDep(snap, "amd64_general", "amd64 general pool"),
		nodeCovDep(snap, "nfs-client", "NFS client nodes"),
	}
	wl := firstReadyDeployment(snap, snap.appDeployRefs("daemon"))
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
	if appReadings.ApplicationsDomain == nil {
		// Not ready rather than unknown: an unknown dependency counts as
		// satisfied in domainStatusFromDeps, and unbound must not read as ready.
		return finalizeDomain("applications", "General applications", []ServiceDependencyView{{ID: "application-readings", Label: "Application readings",
			Reachability: probe.ReachFail, Detail: "no reader of the applications is bound"}}, "")
	}
	return appReadings.ApplicationsDomain(tenantReadinessInput(snap))
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
	standbyText := strings.Contains(lower, "standby") ||
		strings.Contains(lower, "scaled to zero") ||
		strings.Contains(lower, "retired wave 5")
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
	return resourceview.AggregateServiceReadiness(domains)
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
	case PoolPlanned:
		reach = probe.ReachDegraded
		detail = "pool planned — no live nodes"
	case PoolDegraded:
		reach = probe.ReachDegraded
		detail = fmt.Sprintf("pool degraded — %d/%d ready", p.NodesReady, p.NodesTotal)
	}
	if p.NodesReady == 0 && p.Status != PoolPlanned {
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

// appDeployRefs is one ref to the named Deployment per application namespace.
func (snap readinessSnapshot) appDeployRefs(name string) []deployRef {
	out := make([]deployRef, 0, len(snap.appEnvs))
	for _, e := range snap.appEnvs {
		out = append(out, deployRef{e.Namespace, name})
	}
	return out
}

func tenantReadinessInput(snap readinessSnapshot) resourceview.ApplicationsSnapshot {
	return resourceview.ApplicationsSnapshot{Deployments: snap.deployments, IngressRoutes: snap.ingressRoutes, Environments: snap.appEnvs, Schedulable: schedulableAnyDep(snap, "Schedulable nodes"), IngressController: deploymentDep(snap, "kube-system", "traefik", "Traefik ingress controller"), DeploymentReach: deploymentReach, Finalize: finalizeDomain}
}
