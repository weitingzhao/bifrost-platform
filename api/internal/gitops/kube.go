package gitops

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Kube is what this package needs from foundation's Kubernetes adapter: a
// client and the REST config behind it. The server passes *cluster.Service,
// the one every other domain is given; until 2026-11-19 this package built
// its own from the same cluster entry, which made it import cluster, a package
// mixed across domains (LAW-10).
type Kube interface {
	KubernetesClient() (kubernetes.Interface, string, error)
	RestConfig() (*rest.Config, string, error)
}
