package ibgateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type Service struct {
	cfg     Config
	cluster *cluster.Service
}

func NewService(clusterSvc *cluster.Service) *Service {
	cfg, err := ConfigFromEnv()
	if err != nil {
		cfg = Config{}
	}
	return &Service{cfg: cfg, cluster: clusterSvc}
}

func (s *Service) Status(ctx context.Context) StatusResponse {
	now := time.Now().UTC()
	resp := StatusResponse{
		Reachable:       false,
		Reachability:    probe.ReachUnknown,
		Autonomy:        "L0",
		ConsumerGroup:   consumerGroupName,
		Deployment:      DeploymentStatus{Namespace: dataNamespace, Name: gatewayDeployName, Reach: probe.ReachUnknown},
		GeneratedAt:     now,
	}

	if s.cfg.RedisPlatformPass == "" {
		resp.Error = "REDIS_IB_PLATFORM_PASS not configured"
		resp.Hint = "Copy platform password from bifrost-platform-plugin/.env into bifrost-platform/.env"
		resp.Summary = "redis-ib probe unavailable"
		return resp
	}

	deployReach, mode, ready, deployDetail := s.readDeployment(ctx)
	resp.Deployment.Ready = ready
	resp.Deployment.Mode = mode
	resp.Deployment.Reach = deployReach
	resp.Deployment.Detail = deployDetail
	resp.Mode = mode

	ingestor, err := s.redisCLI("HGETALL", "bifrost:health:ws_ib_ingestor")
	if err != nil {
		resp.RedisReach = probe.ReachFail
		resp.Error = err.Error()
		resp.Hint = "Ensure redis-ib @ data NS and kubectl access"
		resp.Summary = fmt.Sprintf("redis-ib probe failed · deployment %s", ready)
		return resp
	}
	resp.RedisReach = probe.ReachOK
	resp.IngestorHealth = parseRedisHash(ingestor)

	accountRaw, _ := s.redisCLI("HGETALL", "bifrost:health:ws_ib_account_agent")
	resp.AccountHealth = parseRedisHash(accountRaw)
	operatorRaw, _ := s.redisCLI("HGETALL", "bifrost:health:ws_ib_operator")
	resp.OperatorHealth = parseRedisHash(operatorRaw)

	tick, _ := s.redisCLI("GET", "ib:ingester:tick:NVDA|STK|||")
	resp.SampleTick = tick
	resp.Slots = s.readSlots()

	connected := strings.EqualFold(resp.IngestorHealth["connected"], "true")
	hostOK := strings.EqualFold(resp.AccountHealth["host_connected"], "true")
	secOK := strings.EqualFold(resp.AccountHealth["secondary_connected"], "true")
	deployOK := deployReach == probe.ReachOK

	resp.Reachable = deployOK && resp.RedisReach == probe.ReachOK && connected
	resp.Reachability = classifyReach(deployOK, connected, hostOK, secOK, mode)
	resp.Summary = fmt.Sprintf("%s · ib-gateway %s · host=%t secondary=%t · redis-ib ok", mode, ready, hostOK, secOK)
	resp.Cutover = s.readCutoverStatus(ctx)
	return resp
}

func (s *Service) readCutoverStatus(ctx context.Context) *CutoverStatus {
	out := &CutoverStatus{
		LegacySocketRetired: false,
		Reach:               probe.ReachUnknown,
		Environments:        []TradeCutoverEnv{},
	}
	if s.cluster == nil {
		out.Reach = probe.ReachFail
		return out
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		out.Reach = probe.ReachFail
		return out
	}
	allRetired := true
	for _, ns := range tradeCutoverNamespaces {
		env := TradeCutoverEnv{Namespace: ns, Reach: probe.ReachOK}
		replicas := 0
		for _, stsName := range legacyIBStatefulSets {
			sts, stsErr := clientset.AppsV1().StatefulSets(ns).Get(ctx, stsName, metav1.GetOptions{})
			if stsErr != nil {
				continue
			}
			if sts.Spec.Replicas != nil {
				replicas += int(*sts.Spec.Replicas)
			}
		}
		env.LegacyIbReplicas = replicas
		if replicas > 0 {
			allRetired = false
			env.Reach = probe.ReachFail
			env.Detail = fmt.Sprintf("legacy IB socket replicas=%d", replicas)
		} else {
			env.Detail = "legacy IB StatefulSets scaled to 0"
		}
		svc, svcErr := clientset.CoreV1().Services(ns).Get(ctx, "redis-ib", metav1.GetOptions{})
		if svcErr != nil {
			env.RedisIbExternalName = false
			env.Reach = probe.ReachFail
			env.Detail = "redis-ib ExternalName missing"
			allRetired = false
		} else {
			env.RedisIbExternalName = svc.Spec.Type == "ExternalName" &&
				svc.Spec.ExternalName == "redis-ib.data.svc.cluster.local"
			if !env.RedisIbExternalName {
				if env.Reach == probe.ReachOK {
					env.Reach = probe.ReachDegraded
				}
				env.Detail = "redis-ib service not ExternalName → data NS"
			}
		}
		out.Environments = append(out.Environments, env)
	}
	out.LegacySocketRetired = allRetired
	if allRetired {
		out.Reach = probe.ReachOK
	} else {
		out.Reach = probe.ReachFail
	}
	return out
}

