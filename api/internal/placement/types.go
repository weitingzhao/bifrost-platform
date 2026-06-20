package placement

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// NodeInput is a placement-neutral node snapshot (avoids import cycle with cluster).
type NodeInput struct {
	Name          string
	Architecture  string
	WorkloadLabel string
	CapabilityIDs []string
	Reachability  probe.Reachability
}

type PoolStatus string

const (
	PoolStatusLive     PoolStatus = "live"
	PoolStatusPlanned  PoolStatus = "planned"
	PoolStatusDegraded PoolStatus = "degraded"
)

type Response struct {
	ClusterID    string             `json:"cluster_id"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Pools        []PoolView         `json:"pools"`
	Rules        []RuleView         `json:"rules"`
	Violations   []ViolationView    `json:"violations"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type PoolView struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Arch        string     `json:"arch,omitempty"`
	Workload    string     `json:"workload_label,omitempty"`
	Status      PoolStatus `json:"status"`
	NodesTotal  int        `json:"nodes_total"`
	NodesReady  int        `json:"nodes_ready"`
	PlannedHost string     `json:"planned_host,omitempty"`
	NodeNames   []string   `json:"node_names"`
}

type RuleView struct {
	WorkloadClass    string             `json:"workload_class"`
	Namespace        string             `json:"namespace"`
	Services         string             `json:"services,omitempty"`
	RequiredSelector string             `json:"required_selector"`
	PoolID           string             `json:"pool_id"`
	Satisfied        bool               `json:"satisfied"`
	Reachability     probe.Reachability `json:"reachability"`
	GapReason        string             `json:"gap_reason,omitempty"`
	PlannedBinding   string             `json:"planned_binding,omitempty"`
}

type ViolationView struct {
	Severity string `json:"severity"` // critical | warning
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Evaluate builds placement status from live cluster nodes.
func Evaluate(clusterID string, nodes []NodeInput) Response {
	now := time.Now().UTC()
	pools := evaluatePools(nodes)
	rules := evaluateRules(pools)
	violations := collectViolations(pools, rules)

	reach := probe.ReachOK
	detail := "placement policies satisfied"
	for _, v := range violations {
		if v.Severity == "critical" {
			reach = probe.ReachFail
			detail = v.Message
			break
		}
	}
	if reach == probe.ReachOK {
		for _, v := range violations {
			if v.Severity == "warning" {
				reach = probe.ReachDegraded
				detail = v.Message
				break
			}
		}
	}

	return Response{
		ClusterID:    clusterID,
		Reachability: reach,
		Detail:       detail,
		Pools:        pools,
		Rules:        rules,
		Violations:   violations,
		GeneratedAt:  now,
	}
}

// Amd64CIReady returns true when at least one amd64 node is Ready (CI build pool).
func Amd64CIReady(nodes []NodeInput) bool {
	for _, n := range nodes {
		if n.Architecture == "amd64" && n.Reachability == probe.ReachOK {
			return true
		}
	}
	return false
}
