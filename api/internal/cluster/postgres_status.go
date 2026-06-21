package cluster

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

const (
	cnpgClusterName    = "bifrost-postgres"
	cnpgNamespace      = "data"
	cnpgOperatorNS     = "cnpg-system"
	cnpgOperatorDeploy = "cnpg-controller-manager"
)

var cnpgClusterGVR = schema.GroupVersionResource{
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

type PostgresStatusResponse struct {
	ClusterID      string                 `json:"cluster_id"`
	Reachability   probe.Reachability     `json:"reachability"`
	Summary        string                 `json:"summary"`
	MigrationPhase string                 `json:"migration_phase"`
	MigrationStep  int                    `json:"migration_step"`
	MigrationTotal int                    `json:"migration_total"`
	Operator       ServiceDependencyView  `json:"operator"`
	CnpgCluster    ServiceDependencyView  `json:"cnpg_cluster"`
	Instances      []PostgresInstanceView `json:"instances"`
	InstancesSpec  int                    `json:"instances_spec"`
	InstancesReady int                    `json:"instances_ready"`
	PrimaryPod     string                 `json:"primary_pod,omitempty"`
	PrimaryNode    string                 `json:"primary_node,omitempty"`
	RwService      string                 `json:"rw_service"`
	RoService      string                 `json:"ro_service"`
	StorageClass   string                 `json:"storage_class"`
	StorageSize    string                 `json:"storage_size"`
	Backup         ServiceDependencyView  `json:"backup"`
	MinIO          ServiceDependencyView  `json:"minio"`
	Databases      []PostgresDatabaseView `json:"databases"`
	Legacy         []PostgresLegacyView   `json:"legacy"`
	Embedded       []PostgresLegacyView   `json:"embedded"`
	PostgresRole   ServiceDependencyView  `json:"postgres_role"`
	GeneratedAt    time.Time              `json:"generated_at"`
}

func (s *Service) PostgresStatus(ctx context.Context) PostgresStatusResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	resp := PostgresStatusResponse{
		ClusterID:      base.ClusterID,
		MigrationPhase: "data-layer-k3s",
		MigrationStep:  3,
		MigrationTotal: 7,
		RwService:      fmt.Sprintf("%s-rw.%s.svc.cluster.local:5432", cnpgClusterName, cnpgNamespace),
		RoService:      fmt.Sprintf("%s-ro.%s.svc.cluster.local:5432", cnpgClusterName, cnpgNamespace),
		GeneratedAt:    now,
		Databases: []PostgresDatabaseView{
			{Name: "bifrost_dev", Environment: "dev", CrName: "bifrost-dev"},
			{Name: "bifrost_stg", Environment: "stg", CrName: "bifrost-stg"},
			{Name: "bifrost_prod", Environment: "prod", CrName: "bifrost-prod"},
		},
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		resp.Reachability = probe.ReachFail
		resp.Summary = err.Error()
		return resp
	}

	snap, _ := s.buildReadinessSnapshot(ctx)
	resp.PostgresRole = nodeCovDep(snap, "postgres-role", "PostgreSQL host nodes")

	resp.Operator = operatorDep(ctx, clientset)
	resp.MinIO = minioDep(snap)

	dyn, dynErr := s.buildDynamicClient()
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
	resp.Legacy = []PostgresLegacyView{
		{Kind: "bare-metal", Host: "192.168.10.80", Reach: probe.ReachOK, Detail: "Prod authoritative until phase ⑤ cutover"},
	}

	for i := range resp.Databases {
		resp.Databases[i].Reach = probe.ReachOK
		resp.Databases[i].Detail = "Database CR on CNPG cluster"
	}

	resp.Reachability, resp.Summary = summarizePostgresStatus(resp)
	return resp
}

func (s *Service) buildDynamicClient() (dynamic.Interface, error) {
	cfg, _, err := s.RestConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

func operatorDep(ctx context.Context, clientset kubernetes.Interface) ServiceDependencyView {
	dep, err := clientset.AppsV1().Deployments(cnpgOperatorNS).Get(ctx, cnpgOperatorDeploy, metav1.GetOptions{})
	if err != nil {
		return ServiceDependencyView{
			ID: "cnpg-operator", Label: "CloudNativePG operator",
			Reachability: probe.ReachDegraded, Detail: "not deployed in cnpg-system",
		}
	}
	reach, detail := deploymentReach(dep)
	return ServiceDependencyView{
		ID: "cnpg-operator", Label: "CloudNativePG operator", Reachability: reach, Detail: detail,
	}
}

func minioDep(snap readinessSnapshot) ServiceDependencyView {
	return deploymentDep(snap, cnpgNamespace, "minio", "MinIO backup (nfs-hot)")
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
	obj, err := dyn.Resource(cnpgClusterGVR).Namespace(cnpgNamespace).Get(ctx, cnpgClusterName, metav1.GetOptions{})
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

	pods, listErr := clientset.CoreV1().Pods(cnpgNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "cnpg.io/cluster=" + cnpgClusterName,
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

func backupDep(ctx context.Context, dyn dynamic.Interface, snap readinessSnapshot, minioReach probe.Reachability) ServiceDependencyView {
	_, err := dyn.Resource(schema.GroupVersionResource{
		Group: "postgresql.cnpg.io", Version: "v1", Resource: "scheduledbackups",
	}).Namespace(cnpgNamespace).Get(ctx, "bifrost-postgres-daily", metav1.GetOptions{})
	if err != nil {
		return ServiceDependencyView{
			ID: "cnpg-backup", Label: "Scheduled backup",
			Reachability: probe.ReachDegraded, Detail: "ScheduledBackup bifrost-postgres-daily missing",
		}
	}

	obj, err := dyn.Resource(cnpgClusterGVR).Namespace(cnpgNamespace).Get(ctx, cnpgClusterName, metav1.GetOptions{})
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
	nfsHot := clusterCapDep(snap, "storage-class-nfs-hot", "nfs-hot")
	if nfsHot.Reachability != probe.ReachOK {
		reach = probe.ReachDegraded
		detail += " · nfs-hot unavailable"
	}
	return ServiceDependencyView{
		ID: "cnpg-backup", Label: "WAL archive (barman)", Reachability: reach, Detail: detail,
	}
}

func embeddedPostgresProbe(snap readinessSnapshot) []PostgresLegacyView {
	envs := []struct {
		ns string
	}{
		{"bifrost-stg"},
		{"bifrost-dev"},
		{"bifrost-prod"},
	}
	out := make([]PostgresLegacyView, 0, len(envs))
	for _, e := range envs {
		key := e.ns + "/postgres"
		d, ok := snap.deployments[key]
		if !ok {
			out = append(out, PostgresLegacyView{
				Kind: "embedded", Namespace: e.ns, Reach: probe.ReachOK,
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
				Kind: "embedded", Namespace: e.ns, Reach: probe.ReachOK,
				Detail: "scaled to zero",
			})
			continue
		}
		reach, detail := deploymentReach(&d)
		out = append(out, PostgresLegacyView{
			Kind: "embedded", Namespace: e.ns, Reach: reach,
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

func stringFromUnstructured(obj *unstructured.Unstructured, fields ...string) string {
	v, found, _ := unstructured.NestedString(obj.Object, fields...)
	if !found {
		return ""
	}
	return v
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
