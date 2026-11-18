package datastore

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Where the platform's PostgreSQL runs. The backup, WAL repair and pod exec
// actions that stay in cluster act on the same objects.
const (
	CNPGClusterName        = "bifrost-postgres"
	CNPGNamespace          = "data"
	CNPGOperatorNamespace  = "cnpg-system"
	CNPGOperatorDeployment = "cnpg-controller-manager"
	cnpgLanServiceName     = "bifrost-postgres-lan"
)

// CNPGClusterGVR is the CloudNativePG Cluster resource.
var CNPGClusterGVR = schema.GroupVersionResource{
	Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters",
}

type PostgresInstanceView struct {
	PodName string             `json:"pod_name"`
	Role    string             `json:"role"`
	Node    string             `json:"node"`
	Phase   string             `json:"phase"`
	Reach   probe.Reachability `json:"reachability"`
	Detail  string             `json:"detail,omitempty"`
}

type PostgresDatabaseView struct {
	Name        string             `json:"name"`
	Environment string             `json:"environment"`
	CrName      string             `json:"cr_name,omitempty"`
	Reach       probe.Reachability `json:"reachability"`
	Detail      string             `json:"detail,omitempty"`
}

type PostgresLegacyView struct {
	Kind      string             `json:"kind"`
	Namespace string             `json:"namespace,omitempty"`
	Host      string             `json:"host,omitempty"`
	Reach     probe.Reachability `json:"reachability"`
	Detail    string             `json:"detail,omitempty"`
}

// PostgresLanAccessView surfaces the LAN NodePort entry point for external SQL
// clients (DBeaver). The NodePort is provisioned by bifrost-postgres-lan Service.
type PostgresLanAccessView struct {
	Available bool               `json:"available"`
	Host      string             `json:"host,omitempty"`
	NodePort  int                `json:"node_port,omitempty"`
	Endpoint  string             `json:"endpoint,omitempty"`
	User      string             `json:"user,omitempty"`
	Reach     probe.Reachability `json:"reachability"`
	Detail    string             `json:"detail,omitempty"`
}

type PostgresStatusResponse struct {
	ClusterID      string                 `json:"cluster_id"`
	Reachability   probe.Reachability     `json:"reachability"`
	Summary        string                 `json:"summary"`
	Operator       ServiceDependencyView  `json:"operator"`
	CnpgCluster    ServiceDependencyView  `json:"cnpg_cluster"`
	Instances      []PostgresInstanceView `json:"instances"`
	InstancesSpec  int                    `json:"instances_spec"`
	InstancesReady int                    `json:"instances_ready"`
	PrimaryPod     string                 `json:"primary_pod,omitempty"`
	PrimaryNode    string                 `json:"primary_node,omitempty"`
	RwService      string                 `json:"rw_service"`
	RoService      string                 `json:"ro_service"`
	LanAccess      PostgresLanAccessView  `json:"lan_access"`
	StorageClass   string                 `json:"storage_class"`
	StorageSize    string                 `json:"storage_size"`
	Backup         ServiceDependencyView  `json:"backup"`
	MinIO          ServiceDependencyView  `json:"minio"`
	Databases      []PostgresDatabaseView `json:"databases"`
	Embedded       []PostgresLegacyView   `json:"embedded"`
	PostgresRole   ServiceDependencyView  `json:"postgres_role"`
	GeneratedAt    time.Time              `json:"generated_at"`
}

