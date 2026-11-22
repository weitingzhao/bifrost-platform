package delivery

import (
	"context"
	"sync/atomic"

	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// How delivery reaches the compute domain's node placement (placement).
// Delivery says what its tasks need, in its own words: the resources, the
// disk, and which kinds of node they must never go to (NodeRole). Compute
// says how such a node is labelled, chooses the node, and checks the build
// pool. The server binds it (server/delivery_ports.go). Until 2026-11-21 this
// package imported placement and named its label rules.

// NodeRole is a kind of node a task must never be given. Which label marks
// it is compute's (PlacementReadings.RoleLabel).
type NodeRole string

const (
	// NodeEtcd is a node that runs the cluster's etcd.
	NodeEtcd NodeRole = "etcd"
	// NodeDatabasePrimary is the database primary; a failover moves it.
	NodeDatabasePrimary NodeRole = "database-primary"
)

// TaskProfile is what a task needs of a node.
type TaskProfile struct {
	DiskHeavy   bool
	MaxDiskTier int     // the slowest disk tier a disk-heavy task may go to
	CPUCores    float64 // unrequested CPU the node must have
	MemoryBytes float64 // unrequested memory the node must have
	// MinAvailableMemory is the memory the node must really have available,
	// in bytes; 0 asks nothing beyond the general floor.
	MinAvailableMemory float64
	// Spread picks the least loaded node that is not on the slow disk.
	Spread bool
	// ExcludeNodes: a node of any of these roles is never a candidate.
	ExcludeNodes []NodeRole
}

// NodeMetrics is one node's measurements, as the server's metrics reader
// answers them (compute reads them from Prometheus).
type NodeMetrics struct {
	WriteLatencySeconds float64
	CPUBusy             float64 // 0 to 1, last 5 minutes
	DiskBusy            float64 // 0 to 1, last 5 minutes
	MemoryAvailable     float64 // 0 to 1
	WrittenBytes2h      float64 // written to the node's disks in the last two hours
	RootDiskUsed        float64 // 0 to 1; missing reads leave zero and do not stop work
}

// NodeMetricsReader answers by node InternalIP.
type NodeMetricsReader func(ctx context.Context) (map[string]NodeMetrics, error)

// BuildPlacement is the node chosen and why; Node "" with a reason when
// nothing fits or nothing could be read.
type BuildPlacement struct {
	Node string
	Why  string
}

// NodeInput is what the build pool check reads of one node.
type NodeInput struct {
	Name          string
	Architecture  string
	WorkloadLabel string
	Reachability  probe.Reachability
}

// PlacementReadings is what delivery asks of compute.
type PlacementReadings struct {
	// HostIDLabel is the node label that names a node's host in selectors.
	HostIDLabel string
	// RoleLabel is the label that marks a node of the role.
	RoleLabel func(role NodeRole) (key, value string, ok bool)
	// Place chooses the node for the task now (staying on current when close
	// enough); false when metrics is nil.
	Place func(ctx context.Context, clientset func() (kubernetes.Interface, error), metrics NodeMetricsReader, task TaskProfile, current string) (BuildPlacement, bool)
	// CIPreflightReason is why the amd64 build pool cannot take a kaniko
	// build, "" when it can.
	CIPreflightReason func(nodes []NodeInput) string
}

var placementReadings atomic.Pointer[PlacementReadings]

// BindPlacementReadings sets compute's side. It is called once, at start-up,
// before anything is served. Unbound: nothing is placed (as without node
// metrics), a kaniko pipeline is not build-ready, and a CI run the platform
// starts carries a node affinity no node passes, so it waits rather than
// landing on a node it must never be given.
func BindPlacementReadings(r PlacementReadings) { placementReadings.Store(&r) }

func placements() PlacementReadings {
	if r := placementReadings.Load(); r != nil {
		return *r
	}
	return PlacementReadings{}
}

// errPlacementUnbound is the build pool check's reason while unbound.
const errPlacementUnbound = "the build node check is not wired on this platform (delivery.BindPlacementReadings)"

// placementUnboundKey is the label of the affinity expression no node passes
// (operator Exists on a label no node has), set while the roles are unbound.
const placementUnboundKey = "bifrost.io/placement-not-wired"

// notInExpression is the expression a node passes when the role does not
// name it (NotIn also passes a node without the label); while the role is
// unbound, one no node passes.
func notInExpression(role NodeRole) map[string]any {
	if f := placements().RoleLabel; f != nil {
		if key, value, ok := f(role); ok {
			return map[string]any{"key": key, "operator": "NotIn", "values": []any{value}}
		}
	}
	return map[string]any{"key": placementUnboundKey, "operator": "Exists"}
}
