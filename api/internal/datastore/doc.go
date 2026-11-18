// Package datastore reads the status of the platform's PostgreSQL
// (CloudNativePG) and Redis from Kubernetes. It is a foundation package and
// does not import cluster: cluster holds the connection and calls in.
//
// Contract:
//
//   - ReadPostgres and ReadRedis return PostgresStatusResponse and
//     RedisStatusResponse, the bodies of GET /cluster/postgres and GET
//     /cluster/redis. cluster.Service.PostgresStatus and RedisStatus forward
//     to them; their callers are cluster's handler and delivery's
//     ProdPostgresInCluster.
//   - MatrixSnapshot returns the per-environment reachability the connectivity
//     matrix shows. cluster.Service.DatastoreSnapshot forwards to it; its
//     callers are server and promote.
//   - MinIODependency, RedisTargetDependencies and EmbeddedRedisDependency
//     return the dependency lines cluster's service readiness puts in its
//     PostgreSQL and Redis domains.
//   - CNPGClusterName, CNPGNamespace, CNPGOperatorNamespace,
//     CNPGOperatorDeployment and CNPGClusterGVR name the objects; cluster's
//     backup, WAL repair and pod exec code acts on the same ones.
//
// Input: a Source, whose clients and Snapshot the caller builds. Snapshot
// carries what cluster's readiness snapshot already gathered (Deployments,
// environments, MinIO backend, StorageClass and node coverage readings) and
// cluster's Deployment health rule.
//
// When something cannot be read: without a Kubernetes client the status is
// fail with the error as summary, and every listed database is unknown.
// Without a dynamic client the CNPG cluster and backup lines are degraded with
// the error as detail, and every database is unknown. A database's
// reachability is never observed by itself: it follows the CNPG cluster's, so
// a cluster that was not read leaves it unknown, never ok. A resource that is
// missing is reported as degraded with a detail that says so.
//
// Read only: this package issues get and list calls and nothing else. Actions
// on these datastores (backups, WAL repair, pod exec) are not here.
package datastore
