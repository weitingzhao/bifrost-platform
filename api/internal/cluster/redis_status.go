package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

const (
	redisDataNamespace = "data"
	redisMigrationStep = 6
	redisMigrationTotal = 7
	redisTargetPhase   = 6
)

type redisTargetSpec struct {
	name        string
	environment string
	role        string
	service     string
}

var redisTargetCatalog = []redisTargetSpec{
	{name: "redis-live-stg", environment: "stg", role: "live", service: "redis-live-stg.data.svc.cluster.local:6379"},
	{name: "redis-queue-stg", environment: "stg", role: "queue", service: "redis-queue-stg.data.svc.cluster.local:6379"},
	{name: "redis-live-prod", environment: "prod", role: "live", service: "redis-live-prod.data.svc.cluster.local:6379"},
	{name: "redis-queue-prod", environment: "prod", role: "queue", service: "redis-queue-prod.data.svc.cluster.local:6379"},
	{name: "redis-dev", environment: "dev", role: "live+queue", service: "redis-dev.data.svc.cluster.local:6379"},
}

type RedisTargetInstanceView struct {
	Name         string             `json:"name"`
	Environment  string             `json:"environment"`
	Role         string             `json:"role"`
	Service      string             `json:"service"`
	MaxmemoryPolicy string          `json:"maxmemory_policy,omitempty"`
	Reach        probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail,omitempty"`
}

type RedisEmbeddedView struct {
	Namespace string             `json:"namespace"`
	Host      string             `json:"host"`
	Image     string             `json:"image,omitempty"`
	Reach     probe.Reachability `json:"reachability"`
	Detail    string             `json:"detail,omitempty"`
}

type RedisEnvEndpointView struct {
	Environment   string             `json:"environment"`
	LiveService   string             `json:"live_service"`
	QueueService  string             `json:"queue_service"`
	LiveReach     probe.Reachability `json:"live_reachability"`
	QueueReach    probe.Reachability `json:"queue_reachability"`
	NetworkPolicy string             `json:"network_policy"`
	Detail        string             `json:"detail,omitempty"`
}

// RedisLanEndpointView surfaces NodePort entry points for LAN Redis Insight clients.
type RedisLanEndpointView struct {
	Name        string             `json:"name"`
	Environment string             `json:"environment"`
	Role        string             `json:"role"`
	Host        string             `json:"host,omitempty"`
	NodePort    int                `json:"node_port,omitempty"`
	Endpoint    string             `json:"endpoint,omitempty"`
	Database    string             `json:"database,omitempty"`
	Available   bool               `json:"available"`
	Reach       probe.Reachability `json:"reachability"`
	Detail      string             `json:"detail,omitempty"`
}

type RedisStatusResponse struct {
	ClusterID          string                    `json:"cluster_id"`
	Reachability       probe.Reachability        `json:"reachability"`
	Summary            string                    `json:"summary"`
	MigrationPhase     string                    `json:"migration_phase"`
	MigrationStep      int                       `json:"migration_step"`
	MigrationTotal     int                       `json:"migration_total"`
	MigrationRedisStep int                       `json:"migration_redis_step"`
	TargetsReady       int                       `json:"targets_ready"`
	TargetsTotal       int                       `json:"targets_total"`
	EmbeddedActive     int                       `json:"embedded_active"`
	TargetInstances    []RedisTargetInstanceView `json:"target_instances"`
	EnvEndpoints       []RedisEnvEndpointView    `json:"env_endpoints"`
	LanEndpoints       []RedisLanEndpointView    `json:"lan_endpoints"`
	Embedded           []RedisEmbeddedView       `json:"embedded"`
	Legacy             []PostgresLegacyView      `json:"legacy"`
	Backup             ServiceDependencyView     `json:"backup"`
	MinIO              ServiceDependencyView     `json:"minio"`
	GeneratedAt        time.Time                 `json:"generated_at"`
}

