package ibgateway

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/kubeapi"
)

// Kube is what this package needs from foundation's Kubernetes adapter. The
// server passes *cluster.Service; this package declares the interface so that
// it does not import cluster, a package mixed across domains (law 95). nil
// means no cluster, as before.
type Kube interface {
	KubernetesClient() (kubernetes.Interface, string, error)
	AppNamespaces() []string
	RolloutRestart(ctx context.Context, req kubeapi.RolloutRestartRequest) (kubeapi.ActuationResponse, error)
}
