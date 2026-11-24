package research

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Kube is what this package needs from foundation's Kubernetes adapter: a
// client and the REST config behind it. The server passes *cluster.Service;
// the package declares the interface so that it does not import cluster, a
// package mixed across domains (law 95: reach another domain through an
// interface you declare). nil means no cluster, as before.
type Kube interface {
	KubernetesClient() (kubernetes.Interface, string, error)
	RestConfig() (*rest.Config, string, error)
}
