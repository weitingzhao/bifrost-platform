package topology

import (
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type NodeView struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Host         string   `json:"host,omitempty"`
	Group        string   `json:"group"`
	ComposeRoles []string `json:"compose_roles"`
	K3sRoles     []string `json:"k3s_roles"`
	InK3sCluster bool     `json:"in_k3s_cluster"`
	Grid         config.GridPos `json:"grid"`
	Status       probe.Reachability `json:"status"`
	Detail       string   `json:"detail"`
}

type EdgeView struct {
	ID           string             `json:"id"`
	From         string             `json:"from"`
	To           string             `json:"to"`
	Label        string             `json:"label"`
	Kind         string             `json:"kind"`
	MatrixTarget string             `json:"matrix_target,omitempty"`
	Status       probe.Reachability `json:"status"`
	Detail       string             `json:"detail"`
}

type Response struct {
	Environment     string    `json:"environment"`
	Label           string    `json:"label"`
	DeploymentPhase string    `json:"deployment_phase"`
	GeneratedAt     time.Time `json:"generated_at"`
	Nodes           []NodeView `json:"nodes"`
	Edges           []EdgeView `json:"edges"`
}

func Build(
	topo *config.TopologyFile,
	env config.Environment,
	matrix probe.MatrixResponse,
) Response {
	targetByID := map[string]probe.Target{}
	for _, t := range matrix.Targets {
		targetByID[t.ID] = t
	}

	hostToNode := map[string]string{}
	for _, n := range topo.Nodes {
		if n.Host != "" {
			hostToNode[n.Host] = n.ID
		}
	}
	// Map datastore hosts from environment to nodes
	if env.Postgres.Host != "" {
		if id, ok := hostToNode[env.Postgres.Host]; ok {
			_ = id
		}
	}

	nodeTargets := inferNodeTargets(topo, env, targetByID)

	edges := make([]EdgeView, 0)
	for _, e := range topo.Edges {
		if !edgeAppliesToEnv(e, env.ID) {
			continue
		}
		ev := EdgeView{
			ID: e.ID, From: e.From, To: e.To, Label: e.Label, Kind: e.Kind,
			MatrixTarget: e.MatrixTarget,
		}
		if e.MatrixTarget != "" {
			if t, ok := targetByID[e.MatrixTarget]; ok {
				ev.Status = t.Reachability
				ev.Detail = t.Detail
			} else {
				ev.Status = probe.ReachUnknown
				ev.Detail = "no probe data"
			}
		} else if e.Kind == "ib" {
			ev.Status = probe.ReachUnknown
			ev.Detail = "IB/TWS external — not probed by platform (by design)"
		} else {
			ev.Status = probe.ReachUnknown
			ev.Detail = "static link"
		}
		edges = append(edges, ev)
	}

	nodes := make([]NodeView, 0, len(topo.Nodes))
	for _, n := range topo.Nodes {
		nv := NodeView{
			ID: n.ID, Label: n.Label, Host: n.Host, Group: n.Group,
			ComposeRoles: n.ComposeRoles, K3sRoles: n.K3sRoles,
			InK3sCluster: n.InK3sCluster, Grid: n.Grid,
		}
		targets := nodeTargets[n.ID]
		if len(targets) == 0 {
			if n.Host == "" && n.Group == "external" {
				nv.Status = probe.ReachUnknown
				nv.Detail = "external — configure host in topology.yaml"
			} else if n.ID == "mini-pc-c" || n.Host == "" {
				nv.Status = probe.ReachUnknown
				nv.Detail = "not deployed / host not configured"
			} else {
				nv.Status = probe.ReachUnknown
				nv.Detail = "no linked probes"
			}
		} else {
			nv.Status, nv.Detail = aggregateTargets(targets, targetByID)
		}
		nodes = append(nodes, nv)
	}

	return Response{
		Environment:     env.ID,
		Label:           env.Label,
		DeploymentPhase: topo.DeploymentPhase,
		GeneratedAt:     time.Now().UTC(),
		Nodes:           nodes,
		Edges:           edges,
	}
}

func edgeAppliesToEnv(e config.TopologyEdge, envID string) bool {
	if len(e.Environments) == 0 {
		return true
	}
	for _, id := range e.Environments {
		if id == envID {
			return true
		}
	}
	return false
}

func inferNodeTargets(
	topo *config.TopologyFile,
	env config.Environment,
	targetByID map[string]probe.Target,
) map[string][]string {
	result := make(map[string][]string)

	add := func(nodeID, targetID string) {
		if nodeID == "" || targetID == "" {
			return
		}
		result[nodeID] = append(result[nodeID], targetID)
	}

	// Edge-derived targets
	for _, e := range topo.Edges {
		if !edgeAppliesToEnv(e, env.ID) || e.MatrixTarget == "" {
			continue
		}
		add(e.From, e.MatrixTarget)
		add(e.To, e.MatrixTarget)
	}

	// Host match: postgres/redis targets → node by env host
	for id := range targetByID {
		if strings.HasPrefix(id, "api-") || id == "nginx-spa" || id == "ops-capabilities" {
			if env.ID == "dev" {
				add("macbook", id)
				add("mac-mini-1", id)
			} else {
				add("mini-pc-a", id)
			}
		}
		if id == "postgres" {
			for _, n := range topo.Nodes {
				if n.Host == env.Postgres.Host {
					add(n.ID, id)
				}
			}
		}
		if id == "redis" {
			for _, n := range topo.Nodes {
				if n.Host == env.Redis.Host {
					add(n.ID, id)
				}
			}
			if env.Redis.Host == "127.0.0.1" || env.Redis.Host == "localhost" {
				add("macbook", id)
			}
		}
	}

	// Explicit matrix_targets on nodes
	for _, n := range topo.Nodes {
		for _, tid := range n.MatrixTargets {
			add(n.ID, tid)
		}
	}

	return result
}

func aggregateTargets(targetIDs []string, byID map[string]probe.Target) (probe.Reachability, string) {
	worst := probe.ReachOK
	details := make([]string, 0)
	seen := map[string]bool{}

	for _, id := range targetIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		t, ok := byID[id]
		if !ok {
			worst = worse(worst, probe.ReachUnknown)
			continue
		}
		worst = worse(worst, t.Reachability)
		details = append(details, id+": "+t.Detail)
	}
	if len(details) == 0 {
		return probe.ReachUnknown, "no probe data"
	}
	return worst, strings.Join(details, "; ")
}

func worse(a, b probe.Reachability) probe.Reachability {
	rank := map[probe.Reachability]int{
		probe.ReachOK: 0, probe.ReachDegraded: 1, probe.ReachUnknown: 2, probe.ReachFail: 3,
	}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
