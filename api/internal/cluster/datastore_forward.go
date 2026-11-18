package cluster

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/datastore"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The PostgreSQL and Redis status reads live in internal/datastore. This file
// is cluster's side of them: the connection and readiness snapshot they read
// through, and the names the backup, WAL repair and pod exec code here shares
// with them.

const (
	cnpgClusterName    = datastore.CNPGClusterName
	cnpgNamespace      = datastore.CNPGNamespace
	cnpgOperatorNS     = datastore.CNPGOperatorNamespace
	cnpgOperatorDeploy = datastore.CNPGOperatorDeployment
)

var cnpgClusterGVR = datastore.CNPGClusterGVR

// PostgresStatus answers GET /cluster/postgres.
func (s *Service) PostgresStatus(ctx context.Context) datastore.PostgresStatusResponse {
	return datastore.ReadPostgres(ctx, s.datastoreSource())
}

// RedisStatus answers GET /cluster/redis.
func (s *Service) RedisStatus(ctx context.Context) datastore.RedisStatusResponse {
	return datastore.ReadRedis(ctx, s.datastoreSource())
}

// DatastoreSnapshot is the connectivity matrix's reading of the datastores.
func (s *Service) DatastoreSnapshot(ctx context.Context) probe.DatastoreSnapshot {
	return datastore.MatrixSnapshot(ctx, s.datastoreSource())
}

func (s *Service) datastoreSource() datastore.Source {
	return datastore.Source{
		ClusterID:    s.baseMeta(time.Now().UTC()).ClusterID,
		Environments: s.appEnvs,
		Client: func() (kubernetes.Interface, error) {
			clientset, _, err := s.buildClient()
			return clientset, err
		},
		Dynamic: s.buildDynamicClient,
		Snapshot: func(ctx context.Context) datastore.Snapshot {
			snap, _ := s.buildReadinessSnapshot(ctx)
			return snap.datastoreSnapshot()
		},
	}
}

func (snap readinessSnapshot) datastoreSnapshot() datastore.Snapshot {
	return datastore.Snapshot{
		Environments:    snap.appEnvs,
		Deployments:     snap.deployments,
		MinIO:           datastore.MinIOReading{External: snap.minio.External, Reach: snap.minio.Reach, Detail: snap.minio.Detail},
		NFSHot:          clusterCapDep(snap, "storage-class-nfs-hot", "nfs-hot"),
		PostgresRole:    nodeCovDep(snap, "postgres-role", "PostgreSQL host nodes"),
		DeploymentReach: deploymentReach,
		DeploymentDep: func(namespace, name, label string) ServiceDependencyView {
			return deploymentDep(snap, namespace, name, label)
		},
	}
}

func minioDepLabeled(snap readinessSnapshot, label string) ServiceDependencyView {
	return datastore.MinIODependency(snap.datastoreSnapshot(), label)
}

func stringFromUnstructured(obj *unstructured.Unstructured, fields ...string) string {
	v, found, _ := unstructured.NestedString(obj.Object, fields...)
	if !found {
		return ""
	}
	return v
}
