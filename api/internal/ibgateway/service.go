package ibgateway

import (
	"context"
	"encoding/json"
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
	snapshot, _ := s.redisCLI("GET", "ib:account:snapshot:v1")
	resp.AccountSnapshot = snapshot
	resp.Slots = s.readSlots()

	connected := strings.EqualFold(resp.IngestorHealth["connected"], "true")
	hostOK := strings.EqualFold(resp.AccountHealth["host_connected"], "true")
	secOK := strings.EqualFold(resp.AccountHealth["secondary_connected"], "true")
	deployOK := deployReach == probe.ReachOK

	resp.Reachable = deployOK && resp.RedisReach == probe.ReachOK && connected
	resp.Reachability = classifyReach(deployOK, connected, hostOK, secOK, mode)
	resp.Summary = fmt.Sprintf("%s · ib-gateway %s · host=%t secondary=%t · redis-ib ok", mode, ready, hostOK, secOK)

	// Socket-quality gate: Redis "connected" alone is not enough for Vendor GO.
	if q := assessSocketFeedQuality(mode, resp.IngestorHealth, resp.AccountHealth, resp.SampleTick, resp.AccountSnapshot, now); q.Reach != probe.ReachOK {
		resp.Reachability = worseReach(resp.Reachability, q.Reach)
		if q.Reach == probe.ReachFail {
			resp.Reachable = false
		}
		resp.Summary = fmt.Sprintf("%s · %s", resp.Summary, q.Reason)
		if resp.Hint == "" {
			resp.Hint = q.Hint
		}
	}

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
	target := dataNamespace + "/Deployment/" + gatewayDeployName
	staleSec := s.cfg.SnapshotStaleSec
	if staleSec <= 0 {
		staleSec = defaultSnapshotStaleSec
	}

	softAt := now
	softErr := s.sendOperatorCommand(ctx, "reconnect_all", 30*time.Second)
	if softErr == nil && s.waitSnapshotFresh(ctx, 45*time.Second, staleSec) {
		return ControlResponse{
			OK:              true,
			Action:          "ib-gateway.reconnect",
			Target:          target,
			Autonomy:        "L1",
			ActionTaken:     "soft_reconnect",
			SoftReconnectAt: softAt,
			Message:         "soft reconnect_all succeeded; account snapshot refreshed",
			GeneratedAt:     now,
		}, nil
	}

	if s.cluster == nil {
		msg := "cluster service unavailable"
		if softErr != nil {
			msg = fmt.Sprintf("soft reconnect failed: %v; cluster unavailable", softErr)
		}
		return ControlResponse{
			OK: false, Action: "ib-gateway.reconnect", Target: target,
			Autonomy: "L1", ActionTaken: "rollout_restart", Message: msg, GeneratedAt: now,
		}, fmt.Errorf("cluster service unavailable")
	}
	resp, err := s.cluster.RolloutRestart(ctx, cluster.RolloutRestartRequest{
		Namespace: dataNamespace,
		Kind:      "Deployment",
		Name:      gatewayDeployName,
	})
	msg := resp.Message
	if softErr != nil {
		msg = fmt.Sprintf("soft reconnect failed (%v); rollout restart: %s", softErr, msg)
	} else {
		msg = fmt.Sprintf("snapshot still stale after soft reconnect; rollout restart: %s", msg)
	}
	return ControlResponse{
		OK:          resp.OK,
		Action:      "ib-gateway.reconnect",
		Target:      resp.Target,
		Autonomy:    "L1",
		ActionTaken: "rollout_restart",
		Message:     msg,
		GeneratedAt: now,
	}, err
}

func (s *Service) SelfHealStatus(ctx context.Context) SelfHealStatusResponse {
	now := time.Now().UTC()
	out := SelfHealStatusResponse{
		Reach:             probe.ReachUnknown,
		Enabled:           true,
		AutoRepairEnabled: s.cfg.AutoRepairEnabled,
		GeneratedAt:       now,
	}
	if s.cfg.RedisPlatformPass == "" {
		out.Reach = probe.ReachFail
		out.Error = "REDIS_IB_PLATFORM_PASS not configured"
		return out
	}
	raw, err := s.redisCLI("HGETALL", selfHealRedisKey)
	if err != nil {
		out.Reach = probe.ReachFail
		out.Error = err.Error()
		return out
	}
	fields := parseRedisHash(raw)
	out.LastAction = fields["last_action"]
	if v, ok := parseFloatField(fields["last_action_ts"]); ok {
		out.LastActionTS = v
	}
	if v, ok := parseIntField(fields["stale_streak"]); ok {
		out.StaleStreak = v
	}
	if v, ok := parseFloatField(fields["cooldown_until"]); ok {
		out.CooldownUntil = v
	}
	out.Reason = fields["reason"]
	if fields["enabled"] == "" {
		out.Enabled = true
	} else {
		out.Enabled = strings.EqualFold(fields["enabled"], "true") || fields["enabled"] == "1"
	}
	out.RolloutRecommended = strings.EqualFold(fields["rollout_recommended"], "true") || fields["rollout_recommended"] == "1"
	if v, ok := parseFloatField(fields["snapshot_age_sec"]); ok {
		out.SnapshotAgeSec = v
	}
	snapRaw, _ := s.redisCLI("GET", accountSnapshotKey)
	if age, ok := snapshotAgeSec(snapRaw, now); ok {
		out.SnapshotAgeSec = age
	}
	out.Reach = probe.ReachOK
	return out
}