func (s *Service) Reconnect(ctx context.Context) (ControlResponse, error) {
	now := time.Now().UTC()
	if s.cluster == nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.reconnect", Target: dataNamespace + "/Deployment/" + gatewayDeployName,
			Autonomy: "L1", Message: "cluster service unavailable", GeneratedAt: now,
		}, fmt.Errorf("cluster service unavailable")
	}
	resp, err := s.cluster.RolloutRestart(ctx, cluster.RolloutRestartRequest{
		Namespace: dataNamespace,
		Kind:      "Deployment",
		Name:      gatewayDeployName,
	})
	return ControlResponse{
		OK: resp.OK, Action: "ib-gateway.reconnect", Target: resp.Target,
		Autonomy: "L1", Message: resp.Message, GeneratedAt: now,
	}, err
}

func (s *Service) SetMaintenance(_ context.Context, req ControlRequest) (ControlResponse, error) {
	now := time.Now().UTC()
	accountID := strings.TrimSpace(req.AccountID)
	if accountID == "" {
		accountID = "wzhao1503"
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	key := "ib:control:" + accountID
	payload := fmt.Sprintf(`{"maintenance":%t,"updated_at":%d,"source":"platform-api"}`, enabled, now.Unix())
	if s.cfg.RedisPlatformPass == "" {
		return ControlResponse{
			OK: false, Action: "ib-gateway.maintenance", Target: key,
			Autonomy: "L1", Message: "REDIS_IB_PLATFORM_PASS not configured", GeneratedAt: now,
		}, fmt.Errorf("REDIS_IB_PLATFORM_PASS not configured")
	}
	if _, err := s.redisCLI("SET", key, payload, "EX", "3600"); err != nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.maintenance", Target: key,
			Autonomy: "L1", Message: err.Error(), GeneratedAt: now,
		}, err
	}
	state := "enabled"
	if !enabled {
		state = "cleared"
	}
	return ControlResponse{
		OK: true, Action: "ib-gateway.maintenance", Target: key,
		Autonomy: "L1", Message: fmt.Sprintf("maintenance %s for account %s", state, accountID), GeneratedAt: now,
	}, nil
}

func (s *Service) readDeployment(ctx context.Context) (probe.Reachability, string, string, string) {
	if s.cluster == nil {
		return probe.ReachFail, "mock", "0/0", "cluster service unavailable"
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return probe.ReachFail, "unknown", "0/0", err.Error()
	}
	deploy, err := clientset.AppsV1().Deployments(dataNamespace).Get(ctx, gatewayDeployName, metav1.GetOptions{})
	if err != nil {
		return probe.ReachFail, "unknown", "0/0", err.Error()
	}
	mode := "mock"
	if cm, cmErr := clientset.CoreV1().ConfigMaps(dataNamespace).Get(ctx, configMapName, metav1.GetOptions{}); cmErr == nil {
		if m := strings.TrimSpace(cm.Data["mode"]); m != "" {
			mode = m
		}
	}
	replicas := int32(0)
	if deploy.Spec.Replicas != nil {
		replicas = *deploy.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", deploy.Status.ReadyReplicas, replicas)
	reach := probe.ReachOK
	detail := "deployment ready"
	if deploy.Status.ReadyReplicas < replicas {
		reach = probe.ReachDegraded
		detail = "deployment progressing"
	}
	if replicas > 0 && deploy.Status.AvailableReplicas == 0 {
		reach = probe.ReachFail
		detail = "deployment unavailable"
	}
	return reach, mode, ready, detail
}

func (s *Service) readSlots() []SlotStatus {
	accounts := []struct {
		slot, account string
	}{
		{"host", "wzhao1503"},
		{"secondary", "vzhao1503"},
	}
	out := make([]SlotStatus, 0, len(accounts))
	for _, acct := range accounts {
		slot := SlotStatus{Slot: acct.slot, AccountID: acct.account, Reach: probe.ReachUnknown}
		raw, err := s.redisCLI("GET", "ib:health:"+acct.account)
		if err != nil || raw == "" {
			slot.Status = "unknown"
			slot.Detail = "no ib:health key"
			out = append(out, slot)
			continue
		}
		slot.Connected = strings.Contains(raw, `"status": "connected"`) || strings.Contains(raw, `"status":"connected"`)
		if slot.Connected {
			slot.Status = "connected"
			slot.Reach = probe.ReachOK
		} else {
			slot.Status = "disconnected"
			slot.Reach = probe.ReachDegraded
		}
		out = append(out, slot)
	}
	return out
}

func classifyReach(deployOK, ingestorOK, hostOK, secOK bool, mode string) probe.Reachability {
	if !deployOK || !ingestorOK {
		return probe.ReachFail
	}
	if mode == "live" && (!hostOK || !secOK) {
		return probe.ReachDegraded
	}
	if !hostOK {
		return probe.ReachDegraded
	}
	return probe.ReachOK
}