// ReadPostgres reads the status of the platform's PostgreSQL.
func ReadPostgres(ctx context.Context, src Source) PostgresStatusResponse {
	now := time.Now().UTC()
	resp := PostgresStatusResponse{
		ClusterID:   src.ClusterID,
		RwService:   fmt.Sprintf("%s-rw.%s.svc.cluster.local:5432", CNPGClusterName, CNPGNamespace),
		RoService:   fmt.Sprintf("%s-ro.%s.svc.cluster.local:5432", CNPGClusterName, CNPGNamespace),
		GeneratedAt: now,
		Databases:   []PostgresDatabaseView{},
	}
	// The CNPG Database CRs are named after the namespace (infra k8s/data/databases.yaml).
	for _, e := range src.Environments {
		if e.Database != "" {
			resp.Databases = append(resp.Databases, PostgresDatabaseView{Name: e.Database, Environment: e.ID, CrName: e.Namespace})
		}
	}

	clientset, err := src.Client()
	if err != nil {
		resp.Reachability = probe.ReachFail
		resp.Summary = err.Error()
		markDatabasesFromCluster(resp.Databases, ServiceDependencyView{Reachability: probe.ReachUnknown, Detail: err.Error()})
		return resp
	}

	snap := src.Snapshot(ctx)
	resp.PostgresRole = snap.PostgresRole

	resp.Operator = operatorDep(ctx, clientset, snap)
	resp.MinIO = minioDep(snap)
	resp.LanAccess = lanAccessProbe(ctx, clientset)

	dyn, dynErr := src.Dynamic()
	if dynErr != nil {
		resp.CnpgCluster = ServiceDependencyView{
			ID: "cnpg-cluster", Label: "CloudNativePG cluster",
			Reachability: probe.ReachDegraded, Detail: dynErr.Error(),
		}
	} else {
		resp.CnpgCluster, resp.Instances, resp.InstancesSpec, resp.InstancesReady,
			resp.PrimaryPod, resp.PrimaryNode, resp.StorageClass, resp.StorageSize = cnpgClusterProbe(ctx, dyn, clientset)
	}

	if dynErr != nil {
		resp.Backup = ServiceDependencyView{
			ID: "cnpg-backup", Label: "WAL archive (barman)",
			Reachability: probe.ReachDegraded, Detail: dynErr.Error(),
		}
	} else {
		resp.Backup = backupDep(ctx, dyn, snap, resp.MinIO.Reachability)
	}
	resp.Embedded = embeddedPostgresProbe(snap)
	if dynErr != nil {
		markDatabasesFromCluster(resp.Databases, ServiceDependencyView{Reachability: probe.ReachUnknown, Detail: dynErr.Error()})
	} else {
		markDatabasesFromCluster(resp.Databases, resp.CnpgCluster)
	}

	resp.Reachability, resp.Summary = summarizePostgresStatus(resp)
	return resp
}

// markDatabasesFromCluster sets each logical database's reachability from the
// one observation the platform has for it: the status of the CNPG cluster that
// serves it. The database itself is not probed (platform RBAC cannot read the
// CNPG Database CRs and the API opens no SQL connection here), so a cluster that
// was not read leaves every database unknown, never ok.
func markDatabasesFromCluster(dbs []PostgresDatabaseView, cluster ServiceDependencyView) {
	reach := cluster.Reachability
	if reach == "" {
		reach = probe.ReachUnknown
	}
	for i := range dbs {
		dbs[i].Reach = reach
		if reach == probe.ReachUnknown {
			dbs[i].Detail = "unknown — CNPG cluster status not read: " + cluster.Detail
			continue
		}
		dbs[i].Detail = fmt.Sprintf("from CNPG cluster %s status (%s); the database itself is not probed", CNPGClusterName, cluster.Detail)
	}
}

func operatorDep(ctx context.Context, clientset kubernetes.Interface, snap Snapshot) ServiceDependencyView {
	dep, err := clientset.AppsV1().Deployments(CNPGOperatorNamespace).Get(ctx, CNPGOperatorDeployment, metav1.GetOptions{})
	if err != nil {
		return ServiceDependencyView{
			ID: "cnpg-operator", Label: "CloudNativePG operator",
			Reachability: probe.ReachDegraded, Detail: "not deployed in cnpg-system",
		}
	}
	reach, detail := snap.DeploymentReach(dep)
	return ServiceDependencyView{
		ID: "cnpg-operator", Label: "CloudNativePG operator", Reachability: reach, Detail: detail,
	}
}

func minioDep(snap Snapshot) ServiceDependencyView {
	return MinIODependency(snap, "MinIO backup (nfs-hot)")
}

// MinIODependency reports the backup MinIO: the minio Deployment when it runs in
// the cluster, the health of the server behind Service data/minio when that
// Service points outside it (the Deployment is then 0 on purpose).
func MinIODependency(snap Snapshot, label string) ServiceDependencyView {
	if snap.MinIO.External {
		return ServiceDependencyView{
			ID: "minio-backup-external", Label: "MinIO backup (outside the cluster)",
			Reachability: snap.MinIO.Reach, Detail: snap.MinIO.Detail,
		}
	}
	return snap.DeploymentDep(CNPGNamespace, "minio", label)
}