func (s *Service) SetSelfHealEnabled(_ context.Context, enabled bool) (ControlResponse, error) {
	now := time.Now().UTC()
	key := selfHealRedisKey
	if s.cfg.RedisPlatformPass == "" {
		return ControlResponse{
			OK: false, Action: "ib-gateway.self-heal", Target: key,
			Autonomy: "L1", Message: "REDIS_IB_PLATFORM_PASS not configured", GeneratedAt: now,
		}, fmt.Errorf("REDIS_IB_PLATFORM_PASS not configured")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	if _, err := s.redisCLI("HSET", key, "enabled", val, "updated_at", fmt.Sprintf("%d", now.Unix())); err != nil {
		return ControlResponse{
			OK: false, Action: "ib-gateway.self-heal", Target: key,
			Autonomy: "L1", Message: err.Error(), GeneratedAt: now,
		}, err
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	return ControlResponse{
		OK: true, Action: "ib-gateway.self-heal", Target: key,
		Autonomy: "L1", Message: fmt.Sprintf("plugin self-heal %s", state), GeneratedAt: now,
	}, nil
}

func parseFloatField(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return 0, false
	}
	return f, true
}

func parseIntField(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return 0, false
	}
	return n, true
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

type feedQuality struct {
	Reach  probe.Reachability
	Reason string
	Hint   string
}

func worseReach(a, b probe.Reachability) probe.Reachability {
	order := map[probe.Reachability]int{
		probe.ReachOK:       0,
		probe.ReachUnknown:  1,
		probe.ReachDegraded: 2,
		probe.ReachFail:     3,
	}
	if order[b] > order[a] {
		return b
	}
	return a
}

// assessSocketFeedQuality downgrades optimistic Redis "connected" when heartbeat/tick/
// account-snapshot evidence shows the TWS API socket is not delivering a live session.
func assessSocketFeedQuality(
	mode string,
	ingestor, account map[string]string,
	sampleTick string,
	accountSnapshot string,
	now time.Time,
) feedQuality {
	if !strings.EqualFold(mode, "live") {
		return feedQuality{Reach: probe.ReachOK}
	}
	connected := strings.EqualFold(ingestor["connected"], "true")
	if !connected {
		return feedQuality{
			Reach:  probe.ReachFail,
			Reason: "ingestor not connected",
			Hint:   "Check TWS API on .30/.32 (port 7496) and TrustedIPs for K3s nodes (.73/.54/.56)",
		}
	}
	cid := strings.TrimSpace(ingestor["client_id"])
	if cid == "" || cid == "0" {
		hostCID := strings.TrimSpace(account["host_client_id"])
		if hostCID == "" || hostCID == "0" {
			return feedQuality{
				Reach:  probe.ReachFail,
				Reason: "connected flag set but no client_id — TWS API session missing",
				Hint:   "On Win11 TWS: API Clients should show Platform client ids (70/72). Verify TrustedIPs + Enable Socket Clients.",
			}
		}
	}
	// Ghost-session detector: plugin may keep ib_insync "connected" while TWS has no
	// live API clients — managedAccounts / account snapshot stay empty.
	if q := assessAccountSnapshotQuality(accountSnapshot, now); q.Reach != probe.ReachOK {
		return q
	}
	if age, ok := parseUnixAgeSec(ingestor["last_msg_ts"], now); ok && age > 90 {
		return feedQuality{
			Reach:  probe.ReachFail,
			Reason: fmt.Sprintf("IB socket heartbeat stale (%.0fs)", age),
			Hint:   "Redis health still marked connected but last_msg is stale — treat as dead TWS API socket",
		}
	}
	if age, ok := parseUnixAgeSec(account["last_msg_ts"], now); ok && age > 90 {
		return feedQuality{
			Reach:  probe.ReachFail,
			Reason: fmt.Sprintf("IB account heartbeat stale (%.0fs)", age),
			Hint:   "Account-agent last_msg stale — Host/Secondary TWS API session likely down",
		}
	}
	tick := strings.TrimSpace(sampleTick)
	if tick == "" {
		return feedQuality{
			Reach:  probe.ReachDegraded,
			Reason: "no sample tick (NVDA) on redis-ib",
			Hint:   "Deployment may be up but market-data path from TWS is empty",
		}
	}
	bid, ask, last, tickTS, tickOK := parseSampleTick(tick)
	if !tickOK {
		return feedQuality{
			Reach:  probe.ReachDegraded,
			Reason: "sample tick unparseable",
		}
	}
	if tickTS > 0 {
		age := float64(now.Unix()) - tickTS
		if tickTS > 1e12 {
			age = float64(now.UnixMilli()) - tickTS
			age = age / 1000
		}
		if age > 180 {
			return feedQuality{
				Reach:  probe.ReachFail,
				Reason: fmt.Sprintf("sample tick stale (%.0fs) — socket not delivering", age),
				Hint:   "Fresh Redis connected flag with frozen tick usually means TWS API client is not live",
			}
		}
	}
	if inUSEquityRTH(now) && bid <= 0 && ask <= 0 {
		reason := "RTH but no usable BBO (bid/ask≤0)"
		if last > 0 {
			reason += fmt.Sprintf(" · last=%.2f only", last)
		}
		return feedQuality{
			Reach:  probe.ReachFail,
			Reason: reason,
			Hint:   "During market hours missing BBO strongly indicates TWS socket/market-data failure — check API Clients on .30/.32",
		}
	}
	return feedQuality{Reach: probe.ReachOK}
}

// assessAccountSnapshotQuality fails when Redis claims a live IB session but the
// account snapshot has zero managed accounts (ghost / half-dead TWS API client).
func assessAccountSnapshotQuality(raw string, now time.Time) feedQuality {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return feedQuality{
			Reach:  probe.ReachFail,
			Reason: "no account snapshot on redis-ib — TWS API session not verified",
			Hint:   "IB Gateway writes ib:account:snapshot:v1 every market loop; missing key means socket path is dead",
		}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return feedQuality{
			Reach:  probe.ReachDegraded,
			Reason: "account snapshot unparseable",
		}
	}
	if updated, ok := asFloatOK(m["updated_at"]); ok {
		age := float64(now.Unix()) - updated
		if age > 90 {
			return feedQuality{
				Reach:  probe.ReachFail,
				Reason: fmt.Sprintf("account snapshot stale (%.0fs)", age),
				Hint:   "Snapshot not refreshing — treat as dead TWS API client",
			}
		}
	}
	accounts, _ := m["accounts_snapshot"].([]any)
	if len(accounts) == 0 {
		hostClaim := truthy(m["host_connected"])
		secClaim := truthy(m["secondary_connected"])
		if hostClaim || secClaim {
			return feedQuality{
				Reach:  probe.ReachFail,
				Reason: "connected but accounts_snapshot empty — ghost TWS API client",
				Hint:   "Plugin claims Host/Secondary connected but managedAccounts is empty. TWS API Clients on .30/.32 likely show no session — reconnect IB Gateway / verify Socket Client.",
			}
		}
		return feedQuality{
			Reach:  probe.ReachFail,
			Reason: "account snapshot has no managed accounts",
			Hint:   "TWS API session not delivering account data",
		}
	}
	return feedQuality{Reach: probe.ReachOK}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	case float64:
		return t != 0
	default:
		return false
	}
}

