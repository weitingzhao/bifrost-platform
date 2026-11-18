package elasticreadings

import (
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

func hasCapabilityID(n resourceview.ElasticNode, id string) bool {
	for _, c := range n.CapabilityIDs {
		if c == id {
			return true
		}
	}
	return false
}

func filterNodes(nodes []resourceview.ElasticNode, pred func(resourceview.ElasticNode) bool) []resourceview.ElasticNode {
	var out []resourceview.ElasticNode
	for _, n := range nodes {
		if pred(n) {
			out = append(out, n)
		}
	}
	return out
}

// GPUDomain is the GPU / AI domain of service readiness.
func GPUDomain(snap resourceview.ElasticSnapshot) resourceview.ServiceDomainView {
	gpuNodes := filterNodes(snap.Nodes, func(n resourceview.ElasticNode) bool {
		return hasCapabilityID(n, "gpu-pool")
	})
	demand := snap.DesiredReplicas("ai", "ollama") > 0
	deps := []resourceview.ServiceDependencyView{
		elasticPoolDep(snap, "gpu", "GPU node pool", gpuNodes, demand),
	}
	if len(gpuNodes) == 0 {
		deps = append(deps, resourceview.ServiceDependencyView{
			ID: "gpu-nodes", Label: "GPU nodes",
			Reachability: probe.ReachDegraded, Detail: "no workload=gpu node registered",
		})
	} else {
		deps = append(deps, gpuNodesDep(gpuNodes, demand))
	}
	wl := snap.DeploymentDep("ai", "ollama", "Ollama (ai)")
	if wl.Reachability == probe.ReachDegraded && strings.Contains(wl.Detail, "not deployed") {
		wl.Detail = "scaled to zero or not deployed (make gpu-ollama-up)"
	}
	deps = append(deps, wl)
	domain := snap.Finalize("gpu", "GPU / AI", deps, "Elastic compute on gpu-server")
	return applyElasticDomainVerdict(domain, gpuNodes, demand, "Standby — no demand", "Compute needed but GPU node offline")
}

// WorkersDomain is the general workers domain of service readiness: standby
// when the only gaps left are dependencies expected to be off.
func WorkersDomain(snap resourceview.ElasticSnapshot) resourceview.ServiceDomainView {
	deps := []resourceview.ServiceDependencyView{
		snap.PoolDep("amd64_general", "amd64 general pool"),
		snap.NodeCovDep("nfs-client", "NFS client nodes"),
	}
	if wl := snap.DaemonDep(); wl != nil {
		deps = append(deps, *wl)
	} else {
		deps = append(deps, resourceview.ServiceDependencyView{
			ID: "workload-daemon", Label: "Trading daemon",
			Reachability: probe.ReachDegraded, Detail: "not deployed",
		})
	}
	domain := snap.Finalize("workers", "General workers", deps, "Daemon · data pipelines")
	if domain.Status == "partial" || domain.Status == "standby" {
		onlyStandby := true
		for _, d := range domain.Dependencies {
			if d.Reachability == probe.ReachOK {
				continue
			}
			if !snap.IsStandby(d) {
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

// WarehouseDomain is the data warehouse domain of service readiness.
func WarehouseDomain(snap resourceview.ElasticSnapshot) resourceview.ServiceDomainView {
	whNodes := filterNodes(snap.Nodes, func(n resourceview.ElasticNode) bool {
		return hasCapabilityID(n, "warehouse") || hasCapabilityID(n, "gpu-pool")
	})
	demand := snap.DesiredReplicas("data-warehouse", "minio") > 0
	deps := []resourceview.ServiceDependencyView{
		elasticNodeCovDep(snap, "warehouse", "Warehouse nodes", demand),
		elasticPoolDep(snap, "gpu", "GPU / warehouse host", whNodes, demand),
	}
	wl := snap.DeploymentDep("data-warehouse", "minio", "MinIO (data-warehouse)")
	if wl.Reachability == probe.ReachDegraded && (strings.Contains(wl.Detail, "standby") || strings.Contains(wl.Detail, "scaled to zero")) {
		wl.Detail = "scaled to zero — make gpu-warehouse-up to start"
	}
	deps = append(deps, wl)
	domain := snap.Finalize("warehouse", "Data warehouse", deps, "MinIO object store on gpu-server")
	return applyElasticDomainVerdict(domain, whNodes, demand, "Standby — no demand", "Compute needed but warehouse node offline")
}

func gpuNodesDep(nodes []resourceview.ElasticNode, demand bool) resourceview.ServiceDependencyView {
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
	return resourceview.ServiceDependencyView{ID: "gpu-nodes", Label: "GPU nodes", Reachability: reach, Detail: detail}
}

func nodesAllElasticStandby(nodes []resourceview.ElasticNode) bool {
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
func elasticPoolDep(snap resourceview.ElasticSnapshot, id, label string, nodes []resourceview.ElasticNode, demand bool) resourceview.ServiceDependencyView {
	if nodesAllElasticStandby(nodes) || (len(nodes) > 0 && countElasticStandby(nodes) == len(nodes)) {
		if demand {
			return resourceview.ServiceDependencyView{
				ID: "pool-" + id, Label: label,
				Reachability: probe.ReachFail,
				Detail:       "Compute needed but node offline",
			}
		}
		return resourceview.ServiceDependencyView{
			ID: "pool-" + id, Label: label,
			Reachability: probe.ReachOK,
			Detail:       "standby — no demand",
		}
	}
	return snap.PoolDep(id, label)
}

func countElasticStandby(nodes []resourceview.ElasticNode) int {
	n := 0
	for _, node := range nodes {
		if node.ElasticMode == "standby" {
			n++
		}
	}
	return n
}

// elasticNodeCovDep — warehouse/gpu coverage when the only matching nodes are elastic standby.
func elasticNodeCovDep(snap resourceview.ElasticSnapshot, id, label string, demand bool) resourceview.ServiceDependencyView {
	base := snap.NodeCovDep(id, label)
	if base.Reachability == probe.ReachOK {
		return base
	}
	c, ok := snap.NodeCoverage[id]
	if !ok || c.NodesTotal == 0 {
		return base
	}
	// All labeled nodes present but none Ready — treat as standby when no demand.
	if c.NodesReady == 0 && !demand {
		return resourceview.ServiceDependencyView{
			ID: id, Label: label,
			Reachability: probe.ReachOK,
			Detail:       fmt.Sprintf("standby — no demand (%s)", strings.Join(c.NodeNames, ", ")),
		}
	}
	if c.NodesReady == 0 && demand {
		return resourceview.ServiceDependencyView{
			ID: id, Label: label,
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("Compute needed but node offline (%s)", strings.Join(c.NodeNames, ", ")),
		}
	}
	return base
}

// applyElasticDomainVerdict — product formula for GPU/Warehouse elastic domains.
func applyElasticDomainVerdict(
	domain resourceview.ServiceDomainView,
	nodes []resourceview.ElasticNode,
	demand bool,
	standbySummary, demandSummary string,
) resourceview.ServiceDomainView {
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