func (s *Service) RedisStatus(ctx context.Context) RedisStatusResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	resp := RedisStatusResponse{
		ClusterID:          base.ClusterID,
		MigrationPhase:     "data-layer-k3s",
		MigrationStep:      redisMigrationStep,
		MigrationTotal:     redisMigrationTotal,
		MigrationRedisStep: redisTargetPhase,
		TargetsTotal:       len(redisTargetCatalog),
		GeneratedAt:        now,
		Legacy: []PostgresLegacyView{
			{
				Kind: "bare-metal", Host: "192.168.10.70",
				Reach: probe.ReachOK,
				Detail: "Compose / legacy prod Redis until phase ⑥ · separate from K3s embedded",
			},
		},
	}

	clientset, _, err := s.buildClient()
	if err != nil {
		resp.Reachability = probe.ReachFail
		resp.Summary = err.Error()
		return resp
	}

	snap, _ := s.buildReadinessSnapshot(ctx)
	resp.MinIO = minioDep(snap)
	resp.Backup = redisBackupDep(snap, resp.MinIO.Reachability)

	services := listDataRedisServices(ctx, clientset)
	for _, spec := range redisTargetCatalog {
		inst := probeRedisTarget(spec, snap, services)
		resp.TargetInstances = append(resp.TargetInstances, inst)
		if inst.Reach == probe.ReachOK {
			resp.TargetsReady++
		}
	}

	resp.EnvEndpoints = buildRedisEnvEndpoints(resp.TargetInstances)
	resp.LanEndpoints = redisLanAccessProbe(ctx, clientset)
	resp.Embedded = embeddedRedisProbe(snap)
	for _, e := range resp.Embedded {
		if e.Reach == probe.ReachOK && !strings.Contains(e.Detail, "removed") {
			resp.EmbeddedActive++
		}
	}

	resp.Reachability, resp.Summary = summarizeRedisStatus(resp)
	return resp
}

func listDataRedisServices(ctx context.Context, clientset kubernetes.Interface) map[string]corev1.Service {
	out := make(map[string]corev1.Service)
	list, err := clientset.CoreV1().Services(redisDataNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out
	}
	for _, svc := range list.Items {
		if strings.HasPrefix(svc.Name, "redis-") {
			out[svc.Name] = svc
		}
	}
	return out
}

func probeRedisTarget(spec redisTargetSpec, snap readinessSnapshot, services map[string]corev1.Service) RedisTargetInstanceView {
	policy := "noeviction"
	if spec.role == "queue" || spec.role == "live+queue" {
		if spec.role == "queue" {
			policy = "allkeys-lru"
		} else {
			policy = "noeviction (live) · db=1 queue on dev"
		}
	}

	view := RedisTargetInstanceView{
		Name:            spec.name,
		Environment:     spec.environment,
		Role:            spec.role,
		Service:         spec.service,
		MaxmemoryPolicy: policy,
	}

	key := redisDataNamespace + "/" + spec.name
	if dep, ok := snap.deployments[key]; ok {
		reach, detail := deploymentReach(&dep)
		view.Reach = reach
		view.Detail = detail + " · Bitnami target @ data NS"
		if reach == probe.ReachOK {
			return view
		}
		return view
	}
	if _, ok := services[spec.name]; ok {
		view.Reach = probe.ReachDegraded
		view.Detail = "Service exists · workload not ready"
		return view
	}

	view.Reach = probe.ReachDegraded
	view.Detail = "not deployed — phase ⑥ (k8s/data/redis/)"
	return view
}