func asFloatOK(v any) (float64, bool) {
	f := asFloat(v)
	if v == nil {
		return 0, false
	}
	switch v.(type) {
	case float64, float32, int, int64, json.Number, string:
		return f, true
	default:
		return 0, false
	}
}

func parseUnixAgeSec(raw string, now time.Time) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var ts float64
	if _, err := fmt.Sscanf(raw, "%f", &ts); err != nil {
		return 0, false
	}
	if ts > 1e12 { // ms
		ts = ts / 1000
	}
	return float64(now.Unix()) - ts, true
}

func parseSampleTick(raw string) (bid, ask, last, ts float64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw[0] != '{' {
		return 0, 0, 0, 0, false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return 0, 0, 0, 0, false
	}
	bid = asFloat(m["bid"])
	ask = asFloat(m["ask"])
	last = asFloat(m["last"])
	ts = asFloat(m["ts"])
	return bid, ask, last, ts, true
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscanf(t, "%f", &f)
		return f
	default:
		return 0
	}
}

// inUSEquityRTH approximates America/New_York regular session Mon–Fri 09:30–16:00.
func inUSEquityRTH(now time.Time) bool {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.FixedZone("EST", -5*3600)
	}
	local := now.In(loc)
	wd := local.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	mins := local.Hour()*60 + local.Minute()
	return mins >= 9*60+30 && mins < 16*60
}
