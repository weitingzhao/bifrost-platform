package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// ComputeNodes is the compute domain's side of the nodes: which nodes compute
// powers on demand, whether such a node is active, standby or degraded, and
// its power (read, wake, power off). It was this package's (node_power.go,
// elastic_node_health.go) until 2026-11-19 and is package computenodes now.
// This package declares the shape and the server binds computenodes
// (BindComputeNodes); it does not import computenodes. Each function is
// given the Service it serves, since the compute nodes are its cluster
// entry's.
type ComputeNodes struct {
	// Managed says whether the node is one compute powers on demand.
	Managed func(s *Service, name string) bool
	// NodeMode says whether a managed node is active, standby or degraded
	// (the elasticMode* values), with why. forDisplay is true for a page
	// that polls: the host's answer may then be a moment old, and nothing
	// may act on it.
	NodeMode func(ctx context.Context, s *Service, clientset kubernetes.Interface, node *corev1.Node, forDisplay bool) (mode, detail string)
	// NodePower is the document of GET /cluster/nodes/{name}/power.
	NodePower func(ctx context.Context, s *Service, name string) (any, error)
	// WakeNode sends the node a Wake-on-LAN packet.
	WakeNode func(ctx context.Context, s *Service, name string) (ActuationResponse, error)
	// PowerOffNode drains the node and powers it off.
	PowerOffNode func(ctx context.Context, s *Service, name string) (ActuationResponse, error)
}

// computeNodes is bound once, at start-up, as appReadings is.
var computeNodes ComputeNodes

// BindComputeNodes sets compute's side of the nodes. Without it no node is
// compute's: every node counts as a core node, and the power read, wake and
// power off answer as they do for a node that is not a managed compute node.
func BindComputeNodes(c ComputeNodes) { computeNodes = c }

// notManagedComputeNode is the error the power actions give for a node that
// is not compute's, and the one they give when nothing is bound.
func notManagedComputeNode(name string) error {
	return fmt.Errorf("node %q is not a managed compute node", name)
}

func (s *Service) isComputeManaged(name string) bool {
	if computeNodes.Managed == nil {
		return false
	}
	return computeNodes.Managed(s, name)
}

// NodePower forwards to compute's power read of a node.
func (s *Service) NodePower(ctx context.Context, nodeName string) (any, error) {
	if computeNodes.NodePower == nil {
		return nil, notManagedComputeNode(nodeName)
	}
	return computeNodes.NodePower(ctx, s, nodeName)
}

// WakeNode forwards to compute's wake of a node. The action wiring
// (server/actions_wire.go, trust-anchored) names HandleWakeNode.
func (s *Service) WakeNode(ctx context.Context, nodeName string) (ActuationResponse, error) {
	if computeNodes.WakeNode == nil {
		return ActuationResponse{}, notManagedComputeNode(nodeName)
	}
	return computeNodes.WakeNode(ctx, s, nodeName)
}

// PowerOffNode forwards to compute's power off of a node. The action wiring
// (server/actions_wire.go, trust-anchored) names HandlePowerOffNode.
func (s *Service) PowerOffNode(ctx context.Context, nodeName string) (ActuationResponse, error) {
	if computeNodes.PowerOffNode == nil {
		return ActuationResponse{}, notManagedComputeNode(nodeName)
	}
	return computeNodes.PowerOffNode(ctx, s, nodeName)
}
