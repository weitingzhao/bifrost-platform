// Package kubeapi holds the shapes of foundation's Kubernetes adapter that
// other domains pass through it: the request and answer of an actuation
// (rollout restart, scale), a pod's logs, the error a client read returns, and
// the node pool shapes the compute domain's evaluator reads and answers. Types
// only; the adapter itself stays in cluster, which aliases these names, so
// that a domain can declare the interface it needs (LAW-10) without importing
// cluster, a package mixed across domains. Moved from cluster on 2026-11-19,
// unchanged.
package kubeapi

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type ActuationResponse struct {
	OK          bool      `json:"ok"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	Changed     bool      `json:"changed"`
	Message     string    `json:"message"`
	Namespaces  []string  `json:"namespaces,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}

type RolloutRestartRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

type ScaleRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type LogsResponse struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	TailLines int64  `json:"tail_lines"`
	Logs      string `json:"logs"`
}

func (r RolloutRestartRequest) Target() string {
	return r.Namespace + "/" + r.Kind + "/" + r.Name
}

func (r ScaleRequest) Target() string {
	return r.Namespace + "/" + r.Kind + "/" + r.Name
}

type ClientError struct {
	Reachability probe.Reachability
	Detail       string
}

func (e *ClientError) Error() string {
	return e.Detail
}

// PoolNode is a node as a pool evaluator reads it.
type PoolNode struct {
	Name          string
	Architecture  string
	WorkloadLabel string
	CapabilityIDs []string
	Reachability  probe.Reachability
}

// The pool statuses service readiness tells apart; any other is live.
const (
	PoolPlanned  = "planned"
	PoolDegraded = "degraded"
)

// PoolReading is one pool as service readiness reads it.
type PoolReading struct {
	ID         string
	Status     string
	NodesTotal int
	NodesReady int
}

// PoolEvaluation is an evaluator's answer: Body is the document of
// GET /cluster/placement, as the evaluator writes it; Pools is what service
// readiness reads.
type PoolEvaluation struct {
	Body  any
	Pools []PoolReading
}

// PoolEvaluator evaluates the pools of a cluster's nodes. reach and detail
// are how the node list itself was read; an evaluator carries a reading that
// is not ok into its document.
type PoolEvaluator func(clusterID string, nodes []PoolNode, reach probe.Reachability, detail string) PoolEvaluation
