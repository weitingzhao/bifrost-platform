package resourceview

import "github.com/weitingzhao/bifrost-platform/api/internal/probe"

// The shapes the cluster package hands to whoever reads the elastic compute
// domains (GPU, data warehouse) of service readiness, and gets back. They are
// the base's: cluster gathers the nodes, coverage and deployments; when a
// pool that is powered off counts as standby and when as missing compute is
// compute's reading (elasticreadings). Those functions lived in cluster until
// 2026-11-16.

// ElasticNode is a node as the elastic readings read it.
type ElasticNode struct {
	Name          string
	Status        string
	ElasticMode   string
	Unschedulable bool
	Reachability  probe.Reachability
	CapabilityIDs []string
}

// ElasticCoverage is how many nodes carry a capability and how many are ready.
type ElasticCoverage struct {
	NodesReady int
	NodesTotal int
	NodeNames  []string
}

// ElasticSnapshot is what service readiness has gathered when it asks for an
// elastic domain. The four functions are cluster's own dependency readings,
// so a domain is assembled from the same pieces as the others.
type ElasticSnapshot struct {
	Nodes           []ElasticNode
	NodeCoverage    map[string]ElasticCoverage
	DesiredReplicas func(namespace, name string) int32
	PoolDep         func(id, label string) ServiceDependencyView
	NodeCovDep      func(id, label string) ServiceDependencyView
	DeploymentDep   func(namespace, name, label string) ServiceDependencyView
	Finalize        func(id, label string, deps []ServiceDependencyView, purpose string) ServiceDomainView
}

// ElasticNodeObservation is what the cluster package can observe of an
// on-demand node when it asks whether the node is active, standby or
// degraded. The three functions are asked in the reader's order and only
// when needed: the last is an SSH probe of the host.
type ElasticNodeObservation struct {
	Ready                bool
	PendingPods          func() (int, error)
	WorkloadsUnsatisfied func() (bool, string)
	HostReachable        func() bool
}

// The modes of an on-demand node.
const (
	ElasticNodeActive   = "active"
	ElasticNodeStandby  = "standby"
	ElasticNodeDegraded = "degraded"
)
