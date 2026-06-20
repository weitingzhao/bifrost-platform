package placement

import (
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type poolDef struct {
	id            string
	label         string
	arch          string
	workload      string
	capabilityID  string
	status        PoolStatus
	plannedHost   string
}

type ruleDef struct {
	workloadClass    string
	namespace        string
	services         string
	requiredSelector string
	poolID           string
	plannedBinding   string
}

var poolDefs = []poolDef{
	{id: "amd64_ci", label: "amd64 CI / Kaniko", arch: "amd64", status: PoolStatusLive},
	{id: "amd64_general", label: "amd64 general runtime", arch: "amd64", status: PoolStatusLive},
	{id: "arm64_edge", label: "arm64 edge / frontend", arch: "arm64", status: PoolStatusLive},
	{id: "nfs_client", label: "NFS PV clients", capabilityID: "nfs-client", status: PoolStatusLive},
	{id: "gpu", label: "GPU workloads", workload: "gpu", status: PoolStatusPlanned, plannedHost: "gpu-server"},
}

var ruleDefs = []ruleDef{
	{
		workloadClass:    "cicd_build",
		namespace:        "cicd",
		services:         "Tekton Kaniko build tasks",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_ci",
		plannedBinding:   "mini-pc-a / ubt-k3s-01 control-plane",
	},
	{
		workloadClass:    "cicd_control",
		namespace:        "cicd",
		services:         "Gitea · ArgoCD · Registry",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_general",
		plannedBinding:   "mini-pc-a",
	},
	{
		workloadClass:    "stg_runtime",
		namespace:        "bifrost-stg",
		services:         "9 APIs · worker · socket · frontend",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_general",
		plannedBinding:   "ubt-k3s-01 bootstrap",
	},
	{
		workloadClass:    "data",
		namespace:        "data",
		services:         "PostgreSQL · Redis · MinIO",
		requiredSelector: "node-role=postgres (planned)",
		poolID:           "amd64_general",
		plannedBinding:   "mini-pc-b / mini-pc-a",
	},
	{
		workloadClass:    "nfs_storage",
		namespace:        "kube-system · bifrost-*",
		services:         "nfs-subdir-provisioner · NFS PVC mounts",
		requiredSelector: "storage.nfs/client=true",
		poolID:           "nfs_client",
		plannedBinding:   "ubt-k3s-01/02/04",
	},
	{
		workloadClass:    "monitoring",
		namespace:        "monitoring",
		services:         "Prometheus · Loki · Grafana",
		requiredSelector: "kubernetes.io/arch=amd64",
		poolID:           "amd64_general",
		plannedBinding:   "mini-pc-c (second batch)",
	},
	{
		workloadClass:    "ai",
		namespace:        "ai",
		services:         "Ollama · Open-WebUI",
		requiredSelector: "workload=gpu",
		poolID:           "gpu",
		plannedBinding:   "gpu-server",
	},
	{
		workloadClass:    "frontend_edge",
		namespace:        "bifrost",
		services:         "trade-frontend (edge)",
		requiredSelector: "kubernetes.io/arch=arm64 (optional)",
		poolID:           "arm64_edge",
		plannedBinding:   "ops-vm-ubt-01",
	},
}

func evaluatePools(nodes []NodeInput) []PoolView {
	out := make([]PoolView, 0, len(poolDefs))
	for _, def := range poolDefs {
		pv := PoolView{
			ID:          def.id,
			Label:       def.label,
			Arch:        def.arch,
			Workload:    def.workload,
			Status:      def.status,
			PlannedHost: def.plannedHost,
			NodeNames:   []string{},
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
		if def.status == PoolStatusPlanned && pv.NodesReady == 0 {
			pv.Status = PoolStatusPlanned
		} else if pv.NodesReady == 0 && pv.NodesTotal > 0 {
			pv.Status = PoolStatusDegraded
		} else if pv.NodesReady > 0 {
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
			PlannedBinding:   def.plannedBinding,
		}
		switch {
		case pool.Status == PoolStatusPlanned:
			rv.Satisfied = false
			rv.Reachability = probe.ReachDegraded
			rv.GapReason = fmt.Sprintf("pool %s is planned — no live nodes yet", def.poolID)
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
				Code:     "gpu_pool_planned",
				Message:  "GPU pool not joined yet (P5a gpu-server pending)",
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