// lanAccessProbe reads the bifrost-postgres-lan NodePort Service and pairs it
// with a Ready node InternalIP so the UI can show a copy-paste DBeaver endpoint.
func lanAccessProbe(ctx context.Context, clientset kubernetes.Interface) PostgresLanAccessView {
	view := PostgresLanAccessView{User: "bifrost", Reach: probe.ReachDegraded}

	svc, err := clientset.CoreV1().Services(CNPGNamespace).Get(ctx, cnpgLanServiceName, metav1.GetOptions{})
	if err != nil {
		view.Detail = "NodePort service bifrost-postgres-lan not found — apply k8s/data/postgres-nodeport.yaml"
		return view
	}

	var nodePort int32
	for _, p := range svc.Spec.Ports {
		if p.Name == "postgres" || p.Port == 5432 {
			nodePort = p.NodePort
			break
		}
	}
	if nodePort == 0 {
		view.Detail = "bifrost-postgres-lan has no NodePort assigned"
		return view
	}
	view.NodePort = int(nodePort)

	host := firstReadyNodeInternalIP(ctx, clientset)
	if host == "" {
		view.Detail = fmt.Sprintf("NodePort %d ready; no Ready node IP resolved", nodePort)
		return view
	}

	view.Available = true
	view.Host = host
	view.Endpoint = fmt.Sprintf("%s:%d", host, nodePort)
	view.Reach = probe.ReachOK
	view.Detail = "LAN-only — do not expose to public internet"
	return view
}

func firstReadyNodeInternalIP(ctx context.Context, clientset kubernetes.Interface) string {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return ""
	}
	for _, n := range nodes.Items {
		if n.Spec.Unschedulable {
			continue
		}
		ready := false
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		for _, a := range n.Status.Addresses {
			if a.Type == corev1.NodeInternalIP && a.Address != "" {
				return a.Address
			}
		}
	}
	return ""
}

func cnpgClusterProbe(
	ctx context.Context,
	dyn dynamic.Interface,
	clientset kubernetes.Interface,
) (
	clusterDep ServiceDependencyView,
	instances []PostgresInstanceView,
	spec, ready int,
	primaryPod, primaryNode, storageClass, storageSize string,
) {
	obj, err := dyn.Resource(CNPGClusterGVR).Namespace(CNPGNamespace).Get(ctx, CNPGClusterName, metav1.GetOptions{})
	if err != nil {
		return ServiceDependencyView{
				ID: "cnpg-cluster", Label: "CloudNativePG bifrost-postgres",
				Reachability: probe.ReachDegraded, Detail: "cluster CR not found in data namespace",
			},
			nil, 0, 0, "", "", "", ""
	}

	spec = intFromUnstructured(obj, "spec", "instances")
	ready = intFromUnstructured(obj, "status", "readyInstances")
	phase := stringFromUnstructured(obj, "status", "phase")
	storageClass = stringFromUnstructured(obj, "spec", "storage", "storageClass")
	storageSize = stringFromUnstructured(obj, "spec", "storage", "size")

	detail := fmt.Sprintf("phase=%s ready %d/%d", phase, ready, spec)
	reach := probe.ReachOK
	switch {
	case spec < 2:
		reach = probe.ReachDegraded
		detail += " · HA target instances=2"
	case ready < spec:
		reach = probe.ReachDegraded
	case phase != "" && phase != "Cluster in healthy state" && !strings.Contains(strings.ToLower(phase), "healthy"):
		reach = probe.ReachDegraded
	}

	clusterDep = ServiceDependencyView{
		ID: "cnpg-cluster", Label: "CloudNativePG bifrost-postgres",
		Reachability: reach, Detail: detail,
	}

	pods, listErr := clientset.CoreV1().Pods(CNPGNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "cnpg.io/cluster=" + CNPGClusterName,
	})
	if listErr != nil {
		return clusterDep, instances, spec, ready, primaryPod, primaryNode, storageClass, storageSize
	}

	for _, pod := range pods.Items {
		role := pod.Labels["cnpg.io/instanceRole"]
		if role == "" {
			role = pod.Labels["role"]
		}
		instReach := probe.ReachOK
		instDetail := string(pod.Status.Phase)
		if pod.Status.Phase != corev1.PodRunning {
			instReach = probe.ReachDegraded
		}
		if !podReady(&pod) {
			instReach = probe.ReachDegraded
			instDetail = "not ready"
		}
		instances = append(instances, PostgresInstanceView{
			PodName: pod.Name,
			Role:    role,
			Node:    pod.Spec.NodeName,
			Phase:   string(pod.Status.Phase),
			Reach:   instReach,
			Detail:  instDetail,
		})
		if role == "primary" {
			primaryPod = pod.Name
			primaryNode = pod.Spec.NodeName
		}
	}
	return clusterDep, instances, spec, ready, primaryPod, primaryNode, storageClass, storageSize
}

