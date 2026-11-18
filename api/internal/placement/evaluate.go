package placement

import (
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// poolDef says which nodes belong to a pool (by label). A pool's status is
// never written here: it comes from the nodes that match.
type poolDef struct {
	id           string
	label        string
	arch         string
	workload     string
	capabilityID string
}

type ruleDef struct {
	workloadClass    string
	namespace        string
	services         string
	requiredSelector string
	poolID           string
}

var poolDefs = []poolDef{
	{id: "amd64_ci", label: "amd64 CI / Kaniko", arch: "amd64"},
	{id: "amd64_general", label: "amd64 general runtime", arch: "amd64"},
	{id: "nfs_client", label: "NFS PV clients", capabilityID: "nfs-client"},
	{id: "gpu", label: "GPU workloads", workload: "gpu"},
}

var ruleDefs = []ruleDef{
	{
		workloadClass:    "cicd_build",
		namespace:        "cicd",
		services:         "Tekton Kaniko build tasks",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_ci",
	},
	{
		workloadClass:    "cicd_control",
		namespace:        "cicd",
		services:         "Gitea · ArgoCD · Registry",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_general",
	},
	{
		workloadClass:    "stg_runtime",
		namespace:        "bifrost-stg",
		services:         "9 APIs · worker · socket · frontend",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_general",
	},
	{
		workloadClass:    "data",
		namespace:        "data",
		services:         "PostgreSQL · Redis · MinIO",
		requiredSelector: "node-role=postgres",
		poolID:           "amd64_general",
	},
	{
		workloadClass:    "nfs_storage",
		namespace:        "kube-system · bifrost-*",
		services:         "nfs-subdir-provisioner · NFS PVC mounts",
		requiredSelector: "storage.nfs/client=true",
		poolID:           "nfs_client",
	},
	{
		workloadClass:    "monitoring",
		namespace:        "monitoring",
		services:         "Prometheus · Loki · Grafana",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_general",
	},
	{
		workloadClass:    "ai",
		namespace:        "ai",
		services:         "Ollama · Open-WebUI",
		requiredSelector: "workload=gpu",
		poolID:           "gpu",
	},
}

func evaluatePools(nodes []NodeInput) []PoolView {
	out := make([]PoolView, 0, len(poolDefs))
	for _, def := range poolDefs {
		pv := PoolView{
			ID:        def.id,
			Label:     def.label,
			Arch:      def.arch,
			Workload:  def.workload,
			NodeNames: []string{},
		}
		for _, n := range nodes {
			if !nodeMatchesPool(n, def) {
				continue
			}
			pv.NodesTotal++
			pv.NodeNames = append(pv.NodeNames, n.Name)
			if n.Reachability == probe.ReachOK {
				pv.NodesReady++
			}
		}
		// No Ready node, or no node carrying the pool's label, is degraded.
		pv.Status = PoolStatusDegraded
		if pv.NodesReady > 0 {
			pv.Status = PoolStatusLive
		}
		out = append(out, pv)
	}
	return out
}

func nodeMatchesPool(n NodeInput, def poolDef) bool {
	if def.capabilityID != "" {
		for _, id := range n.CapabilityIDs {
			if id == def.capabilityID {
				return true
			}
		}
		return false
	}
	if def.workload != "" {
		return n.WorkloadLabel == def.workload
	}
	if def.arch != "" {
		return n.Architecture == def.arch
	}
	return false
}

func evaluateRules(pools []PoolView) []RuleView {
	poolByID := make(map[string]PoolView, len(pools))
	for _, p := range pools {
		poolByID[p.ID] = p
	}
	out := make([]RuleView, 0, len(ruleDefs))
	for _, def := range ruleDefs {
		pool := poolByID[def.poolID]
		rv := RuleView{
			WorkloadClass:    def.workloadClass,
			Namespace:        def.namespace,
			Services:         def.services,
			RequiredSelector: def.requiredSelector,
			PoolID:           def.poolID,
		}
		switch {
		case pool.NodesReady > 0:
			rv.Satisfied = true
			rv.Reachability = probe.ReachOK
		case pool.NodesTotal > 0:
			rv.Satisfied = false
			rv.Reachability = probe.ReachFail
			rv.GapReason = fmt.Sprintf("pool %s has %d node(s) but none Ready", def.poolID, pool.NodesTotal)
		default:
			rv.Satisfied = false
			rv.Reachability = probe.ReachFail
			rv.GapReason = fmt.Sprintf("no nodes in pool %s", def.poolID)
		}
		out = append(out, rv)
	}
	return out
}

func collectViolations(pools []PoolView, rules []RuleView) []ViolationView {
	var out []ViolationView
	for _, p := range pools {
		if p.ID == "amd64_ci" && p.NodesReady == 0 {
			out = append(out, ViolationView{
				Severity: "critical",
				Code:     "amd64_ci_unavailable",
				Message:  "No Ready amd64 node for Tekton Kaniko builds — PipelineRun will fail with exec format error on arm64",
			})
		}
	}
	for _, r := range rules {
		if r.WorkloadClass == "cicd_build" || r.WorkloadClass == "stg_runtime" {
			if !r.Satisfied && r.PoolID != "gpu" {
				out = append(out, ViolationView{
					Severity: "critical",
					Code:     "rule_" + r.WorkloadClass,
					Message:  fmt.Sprintf("%s (%s): %s", r.WorkloadClass, r.Namespace, r.GapReason),
				})
			}
			continue
		}
		if !r.Satisfied && r.PoolID == "gpu" {
			out = append(out, ViolationView{
				Severity: "warning",
				Code:     "gpu_pool_not_ready",
				Message:  gpuPoolMessage(pools),
			})
		}
		if !r.Satisfied && r.WorkloadClass == "nfs_storage" {
			out = append(out, ViolationView{
				Severity: "warning",
				Code:     "nfs_client_unavailable",
				Message:  "No Ready node with storage.nfs/client=true — NFS PVC pods will fail to mount (label nodes after nfs-common install)",
			})
		}
	}
	return dedupeViolations(out)
}

// gpuPoolMessage describes the gpu pool from the nodes labelled workload=gpu.
func gpuPoolMessage(pools []PoolView) string {
	for _, p := range pools {
		if p.ID != "gpu" {
			continue
		}
		if p.NodesTotal == 0 {
			return "No node is labelled workload=gpu"
		}
		return fmt.Sprintf("GPU pool has %d node(s) labelled workload=gpu, none Ready: %s", p.NodesTotal, strings.Join(p.NodeNames, ", "))
	}
	return "GPU pool not evaluated"
}

func dedupeViolations(in []ViolationView) []ViolationView {
	seen := make(map[string]bool, len(in))
	out := make([]ViolationView, 0, len(in))
	for _, v := range in {
		key := v.Code + "|" + v.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

// CIPreflightReason returns empty if CI builds can run, else English reason.
func CIPreflightReason(nodes []NodeInput) string {
	if Amd64CIReady(nodes) {
		return ""
	}
	var arches []string
	for _, n := range nodes {
		arch := n.Architecture
		if arch == "" {
			arch = "unknown"
		}
		arches = append(arches, fmt.Sprintf("%s=%s", n.Name, arch))
	}
	if len(arches) == 0 {
		return "cluster has no nodes — cannot schedule amd64 Kaniko builds"
	}
	return "no Ready amd64 node for CI builds (need kubernetes.io/arch=amd64); nodes: " + strings.Join(arches, ", ")
}
