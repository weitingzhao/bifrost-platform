package computenodes

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/elasticreadings"
)

// Nodes is compute's side of the nodes, bound to cluster once at start-up.
type Nodes struct {
	// hosts keeps each on-demand host's last answer for pages that poll.
	hosts *elasticreadings.HostReadings
	// liveProbe says whether a host answers; an SSH call (unit tests replace it).
	liveProbe func(ctx context.Context, host string) bool
}

// New is compute's side of the nodes with an empty host reading.
func New() *Nodes {
	return &Nodes{hosts: &elasticreadings.HostReadings{}, liveProbe: probeHostReachableFast}
}

// ClusterPort is what cluster.BindComputeNodes takes.
func (n *Nodes) ClusterPort() cluster.ComputeNodes {
	return cluster.ComputeNodes{
		Managed: func(c *cluster.Service, name string) bool { return computeNodeSpec(c, name) != nil },
		NodeMode: func(ctx context.Context, c *cluster.Service, clientset kubernetes.Interface, node *corev1.Node, forDisplay bool) (string, string) {
			return n.NodeMode(ctx, c, clientset, node, forDisplay)
		},
		NodePower: func(ctx context.Context, c *cluster.Service, name string) (any, error) {
			resp, err := NodePower(ctx, c, name)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
		WakeNode:     WakeNode,
		PowerOffNode: PowerOffNode,
	}
}

func computeNodeSpec(c *cluster.Service, name string) *config.ComputeNodeSpec {
	if c == nil || c.Entry() == nil {
		return nil
	}
	return c.Entry().ComputeNode(name)
}