func backupDep(ctx context.Context, dyn dynamic.Interface, snap Snapshot, minioReach probe.Reachability) ServiceDependencyView {
	_, err := dyn.Resource(schema.GroupVersionResource{
		Group: "postgresql.cnpg.io", Version: "v1", Resource: "scheduledbackups",
	}).Namespace(CNPGNamespace).Get(ctx, "bifrost-postgres-daily", metav1.GetOptions{})
	if err != nil {
		return ServiceDependencyView{
			ID: "cnpg-backup", Label: "Scheduled backup",
			Reachability: probe.ReachDegraded, Detail: "ScheduledBackup bifrost-postgres-daily missing",
		}
	}

	obj, err := dyn.Resource(CNPGClusterGVR).Namespace(CNPGNamespace).Get(ctx, CNPGClusterName, metav1.GetOptions{})
	if err != nil {
		return ServiceDependencyView{
			ID: "cnpg-backup", Label: "Scheduled backup",
			Reachability: probe.ReachDegraded, Detail: "cluster CR missing",
		}
	}
	ep := stringFromUnstructured(obj, "spec", "backup", "barmanObjectStore", "endpointURL")
	dest := stringFromUnstructured(obj, "spec", "backup", "barmanObjectStore", "destinationPath")
	if ep == "" {
		return ServiceDependencyView{
			ID: "cnpg-backup", Label: "WAL archive (barman)",
			Reachability: probe.ReachDegraded, Detail: "barmanObjectStore not configured on cluster",
		}
	}
	reach := probe.ReachOK
	detail := fmt.Sprintf("%s · %s", dest, ep)
	if minioReach != probe.ReachOK {
		reach = probe.ReachDegraded
		detail += " · MinIO not ready"
	}
	// An external MinIO keeps its data on its own disk, not on nfs-hot.
	nfsHot := snap.NFSHot
	if !snap.MinIO.External && nfsHot.Reachability != probe.ReachOK {
		reach = probe.ReachDegraded
		detail += " · nfs-hot unavailable"
	}
	return ServiceDependencyView{
		ID: "cnpg-backup", Label: "WAL archive (barman)", Reachability: reach, Detail: detail,
	}
}

func embeddedPostgresProbe(snap Snapshot) []PostgresLegacyView {
	out := make([]PostgresLegacyView, 0, len(snap.Environments))
	for _, env := range snap.Environments {
		key := env.Namespace + "/postgres"
		d, ok := snap.Deployments[key]
		if !ok {
			out = append(out, PostgresLegacyView{
				Kind: "embedded", Namespace: env.Namespace, Reach: probe.ReachOK,
				Detail: "removed — cutover complete",
			})
			continue
		}
		desired := d.Status.Replicas
		if desired == 0 && d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if desired == 0 {
			out = append(out, PostgresLegacyView{
				Kind: "embedded", Namespace: env.Namespace, Reach: probe.ReachOK,
				Detail: "scaled to zero",
			})
			continue
		}
		reach, detail := snap.DeploymentReach(&d)
		out = append(out, PostgresLegacyView{
			Kind: "embedded", Namespace: env.Namespace, Reach: reach,
			Detail: detail + " · pending cutover to data NS",
		})
	}
	return out
}

func summarizePostgresStatus(r PostgresStatusResponse) (probe.Reachability, string) {
	if r.Operator.Reachability == probe.ReachFail || r.CnpgCluster.Reachability == probe.ReachFail {
		return probe.ReachFail, "CNPG operator or cluster unavailable"
	}
	if r.InstancesSpec < 2 || r.InstancesReady < 2 {
		return probe.ReachDegraded, fmt.Sprintf("HA forming — %d/%d instances ready", r.InstancesReady, postgresMaxInt(r.InstancesSpec, 2))
	}
	if r.Backup.Reachability != probe.ReachOK {
		return probe.ReachDegraded, "Cluster running · backup path incomplete"
	}
	embeddedActive := 0
	for _, e := range r.Embedded {
		if e.Reach != probe.ReachOK && !strings.Contains(e.Detail, "removed") && !strings.Contains(e.Detail, "scaled to zero") {
			embeddedActive++
		}
	}
	if embeddedActive > 0 {
		return probe.ReachDegraded, fmt.Sprintf("CNPG HA ready · %d embedded postgres still active (phase ③+)", embeddedActive)
	}
	return probe.ReachOK, "CNPG HA + backup ready · awaiting app cutover"
}

func intFromUnstructured(obj *unstructured.Unstructured, fields ...string) int {
	v, found, _ := unstructured.NestedFieldCopy(obj.Object, fields...)
	if !found {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func postgresMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
