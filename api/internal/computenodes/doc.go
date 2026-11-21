// Package computenodes is the compute domain's side of the cluster's nodes:
// which nodes compute powers on demand (the cluster entry's compute_nodes),
// whether such a node is active, standby or degraded, and its power: the
// power read, the Wake-on-LAN wake over SSH and the drain-and-power-off.
//
// It moved out of package cluster on 2026-11-19 (node_power.go and the
// elastic node health of elastic_node_health.go), unchanged in behaviour.
// It imports cluster (foundation, the Kubernetes adapter) and runs its
// Kubernetes calls through it; cluster does not import this package. The
// server binds it to cluster (cluster.BindComputeNodes(New().ClusterPort())),
// and cluster keeps the names the action wiring and the routes use
// (HandleNodePower, HandleWakeNode, HandlePowerOffNode) as forwarders.
//
// Contract (cluster.ComputeNodes):
//
//   - Managed says whether a node is one of the cluster entry's compute nodes.
//   - NodeMode says whether a managed node is active, standby or degraded,
//     with why: the reading is elasticreadings.NodeMode, fed with what this
//     package observes (pods pending on the node, the node's workloads, and
//     an SSH probe of its host). For a page that polls the host's answer is
//     the kept reading of elasticreadings.HostReadings; nothing may act on it.
//   - NodePower, WakeNode and PowerOffNode are the document of
//     GET /cluster/nodes/{name}/power and the two actions behind
//     POST /cluster/nodes/{name}/wake and /poweroff. For a node that is not
//     managed they answer `node "<name>" is not a managed compute node`.
package computenodes