func buildRedisEnvEndpoints(targets []RedisTargetInstanceView) []RedisEnvEndpointView {
	byEnv := map[string]*RedisEnvEndpointView{
		"prod": {
			Environment: "prod", LiveService: "redis-live-prod.data.svc",
			QueueService: "redis-queue-prod.data.svc",
			NetworkPolicy: "Only bifrost-prod Pods egress allowed",
		},
		"stg": {
			Environment: "stg", LiveService: "redis-live-stg.data.svc",
			QueueService: "redis-queue-stg.data.svc",
			NetworkPolicy: "Only bifrost-stg Pods egress allowed",
		},
		"dev": {
			Environment: "dev", LiveService: "redis-dev.data.svc or Mac local",
			QueueService: "same instance db=1 (dev simplicity)",
			NetworkPolicy: "Never writes Prod/STG Redis",
		},
	}

	for _, t := range targets {
		row, ok := byEnv[t.Environment]
		if !ok {
			continue
		}
		switch t.Role {
		case "live":
			row.LiveReach = t.Reach
		case "queue":
			row.QueueReach = t.Reach
		case "live+queue":
			row.LiveReach = t.Reach
			row.QueueReach = t.Reach
		}
	}

	out := make([]RedisEnvEndpointView, 0, 3)
	for _, env := range []string{"dev", "stg", "prod"} {
		row := byEnv[env]
		if row.LiveReach == "" {
			row.LiveReach = probe.ReachDegraded
		}
		if row.QueueReach == "" {
			row.QueueReach = probe.ReachDegraded
		}
		switch {
		case row.LiveReach == probe.ReachOK && row.QueueReach == probe.ReachOK:
			row.Detail = "Target endpoints ready"
		case row.Environment == "dev" && row.LiveReach == probe.ReachOK:
			row.Detail = "Dev may use Mac local Redis instead of data NS"
		default:
			row.Detail = "Awaiting phase ⑥ Bitnami deploy"
		}
		out = append(out, *row)
	}
	return out
}

func embeddedRedisProbe(snap readinessSnapshot) []RedisEmbeddedView {
	envs := []string{"bifrost-stg", "bifrost-dev", "bifrost-prod"}
	out := make([]RedisEmbeddedView, 0, len(envs))
	for _, ns := range envs {
		key := ns + "/redis"
		d, ok := snap.deployments[key]
		if !ok {
			out = append(out, RedisEmbeddedView{
				Namespace: ns, Host: "redis", Reach: probe.ReachOK,
				Detail: "removed — cutover complete",
			})
			continue
		}
		desired := d.Status.Replicas
		if desired == 0 && d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if desired == 0 {
			out = append(out, RedisEmbeddedView{
				Namespace: ns, Host: "redis", Reach: probe.ReachOK,
				Detail: "scaled to zero",
			})
			continue
		}
		reach, detail := deploymentReach(&d)
		image := deploymentImage(&d)
		out = append(out, RedisEmbeddedView{
			Namespace: ns,
			Host:      "redis",
			Reach:     reach,
			Detail:    detail + " · single instance live+queue db=0 · retire in phase ⑥",
			Image:     image,
		})
	}
	return out
}

