package datastore

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// ServiceDependencyView is one dependency line of a status answer.
type ServiceDependencyView = resourceview.ServiceDependencyView

// Source is what a read needs from whoever holds the cluster connection. All
// fields are required. Client is called first and Snapshot only when it
// succeeded, so a cluster that cannot be reached costs no snapshot.
type Source struct {
	ClusterID    string
	Environments []config.AppEnv
	Client       func() (kubernetes.Interface, error)
	Dynamic      func() (dynamic.Interface, error)
	Snapshot     func(context.Context) Snapshot
}

// MinIOReading says where the backup MinIO runs and, when it runs outside the
// cluster, how its health check answered.
type MinIOReading struct {
	External bool
	Reach    probe.Reachability
	Detail   string
}

// Snapshot is the part of a cluster readiness snapshot the status reads use.
// DeploymentReach and DeploymentDep are the caller's: the same Deployment
// health rule answers for every other reader of service readiness.
type Snapshot struct {
	Environments    []config.AppEnv
	Deployments     map[string]appsv1.Deployment // "namespace/name"
	MinIO           MinIOReading
	NFSHot          ServiceDependencyView // StorageClass nfs-hot
	PostgresRole    ServiceDependencyView // nodes that can host PostgreSQL
	DeploymentReach func(*appsv1.Deployment) (probe.Reachability, string)
	DeploymentDep   func(namespace, name, label string) ServiceDependencyView
}

func stringFromUnstructured(obj *unstructured.Unstructured, fields ...string) string {
	v, found, _ := unstructured.NestedString(obj.Object, fields...)
	if !found {
		return ""
	}
	return v
}
