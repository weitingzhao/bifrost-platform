package devsession

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubeapi"
)

// Kube is what the cluster provider needs from foundation's Kubernetes
// adapter. The server passes *cluster.Service; this package declares the
// interface so that it does not import cluster, a package mixed across
// domains (law 95). nil means no cluster, as before.
type Kube interface {
	KubernetesClient() (kubernetes.Interface, string, error)
	PodLogs(ctx context.Context, namespace, name string, tailLines int64, container string) (kubeapi.LogsResponse, error)
	RolloutRestart(ctx context.Context, req kubeapi.RolloutRestartRequest) (kubeapi.ActuationResponse, error)
	Scale(ctx context.Context, req kubeapi.ScaleRequest) (kubeapi.ActuationResponse, error)
}