func deploymentImage(d *appsv1.Deployment) string {
	if len(d.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	return d.Spec.Template.Spec.Containers[0].Image
}

func redisBackupDep(snap readinessSnapshot, minioReach probe.Reachability) ServiceDependencyView {
	nfsHot := clusterCapDep(snap, "storage-class-nfs-hot", "nfs-hot")
	reach := probe.ReachDegraded
	detail := "RDB → s3://redis-backup @ MinIO (phase ⑥)"
	if minioReach == probe.ReachOK && nfsHot.Reachability == probe.ReachOK {
		reach = probe.ReachDegraded
		detail = "MinIO ready · CronJob redis-cli --rdb not deployed yet"
	}
	if minioReach != probe.ReachOK {
		detail += " · MinIO not ready"
	}
	return ServiceDependencyView{
		ID: "redis-backup", Label: "RDB backup (nfs-hot)", Reachability: reach, Detail: detail,
	}
}

type redisLanSpec struct {
	lanService string
	targetName string
	environment string
	role       string
	database   string
}

var redisLanCatalog = []redisLanSpec{
	{lanService: "redis-dev-lan", targetName: "redis-dev", environment: "dev", role: "live+queue", database: "db=0 live · db=1 queue"},
	{lanService: "redis-live-stg-lan", targetName: "redis-live-stg", environment: "stg", role: "live", database: "db=0"},
	{lanService: "redis-queue-stg-lan", targetName: "redis-queue-stg", environment: "stg", role: "queue", database: "db=0"},
	{lanService: "redis-live-prod-lan", targetName: "redis-live-prod", environment: "prod", role: "live", database: "db=0"},
	{lanService: "redis-queue-prod-lan", targetName: "redis-queue-prod", environment: "prod", role: "queue", database: "db=0"},
}

func redisLanAccessProbe(ctx context.Context, clientset kubernetes.Interface) []RedisLanEndpointView {
	host := firstReadyNodeInternalIP(ctx, clientset)
	out := make([]RedisLanEndpointView, 0, len(redisLanCatalog))

	for _, spec := range redisLanCatalog {
		view := RedisLanEndpointView{
			Name:        spec.targetName,
			Environment: spec.environment,
			Role:        spec.role,
			Database:    spec.database,
			Reach:       probe.ReachDegraded,
		}

		svc, err := clientset.CoreV1().Services(redisDataNamespace).Get(ctx, spec.lanService, metav1.GetOptions{})
		if err != nil {
			view.Detail = fmt.Sprintf("NodePort service %s not found — apply k8s/data/redis/redis-nodeport.yaml", spec.lanService)
			out = append(out, view)
			continue
		}

		var nodePort int32
		for _, p := range svc.Spec.Ports {
			if p.Name == "redis" || p.Port == 6379 {
				nodePort = p.NodePort
				break
			}
		}
		if nodePort == 0 {
			view.Detail = fmt.Sprintf("%s has no NodePort assigned", spec.lanService)
			out = append(out, view)
			continue
		}
		view.NodePort = int(nodePort)

		if host == "" {
			view.Detail = fmt.Sprintf("NodePort %d ready; no Ready node IP resolved", nodePort)
			out = append(out, view)
			continue
		}

		view.Available = true
		view.Host = host
		view.Endpoint = fmt.Sprintf("%s:%d", host, nodePort)
		view.Reach = probe.ReachOK
		view.Detail = "LAN-only · no password on phase-⑥ instances"
		out = append(out, view)
	}

	return out
}

func summarizeRedisStatus(r RedisStatusResponse) (probe.Reachability, string) {
	if r.TargetsReady == r.TargetsTotal && r.TargetsTotal > 0 && r.EmbeddedActive == 0 {
		return probe.ReachOK, "Bitnami live/queue @ data NS · embedded redis retired"
	}
	if r.TargetsReady > 0 {
		return probe.ReachDegraded, fmt.Sprintf(
			"Partial — %d/%d data NS targets · %d embedded still active",
			r.TargetsReady, r.TargetsTotal, r.EmbeddedActive,
		)
	}
	if r.EmbeddedActive > 0 {
		return probe.ReachDegraded, fmt.Sprintf(
			"Embedded redis in %d namespace(s) · live/queue split pending (phase ⑥)",
			r.EmbeddedActive,
		)
	}
	return probe.ReachDegraded, "No Redis targets in data NS · deploy phase ⑥ manifests"
}

func redisTargetDep(snap readinessSnapshot, name, role, env string) ServiceDependencyView {
	spec := redisTargetSpec{name: name, role: role, environment: env}
	for _, s := range redisTargetCatalog {
		if s.name == name {
			spec = s
			break
		}
	}
	inst := probeRedisTarget(spec, snap, nil)
	return ServiceDependencyView{
		ID:           "redis-target-" + name,
		Label:        fmt.Sprintf("%s (%s · %s)", name, env, role),
		Reachability: inst.Reach,
		Detail:       inst.Detail,
	}
}

func embeddedRedisDep(snap readinessSnapshot, namespace string) *ServiceDependencyView {
	key := namespace + "/redis"
	d, ok := snap.deployments[key]
	if !ok {
		return nil
	}
	desired := d.Status.Replicas
	if desired == 0 && d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	if desired == 0 {
		return nil
	}
	reach, detail := deploymentReach(&d)
	return &ServiceDependencyView{
		ID:           "embedded-redis-" + namespace,
		Label:        fmt.Sprintf("Embedded redis (%s)", namespace),
		Reachability: reach,
		Detail:       detail + " — retire in phase ⑥",
	}
}
