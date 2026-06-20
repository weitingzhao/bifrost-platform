package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

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
	nodes        []NodeView
	clusterCaps  map[string]ClusterCapabilityView
	nodeCoverage map[string]CapabilityCoverageView
	pools        map[string]placement.PoolView
	deployments  map[string]appsv1.Deployment
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
	snap := readinessSnapshot{
		nodes:        nodesResp.Nodes,
		clusterCaps:  capMap,
		nodeCoverage: covMap,
		pools:        poolMap,
		deployments:  deployments,
	}
	if depErr != nil {
		return snap, depErr
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
		"cicd", "data-warehouse", "ai", "tekton-pipelines",
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
		clusterCapDep(snap, "storage-class-nfs-hot", "StorageClass nfs-hot"),
		clusterCapDep(snap, "storage-class-nfs-cold", "StorageClass nfs-cold"),
		clusterCapDep(snap, "nfs-provisioner-hot", "NFS provisioner (hot)"),
		clusterCapDep(snap, "nfs-provisioner-cold", "NFS provisioner (cold)"),
		nodeCovDep(snap, "nfs-client", "NFS client nodes"),
		schedulableArchDep(snap, "amd64", "Schedulable amd64 nodes"),
	}
	wl := firstReadyDeployment(snap, []deployRef{
		{"bifrost-stg", "postgres"},
		{"bifrost-dev", "postgres"},
		{"bifrost-prod", "postgres"},
	})
	if wl != nil {
		deps = append(deps, *wl)
	} else {
		deps = append(deps, ServiceDependencyView{
			ID: "workload-postgres", Label: "PostgreSQL workload",
			Reachability: probe.ReachDegraded, Detail: "not deployed in bifrost-stg/dev/prod",
		})
	}
	return finalizeDomain("database", "PostgreSQL", deps, "Persistent DB on nfs-hot (migrate from local-path)")
}

func evalRedisDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		schedulableArchDep(snap, "amd64", "Schedulable amd64 nodes"),
		clusterCapDep(snap, "storage-class-nfs-hot", "StorageClass nfs-hot (optional PVC)"),
	}
	wl := firstReadyDeployment(snap, []deployRef{
		{"bifrost-stg", "redis"},
		{"bifrost-dev", "redis"},
		{"bifrost-prod", "redis"},
	})
	if wl != nil {
		deps = append(deps, *wl)
	} else {
		deps = append(deps, ServiceDependencyView{
			ID: "workload-redis", Label: "Redis workload",
			Reachability: probe.ReachDegraded, Detail: "not deployed",
		})
	}
	return finalizeDomain("redis", "Redis", deps, "Cache / queue backing store")
}

func evalGPUDomain(snap readinessSnapshot) ServiceDomainView {
	gpuNodes := filterNodes(snap.nodes, func(n NodeView) bool {
		return hasCapabilityID(n, "gpu-pool")
	})
	deps := []ServiceDependencyView{
		poolDep(snap, "gpu", "GPU node pool"),
	}
	if len(gpuNodes) == 0 {
		deps = append(deps, ServiceDependencyView{
			ID: "gpu-nodes", Label: "GPU nodes",
			Reachability: probe.ReachDegraded, Detail: "no workload=gpu node registered",
		})
	} else {
		deps = append(deps, gpuNodesDep(gpuNodes))
	}
	wl := deploymentDep(snap, "ai", "ollama", "Ollama (ai)")
	if wl.Reachability == probe.ReachDegraded && strings.Contains(wl.Detail, "not deployed") {
		wl.Detail = "scaled to zero or not deployed (make gpu-ollama-up)"
	}
	deps = append(deps, wl)
	return finalizeDomain("gpu", "GPU / AI", deps, "Elastic compute on gpu-server")
}

func evalWarehouseDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		nodeCovDep(snap, "warehouse", "Warehouse nodes"),
		poolDep(snap, "gpu", "GPU / warehouse host"),
	}
	wl := deploymentDep(snap, "data-warehouse", "minio", "MinIO (data-warehouse)")
	if wl.Reachability == probe.ReachDegraded && strings.Contains(wl.Detail, "standby") {
		wl.Detail = "scaled to zero — make gpu-warehouse-up to start"
	}
	deps = append(deps, wl)
	domain := finalizeDomain("warehouse", "Data warehouse", deps, "MinIO object store on gpu-server")
	if domain.Status == "ready" && wl.Reachability == probe.ReachDegraded {
		domain.Status = "standby"
		domain.Reachability = probe.ReachDegraded
		domain.Summary = "Host ready · MinIO standby (scale up when needed)"
	}
	return domain
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
	return finalizeDomain("workers", "General workers", deps, "Daemon · Celery · data pipelines")
}

func evalApplicationsDomain(snap readinessSnapshot) ServiceDomainView {
	deps := []ServiceDependencyView{
		schedulableAnyDep(snap, "Schedulable nodes"),
		poolDep(snap, "arm64_edge", "arm64 edge pool (optional)"),
	}
	deps = append(deps, deploymentDep(snap, "bifrost-stg", "nginx", "Ingress nginx"))
	deps = append(deps, deploymentDep(snap, "bifrost-stg", "frontend", "Trade frontend"))
	apiReady, apiDetail := countReadyDeployments(snap, "bifrost-stg", "api-")
	deps = append(deps, ServiceDependencyView{
		ID: "apis", Label: "FastAPI services",
		Reachability: apiReadyReach(apiReady),
		Detail:       apiDetail,
	})
	return finalizeDomain("applications", "General applications", deps, "Frontend · nginx · 9 API domains")
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

func domainStatusFromDeps(deps []ServiceDependencyView) (status string, reach probe.Reachability, summary string) {
	fail := 0
	degraded := 0
	standby := 0
	var gaps []string
	for _, d := range deps {
		switch d.Reachability {
		case probe.ReachFail:
			fail++
			gaps = append(gaps, d.Label)
		case probe.ReachDegraded:
			degraded++
			if strings.Contains(strings.ToLower(d.Detail), "standby") || strings.Contains(strings.ToLower(d.Detail), "scaled to zero") {
				standby++
			}
		}
	}
	switch {
	case fail > 0:
		return "unavailable", probe.ReachFail, fmt.Sprintf("Blocked: %s", strings.Join(gaps, ", "))
	case standby > 0 && fail == 0 && degraded == standby:
		return "standby", probe.ReachDegraded, "Infrastructure ready · workloads scaled to zero"
	case degraded > 0:
		return "partial", probe.ReachDegraded, fmt.Sprintf("%d dependency gap(s)", degraded)
	default:
		return "ready", probe.ReachOK, "All dependencies satisfied"
	}
}

func aggregateServiceReadiness(domains []ServiceDomainView) (probe.Reachability, string) {
	fail := 0
	degraded := 0
	for _, d := range domains {
		switch d.Reachability {
		case probe.ReachFail:
			fail++
		case probe.ReachDegraded:
			degraded++
		}
	}
	switch {
	case fail > 0:
		return probe.ReachDegraded, fmt.Sprintf("%d domain(s) unavailable", fail)
	case degraded > 0:
		return probe.ReachDegraded, fmt.Sprintf("%d domain(s) partial or standby", degraded)
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

func gpuNodesDep(nodes []NodeView) ServiceDependencyView {
	ready := 0
	names := make([]string, 0, len(nodes))
	standby := 0
	for _, n := range nodes {
		names = append(names, n.Name)
		if n.ElasticMode == "standby" || n.Unschedulable {
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
		reach = probe.ReachDegraded
		detail = fmt.Sprintf("standby/cordoned (%s)", strings.Join(names, ", "))
	} else if ready == 0 {
		reach = probe.ReachFail
		detail = "GPU nodes not ready"
	}
	return ServiceDependencyView{ID: "gpu-nodes", Label: "GPU nodes", Reachability: reach, Detail: detail}
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
	switch {
	case ready >= 5:
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
